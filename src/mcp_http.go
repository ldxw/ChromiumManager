package main

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

// Streamable HTTP 传输：POST /mcp，请求体是一条（或一批）JSON-RPC 报文，
// 响应是 application/json 的单个 JSON-RPC 响应。不支持服务端主动推送，
// 因此 GET /mcp 返回 405（协议允许在无推送能力时这样做）。
//
// 鉴权：设置了 MCP_TOKEN 时要求 Authorization: Bearer <token>；
// 未设置时不做校验（服务默认只监听 127.0.0.1）。

const mcpMaxBody = 8 << 20

func mcpToken() string {
	return strings.TrimSpace(os.Getenv("MCP_TOKEN"))
}

func mcpAuthorized(r *http.Request) bool {
	token := mcpToken()
	if token == "" {
		return true
	}
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	got := strings.TrimSpace(h[len(prefix):])
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

func mcpHTTPHandler(s *mcpServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !mcpAuthorized(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		switch r.Method {
		case http.MethodPost:
		case http.MethodGet, http.MethodDelete:
			// 无服务端推送流，也没有会话状态需要删除。
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		default:
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
			http.Error(w, "unsupported content type: "+ct, http.StatusUnsupportedMediaType)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, mcpMaxBody+1))
		if err != nil {
			http.Error(w, "read body failed", http.StatusBadRequest)
			return
		}
		if len(body) > mcpMaxBody {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}

		trimmed := strings.TrimSpace(string(body))
		if trimmed == "" {
			writeRPCResponse(w, &rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: rpcInvalidRequest, Message: "empty body"}})
			return
		}

		// JSON-RPC 批量：逐个处理，只回有 id 的那些。
		if strings.HasPrefix(trimmed, "[") {
			var batch []json.RawMessage
			if err := json.Unmarshal([]byte(trimmed), &batch); err != nil {
				writeRPCResponse(w, &rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: rpcParseError, Message: "parse error: " + err.Error()}})
				return
			}
			if len(batch) == 0 {
				writeRPCResponse(w, &rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: rpcInvalidRequest, Message: "empty batch"}})
				return
			}
			out := make([]*rpcResponse, 0, len(batch))
			for _, raw := range batch {
				if resp := s.handle(raw); resp != nil {
					out = append(out, resp)
				}
			}
			if len(out) == 0 {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(out); err != nil {
				log.Printf("[MCP] write batch response failed: %v", err)
			}
			return
		}

		resp := s.handle([]byte(trimmed))
		if resp == nil {
			// 通知：协议规定返回 202 且无响应体。
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeRPCResponse(w, resp)
	}
}

func writeRPCResponse(w http.ResponseWriter, resp *rpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("[MCP] write response failed: %v", err)
	}
}
