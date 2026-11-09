package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 浏览器操作工具。每个运行中的 profile 复用一个 cdpClient（含一个 page session），
// 首次调用时懒连接，连接断开或页面失效时自动重连一次。

const cdpScreenshotMax = 30 * time.Second

type cdpSession struct {
	client   *cdpClient
	session  string
	targetID string
}

type browserConn struct {
	mu      sync.Mutex
	profile string
	sess    *cdpSession
}

var (
	browserConnsMu sync.Mutex
	browserConns   = map[string]*browserConn{}
)

func browserConnFor(profileID string) *browserConn {
	browserConnsMu.Lock()
	defer browserConnsMu.Unlock()
	bc, ok := browserConns[profileID]
	if !ok {
		bc = &browserConn{profile: profileID}
		browserConns[profileID] = bc
	}
	return bc
}

// dropBrowserConn 关闭并移除某个 profile 的连接缓存。配置停止时需要调用。
func dropBrowserConn(profileID string) {
	browserConnsMu.Lock()
	bc := browserConns[profileID]
	delete(browserConns, profileID)
	browserConnsMu.Unlock()
	if bc != nil {
		bc.mu.Lock()
		if bc.sess != nil && bc.sess.client != nil {
			bc.sess.client.Close()
		}
		bc.sess = nil
		bc.mu.Unlock()
	}
}

func dropAllBrowserConns() {
	browserConnsMu.Lock()
	ids := make([]string, 0, len(browserConns))
	for id := range browserConns {
		ids = append(ids, id)
	}
	browserConnsMu.Unlock()
	for _, id := range ids {
		dropBrowserConn(id)
	}
}

