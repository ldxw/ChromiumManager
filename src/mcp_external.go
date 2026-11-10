package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// MCP 外部监听端口：给容器外的 AI 客户端直连，不经过 nginx 反代，
// 也不带 Web 界面的 Basic Auth。只挂 /mcp、/healthz、/chrome-mcp、/cdp
// 四条路由，管理界面不会因此暴露。
//
// 通过 MCP_HTTP_ADDR 配置，默认 0.0.0.0:10102；设为空、off 或 0 则关闭。
// 鉴权沿用 MCP_TOKEN 或 MCP_KEYS：都为空时不校验，只适合本机或受控网络。

const defaultMCPExtAddr = "0.0.0.0:10102"

var mcpExtServer *http.Server

func mcpExtAddr() string {
	v := strings.TrimSpace(os.Getenv("MCP_HTTP_ADDR"))
	if v == "" {
		return defaultMCPExtAddr
	}
	if v == "0" || strings.EqualFold(v, "off") || strings.EqualFold(v, "false") {
		return ""
	}
	return v
}

// startMCPExternalServer 启动独立的外部 MCP HTTP 服务，启动后立即返回。
func startMCPExternalServer(s *mcpServer) {
	addr := mcpExtAddr()
	if addr == "" {
		log.Printf("[MCP] 外部端口已关闭（MCP_HTTP_ADDR=%q）", os.Getenv("MCP_HTTP_ADDR"))
		return
	}

	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHTTPHandler(s))
	mux.HandleFunc("GET /chrome-mcp", serveChromeMCPShim)
	registerCDPProxy(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok"))
	})

	mcpExtServer = &http.Server{Addr: addr, Handler: withMiddleware(mux)}

	if !mcpKeys.enabled() && mcpToken() == "" {
		log.Printf("[MCP] WARNING: 外部端口 %s 未设置 MCP_TOKEN 也未设置 MCP_KEYS，"+
			"任何能连到该端口的人都可调用全部已启用工具", addr)
	}

	go func() {
		log.Printf("[MCP] 外部端口监听 %s（POST /mcp）", addr)
		if err := mcpExtServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[MCP] external listen error: %v", err)
		}
	}()
}

func stopMCPExternalServer() {
	if mcpExtServer == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mcpExtServer.Shutdown(ctx); err != nil {
		log.Printf("[MCP] external forced shutdown: %v", err)
	}
}
