package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

func getProfiles(w http.ResponseWriter, r *http.Request) {
	groupIDStr := r.URL.Query().Get("groupId")
	proxyIDStr := r.URL.Query().Get("proxyId")
	keyword := r.URL.Query().Get("keyword")
	page, pageSize := 1, 10
	fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
	fmt.Sscanf(r.URL.Query().Get("pageSize"), "%d", &pageSize)

	// Clamp pagination parameters to reasonable bounds
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 10
	}

	query := `SELECT p.id, p.name, p.group_id, p.sort, p.proxy, p.args, p.fingerprint, p.notes, p.created_at, p.updated_at
		FROM profiles p LEFT JOIN proxies px ON p.proxy = px.id WHERE 1=1`
	var args []interface{}
	if rawGroupID := decodeID(groupIDStr); rawGroupID > 0 {
		query += " AND p.group_id = ?"
		args = append(args, rawGroupID)
	}
	if rawProxyID := decodeID(proxyIDStr); rawProxyID > 0 {
		query += " AND p.proxy = ?"
		args = append(args, rawProxyID)
	}
	if keyword != "" {
		query += " AND (p.name LIKE ? OR px.ip LIKE ? OR px.lang LIKE ? OR px.timezone LIKE ?)"
		k := "%" + keyword + "%"
		args = append(args, k, k, k, k)
	}

	var total int
	if err := db.QueryRow("SELECT COUNT(*) FROM ("+query+")", args...).Scan(&total); err != nil {
		writeJSON(w, Response[any]{Code: 500, Message: "count query failed: " + err.Error()})
		return
	}

	query += " ORDER BY p.sort DESC, p.created_at DESC LIMIT ? OFFSET ?"
	args = append(args, pageSize, (page-1)*pageSize)

	rows, err := db.Query(query, args...)
	if err != nil {
		writeJSON(w, Response[any]{Code: 500, Message: err.Error()})
		return
	}
	defer rows.Close()

	var list []Profile
	for rows.Next() {
		var p Profile
		var rawID, rawGroupID, rawProxy int64
		if err := rows.Scan(&rawID, &p.Name, &rawGroupID, &p.Sort, &rawProxy, &p.Args, &p.Fingerprint, &p.Notes, &p.CreatedAt, &p.UpdatedAt); err == nil {
			p.ID = encodeID(rawID)
			if rawGroupID > 0 {
				p.GroupID = encodeID(rawGroupID)
			}
			if rawProxy > 0 {
				p.Proxy = encodeID(rawProxy)
			}
			list = append(list, p)
		}
	}
	writeJSON(w, Response[any]{Code: 200, Message: "success", Data: map[string]interface{}{
		"list":  list,
		"total": total,
	}})
}

func getProfile(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	rawID := decodeID(idStr)
	if rawID <= 0 {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}
	var p Profile
	var rawGroupID, rawProxy int64
	err := db.QueryRow("SELECT id, name, group_id, sort, proxy, args, fingerprint, notes, created_at, updated_at FROM profiles WHERE id=?", rawID).
		Scan(&rawID, &p.Name, &rawGroupID, &p.Sort, &rawProxy, &p.Args, &p.Fingerprint, &p.Notes, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		writeJSON(w, Response[any]{Code: 404, Message: "profile not found"})
		return
	}
	p.ID = encodeID(rawID)
	if rawGroupID > 0 {
		p.GroupID = encodeID(rawGroupID)
	}
	if rawProxy > 0 {
		p.Proxy = encodeID(rawProxy)
	}
	writeJSON(w, Response[any]{Code: 200, Message: "success", Data: p})
}

func addProfile(w http.ResponseWriter, r *http.Request) {
	var p Profile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}
	p.CreatedAt = time.Now().UnixMilli()
	p.UpdatedAt = p.CreatedAt

	enrichFingerprint(&p, true)

	rawGroupID := decodeID(p.GroupID)
	rawProxy := decodeID(p.Proxy)

	res, err := db.Exec("INSERT INTO profiles (name, group_id, sort, proxy, args, fingerprint, notes, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		p.Name, rawGroupID, p.Sort, rawProxy, p.Args, p.Fingerprint, p.Notes, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "UNIQUE") {
			msg = "配置名称已存在！"
		}
		writeJSON(w, Response[any]{Code: 500, Message: msg})
		return
	}
	rowID, _ := res.LastInsertId()
	p.ID = encodeID(rowID)
	writeJSON(w, Response[any]{Code: 200, Message: "success", Data: p})
}