// getCDPSession 返回（必要时建立）profile 的 CDP 会话。
func getCDPSession(profileID string) (*cdpSession, error) {
	if !isRunning(profileID) {
		return nil, fmt.Errorf("%w：请先 launch_profile", errNotRunning)
	}

	runningMu.Lock()
	rp := runningProfiles[profileID]
	runningMu.Unlock()
	if rp == nil {
		return nil, errNotRunning
	}

	wsURL := rp.getDevToolsURL()
	if wsURL == "" {
		return nil, errors.New("DevTools 端口还没就绪，稍后重试（launch 后通常 1–3 秒）")
	}

	bc := browserConnFor(profileID)
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if bc.sess != nil && bc.sess.client != nil {
		select {
		case <-bc.sess.client.closed:
			bc.sess = nil // 连接已断，落到下面重连
		default:
			return bc.sess, nil
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := dialCDP(ctx, wsURL)
	if err != nil {
		return nil, err
	}
	sessionID, targetID, err := client.attachPage(ctx)
	if err != nil {
		client.Close()
		return nil, err
	}
	bc.sess = &cdpSession{client: client, session: sessionID, targetID: targetID}
	return bc.sess, nil
}

// call 发一条页面级命令。
func (s *cdpSession) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return s.client.send(ctx, s.session, method, params)
}

func cdpCtx(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

// resetSession 丢弃当前会话，下次调用重连。
func resetSession(profileID string) {
	dropBrowserConn(profileID)
}

// ------------------------------------------------------------------ 工具注册

func registerBrowserTools(s *mcpServer) {
	s.tools["navigate"] = mcpTool{
		Name: "navigate",
		Description: "在指定配置的浏览器里打开 URL 并等待页面加载完成。" +
			"返回最终 URL、标题和 HTTP 状态码。配置需已 launch_profile。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":      map[string]any{"type": "string", "description": "配置 _id"},
				"url":     map[string]any{"type": "string", "description": "目标 URL，没写协议时按 https 处理"},
				"timeout": map[string]any{"type": "integer", "description": "等待加载的毫秒数，默认 15000，上限 60000"},
			},
			"required": []string{"id", "url"},
		},
		Handler: toolNavigate,
	}

	s.tools["read_page"] = mcpTool{
		Name: "read_page",
		Description: "读取当前页面：标题、URL、可见文本（默认截断到 8000 字）。" +
			"selector 可选，只取匹配元素的文本。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":       map[string]any{"type": "string", "description": "配置 _id"},
				"selector": map[string]any{"type": "string", "description": "CSS 选择器，留空取 document.body"},
				"maxChars": map[string]any{"type": "integer", "description": "文本上限，默认 8000"},
			},
			"required": []string{"id"},
		},
		Handler: toolReadPage,
	}

	s.tools["click"] = mcpTool{
		Name:        "click",
		Description: "点击匹配 CSS 选择器的元素（先滚动到可见区域）。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":       map[string]any{"type": "string", "description": "配置 _id"},
				"selector": map[string]any{"type": "string", "description": "CSS 选择器"},
			},
			"required": []string{"id", "selector"},
		},
		Handler: toolClick,
	}

	s.tools["fill"] = mcpTool{
		Name:        "fill",
		Description: "把文本填入匹配 CSS 选择器的输入框（清空后输入，触发 input 事件）。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":       map[string]any{"type": "string", "description": "配置 _id"},
				"selector": map[string]any{"type": "string", "description": "CSS 选择器"},
				"text":     map[string]any{"type": "string", "description": "要填入的文本"},
				"submit":   map[string]any{"type": "boolean", "description": "填完是否按 Enter"},
			},
			"required": []string{"id", "selector", "text"},
		},
		Handler: toolFill,
	}

	s.tools["screenshot"] = mcpTool{
		Name: "screenshot",
		Description: "对整页或指定元素截图，PNG base64 写入文件。" +
			"返回图片绝对路径；客户端可直接读取该文件展示。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":       map[string]any{"type": "string", "description": "配置 _id"},
				"selector": map[string]any{"type": "string", "description": "CSS 选择器，留空截整页"},
				"fullPage": map[string]any{"type": "boolean", "description": "是否整页（含滚动区域），默认 false"},
				"path":     map[string]any{"type": "string", "description": "输出文件路径，留空自动放在 <DATA_DIR>/screenshots/"},
			},
			"required": []string{"id"},
		},
		Handler: toolScreenshot,
	}
	// 绑定 key 下的描述与 schema 由 boundToolPresentation 覆写：不落盘、去掉 path。

	s.tools["eval_js"] = mcpTool{
		Name: "eval_js",
		Description: "在页面上下文执行 JavaScript 表达式，返回其 JSON 序列化结果。" +
			"适合读取 DOM 属性、滚动、点击复杂元素等。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":           map[string]any{"type": "string", "description": "配置 _id"},
				"expression":   map[string]any{"type": "string", "description": "JS 表达式，返回值需可 JSON 序列化"},
				"awaitPromise": map[string]any{"type": "boolean", "description": "表达式返回 Promise 时是否等待，默认 true"},
			},
			"required": []string{"id", "expression"},
		},
		Handler: toolEvalJS,
	}
}

// ------------------------------------------------------------------ 参数与工具实现

func normalizeURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return u
	}
	if strings.Contains(u, "://") || strings.HasPrefix(u, "about:") {
		return u
	}
	return "https://" + u
}

// normalizeURLScoped 是绑定 key 的版本：只允许 http/https。
// file:// 能读整份 data.db（含全部 cookie 与代理凭据），chrome://、data:、
// javascript: 也都能绕出页面边界，对外部使用方一律不开放。
func normalizeURLScoped(u string) (string, error) {
	target := normalizeURL(u)
	if target == "" {
		return "", errors.New("url 不能为空")
	}
	lower := strings.ToLower(target)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return target, nil
	}
	return "", fmt.Errorf("只允许 http:// 和 https:// 地址，收到：%s", target)
}

func currentPageInfo(sess cdpSession) (string, string, error) {
	ctx, cancel := cdpCtx(cdpCallTimeout)
	defer cancel()
	raw, err := sess.call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    "JSON.stringify({url: location.href, title: document.title})",
		"returnByValue": true,
	})
	if err != nil {
		return "", "", err
	}
	var out struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", nil
	}
	var info struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	}
	_ = json.Unmarshal([]byte(out.Result.Value), &info)
	return info.URL, info.Title, nil
}

