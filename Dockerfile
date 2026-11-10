FROM node:22-alpine AS nodebuilder
WORKDIR /build
RUN npm i -g pnpm@latest-10
COPY src/web/package.json src/web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY src/web/ .
RUN pnpm build

FROM golang:1.26-alpine AS builder
WORKDIR /build
COPY src/ .
COPY --from=nodebuilder /build/dist web/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o manager .

# 上游包按 x86_64 / arm64 命名，与 Docker 的 TARGETARCH 不同。
# 解包与目标架构无关，放在构建平台执行，避免 QEMU 下跑 apk/tar
FROM --platform=$BUILDPLATFORM alpine:3.20 AS chromium
ARG TARGETARCH
RUN apk add --no-cache xz
COPY ungoogled-chromium-*_linux.tar.xz /tmp/
RUN set -eu; \
  case "${TARGETARCH}" in \
    amd64) chromium_arch=x86_64 ;; \
    arm64) chromium_arch=arm64 ;; \
    *) echo "不支持的目标架构：${TARGETARCH}" >&2; exit 1 ;; \
  esac; \
  set -- /tmp/ungoogled-chromium-*-"${chromium_arch}"_linux.tar.xz; \
  if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then \
    if [ "$#" -eq 1 ] && [ ! -f "$1" ]; then \
      echo "构建上下文里没有 ${chromium_arch} 对应的上游包（期望匹配：$1）" >&2; \
    else \
      echo "构建上下文里 ${chromium_arch} 上游包不唯一，匹配到 $# 个：$*" >&2; \
    fi; \
    ls -l /tmp >&2; \
    exit 1; \
  fi; \
  mkdir -p /opt/chromium; \
  xz -t "$1"; \
  xz -dc "$1" | tar -xf - --strip-components=1 -C /opt/chromium; \
  [ -x /opt/chromium/chrome ] || { echo "解包后 /opt/chromium/chrome 不存在或不可执行" >&2; ls -l /opt/chromium >&2; exit 1; }

FROM ghcr.io/linuxserver/baseimage-selkies:ubuntunoble

ENV TITLE=ChromiumManager
ENV DATA_DIR=/config/data
# 默认不注册删除类 MCP 工具；MCP_TOKEN 留空时 HTTP 传输不校验
ENV MCP_ENABLE_DESTRUCTIVE=0
ENV MCP_TOKEN=
# 多 key 绑定，形如 {"key1":"窗口1","key2":"项目A/窗口1"}；设置后每个 key 只能操作自己那个配置。
# value 写「配置名」落到 MCP_GROUP_NAME，写「分组名/配置名」则落到该分组，两种写法可混用；
# 配置名不能含 /。也可以用 MCP_KEYS_FILE 指向一个 JSON 文件（MCP_KEYS 优先）。
ENV MCP_KEYS=
ENV MCP_KEYS_FILE=
# 省写分组时用的默认分组名；value 里带「分组名/」的条目不使用它
ENV MCP_GROUP_NAME=mcp
# 外部 MCP 监听地址，容器外可直接连；设为 off 关闭
ENV MCP_HTTP_ADDR=0.0.0.0:10102
# Chromium 沙箱：auto 按容器 seccomp 状态判断，1 强制启用，0 强制关闭。
# 真正启用还需要 --security-opt seccomp=unconfined，见 README。
ENV CHROME_SANDBOX=auto
# 关掉 openbox 的窗口边框与标题栏（基础镜像 init-selkies-config 会据此往
# /etc/xdg/openbox/rc.xml 的通配规则写入 <decor>no</decor>），界面看起来就是一个
# 独立应用而不是桌面里的小窗口。注意基础镜像判据是「变量已定义」，传空值也生效。
ENV NO_DECOR=1
# 远程桌面的固定分辨率。不设时按「客户端 CSS 尺寸 × devicePixelRatio」算流分辨率，手机
# DPR 常为 3~4，竖屏可达 2940x4881（约 1400 万像素）：客户端解不动、带宽白涨，而且窗口
# 尺寸一变就整条管道重配置。锁定后尺寸与设备无关，客户端只做等比缩放。
# 两个值要一起改，只设一个时另一个会被补成 1024/768。
ENV SELKIES_MANUAL_WIDTH=1920
ENV SELKIES_MANUAL_HEIGHT=1080

