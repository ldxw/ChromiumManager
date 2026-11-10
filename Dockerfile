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
# 鉴权（设了 MCP_TOKEN 才校验），Web 界面仍走 Basic Auth。
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

# 修补基础镜像里打包的 Selkies 前端（文件名带内容哈希，随上游版本变化）。
#
# 改的必须是 selkies-dashboard：nginx 的 root 是 /usr/share/selkies/web，但该目录在容器
# 启动时由 init-nginx 用 `rm -Rf web && cp -a $DASHBOARD web` 重建（DASHBOARD 默认
# selkies-dashboard），所以只有改源目录才能带进最终产物；改 web 会在启动时被覆盖掉。
# 每处 sed 前后都做计数校验：上游改写法或改名时构建直接失败，而不是悄悄产出一个坏镜像。
#
# 一、解码器提示
# 上游把解码器配置写死成 hardwareAcceleration:"prefer-software"。真机 WebCodecs 实测：
# 带这个提示时所有配置（含 1280x720）一律 supported:false，不传该字段则全部 true。
# 默认 encoder 是 x264enc，走分片（stripe）解码器：isConfigSupported 失败时只打一行
# "VNC stripe decoder config not supported" 并丢掉解码器，下一帧重建再失败，屏幕始终没有
# 画面，遮罩停在 "Waiting for stream..."。（jpeg 走主解码器，失败会进 Fi() 降档并 3 秒
# reload。）因此把该字段整条删掉，退回实测可行的「不传」路径。
#
# 二、手机端禁止缩放
# index.html 的 viewport 写死 maximum-scale=1,user-scalable=no，浏览器直接禁止捏合缩放；
# 1920x1080 的流缩到手机屏上，内容小到看不清。改成 maximum-scale=10,user-scalable=yes。
#
# 三、两指手势被转成滚轮
# _handleTouchEvent 的两指分支把 h 置 true（并调 _triggerMouseWheel 当滚动），函数结尾
# `h&&this.element.contains(s.target)&&s.preventDefault()` 因此取消浏览器的默认手势。
# 光改 viewport 不够：实测只改 meta 时两指 touchmove 仍全部 preventDefault。给结尾加
# `!this._isTwoFingerGesture`，两指手势不再被拦，单指点击/拖动/长按照旧由 Selkies 接管。
RUN f="$(ls /usr/share/selkies/selkies-dashboard/assets/index-*.js)" && \
  i=/usr/share/selkies/selkies-dashboard/index.html && \
  test "$(printf '%s\n' "$f" | wc -l)" -eq 1 && \
  test "$(grep -oF ',hardwareAcceleration:"prefer-software"' "$f" | wc -l)" -eq 2 && \
  sed -i 's/,hardwareAcceleration:"prefer-software"//g' "$f" && \
  test "$(grep -oF 'prefer-software' "$f" | wc -l)" -eq 0 && \
  test "$(grep -oF 'h&&this.element.contains(s.target)&&s.preventDefault()' "$f" | wc -l)" -eq 1 && \
  sed -i 's/h&&this\.element\.contains(s\.target)&&s\.preventDefault()/h\&\&!this._isTwoFingerGesture\&\&this.element.contains(s.target)\&\&s.preventDefault()/' "$f" && \
  test "$(grep -oF 'h&&!this._isTwoFingerGesture&&this.element.contains(s.target)&&s.preventDefault()' "$f" | wc -l)" -eq 1 && \
  test "$(grep -oF 'h&&this.element.contains(s.target)&&s.preventDefault()' "$f" | wc -l)" -eq 0 && \
  test "$(grep -oF 'maximum-scale=1,user-scalable=no' "$i" | wc -l)" -eq 1 && \
  sed -i 's/maximum-scale=1,user-scalable=no/maximum-scale=10,user-scalable=yes/' "$i" && \
  test "$(grep -oF 'maximum-scale=10,user-scalable=yes' "$i" | wc -l)" -eq 1 && \
  test "$(grep -oF 'user-scalable=no' "$i" | wc -l)" -eq 0

COPY /root /

EXPOSE 3000
# 外部 MCP 直连端口（MCP_HTTP_ADDR 默认 0.0.0.0:10102）
EXPOSE 10102

VOLUME /config