func toolNavigate(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		ID      string `json:"id"`
		URL     string `json:"url"`
		Timeout int    `json:"timeout"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}

	profileID, err := resolveTarget(ctx, a.ID)
	if err != nil {
		return nil, err
	}

	var target string
	if bindingFrom(ctx) != nil {
		target, err = normalizeURLScoped(a.URL)
	} else {
		target = normalizeURL(a.URL)
		if target == "" {
			err = errors.New("url 不能为空")
		}
	}
	if err != nil {
		return nil, err
	}

	wait := a.Timeout
	if wait <= 0 {
		wait = 15000
	}
	if wait > 60000 {
		wait = 60000
	}

	sess, err := getCDPSession(profileID)
	if err != nil {
		return nil, err
	}

	ctx, cancel := cdpCtx(time.Duration(wait)*time.Millisecond + 3*time.Second)
	defer cancel()

	raw, err := sess.call(ctx, "Page.navigate", map[string]any{"url": target})
	if err != nil {
		resetSession(profileID)
		return nil, fmt.Errorf("导航失败：%v", err)
	}
	var nav struct {
		FrameID   string `json:"frameId"`
		ErrorText string `json:"errorText"`
	}
	_ = json.Unmarshal(raw, &nav)
	if nav.ErrorText != "" {
		return nil, fmt.Errorf("导航失败：%s", nav.ErrorText)
	}

	// 轮询 readyState 直到 complete 或超时，比等 Page.loadEventFired 事件简单可靠。
	deadline := time.Now().Add(time.Duration(wait) * time.Millisecond)
	state := ""
	for {
		rctx, rcancel := cdpCtx(5 * time.Second)
		raw, err = sess.call(rctx, "Runtime.evaluate", map[string]any{
			"expression":    "document.readyState",
			"returnByValue": true,
		})
		rcancel()
		if err == nil {
			var out struct {
				Result struct {
					Value string `json:"value"`
				} `json:"result"`
			}
			if json.Unmarshal(raw, &out) == nil {
				state = out.Result.Value
			}
		}
		if state == "complete" || time.Now().After(deadline) {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}

	finalURL, title, _ := currentPageInfo(*sess)
	return map[string]any{
		"url":        finalURL,
		"title":      title,
		"readyState": state,
		"loaded":     state == "complete",
	}, nil
}

func evalString(sess cdpSession, expr string) (string, error) {
	ctx, cancel := cdpCtx(cdpCallTimeout)
	defer cancel()
	raw, err := sess.call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    expr,
		"returnByValue": true,
		"awaitPromise":  true,
	})
	if err != nil {
		return "", err
	}
	var out struct {
		Result struct {
			Type  string `json:"type"`
			Value any    `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("解析 JS 结果失败：%v", err)
	}
	if out.ExceptionDetails != nil {
		return "", fmt.Errorf("页面脚本抛错：%s", out.ExceptionDetails.Text)
	}
	switch v := out.Result.Value.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	default:
		b, _ := json.Marshal(v)
		return string(b), nil
	}
}

