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
    '  }' > /tmp/mcp-location.conf && \
  sed -i '/error_page 500 502 503 504/r /tmp/mcp-location.conf' /defaults/default.conf && \
  rm -f /tmp/mcp-location.conf && \
  test "$(grep -c 'location /mcp' /defaults/default.conf)" -eq 2 && \
  test "$(grep -c 'auth_basic off;' /defaults/default.conf)" -eq 2

COPY /root /

EXPOSE 3000
# 外部 MCP 直连端口（MCP_HTTP_ADDR 默认 0.0.0.0:10102）
EXPOSE 10102

VOLUME /config
