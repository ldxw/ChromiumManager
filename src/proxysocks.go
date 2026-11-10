package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Chromium 的 --proxy-server 不支持在 SOCKS 地址里带用户名密码：带 userinfo 的
// 地址直接报 ERR_NO_SUPPORTED_PROXIES；即使剥掉 userinfo，浏览器也只向上游发出
// 「无认证」方法（05 01 00），代理要求用户名密码时握手失败，报
// ERR_SOCKS_CONNECTION_FAILED。SOCKS 的凭据属于协议层握手，webRequest.onAuthRequired
// 只处理 HTTP 407，管不到。
//
// 这里在 127.0.0.1 起一个不带认证的 SOCKS5 中继：浏览器连中继，中继向上游用
// RFC 1929 完成用户名密码认证，再把 CONNECT 请求原样转发（域名不落地解析，交给
// 上游）。中继随实例退出关闭。

type socksRelay struct {
	ln       net.Listener
	upstream string
	user     string
	pass     string

	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
}

// startSocks5Relay 监听一个随机本地端口，返回可直接交给 --proxy-server 的中继。
// upstream 是去掉 userinfo 的 socks5://host:port。
func startSocks5Relay(upstream, user, pass string) (*socksRelay, error) {
	u, err := url.Parse(upstream)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if host == "" {
		return nil, fmt.Errorf("SOCKS5 代理地址里没有主机: %q", upstream)
	}
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, "1080")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	r := &socksRelay{ln: ln, upstream: host, user: user, pass: pass, conns: map[net.Conn]struct{}{}}
	go r.serve()
	return r, nil
}

// URL 是给 Chromium 的 --proxy-server 值。中继本身不做认证。
func (r *socksRelay) URL() string {
	return "socks5://" + r.ln.Addr().String()
}

func (r *socksRelay) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	conns := make([]net.Conn, 0, len(r.conns))
	for c := range r.conns {
		conns = append(conns, c)
	}
	r.mu.Unlock()

	r.ln.Close()
	for _, c := range conns {
		c.Close()
	}
}

func (r *socksRelay) serve() {
	for {
		c, err := r.ln.Accept()
		if err != nil {
			return
		}
		if !r.track(c) {
			c.Close()
			return
		}
		go r.handle(c)
	}
}

func (r *socksRelay) track(c net.Conn) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	r.conns[c] = struct{}{}
	return true
}

func (r *socksRelay) untrack(c net.Conn) {
	r.mu.Lock()
	delete(r.conns, c)
	r.mu.Unlock()
}

func (r *socksRelay) handle(c net.Conn) {
	defer func() {
		r.untrack(c)
		c.Close()
	}()

	c.SetDeadline(time.Now().Add(30 * time.Second))

	if err := serveSocks5Greeting(c); err != nil {
		return
	}
	atyp, addr, port, err := readSocks5Request(c)
	if err != nil {
		writeSocks5Reply(c, 0x07) // command not supported
		return
	}

	up, err := net.DialTimeout("tcp", r.upstream, 15*time.Second)
	if err != nil {
		log.Printf("[SOCKS] 连不上上游 %s：%v", r.upstream, err)
		writeSocks5Reply(c, 0x05) // connection refused
		return
	}
	defer up.Close()
	// 握手阶段给较短上限：上游不是 SOCKS（比如错填成 HTTP 代理）时不会回任何字节，
	// 浏览器那边只能干等。超时后立刻回错误码，比拖满 30 秒好排查。
	up.SetDeadline(time.Now().Add(10 * time.Second))

	if err := negotiateUpstream(up, r.user, r.pass); err != nil {
		// 凭据错、上游不支持用户名密码、上游半死不活都落到这里。
		log.Printf("[SOCKS] 上游 %s 认证失败：%v", r.upstream, err)
		writeSocks5Reply(c, 0x01) // general failure
		return
	}

	req := append([]byte{0x05, 0x01, 0x00, atyp}, addr...)
	req = append(req, port...)
	if _, err := up.Write(req); err != nil {
		writeSocks5Reply(c, 0x01)
		return
	}
	rep, err := readSocks5Reply(up)
	if err != nil {
		writeSocks5Reply(c, 0x01)
		return
	}
	if rep != 0x00 {
		writeSocks5Reply(c, rep)
		return
	}
	writeSocks5Reply(c, 0x00)

	c.SetDeadline(time.Time{})
	up.SetDeadline(time.Time{})
	relayBoth(c, up)
}

// serveSocks5Greeting 完成客户端侧「无认证」方法协商。
func serveSocks5Greeting(c net.Conn) error {
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return err
	}
	if hdr[0] != 0x05 {
		return fmt.Errorf("不是 SOCKS5 请求（ver=%d）", hdr[0])
	}
	methods := make([]byte, int(hdr[1]))
	if _, err := io.ReadFull(c, methods); err != nil {
		return err
	}
	for _, m := range methods {
		if m == 0x00 {
			_, err := c.Write([]byte{0x05, 0x00})
			return err
		}
	}
	c.Write([]byte{0x05, 0xff})
	return errors.New("客户端不支持无认证方法")
}

