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
// 手机/Aurabot 侧取不到容器的文件系统，所以这里把它原样交出去：
//
//	curl -fsS http://<宿主机>:3000/chrome-mcp -o chrome-mcp && chmod +x chrome-mcp
//
// MCP_SHIM_PATH 可改源文件位置，默认 /usr/local/bin/chrome-mcp。
func serveChromeMCPShim(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(os.Getenv("MCP_SHIM_PATH"))
	if path == "" {
		path = "/usr/local/bin/chrome-mcp"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "chrome-mcp 不在本机（"+path+"），可从镜像里 docker cp 取出", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="chrome-mcp"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
}