func updateProfile(w http.ResponseWriter, r *http.Request) {
	var p Profile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}
	p.UpdatedAt = time.Now().UnixMilli()

	enrichFingerprint(&p, false)

	rawID := decodeID(p.ID)
	rawGroupID := decodeID(p.GroupID)
	rawProxy := decodeID(p.Proxy)

	_, err := db.Exec("UPDATE profiles SET name=?, group_id=?, sort=?, proxy=?, args=?, fingerprint=?, notes=?, updated_at=? WHERE id=?",
		p.Name, rawGroupID, p.Sort, rawProxy, p.Args, p.Fingerprint, p.Notes, p.UpdatedAt, rawID)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "UNIQUE") {
			msg = "配置名称已存在！"
		}
		writeJSON(w, Response[any]{Code: 500, Message: msg})
		return
	}
	writeJSON(w, Response[any]{Code: 200, Message: "success", Data: p})
}

func deleteProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}
	if err := deleteProfileByID(req.ID); err != nil {
		code := 500
		if errors.Is(err, errProfileNotFound) {
			code = 404
		}
		writeJSON(w, Response[any]{Code: code, Message: err.Error()})
		return
	}
	writeJSON(w, Response[any]{Code: 200, Message: "success"})
}

// deleteProfileByID removes a profile row and its user-data-dir from disk.
func deleteProfileByID(idStr string) error {
	rawID := decodeID(idStr)

	// Query fingerprint seed before deletion to locate user data directory
	var fp FingerprintConfig
	if err := db.QueryRow("SELECT fingerprint FROM profiles WHERE id=?", rawID).Scan(&fp); err != nil {
		return errProfileNotFound
	}

	if _, err := db.Exec("DELETE FROM profiles WHERE id=?", rawID); err != nil {
		return err
	}

	// Clean up profile user data directory from disk
	if fp.Seed != 0 {
		userDataDir := filepath.Join(dataDir, "profiles", encodeID(int64(fp.Seed)))
		if err := os.RemoveAll(userDataDir); err != nil {
			log.Printf("[Profile] failed to remove user data dir %s: %v", userDataDir, err)
		} else {
			log.Printf("[Profile] removed user data dir: %s", userDataDir)
		}
	}
	return nil
}

func showProfile(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	runningMu.Lock()
	rp, ok := runningProfiles[id]
	runningMu.Unlock()

	if !ok {
		writeJSON(w, Response[any]{Code: 404, Message: "not running"})
		return
	}

	writeJSON(w, Response[any]{Code: 200, Message: "success"})

	if rp.cmd.Process != nil {
		pid := uint32(rp.cmd.Process.Pid)
		go func() {
			time.Sleep(200 * time.Millisecond)
			bringWindowToFront(pid)
		}()
	}
}

func launchProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}
	if err := launchProfileByID(req.ID); err != nil {
		code := 500
		if errors.Is(err, errProfileNotFound) {
			code = 404
		}
		writeJSON(w, Response[any]{Code: code, Message: err.Error()})
		return
	}
	writeJSON(w, Response[any]{Code: 200, Message: "success"})
}