// readSocks5Request 读 CONNECT 请求，返回可原样转发给上游的 ATYP/ADDR/PORT 三段。
func readSocks5Request(c net.Conn) (atyp byte, addr, port []byte, err error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return 0, nil, nil, err
	}
	if hdr[0] != 0x05 || hdr[1] != 0x01 {
		return 0, nil, nil, fmt.Errorf("只支持 SOCKS5 CONNECT（cmd=%d）", hdr[1])
	}
	atyp = hdr[3]
	switch atyp {
	case 0x01:
		addr = make([]byte, 4)
		if _, err = io.ReadFull(c, addr); err != nil {
			return 0, nil, nil, err
		}
	case 0x03:
		l := make([]byte, 1)
		if _, err = io.ReadFull(c, l); err != nil {
			return 0, nil, nil, err
		}
		addr = make([]byte, 1+int(l[0]))
		addr[0] = l[0]
		if _, err = io.ReadFull(c, addr[1:]); err != nil {
			return 0, nil, nil, err
		}
	case 0x04:
		addr = make([]byte, 16)
		if _, err = io.ReadFull(c, addr); err != nil {
			return 0, nil, nil, err
		}
	default:
		return 0, nil, nil, fmt.Errorf("不支持的地址类型 %d", atyp)
	}
	port = make([]byte, 2)
	if _, err = io.ReadFull(c, port); err != nil {
		return 0, nil, nil, err
	}
	return atyp, addr, port, nil
}

// negotiateUpstream 与上游协商认证方法：同时提供「无认证」和「用户名密码」，
// 上游选哪个就走哪个。用户填了凭据但代理其实不要求认证时，不至于直接失败。
func negotiateUpstream(c net.Conn, user, pass string) error {
	if len(user) > 255 || len(pass) > 255 {
		return errors.New("SOCKS5 用户名或密码超过 255 字节")
	}
	if _, err := c.Write([]byte{0x05, 0x02, 0x00, 0x02}); err != nil {
		return err
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(c, buf); err != nil {
		return err
	}
	if buf[0] != 0x05 {
		return fmt.Errorf("上游不是 SOCKS5（ver=%d）", buf[0])
	}
	switch buf[1] {
	case 0x00:
		return nil
	case 0x02:
		// RFC 1929 子协商：VER USERLEN USER PASSLEN PASS，VER 固定 0x01。
	default:
		return fmt.Errorf("上游不接受无认证也不接受用户名密码（method=%d）", buf[1])
	}

	req := []byte{0x01, byte(len(user))}
	req = append(req, user...)
	req = append(req, byte(len(pass)))
	req = append(req, pass...)
	if _, err := c.Write(req); err != nil {
		return err
	}
	if _, err := io.ReadFull(c, buf); err != nil {
		return err
	}
	if buf[1] != 0x00 {
		return errors.New("上游拒绝 SOCKS5 用户名密码")
	}
	return nil
}

// readSocks5Reply 读上游对 CONNECT 的回应，返回 REP 字节。
func readSocks5Reply(c net.Conn) (byte, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return 0, err
	}
	var n int
	switch hdr[3] {
	case 0x01:
		n = 4
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(c, l); err != nil {
			return 0, err
		}
		n = int(l[0])
	case 0x04:
		n = 16
	default:
		n = 0
	}
	if n > 0 {
		if _, err := io.CopyN(io.Discard, c, int64(n)); err != nil {
			return 0, err
		}
	}
	if _, err := io.CopyN(io.Discard, c, 2); err != nil {
		return 0, err
	}
	return hdr[1], nil
}

func writeSocks5Reply(c net.Conn, rep byte) {
	c.Write([]byte{0x05, rep, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
}

func relayBoth(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(a, b)
		a.Close()
		b.Close()
		done <- struct{}{}
	}()
	io.Copy(b, a)
	a.Close()
	b.Close()
	<-done
}

// proxyScheme 返回代理地址的 scheme，缺省按 http 处理（与历史行为一致）。
func proxyScheme(raw string) string {
	if i := strings.Index(raw, "://"); i > 0 {
		return strings.ToLower(raw[:i])
	}
	return "http"
}

// buildProxyLaunchArgs 把代理地址拆成 Chromium 能接受的形式，返回 --proxy-server
// 的取值、需要额外追加的启动参数，以及随实例退出调用的 cleanup。
// 无凭据时代码路径与以前完全一致：原样返回、无额外参数。
func buildProxyLaunchArgs(raw string, seed int32) (proxyArg string, extra []string, cleanup func(), err error) {
	raw = strings.TrimSpace(raw)
	clean, user, pass, hasAuth := splitProxyCredentials(raw)
	if !hasAuth {
		return raw, nil, nil, nil
	}

	switch proxyScheme(raw) {
	case "socks5", "socks5h":
		relay, rerr := startSocks5Relay(clean, user, pass)
		if rerr != nil {
			return raw, nil, nil, rerr
		}
		return relay.URL(), nil, relay.Close, nil
	case "socks4", "socks4a":
		// SOCKS4 只有 userid、没有密码，且 Chromium 是否解析 userinfo 未验证；
		// 保持原样传入，行为与改动前一致。
		return raw, nil, nil, nil
	default:
		extDir, eerr := ensureProxyAuthExtension(seed, user, pass)
		if eerr != nil {
			return clean, nil, nil, eerr
		}
		// 只加 --load-extension：实测已经足够生效，再加
		// --disable-extensions-except 会把用户自己装的扩展一并禁掉。
		return clean, []string{"--load-extension=" + extDir}, nil, nil
	}
}
