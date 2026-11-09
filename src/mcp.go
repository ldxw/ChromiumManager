package main

import (
	"bufio"
	"context"
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
// ctx 里带当前请求的 key 绑定（HTTP 传输时），stdio 下为 Background。
type mcpTool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     func(ctx context.Context, args json.RawMessage) (any, error)
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
func (s *mcpServer) handle(ctx context.Context, line []byte) *rpcResponse {
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

	result, rerr := s.dispatch(ctx, req.Method, req.Params)
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

func (s *mcpServer) dispatch(ctx context.Context, method string, params json.RawMessage) (any, *rpcError) {
	binding := bindingFrom(ctx)

	switch method {
	case "initialize":
		instructions := "ChromiumManager 指纹浏览器管理。先用 list_profiles 找 _id，" +
			"launch_profile / stop_profile 控制实例，navigate / read_page 操作页面。"
		if binding != nil {
			instructions = fmt.Sprintf("ChromiumManager 指纹浏览器管理。当前 key 已绑定到配置「%s」，"+
				"工具里的 id 可以省略，省略即操作该配置；不能操作其他配置。", binding.name)
		}
		return map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false},
			},
			"serverInfo":   map[string]any{"name": s.name, "version": s.version},
			"instructions": instructions,
		}, nil

	case "notifications/initialized":
		return nil, nil

	case "ping":
		return map[string]any{}, nil

	case "tools/list":
		list := make([]map[string]any, 0, len(s.tools))
		for _, name := range s.toolNames() {
			if binding != nil && !bindingAllows(name) {
				continue
			}
			t := s.tools[name]
			schema, desc := t.InputSchema, t.Description
			if binding != nil {
				schema, desc = boundToolPresentation(name, t)
			}
			list = append(list, map[string]any{
				"name":        t.Name,
				"description": desc,
				"inputSchema": schema,
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
		if binding != nil && !bindingAllows(p.Name) {
			// 绑定 key 看不到的工具一律按不存在处理，不透露还有哪些管理能力。
			return nil, &rpcError{Code: rpcInvalidParams, Message: "unknown tool: " + p.Name}
		}
		if len(p.Arguments) == 0 {
			p.Arguments = json.RawMessage("{}")
		}
		out, err := t.Handler(ctx, p.Arguments)
		if err != nil {
			return toolErrorResult(err.Error()), nil
		}
		if img, ok := out.(mcpImageResult); ok {
			return img.toolResult(), nil
		}
		return toolTextResult(out), nil
	}

	return nil, &rpcError{Code: rpcMethodNotFound, Message: "method not found: " + method}
}

// mcpImageResult 让工具直接回一条 MCP image content，
// 客户端按协议把 base64 当成图片渲染，不需要再读文件。
type mcpImageResult struct {
	Data     string // base64，不带 data: 前缀
	MimeType string
	URL      string
	Title    string
	Bytes    int
}

func (r mcpImageResult) toolResult() map[string]any {
	return map[string]any{
		"content": []map[string]any{
			{"type": "image", "data": r.Data, "mimeType": r.MimeType},
			{"type": "text", "text": fmt.Sprintf("PNG %d 字节\nURL: %s\n标题: %s", r.Bytes, r.URL, r.Title)},
		},
		"isError": false,
	}
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
		resp := s.handle(context.Background(), []byte(line))
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
