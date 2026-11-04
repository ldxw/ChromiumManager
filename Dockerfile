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

COPY /root /

EXPOSE 3000

VOLUME /config
