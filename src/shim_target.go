package main

import (
	"crypto/subtle"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// /chrome-mcp 的下发目标：要么是通用脚本（nil），要么是某个 key 的预填版。
//
// 预填版只在该 key 的凭据确实由请求自己带上来时才生成。凭据可以放在
// Authorization: Bearer <key>，也可以放在 ?token=<key>（和 /cdp 一致，因为
// 浏览器里点链接下载时加不了请求头）。

type shimPrefill struct {
	token    string // 写进 CDP_TOKEN 默认值的凭据
	httpBase string // 写进 CDP_HTTP 默认值的 manager 地址
	slug     string // 文件名后缀，空表示用通用文件名
}

// shimSlugFromPath 从 /chrome-mcp-<slug> 取后缀；/chrome-mcp 返回空串。
func shimSlugFromPath(p string) string {
	const prefix = "/chrome-mcp"
	if !strings.HasPrefix(p, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(p, prefix)
	if rest == "" {
		return ""
	}
	return strings.Trim(rest, "-/")
}

// shimCredential 取请求自带的凭据：Bearer 头优先，其次 ?token=。
func shimCredential(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		if v := strings.TrimSpace(h[len("Bearer "):]); v != "" {
			return v
		}
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

// shimTarget 决定本次下载给谁。返回 nil 表示下发通用脚本。
//
// 错误用 (nil, status, msg) 表达，调用方据此拒绝，且不会泄漏任何 token：
//   - 指定了 slug 但该 slug 不存在 → 404
//   - 指定了 slug 且凭据不匹配 → 403（不区分「slug 不存在」与「凭据不对」会让
//     人能枚举出哪些 slug 存在，所以两者都先校验凭据再判定）
//   - 未启用 MCP_KEYS 时只认 MCP_TOKEN，没设 token 就给通用脚本
func shimTarget(r *http.Request, slug string) (*shimPrefill, int, string) {
	cred := shimCredential(r)

	if !mcpKeys.enabled() {
		// 单 token 模式：slug 没有意义，给了就是拼错了。
		if slug != "" {
			return nil, http.StatusNotFound, "未启用 MCP_KEYS，不存在按 key 区分的下载文件"
		}
		token := mcpToken()
		if token == "" || cred == "" {
			return nil, 0, ""
		}
		if !constantTimeEqualStrings(cred, token) {
			return nil, http.StatusUnauthorized, "token 不匹配"
		}
		return &shimPrefill{token: token, httpBase: shimHTTPBase(r), slug: ""}, 0, ""
	}

	// 多 key 模式：任何预填都必须先证明自己是这个 key。
	if cred == "" {
		if slug != "" {
			return nil, http.StatusUnauthorized, "下载按 key 区分的文件需要凭据：Authorization: Bearer <key> 或 ?token=<key>"
		}
		return nil, 0, ""
	}

	b, ok := mcpKeys.lookup(cred)
	if !ok {
		return nil, http.StatusUnauthorized, "key 无效"
	}
	if slug != "" && slug != b.slug {
		return nil, http.StatusForbidden, "该凭据只能下载自己的文件（chrome-mcp-" + b.slug + "）"
	}

	return &shimPrefill{token: b.token, httpBase: shimHTTPBase(r), slug: b.slug}, 0, ""
}

// shimHTTPBase 推出写进脚本的 manager 地址。
//
// 预填版必须给明文地址：puppeteer 只解析 ws://，拿到 wss:// 会一直等到超时。
// 所以这里不沿用请求的 scheme，而是按下面顺序取：
//
//	MCP_SHIM_CDP_URL  显式指定，原样使用（内网域名/隧道场景）
//	MCP_HTTP_ADDR     形如 0.0.0.0:10102，取其中的端口配到请求的 Host 上
//	请求 Host 自带端口 沿用该端口（经 nginx 的 3000/3001 或自定义端口访问）
//	其余              http://<请求 Host>（80/443，脚本侧仍走明文）
func shimHTTPBase(r *http.Request) string {
	if v := strings.TrimSpace(os.Getenv("MCP_SHIM_CDP_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}

	host, reqPort := r.Host, ""
	if h, p, err := net.SplitHostPort(r.Host); err == nil {
		host, reqPort = h, p
	}
	if host == "" {
		host = "127.0.0.1"
	}

	// 外部端口关闭时 mcpExtAddr() 返回空串，此时退回请求自身的端口：
	// 否则从 3000 下载的脚本会拿到 http://<host>（80 端口），连不上。
	if port := shimPortFromAddr(mcpExtAddr()); port != "" {
		return "http://" + net.JoinHostPort(host, port)
	}
	if reqPort != "" {
		return "http://" + net.JoinHostPort(host, reqPort)
	}
	return "http://" + host
}

// shimPortFromAddr 从 "0.0.0.0:10102" / ":10102" / "10102" 取端口，取不到返回空。
func shimPortFromAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if _, port, err := net.SplitHostPort(addr); err == nil {
		if p, err := strconv.Atoi(port); err == nil && p > 0 && p < 65536 {
			return port
		}
		return ""
	}
	if p, err := strconv.Atoi(addr); err == nil && p > 0 && p < 65536 {
		return addr
	}
	return ""
}

// constantTimeEqualStrings 用常量时间比较两个凭据，避免按耗时逐字符试探。
func constantTimeEqualStrings(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
