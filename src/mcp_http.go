package main

import (
	"context"
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
// 鉴权：设置了 MCP_KEYS 时按 key 查表，命中的请求携带该 key 的绑定信息；
// 未设置 MCP_KEYS 时退回 MCP_TOKEN，未设置 MCP_TOKEN 则不做校验。

const mcpMaxBody = 8 << 20

func mcpToken() string {
	return strings.TrimSpace(os.Getenv("MCP_TOKEN"))
}

// mcpAuthorized 返回请求对应的绑定。第二个返回 false 表示未通过鉴权。
// 绑定为 nil 表示这是不受限的 MCP_TOKEN 通道（或未启用鉴权）。
func mcpAuthorized(r *http.Request) (*mcpBinding, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	var got string
	if strings.HasPrefix(h, prefix) {
		got = strings.TrimSpace(h[len(prefix):])
	}

	if mcpKeys.enabled() {
		if got == "" {
			return nil, false
		}
		b, ok := mcpKeys.lookup(got)
		if !ok {
			return nil, false
		}
		return b, true
	}

	token := mcpToken()
	if token == "" {
		return nil, true
	}
	if got == "" {
		return nil, false
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
		return nil, false
	}
	return nil, true
}

func mcpHTTPHandler(s *mcpServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		binding, ok := mcpAuthorized(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := r.Context()
		if binding != nil {
			ctx = context.WithValue(ctx, mcpBindingCtxKey{}, binding)
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
				if resp := s.handle(ctx, raw); resp != nil {
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

		resp := s.handle(ctx, []byte(trimmed))
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
