package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"
)

// 最小 MCP 实现：JSON-RPC 2.0 over stdio（换行分隔的 JSON 对象）。
// 只覆盖 initialize / notifications/initialized / ping / tools/list / tools/call，
// 不引入官方 SDK，避免为这五个方法拖进一整套依赖。

const mcpProtocolVersion = "2025-06-18"

const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// mcpTool 是一个工具定义 + 处理器。args 是 tools/call 里的 arguments 原文。
type mcpTool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     func(args json.RawMessage) (any, error)
}

type mcpServer struct {
	name    string
	version string
	tools   map[string]mcpTool
}

func newMCPServer() *mcpServer {
	s := &mcpServer{name: "ChromiumManager", version: appVersion, tools: map[string]mcpTool{}}
	registerReadOnlyTools(s)
	registerWriteTools(s)
	registerBrowserTools(s)
	if isDestructiveEnabled() {
		registerDestructiveTools(s)
	}
	return s
}

// isDestructiveEnabled 控制删除类工具的注册。默认关闭。
func isDestructiveEnabled() bool {
	return os.Getenv("MCP_ENABLE_DESTRUCTIVE") == "1"
}

func (s *mcpServer) toolNames() []string {
	names := make([]string, 0, len(s.tools))
	for n := range s.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// handle 处理单条 JSON-RPC 报文。返回 nil 表示这是通知、不应回复。
func (s *mcpServer) handle(line []byte) *rpcResponse {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return &rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: rpcParseError, Message: "parse error: " + err.Error()}}
	}

	isNotification := len(req.ID) == 0 || string(req.ID) == "null"

	if req.JSONRPC != "2.0" || req.Method == "" {
		if isNotification {
			return nil
		}
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: rpcInvalidRequest, Message: "invalid JSON-RPC 2.0 request"}}
	}

	result, rerr := s.dispatch(req.Method, req.Params)
	if isNotification {
		if rerr != nil {
			log.Printf("[MCP] notification %s failed: %s", req.Method, rerr.Message)
		}
		return nil
	}
	resp := &rpcResponse{JSONRPC: "2.0", ID: req.ID}
	if rerr != nil {
		resp.Error = rerr
	} else {
		resp.Result = result
	}
	return resp
}

func (s *mcpServer) dispatch(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false},
			},
			"serverInfo": map[string]any{"name": s.name, "version": s.version},
			"instructions": "ChromiumManager 指纹浏览器管理。先用 list_profiles 找 _id，" +
				"launch_profile / stop_profile 控制实例，navigate / read_page 操作页面。",
		}, nil

	case "notifications/initialized":
		return nil, nil

	case "ping":
		return map[string]any{}, nil

	case "tools/list":
		list := make([]map[string]any, 0, len(s.tools))
		for _, name := range s.toolNames() {
			t := s.tools[name]
			list = append(list, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
			})
		}
		return map[string]any{"tools": list}, nil

	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: rpcInvalidParams, Message: "invalid params: " + err.Error()}
		}
		t, ok := s.tools[p.Name]
		if !ok {
			return nil, &rpcError{Code: rpcInvalidParams, Message: "unknown tool: " + p.Name}
		}
		if len(p.Arguments) == 0 {
			p.Arguments = json.RawMessage("{}")
		}
		out, err := t.Handler(p.Arguments)
		if err != nil {
			return toolErrorResult(err.Error()), nil
		}
		return toolTextResult(out), nil
	}

	return nil, &rpcError{Code: rpcMethodNotFound, Message: "method not found: " + method}
}

// toolTextResult 把任意值序列化成一条 text content。字符串原样输出。
func toolTextResult(v any) map[string]any {
	var text string
	switch x := v.(type) {
	case string:
		text = x
	case nil:
		text = "ok"
	default:
		b, err := json.MarshalIndent(x, "", "  ")
		if err != nil {
			text = fmt.Sprint(x)
		} else {
			text = string(b)
		}
	}
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": false,
	}
}

func toolErrorResult(msg string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": msg}},
		"isError": true,
	}
}

// serveStdio 跑 stdio 传输：逐行读 stdin 的 JSON-RPC，逐行写 stdout。
// 调用前必须把 log 输出改到 stderr，否则会污染协议管道。
func (s *mcpServer) serveStdio(in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	w := bufio.NewWriter(out)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		resp := s.handle([]byte(line))
		if resp == nil {
			continue
		}
		enc, err := json.Marshal(resp)
		if err != nil {
			log.Printf("[MCP] marshal response failed: %v", err)
			continue
		}
		if _, err := w.Write(enc); err != nil {
			return err
		}
		if err := w.WriteByte('\n'); err != nil {
			return err
		}
		if err := w.Flush(); err != nil {
			return err
		}
	}
	return scanner.Err()
}
