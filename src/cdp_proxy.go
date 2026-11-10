package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"sort"
	"strconv"
	"strings"
)

// CDP 反向代理：把运行中实例的 DevTools 接口挂到 manager 自己的 HTTP 端口下，
// 路径形如 /cdp/<配置 _id>/json/version、/cdp/<配置 _id>/devtools/browser/<uuid>。
//
// 为什么需要它：Chromium 的 DevTools 只监听 127.0.0.1，而
// --remote-debugging-address 在 156 上并不生效（实测 headless 与 headful 传
// 0.0.0.0 后 ss 仍是 127.0.0.1:PORT，从容器 eth 地址连不上）。于是改成在容器内
// 做这一层反代：
//   - 不用给每个实例占一个固定宿主端口，随机端口也能用；
//   - 复用 3000/3001/10102 上已有的 nginx 与外部监听，不额外开口子；
//   - 鉴权沿用 MCP_TOKEN（HTTP 头或 ?token=），比裸 DevTools 端口安全；
//   - 端口 0 的实例也能用，不必在自定义启动参数里配 --remote-debugging-port。
//
// /json/version 与 /json/list 的响应会被改写：webSocketDebuggerUrl 里的
// 127.0.0.1:PORT 换成本代理路径，客户端拿到就能直接连。
//
// /cdp（不带 _id）是给伪装脚本用的便捷入口：只有一个实例在跑时直接返回它的
// /json/version，省掉把配置 _id 填进手机模块这一步。

func registerCDPProxy(mux *http.ServeMux) {
	mux.HandleFunc("/cdp/", cdpProxyHandler)
	mux.HandleFunc("/cdp", cdpAutoHandler)
}

// cdpAuthorized 复用 /mcp 的鉴权来源，但额外接受 query 里的 token：
// puppeteer 建 CDP 连接时没法加自定义请求头，只能把凭据放进 URL。
//
// 设置了 MCP_KEYS 时同样只认这些 key，且要求它有权操作目标配置（见 cdpBindingFor）。
func cdpAuthorized(r *http.Request) bool {
	_, ok := cdpAuth(r)
	return ok
}

// cdpAuth 是 cdpAuthorized 的完整版，顺带返回命中的绑定（未启用 MCP_KEYS 时为 nil）。
func cdpAuth(r *http.Request) (*mcpBinding, bool) {
	// 先把 query 里的 token 搬进 Authorization，好让两套传输共用 mcpAuthorized。
	headerReq := r
	got := strings.TrimSpace(r.URL.Query().Get("token"))
	if got != "" && !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		clone := r.Clone(r.Context())
		clone.Header.Set("Authorization", "Bearer "+got)
		headerReq = clone
	}

	if b, ok := mcpAuthorized(headerReq); ok {
		return b, true
	}

	// 未启用 MCP_KEYS 时还允许直接比对 MCP_TOKEN 的 ?token=。
	if mcpKeys.enabled() {
		return nil, false
	}
	token := mcpToken()
	if token == "" {
		return nil, true
	}
	if got == "" {
		return nil, false
	}
	return nil, subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

// cdpBindingFor 校验命中 key 的绑定是否允许访问 profileID。
// 绑定的 key 只能连自己那一个配置，否则等于绕过了 /mcp 的隔离。
func cdpBindingFor(b *mcpBinding, profileID string) error {
	if b == nil {
		return nil
	}
	if err := b.ensureValid(); err != nil {
		return err
	}
	if profileID != "" && profileID != b.profileID {
		return fmt.Errorf("当前 key 已绑定到配置「%s」，不能连接其他配置", b.String())
	}
	return nil
}

