package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// 最小 CDP 客户端。协议报文自己拼，帧层交给 gorilla/websocket（单包、无传递依赖）。
//
// 连接挂在 browser 目标上，用 Target.attachToTarget(flatten) 拿到 sessionId，
// 之后所有页面命令都带这个 sessionId，一条连接就能复用而不必每 tab 重连。

// cdpCallTimeout 是单条 CDP 命令的默认超时。截图会单独放宽。
const cdpCallTimeout = 20 * time.Second

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *cdpError) Error() string {
	return fmt.Sprintf("CDP 错误 %d：%s", e.Code, e.Message)
}

type cdpMessage struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *cdpError       `json:"error,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

type cdpResult struct {
	result json.RawMessage
	err    error
}

type cdpClient struct {
	conn      *websocket.Conn
	writeMu   sync.Mutex
	mu        sync.Mutex
	pending   map[int64]chan cdpResult
	nextID    atomic.Int64
	closed    chan struct{}
	closeOnce sync.Once
}

// dialCDP 连接一个 DevTools websocket 地址（ws://127.0.0.1:PORT/devtools/browser/...）。
func dialCDP(ctx context.Context, wsURL string) (*cdpClient, error) {
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	conn, resp, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("连接 CDP 失败（HTTP %d）：%v", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("连接 CDP 失败：%v", err)
	}
	// 截图以 base64 返回，整页截图可能几十 MB，默认 32KB 的读上限不够。
	conn.SetReadLimit(128 << 20)

	c := &cdpClient{
		conn:    conn,
		pending: map[int64]chan cdpResult{},
		closed:  make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

func (c *cdpClient) readLoop() {
	defer c.shutdown()
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var msg cdpMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.ID == 0 {
			// 事件（Page.loadEventFired、Target.detachedFromTarget 等）当前不使用。
			continue
		}
		c.mu.Lock()
		ch := c.pending[msg.ID]
		delete(c.pending, msg.ID)
		c.mu.Unlock()
		if ch == nil {
			continue
		}
		if msg.Error != nil {
			ch <- cdpResult{err: msg.Error}
		} else {
			ch <- cdpResult{result: msg.Result}
		}
	}
}

// shutdown 唤醒所有在等的调用方。可重复调用。
func (c *cdpClient) shutdown() {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.mu.Lock()
		pending := c.pending
		c.pending = map[int64]chan cdpResult{}
		c.mu.Unlock()
		for _, ch := range pending {
			ch <- cdpResult{err: errors.New("CDP 连接已断开")}
		}
	})
}

func (c *cdpClient) Close() error {
	c.shutdown()
	return c.conn.Close()
}

// send 发一条 CDP 命令并等待同 id 的响应。sessionID 为空表示 browser 级命令。
func (c *cdpClient) send(ctx context.Context, sessionID, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	msg := map[string]any{"id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if sessionID != "" {
		msg["sessionId"] = sessionID
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("序列化 CDP 命令失败：%v", err)
	}

	ch := make(chan cdpResult, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	c.writeMu.Lock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err = c.conn.WriteMessage(websocket.TextMessage, payload)
	c.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("发送 CDP 命令 %s 失败：%v", method, err)
	}

	select {
	case r := <-ch:
		return r.result, r.err
	case <-ctx.Done():
		return nil, fmt.Errorf("CDP 命令 %s 超时：%v", method, ctx.Err())
	case <-c.closed:
		return nil, errors.New("CDP 连接已断开")
	}
}

// attachPage 找到一个 page 目标并附加 flatten session，返回 (sessionID, targetID)。
func (c *cdpClient) attachPage(ctx context.Context) (string, string, error) {
	raw, err := c.send(ctx, "", "Target.getTargets", nil)
	if err != nil {
		return "", "", err
	}
	var targets struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
			URL      string `json:"url"`
		} `json:"targetInfos"`
	}
	if err := json.Unmarshal(raw, &targets); err != nil {
		return "", "", fmt.Errorf("解析 Target.getTargets 失败：%v", err)
	}

	targetID := ""
	for _, t := range targets.TargetInfos {
		if t.Type == "page" {
			targetID = t.TargetID
			break
		}
	}

	if targetID == "" {
		raw, err = c.send(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"})
		if err != nil {
			return "", "", fmt.Errorf("创建标签页失败：%v", err)
		}
		var created struct {
			TargetID string `json:"targetId"`
		}
		if err := json.Unmarshal(raw, &created); err != nil {
			return "", "", fmt.Errorf("解析 Target.createTarget 失败：%v", err)
		}
		targetID = created.TargetID
	}

	raw, err = c.send(ctx, "", "Target.attachToTarget", map[string]any{
		"targetId": targetID,
		"flatten":  true,
	})
	if err != nil {
		return "", "", fmt.Errorf("附加标签页失败：%v", err)
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(raw, &attached); err != nil {
		return "", "", fmt.Errorf("解析 Target.attachToTarget 失败：%v", err)
	}
	if attached.SessionID == "" {
		return "", "", errors.New("附加标签页后没有拿到 sessionId")
	}
	return attached.SessionID, targetID, nil
}