// launchProfileByID 启动一个配置的浏览器实例，返回错误而不是写 HTTP 响应，
// 供 HTTP handler 与 MCP 工具共用。
func launchProfileByID(idStr string) error {
	rawID := decodeID(idStr)

	var p Profile
	var rawProxy int64
	var cookieStr string
	err := db.QueryRow("SELECT id, name, proxy, args, fingerprint, cookie FROM profiles WHERE id=?", rawID).
		Scan(&rawID, &p.Name, &rawProxy, &p.Args, &p.Fingerprint, &cookieStr)
	if err != nil {
		return fmt.Errorf("%w: %v", errProfileNotFound, err)
	}
	p.ID = idStr

	userDataDir := filepath.Join(dataDir, "profiles", encodeID(int64(p.Fingerprint.Seed)))
	absUserDataDir, err := filepath.Abs(userDataDir)
	if err != nil {
		absUserDataDir = userDataDir
	}
	os.MkdirAll(absUserDataDir, 0755)

	singletonFiles, _ := filepath.Glob(filepath.Join(absUserDataDir, "Singleton*"))
	for _, f := range singletonFiles {
		os.Remove(f)
	}

	cookiePath := filepath.Join(absUserDataDir, "Default", "Cookies")

	if _, err := os.Stat(cookiePath); os.IsNotExist(err) && cookieStr != "" {
		var cookies []Cookie
		if json.Unmarshal([]byte(cookieStr), &cookies) == nil && len(cookies) > 0 {
			writeCookiesToFile(cookiePath, cookies)
		}
	}

	chromePath := findBrowserPath()
	if chromePath == "" {
		return errors.New("Chromium not found: expected bin/chromium next to the program")
	}
	launchArgs := []string{
		"--user-data-dir=" + absUserDataDir,
		"--profile-name=" + p.Name,
		"--no-first-run",
		"--no-default-browser-check",
		"--password-store=basic",
		// Port 0 lets Chromium pick a free port and publish it in
		// DevToolsActivePort, which watchDevToolsPort polls.
		"--remote-debugging-port=0",
	}

	proxy := getProxyInfo(rawProxy)
	if proxy.URL != "" {
		launchArgs = append(launchArgs, "--proxy-server="+proxy.URL)
	}

	fp := p.Fingerprint
	if fp.RandomFingerprint && fp.Seed != 0 {
		launchArgs = append(launchArgs, fmt.Sprintf("--fingerprint=%d", fp.Seed))
	}
	if fp.Platform != "" {
		launchArgs = append(launchArgs, "--fingerprint-platform="+fp.Platform)
	}
	if fp.Brand != "" {
		launchArgs = append(launchArgs, "--fingerprint-brand="+fp.Brand)
	}
	if fp.HardwareConcurrency != "" {
		launchArgs = append(launchArgs, "--fingerprint-hardware-concurrency="+fp.HardwareConcurrency)
	}
	if fp.DeviceMemory != "" {
		launchArgs = append(launchArgs, "--fingerprint-device-memory="+fp.DeviceMemory)
	}
	if fp.Screen != "" {
		launchArgs = append(launchArgs, "--fingerprint-screen="+fp.Screen)
	}
	for _, feature := range fp.DisableFeatures {
		switch feature {
		case "webrtc":
			launchArgs = append(launchArgs, "--force-webrtc-ip-handling-policy")
			launchArgs = append(launchArgs, "--webrtc-ip-handling-policy=disable_non_proxied_udp")
		}
	}

	effLang := fp.Lang
	if fp.ProxyLang && proxy.Lang != "" {
		effLang = proxy.Lang
	}
	effTimezone := fp.Timezone
	if fp.ProxyTimezone && proxy.Timezone != "" {
		effTimezone = proxy.Timezone
	}
	effLocation := fp.Location
	if fp.ProxyLocation && proxy.Location != "" {
		effLocation = proxy.Location
	}
	if effLang != "" {
		launchArgs = append(launchArgs, "--lang="+effLang)
		launchArgs = append(launchArgs, "--accept-lang="+effLang)
	}
	if effTimezone != "" {
		launchArgs = append(launchArgs, "--fingerprint-timezone="+effTimezone)
	}
	if effLocation != "" {
		launchArgs = append(launchArgs, "--fingerprint-location="+effLocation)
	}
	if len(fp.DisableFingerprint) > 0 {
		launchArgs = append(launchArgs, "--disable-fingerprint="+strings.Join(fp.DisableFingerprint, ","))
	}

	if p.Args != "" {
		launchArgs = append(launchArgs, splitArgs(p.Args)...)
	}

	cmd := exec.Command(chromePath, launchArgs...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("Failed to launch: %w", err)
	}

	done := make(chan struct{})
	rp := &runningProfile{cmd: cmd, done: done, profileID: idStr, cookiePath: cookiePath}
	runningMu.Lock()
	runningProfiles[p.ID] = rp
	runningMu.Unlock()

	broadcastRunning()

	// Chromium writes the real port and ws path to DevToolsActivePort once CDP is
	// ready; poll it in the background so list_running can report a devtools URL.
	go watchDevToolsPort(rp, absUserDataDir)

	go func() {
		cmd.Wait()
		close(done)
		log.Printf("[Chrome] profile %s (%s) exited", p.Name, p.ID)

		// 进程没了，缓存的 CDP 连接也没用了；不关会一直挂在已死的 websocket 上。
		dropBrowserConn(p.ID)

		if cookies := readCookiesFromFile(cookiePath); len(cookies) > 0 {
			if data, err := json.Marshal(cookies); err == nil {
				db.Exec("UPDATE profiles SET cookie=? WHERE id=?", string(data), rawID)
				log.Printf("[Chrome] exported %d cookies for profile %s", len(cookies), p.ID)
			}
		}

		runningMu.Lock()
		delete(runningProfiles, p.ID)
		runningMu.Unlock()
		broadcastRunning()
	}()

	return nil
}

func stopProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}
	if err := stopProfileByID(req.ID); err != nil {
		code := 500
		if errors.Is(err, errNotRunning) {
			code = 404
		}
		writeJSON(w, Response[any]{Code: code, Message: err.Error()})
		return
	}
	writeJSON(w, Response[any]{Code: 200, Message: "success"})
}

// stopProfileByID 停止一个配置实例：非 Windows 先发 SIGTERM，5 秒后仍在则 Kill。
func stopProfileByID(id string) error {
	runningMu.Lock()
	rp, ok := runningProfiles[id]
	runningMu.Unlock()

	if !ok {
		return errNotRunning
	}

	if rp.cmd.Process != nil {
		if runtime.GOOS == "windows" {
			closeWindowsByPID(uint32(rp.cmd.Process.Pid))
		} else {
			rp.cmd.Process.Signal(syscall.SIGTERM)
		}
		go func() {
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			select {
			case <-rp.done:
			case <-timer.C:
				rp.cmd.Process.Kill()
			}
		}()
	}

	return nil
}