// cdpAutoHandler 处理 /cdp：恰好一个实例在跑时转发到它的 /json/version。
// 0 个或多于 1 个都明确报错并把候选列出来，避免静默连到错误的浏览器。
func cdpAutoHandler(w http.ResponseWriter, r *http.Request) {
	binding, ok := cdpAuth(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer realm="cdp"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	runningMu.Lock()
	ids := make([]string, 0, len(runningProfiles))
	for id := range runningProfiles {
		ids = append(ids, id)
	}
	runningMu.Unlock()
	sort.Strings(ids)

	// 绑定的 key 只看得见自己那一个配置，不必要求它填 _id，也不会得知别人在跑什么。
	if binding != nil {
		if err := cdpBindingFor(binding, ""); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		ids = []string{binding.profileID}
	}

	switch len(ids) {
	case 0:
		http.Error(w, "没有运行中的配置：先 launch_profile，或用 /cdp/<配置 _id>/json/version",
			http.StatusServiceUnavailable)
		return
	case 1:
		r.URL.Path = "/cdp/" + ids[0] + "/json/version"
		cdpProxyHandler(w, r)
		return
	default:
		var b strings.Builder
		for _, id := range ids {
			name := ""
			if raw := decodeID(id); raw > 0 {
				if p, err := loadProfile(raw); err == nil {
					name = " (" + p.Name + ")"
				}
			}
			fmt.Fprintf(&b, "\n  /cdp/%s/json/version%s", id, name)
		}
		http.Error(w, "有多个运行中的配置，请指定其中一个："+b.String(),
			http.StatusConflict)
	}
}

func cdpProxyHandler(w http.ResponseWriter, r *http.Request) {
	binding, ok := cdpAuth(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer realm="cdp"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/cdp/")
	profileID, tail := rest, "/json/version"
	if i := strings.Index(rest, "/"); i >= 0 {
		profileID, tail = rest[:i], rest[i:]
	}
	if profileID == "" {
		http.Error(w, "用法：/cdp/<配置 _id>/json/version，或 /cdp 取唯一运行实例",
			http.StatusNotFound)
		return
	}

	// 绑定的 key 只能连自己的配置，否则等于绕过 /mcp 那边的隔离。
	if err := cdpBindingFor(binding, profileID); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	runningMu.Lock()
	rp := runningProfiles[profileID]
	runningMu.Unlock()
	if rp == nil {
		http.Error(w, "配置不在运行中："+profileID, http.StatusNotFound)
		return
	}

	wsURL := rp.getDevToolsURL()
	if wsURL == "" {
		http.Error(w, "DevTools 端口还没就绪，稍后重试（launch 后通常 1–3 秒）",
			http.StatusServiceUnavailable)
		return
	}
	upstream := strings.TrimPrefix(wsURL, "ws://")
	if j := strings.Index(upstream, "/"); j >= 0 {
		upstream = upstream[:j]
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = upstream
			req.URL.Path = tail
			req.Host = upstream
			// token 只用于 manager 这一层，不转给 Chromium。
			q := req.URL.Query()
			q.Del("token")
			req.URL.RawQuery = q.Encode()
			req.Header.Del("Authorization")
		},
		// -1 让响应不缓冲：/json/list 与 DevTools 的 websocket 都是流式的。
		// ReverseProxy 自 Go 1.12 起直接处理 101 并 hijack 连接，ws 升级无需额外代码。
		FlushInterval: -1,
		ModifyResponse: func(resp *http.Response) error {
			if resp.StatusCode != http.StatusOK ||
				!strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
				return nil
			}
			body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
			resp.Body.Close()
			if err != nil {
				return err
			}
			if rewritten, ok := rewriteCDPJSON(body, r, profileID); ok {
				body = rewritten
			}
			resp.Body = io.NopCloser(bytes.NewReader(body))
			resp.ContentLength = int64(len(body))
			resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
			return nil
		},
	}
	proxy.ServeHTTP(w, r)
}

// rewriteCDPJSON 把响应里所有 webSocketDebuggerUrl 换成本代理对外可达的地址。
// 兼容 /json/version（单个对象）与 /json/list（数组）。
func rewriteCDPJSON(body []byte, r *http.Request, profileID string) ([]byte, bool) {
	wsHost := externalCDPHost(r)

	var single map[string]any
	if err := json.Unmarshal(body, &single); err == nil {
		if !rewriteWSEntry(single, wsHost, profileID, r) {
			return nil, false
		}
		out, err := json.Marshal(single)
		return out, err == nil
	}

	var list []map[string]any
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, false
	}
	changed := false
	for _, e := range list {
		if rewriteWSEntry(e, wsHost, profileID, r) {
			changed = true
		}
	}
	if !changed {
		return nil, false
	}
	out, err := json.Marshal(list)
	return out, err == nil
}

// rewriteWSEntry 把一个条目里的 ws 地址换成本代理路径，返回是否改动。
// wsHost 是调用方眼里的 host[:port]，profileID 决定挂在哪个反代前缀下。
func rewriteWSEntry(entry map[string]any, wsHost, profileID string, r *http.Request) bool {
	raw, ok := entry["webSocketDebuggerUrl"].(string)
	if !ok || raw == "" {
		return false
	}
	// 只取路径部分，丢掉上游的 ws://127.0.0.1:<随机端口>。
	path := "/"
	if i := strings.Index(raw, "://"); i >= 0 {
		raw = raw[i+3:]
		if j := strings.Index(raw, "/"); j >= 0 {
			path = raw[j:]
		}
	}

	scheme := "ws"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "wss"
	}
	// 客户端的 CDP 连接无法带请求头，凭据只能放进 URL。这里把本次请求用过的
	// token 原样带出去，客户端拿到后直接连即可，不必自己再拼。
	u := scheme + "://" + strings.TrimSuffix(wsHost, "/") + "/cdp/" + profileID + path
	if tok := cdpRequestToken(r); tok != "" {
		u += "?token=" + tok
	}
	entry["webSocketDebuggerUrl"] = u
	return true
}

// cdpRequestToken 取出本次请求携带的凭据（query 的 token 或 Bearer 头）。
// 启用 MCP_KEYS 时凭据是一个 key，同样要沿用到改写后的 ws 地址上，所以这里
// 不再以 MCP_TOKEN 是否为空作为前提。
func cdpRequestToken(r *http.Request) string {
	if tok := strings.TrimSpace(r.URL.Query().Get("token")); tok != "" {
		return tok
	}
	const prefix = "Bearer "
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}

// externalCDPHost 返回调用方眼里的 host[:port]。
// 优先用反向代理传来的 X-Forwarded-Host（带端口），否则用请求自己的 Host。
func externalCDPHost(r *http.Request) string {
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if i := strings.Index(host, ","); i >= 0 {
		host = strings.TrimSpace(host[:i])
	}
	if host != "" {
		return host
	}
	return r.Host
}
