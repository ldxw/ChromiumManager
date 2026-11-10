package main

import (
	"net/http"
	"os"
	"strings"
)

// 浏览器伪装脚本的下载入口。
//
// 抖音一类的自动化模块只认一个本地浏览器可执行文件，而浏览器在容器里。
// 镜像里的 /usr/local/bin/chrome-mcp 被当作那个路径调用时，会把已运行实例的
// DevTools websocket 地址按 Chrome 的格式打到 stderr，调用方据此连上远端。
//
// 手机/Aurabot 侧取不到容器的文件系统，所以这里把它交出去。三种取法：
//
//	GET /chrome-mcp                     通用脚本，不含任何凭据，靠环境变量自己配
//	GET /chrome-mcp-<slug>              该 key 的预填版，凭据从请求头/query 取
//	GET /chrome-mcp?token=<key>         同上，文件名 chrome-mcp-<slug>
//
// 预填版把 token 与 manager 地址写成脚本里的默认值，下载下来 chmod +x 就能用，
// 不必再配环境变量。凭据一律取自请求本身（Authorization: Bearer 或 ?token=），
// 不能指名要别人的 key，也就不会因为这条路由把多 key 隔离打穿。
//
// MCP_SHIM_PATH 可改源文件位置，默认 /usr/local/bin/chrome-mcp。
//
// 路由判据是 isShimPath + shimHandler：ServeMux 的通配符必须占满整段路径
// （`/chrome-mcp-{slug}` 在注册时直接 panic），`/chrome-mcp-<slug>` 没法用
// 模式匹配，所以在 mux 前面按前缀分流。
func serveChromeMCPShim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "只支持 GET", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimSpace(os.Getenv("MCP_SHIM_PATH"))
	if path == "" {
		path = "/usr/local/bin/chrome-mcp"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "chrome-mcp 不在本机（"+path+"），可从镜像里 docker cp 取出", http.StatusNotFound)
		return
	}

	slug := shimSlugFromPath(r.URL.Path)
	target, status, msg := shimTarget(r, slug)
	if status != 0 {
		if status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", `Bearer realm="chrome-mcp"`)
		}
		http.Error(w, msg, status)
		return
	}

	body, filename := buildShimScript(data, target)

	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Write(body)
}

// buildShimScript 返回要下发的内容与文件名。target 为 nil 时原样返回。
func buildShimScript(orig []byte, target *shimPrefill) ([]byte, string) {
	if target == nil {
		return orig, "chrome-mcp"
	}

	s := string(orig)
	// 两处锚点必须命中且各只一次：脚本被改写法时宁可给出未预填的原文，
	// 也不要交出「看着预填了其实没填」的文件。
	var n int
	s, n = replaceOnce(s,
		`CDP_HTTP="${MCP_CDP_HTTP:-http://127.0.0.1:10102}"`,
		`CDP_HTTP="${MCP_CDP_HTTP:-`+target.httpBase+`}"`)
	if n != 1 {
		return orig, "chrome-mcp"
	}
	s, n = replaceOnce(s,
		`CDP_TOKEN="${MCP_CDP_TOKEN:-}"`,
		`CDP_TOKEN="${MCP_CDP_TOKEN:-`+target.token+`}"`)
	if n != 1 {
		return orig, "chrome-mcp"
	}

	name := "chrome-mcp"
	if target.slug != "" {
		name += "-" + target.slug
	}
	return []byte(s), name
}

// replaceOnce 与 strings.Replace(s, old, new, 1) 相同，额外返回命中次数（0 或 1）。
func replaceOnce(s, old, new string) (string, int) {
	if !strings.Contains(s, old) {
		return s, 0
	}
	return strings.Replace(s, old, new, 1), 1
}

// isShimPath 判断是否伪装脚本的下载路径：/chrome-mcp 或 /chrome-mcp-<slug>。
func isShimPath(p string) bool {
	if p == "/chrome-mcp" {
		return true
	}
	return strings.HasPrefix(p, "/chrome-mcp-") && len(p) > len("/chrome-mcp-")
}

// shimHandler 把伪装脚本的下载路径从 mux 里分流出来。ServeMux 的模式没法表达
// 「字面前缀 + 变量」（`/chrome-mcp-{slug}` 注册即 panic），只能在这里判。
func shimHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isShimPath(r.URL.Path) {
			serveChromeMCPShim(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