RUN \
  echo "**** install packages ****" && \
  apt-get update && \
  apt-get install -y --no-install-recommends libgtk-3-0 libnss3 libasound2t64 tint2 desktop-file-utils fonts-noto-cjk-extra && \
  fc-cache -fv && \
  echo "**** cleanup ****" && \
  apt-get clean && \
  rm -rf \
    /config/.cache \
    /var/lib/apt/lists/* \
    /var/tmp/*

COPY --from=chromium /opt/chromium /opt/chromium

COPY --from=builder /build/manager /usr/bin/manager

# MCP 只监听容器内 127.0.0.1:10101，nginx 的两个 server 块（3000 http / 3001 https）
# 都加一条 /mcp 反代，容器外才能通过映射端口访问。init-nginx 启动时会用
# /defaults/default.conf 覆盖 /etc/nginx/sites-available/default 并做占位符替换，
# 所以改模板即可。插在每处 error_page 之后，即各 server 块的末尾。
#
# auth_basic off 是必须的：Basic 和 MCP 的 Bearer 都占用 Authorization 头，一个请求
# 只能带一个，两者同时开启会导致任何请求都过不了其中一层。/mcp 因此只保留 Bearer
# 鉴权（设了 MCP_KEYS 或 MCP_TOKEN 才校验），Web 界面仍走 Basic Auth。
# /chrome-mcp 也走同一条反代：它是给手机侧自动化模块取走的浏览器伪装脚本
# （见下方 shim 说明与 README「给外部自动化模块提供浏览器路径」）。同样关掉
# Basic Auth —— 该文件不含密钥，而取它的往往是不会带 Basic 凭据的自动化模块；
# 真正的访问控制在网络层（是否把 3001 暴露到公网）。
#
# /cdp/ 是 DevTools 反向代理：Chromium 只把调试端口绑在 127.0.0.1 上（实测
# --remote-debugging-address 在 156 上无效），容器外只能经由这里连。必须带
# websocket upgrade 的三个头，否则 /json/version 能通而 CDP 会话建不起来。
# 超时按 ws 长连接放宽；token 走 query，因为 CDP 的 ws 连接加不了请求头。
RUN printf '%s\n' \
    '  location /mcp {' \
    '    auth_basic off;' \
    '    proxy_set_header        Host $host;' \
    '    proxy_set_header        X-Real-IP $remote_addr;' \
    '    proxy_set_header        X-Forwarded-For $proxy_add_x_forwarded_for;' \
    '    proxy_set_header        X-Forwarded-Proto $scheme;' \
    '    proxy_http_version      1.1;' \
    '    proxy_read_timeout      300s;' \
    '    proxy_buffering         off;' \
    '    client_max_body_size    8m;' \
    '    proxy_pass              http://127.0.0.1:10101;' \
    '  }' \
    '  location ^~ /chrome-mcp {' \
    '    auth_basic off;' \
    '    proxy_set_header        Host $host;' \
    '    proxy_set_header        X-Real-IP $remote_addr;' \
    '    proxy_set_header        X-Forwarded-For $proxy_add_x_forwarded_for;' \
    '    proxy_http_version      1.1;' \
    '    proxy_pass              http://127.0.0.1:10101;' \
    '  }' \
    '  location /cdp {' \
    '    auth_basic off;' \
    '    proxy_set_header        Host $host;' \
    '    proxy_set_header        X-Real-IP $remote_addr;' \
    '    proxy_set_header        X-Forwarded-For $proxy_add_x_forwarded_for;' \
    '    proxy_set_header        X-Forwarded-Host $host:$server_port;' \
    '    proxy_set_header        X-Forwarded-Proto $scheme;' \
    '    proxy_set_header        Upgrade $http_upgrade;' \
    '    proxy_set_header        Connection "upgrade";' \
    '    proxy_http_version      1.1;' \
    '    proxy_read_timeout      3600s;' \
    '    proxy_send_timeout      3600s;' \
    '    proxy_buffering         off;' \
    '    proxy_pass              http://127.0.0.1:10101;' \
    '  }' > /tmp/mcp-location.conf && \
  sed -i '/error_page 500 502 503 504/r /tmp/mcp-location.conf' /defaults/default.conf && \
  rm -f /tmp/mcp-location.conf && \
  test "$(grep -c 'location /mcp' /defaults/default.conf)" -eq 2 && \
  test "$(grep -c 'location ^~ /chrome-mcp' /defaults/default.conf)" -eq 2 && \
  test "$(grep -c 'location /cdp' /defaults/default.conf)" -eq 2 && \
  test "$(grep -c 'auth_basic off;' /defaults/default.conf)" -eq 6

# 修补基础镜像里打包的 Selkies 前端。脚本说明见 scripts/patch-selkies.py 文件头：
# 改的必须是 selkies-dashboard（init-nginx 启动时用 `cp -a` 重建 web），bundle 文件名带
# 内容哈希，所以按内容锚点打补丁；每处锚点都断言出现次数，上游改写法时构建直接失败。
COPY scripts/patch-selkies.py /tmp/patch-selkies.py
RUN python3 /tmp/patch-selkies.py && rm -f /tmp/patch-selkies.py

COPY /root /

# 浏览器伪装脚本：外部自动化模块（如手机上的刷抖音模块）只接受一个本地浏览器
# 可执行文件路径，而浏览器在容器里。该脚本被当作那个路径调用时不启动浏览器，只把
# 已运行实例的 DevTools websocket 地址按 Chrome 的格式打到 stderr，调用方据此连上
# 容器内的实例。同时经 GET /chrome-mcp 原样下载（见 src/shim.go 与 README）。
RUN chmod 0755 /usr/local/bin/chrome-mcp && test -x /usr/local/bin/chrome-mcp

EXPOSE 3000
# 外部 MCP 直连端口（MCP_HTTP_ADDR 默认 0.0.0.0:10102）
EXPOSE 10102

VOLUME /config