func toolReadPage(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		ID       string `json:"id"`
		Selector string `json:"selector"`
		MaxChars int    `json:"maxChars"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	profileID, err := resolveTarget(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	if a.MaxChars <= 0 {
		a.MaxChars = 8000
	}

	sess, err := getCDPSession(profileID)
	if err != nil {
		return nil, err
	}
	target := "document.body"
	if a.Selector != "" {
		target = fmt.Sprintf("document.querySelector(%s)", jsString(a.Selector))
	}
	expr := fmt.Sprintf(`(() => {
  const el = %s;
  if (!el) return JSON.stringify({found:false});
  return JSON.stringify({found:true, text: (el.innerText || el.textContent || "").trim()});
})()`, target)

	text, err := evalString(*sess, expr)
	if err != nil {
		resetSession(profileID)
		return nil, err
	}
	var parsed struct {
		Found bool   `json:"found"`
		Text  string `json:"text"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, fmt.Errorf("解析页面文本失败：%v", err)
	}
	if !parsed.Found {
		return nil, fmt.Errorf("页面里找不到元素：%s", a.Selector)
	}

	url, title, _ := currentPageInfo(*sess)
	out := map[string]any{
		"url":   url,
		"title": title,
		"chars": len([]rune(parsed.Text)),
		"text":  parsed.Text,
	}
	if len([]rune(parsed.Text)) > a.MaxChars {
		out["text"] = string([]rune(parsed.Text)[:a.MaxChars])
		out["truncated"] = true
		out["hint"] = fmt.Sprintf("文本已截断到 %d 字，可用 selector 取局部", a.MaxChars)
	}
	return out, nil
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func toolClick(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		ID       string `json:"id"`
		Selector string `json:"selector"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	if a.Selector == "" {
		return nil, errors.New("selector 不能为空")
	}
	profileID, err := resolveTarget(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	sess, err := getCDPSession(profileID)
	if err != nil {
		return nil, err
	}
	expr := fmt.Sprintf(`(() => {
  const el = document.querySelector(%s);
  if (!el) return "missing";
  el.scrollIntoView({block:"center"});
  el.click();
  return "clicked";
})()`, jsString(a.Selector))

	res, err := evalString(*sess, expr)
	if err != nil {
		resetSession(profileID)
		return nil, err
	}
	if res == "missing" {
		return nil, fmt.Errorf("页面里找不到元素：%s", a.Selector)
	}
	return map[string]any{"clicked": a.Selector}, nil
}

func toolFill(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		ID       string `json:"id"`
		Selector string `json:"selector"`
		Text     string `json:"text"`
		Submit   bool   `json:"submit"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	if a.Selector == "" {
		return nil, errors.New("selector 不能为空")
	}
	profileID, err := resolveTarget(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	sess, err := getCDPSession(profileID)
	if err != nil {
		return nil, err
	}
	expr := fmt.Sprintf(`(() => {
  const el = document.querySelector(%s);
  if (!el) return "missing";
  el.focus();
  const text = %s;
  if (el.isContentEditable) {
    el.textContent = text;
  } else {
    const proto = Object.getPrototypeOf(el);
    const setter = Object.getOwnPropertyDescriptor(proto, "value");
    if (setter && setter.set) setter.set.call(el, text); else el.value = text;
  }
  el.dispatchEvent(new Event("input", {bubbles:true}));
  el.dispatchEvent(new Event("change", {bubbles:true}));
  if (%t) {
    el.dispatchEvent(new KeyboardEvent("keydown", {key:"Enter", keyCode:13, bubbles:true}));
    el.dispatchEvent(new KeyboardEvent("keyup", {key:"Enter", keyCode:13, bubbles:true}));
    if (el.form) el.form.requestSubmit ? el.form.requestSubmit() : el.form.submit();
  }
  return "filled";
})()`, jsString(a.Selector), jsString(a.Text), a.Submit)

	res, err := evalString(*sess, expr)
	if err != nil {
		resetSession(profileID)
		return nil, err
	}
	if res == "missing" {
		return nil, fmt.Errorf("页面里找不到元素：%s", a.Selector)
	}
	return map[string]any{"filled": a.Selector, "submitted": a.Submit}, nil
}

func toolScreenshot(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		ID       string `json:"id"`
		Selector string `json:"selector"`
		FullPage bool   `json:"fullPage"`
		Path     string `json:"path"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	profileID, err := resolveTarget(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	bound := bindingFrom(ctx) != nil
	sess, err := getCDPSession(profileID)
	if err != nil {
		return nil, err
	}

	ctx, cancel := cdpCtx(cdpScreenshotMax)
	defer cancel()
	params := map[string]any{"format": "png"}
	if a.FullPage && a.Selector == "" {
		params["captureBeyondViewport"] = true
	}

	var raw json.RawMessage
	if a.Selector != "" {
		// 元素截图：先量出元素的文档坐标，再用 clip。
		expr := fmt.Sprintf(`(() => {
  const el = document.querySelector(%s);
  if (!el) return "";
  el.scrollIntoView({block:"start"});
  const r = el.getBoundingClientRect();
  return JSON.stringify({x: r.left + window.scrollX, y: r.top + window.scrollY, width: r.width, height: r.height});
})()`, jsString(a.Selector))
		box, err := evalString(*sess, expr)
		if err != nil {
			resetSession(profileID)
			return nil, err
		}
		if box == "" {
			return nil, fmt.Errorf("页面里找不到元素：%s", a.Selector)
		}
		var r struct {
			X      float64 `json:"x"`
			Y      float64 `json:"y"`
			Width  float64 `json:"width"`
			Height float64 `json:"height"`
		}
		if err := json.Unmarshal([]byte(box), &r); err != nil {
			return nil, fmt.Errorf("解析元素位置失败：%v", err)
		}
		params["clip"] = map[string]any{"x": r.X, "y": r.Y, "width": r.Width, "height": r.Height, "scale": 1}
		raw, err = sess.call(ctx, "Page.captureScreenshot", params)
		if err != nil {
			resetSession(profileID)
			return nil, err
		}
	} else {
		var err error
		raw, err = sess.call(ctx, "Page.captureScreenshot", params)
		if err != nil {
			resetSession(profileID)
			return nil, err
		}
	}

	var shot struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(raw, &shot); err != nil {
		return nil, fmt.Errorf("解析截图结果失败：%v", err)
	}
	png, err := base64.StdEncoding.DecodeString(shot.Data)
	if err != nil {
		return nil, fmt.Errorf("解码截图失败：%v", err)
	}

	// 绑定 key 一律只回图片内容：既不落盘，也不接受调用方指定的 path。
	if bound {
		url, title, _ := currentPageInfo(*sess)
		return mcpImageResult{
			Data:     shot.Data,
			MimeType: "image/png",
			URL:      url,
			Title:    title,
			Bytes:    len(png),
		}, nil
	}

	path := a.Path
	if path == "" {
		dir := filepath.Join(dataDir, "screenshots")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("创建截图目录失败：%v", err)
		}
		path = filepath.Join(dir, fmt.Sprintf("%s-%d.png", safeFilePart(profileID), time.Now().UnixMilli()))
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if dir := filepath.Dir(abs); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("创建目录 %s 失败：%v", dir, err)
		}
	}
	if err := os.WriteFile(abs, png, 0644); err != nil {
		return nil, fmt.Errorf("写入截图失败：%v", err)
	}

	url, title, _ := currentPageInfo(*sess)
	return map[string]any{
		"path":  abs,
		"bytes": len(png),
		"url":   url,
		"title": title,
	}, nil
}

func safeFilePart(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "shot"
	}
	return b.String()
}

func toolEvalJS(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		ID           string `json:"id"`
		Expression   string `json:"expression"`
		AwaitPromise *bool  `json:"awaitPromise"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.Expression) == "" {
		return nil, errors.New("expression 不能为空")
	}
	profileID, err := resolveTarget(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	sess, err := getCDPSession(profileID)
	if err != nil {
		return nil, err
	}

	await := true
	if a.AwaitPromise != nil {
		await = *a.AwaitPromise
	}

	ctx, cancel := cdpCtx(cdpCallTimeout)
	defer cancel()
	raw, err := sess.call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    a.Expression,
		"returnByValue": true,
		"awaitPromise":  await,
	})
	if err != nil {
		resetSession(profileID)
		return nil, err
	}

	var out struct {
		Result struct {
			Type        string          `json:"type"`
			Value       json.RawMessage `json:"value"`
			Description string          `json:"description"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception *struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("解析 JS 结果失败：%v", err)
	}
	if out.ExceptionDetails != nil {
		msg := out.ExceptionDetails.Text
		if out.ExceptionDetails.Exception != nil && out.ExceptionDetails.Exception.Description != "" {
			msg = out.ExceptionDetails.Exception.Description
		}
		return nil, fmt.Errorf("页面脚本抛错：%s", msg)
	}

	if len(out.Result.Value) == 0 {
		return map[string]any{"type": out.Result.Type, "value": nil, "description": out.Result.Description}, nil
	}
	return map[string]any{"type": out.Result.Type, "value": out.Result.Value}, nil
}
