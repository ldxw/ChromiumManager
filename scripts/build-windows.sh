#!/usr/bin/env bash
# 构建 Windows 原生版：前端 → Go 交叉编译 → 拼装捆绑 Chromium → 打包 zip
#
# 用法：
#   scripts/build-windows.sh                    # 上游取最新正式 Release
#   scripts/build-windows.sh --fp-tag X.Y.Z.W   # 指定上游 fingerprint-chromium 标签
#   scripts/build-windows.sh --skip-web         # 复用已有 src/web/dist
#   scripts/build-windows.sh --skip-chromium    # 复用已解包的 bin/chromium
#
# 产物：release/ChromiumManager-<tag>-windows-x64.zip
# 目录布局（与 src/browser.go 的 findBrowserPath 一致）：
#   ChromiumManager.exe
#   bin/chromium/chrome.exe ...
set -Eeuo pipefail

FP_REPO="adryfish/fingerprint-chromium"
FP_ASSET_PATTERN="_windows_x64.zip"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${ROOT}/build"
RELEASE_DIR="${ROOT}/release"
CACHE_DIR="${ROOT}/.cache"

FP_TAG=""
SKIP_WEB=0
SKIP_CHROMIUM=0

log() { printf '[build-windows] %s\n' "$*" >&2; }
die() { printf '[build-windows] 错误：%s\n' "$*" >&2; exit 1; }

while [ "$#" -gt 0 ]; do
  case "$1" in
    --fp-tag) FP_TAG="${2:?--fp-tag 需要参数}"; shift 2 ;;
    --skip-web) SKIP_WEB=1; shift ;;
    --skip-chromium) SKIP_CHROMIUM=1; shift ;;
    -h|--help) sed -n '2,15p' "${BASH_SOURCE[0]}"; exit 0 ;;
    *) die "未知参数：$1" ;;
  esac
done

for tool in go curl unzip; do
  command -v "$tool" >/dev/null 2>&1 || die "缺少命令：$tool"
done

# ---------------------------------------------------------------- 上游标签

if [ -z "$FP_TAG" ]; then
  log "查询上游最新正式 Release"
  FP_TAG="$(curl -fsSL --max-time 60 "https://api.github.com/repos/${FP_REPO}/releases/latest" \
    | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
  [ -n "$FP_TAG" ] || die "无法从 ${FP_REPO} 取得最新标签"
fi
log "上游标签：${FP_TAG}"

# ---------------------------------------------------------------- 前端

if [ "$SKIP_WEB" -eq 0 ]; then
  if command -v pnpm >/dev/null 2>&1; then
    PNPM=(pnpm)
  elif command -v npx >/dev/null 2>&1; then
    log "未安装 pnpm，改用 npx pnpm@10"
    PNPM=(npx -y pnpm@10)
  else
    die "缺少 pnpm（也没有 npx 可用）"
  fi
  log "构建前端"
  ( cd "${ROOT}/src/web" && "${PNPM[@]}" install --frozen-lockfile && "${PNPM[@]}" build )
else
  log "跳过前端构建"
fi
[ -f "${ROOT}/src/web/dist/index.html" ] \
  || die "src/web/dist 不存在，go:embed 无法编译；去掉 --skip-web 重跑"

# ---------------------------------------------------------------- Go

# --skip-chromium 要复用 build/bin/chromium，所以只清可执行文件，不整目录删除
if [ "$SKIP_CHROMIUM" -eq 1 ]; then
  mkdir -p "$BUILD_DIR"
  rm -f "${BUILD_DIR}/ChromiumManager.exe"
else
  rm -rf "$BUILD_DIR"
  mkdir -p "$BUILD_DIR"
fi
log "交叉编译 Windows GUI 可执行文件"
( cd "${ROOT}/src" \
  && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
     go build -trimpath -ldflags="-s -w -H windowsgui" -o "${BUILD_DIR}/ChromiumManager.exe" . )

# ---------------------------------------------------------------- Chromium

if [ "$SKIP_CHROMIUM" -eq 0 ]; then
  asset="$(curl -fsSL --max-time 60 "https://api.github.com/repos/${FP_REPO}/releases/tags/${FP_TAG}" \
    | grep -o "\"name\"[[:space:]]*:[[:space:]]*\"[^\"]*${FP_ASSET_PATTERN}\"" \
    | sed -n 's/.*"\([^"]*\)"$/\1/p' | head -1)"
  [ -n "$asset" ] || die "上游 ${FP_TAG} 没有匹配 ${FP_ASSET_PATTERN} 的附件"

  mkdir -p "$CACHE_DIR"
  zip="${CACHE_DIR}/${asset}"
  if [ -f "$zip" ] && unzip -tq "$zip" >/dev/null 2>&1; then
    log "复用缓存：${asset}"
  else
    log "下载 ${asset}"
    curl -fL --max-time 1800 --retry 3 -o "${zip}.part" \
      "https://github.com/${FP_REPO}/releases/download/${FP_TAG}/${asset}"
    mv "${zip}.part" "$zip"
    unzip -tq "$zip" >/dev/null || die "下载的压缩包损坏：${zip}"
  fi

  log "解包到 bin/chromium"
  mkdir -p "${BUILD_DIR}/bin"
  rm -rf "${BUILD_DIR}/bin/chromium" "${BUILD_DIR}/bin/chromium.tmp"
  unzip -q "$zip" -d "${BUILD_DIR}/bin/chromium.tmp"
  src_dir="$(find "${BUILD_DIR}/bin/chromium.tmp" -maxdepth 1 -mindepth 1 -type d -print -quit)"
  [ -n "$src_dir" ] || die "压缩包内没有顶层目录"
  # 整目录改名，不用 glob 展开，避免漏掉以点开头的文件
  mv "${src_dir}" "${BUILD_DIR}/bin/chromium"
  rm -rf "${BUILD_DIR}/bin/chromium.tmp"
  [ -f "${BUILD_DIR}/bin/chromium/chrome.exe" ] || die "解包后缺少 bin/chromium/chrome.exe"
else
  log "跳过 Chromium 解包"
  [ -f "${BUILD_DIR}/bin/chromium/chrome.exe" ] \
    || die "build/bin/chromium/chrome.exe 不存在；去掉 --skip-chromium 重跑"
fi

# ---------------------------------------------------------------- 打包

mkdir -p "$RELEASE_DIR"
out="${RELEASE_DIR}/ChromiumManager-${FP_TAG}-windows-x64.zip"
rm -f "$out"
log "打包：${out}"
( cd "$BUILD_DIR" && zip -q -r -9 "$out" . )

size="$(du -h "$out" | cut -f1)"
log "完成：${out}（${size}）"
