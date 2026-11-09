package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// 工具集分三档：
//   - 只读（永远注册）
//   - 写（永远注册，可撤销）
//   - 破坏性（MCP_ENABLE_DESTRUCTIVE=1 才注册）

var (
	errProfileNotFound  = errors.New("profile not found")
	errNotRunning       = errors.New("profile is not running")
	errGroupHasProfiles = errors.New("group still has profiles")
)

// ------------------------------------------------------------------ 参数工具

func decodeArgs(args json.RawMessage, dst any) error {
	if len(args) == 0 {
		return nil
	}
	if err := json.Unmarshal(args, dst); err != nil {
		return fmt.Errorf("参数解析失败：%v", err)
	}
	return nil
}

const (
	defaultListLimit = 20
	maxListLimit     = 50
)

func clampListLimit(n int) int {
	if n <= 0 {
		return defaultListLimit
	}
	if n > maxListLimit {
		return maxListLimit
	}
	return n
}

// ------------------------------------------------------------------ 只读工具

func registerReadOnlyTools(s *mcpServer) {
	s.tools["list_profiles"] = mcpTool{
		Name:        "list_profiles",
		Description: "列出指纹浏览器配置（分页）。返回精简字段：_id、name、groupId、proxy、running、notes。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"groupId":  map[string]any{"type": "string", "description": "按分组 _id 过滤"},
				"keyword":  map[string]any{"type": "string", "description": "按名称或代理关键字搜索"},
				"page":     map[string]any{"type": "integer", "description": "页码，从 1 开始，默认 1"},
				"pageSize": map[string]any{"type": "integer", "description": fmt.Sprintf("每页数量，默认 %d，上限 %d", defaultListLimit, maxListLimit)},
			},
		},
		Handler: toolListProfiles,
	}

	s.tools["get_profile"] = mcpTool{
		Name:        "get_profile",
		Description: "按 _id 读取单个配置的完整内容，含 fingerprint 指纹参数与自定义启动参数。",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"id": map[string]any{"type": "string", "description": "配置 _id"}},
			"required":   []string{"id"},
		},
		Handler: toolGetProfile,
	}

	s.tools["list_running"] = mcpTool{
		Name:        "list_running",
		Description: "列出当前正在运行的浏览器实例，含 pid 与对应配置名。",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler:     toolListRunning,
	}

	s.tools["list_groups"] = mcpTool{
		Name:        "list_groups",
		Description: "列出全部分组。",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler:     toolListGroups,
	}

	s.tools["list_proxies"] = mcpTool{
		Name:        "list_proxies",
		Description: "列出代理，含 url、ip、lang、timezone、location。默认最多返回 50 条。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"limit": map[string]any{"type": "integer", "description": fmt.Sprintf("返回条数，默认 %d，上限 %d", defaultListLimit, maxListLimit)},
			},
		},
		Handler: toolListProxies,
	}
}

type profileBrief struct {
	ID        string `json:"_id"`
	Name      string `json:"name"`
	GroupID   string `json:"groupId,omitempty"`
	Proxy     string `json:"proxy,omitempty"`
	Running   bool   `json:"running"`
	Notes     string `json:"notes,omitempty"`
	Args      string `json:"args,omitempty"`
	Sort      int    `json:"sort"`
	UpdatedAt int64  `json:"updatedAt,omitempty"`
}

func isRunning(id string) bool {
	runningMu.Lock()
	_, ok := runningProfiles[id]
	runningMu.Unlock()
	return ok
}

func toolListProfiles(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		GroupID  string `json:"groupId"`
		Keyword  string `json:"keyword"`
		Page     int    `json:"page"`
		PageSize int    `json:"pageSize"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	if a.Page < 1 {
		a.Page = 1
	}
	if a.PageSize <= 0 || a.PageSize > maxListLimit {
		a.PageSize = defaultListLimit
	}

	bound, err := bindingProfile(ctx)
	if err != nil {
		return nil, err
	}

	query := "SELECT p.id, p.name, p.group_id, p.sort, p.proxy, p.args, p.notes, p.updated_at FROM profiles p LEFT JOIN proxies px ON p.proxy = px.id WHERE 1=1"
	var qargs []any
	if bound != nil {
		// 绑定的 key 只能看到自己那一个配置，groupId / keyword 过滤不再起作用。
		query += " AND p.id = ?"
		qargs = append(qargs, decodeID(bound.profileID))
	} else if raw := decodeID(a.GroupID); raw > 0 {
		query += " AND p.group_id = ?"
		qargs = append(qargs, raw)
	}
	if a.Keyword != "" {
		query += " AND (p.name LIKE ? OR px.ip LIKE ? OR px.lang LIKE ? OR px.timezone LIKE ?)"
		k := "%" + a.Keyword + "%"
		qargs = append(qargs, k, k, k, k)
	}

	var total int
	if err := db.QueryRow("SELECT COUNT(*) FROM ("+query+")", qargs...).Scan(&total); err != nil {
		return nil, fmt.Errorf("统计失败：%v", err)
	}

	query += " ORDER BY p.sort DESC, p.created_at DESC LIMIT ? OFFSET ?"
	qargs = append(qargs, a.PageSize, (a.Page-1)*a.PageSize)

	rows, err := db.Query(query, qargs...)
	if err != nil {
		return nil, fmt.Errorf("查询失败：%v", err)
	}
	defer rows.Close()

	list := make([]profileBrief, 0, a.PageSize)
	for rows.Next() {
		var b profileBrief
		var rawID, rawGroup, rawProxy int64
		if err := rows.Scan(&rawID, &b.Name, &rawGroup, &b.Sort, &rawProxy, &b.Args, &b.Notes, &b.UpdatedAt); err != nil {
			return nil, fmt.Errorf("读取行失败：%v", err)
		}
		b.ID = encodeID(rawID)
		if rawGroup > 0 {
			b.GroupID = encodeID(rawGroup)
		}
		if rawProxy > 0 {
			b.Proxy = encodeID(rawProxy)
		}
		list = append(list, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历失败：%v", err)
	}
	// rows 已读完，这里再查运行态是安全的（连接池只有 1 条）。
	for i := range list {
		list[i].Running = isRunning(list[i].ID)
	}

	out := map[string]any{"list": list, "total": total, "page": a.Page, "pageSize": a.PageSize}
	if a.Page*a.PageSize < total {
		out["hint"] = fmt.Sprintf("还有 %d 条未返回，用 page=%d 继续", total-a.Page*a.PageSize, a.Page+1)
	}
	return out, nil
}

func loadProfile(rawID int64) (Profile, error) {
	var p Profile
	var rawGroup, rawProxy int64
	err := db.QueryRow(`SELECT id, name, group_id, sort, proxy, args, fingerprint, notes, created_at, updated_at
		FROM profiles WHERE id=?`, rawID).
		Scan(&rawID, &p.Name, &rawGroup, &p.Sort, &rawProxy, &p.Args, &p.Fingerprint, &p.Notes, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return p, errProfileNotFound
	}
	p.ID = encodeID(rawID)
	if rawGroup > 0 {
		p.GroupID = encodeID(rawGroup)
	}
	if rawProxy > 0 {
		p.Proxy = encodeID(rawProxy)
	}
	return p, nil
}

func toolGetProfile(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		ID string `json:"id"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	id, err := resolveTarget(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	raw := decodeID(id)
	if raw <= 0 {
		return nil, errors.New("id 无效")
	}
	p, err := loadProfile(raw)
	if err != nil {
		return nil, err
	}
	return map[string]any{"profile": p, "running": isRunning(p.ID)}, nil
}

func toolListRunning(ctx context.Context, args json.RawMessage) (any, error) {
	type entry struct {
		ID       string `json:"_id"`
		Name     string `json:"name"`
		PID      int    `json:"pid"`
		DevTools string `json:"devtoolsURL,omitempty"`
	}

	bound, err := bindingProfile(ctx)
	if err != nil {
		return nil, err
	}

	// 绑定 key 只回自己那一条，且不带 DevTools 地址：那是可直接接管浏览器的
	// WebSocket 句柄，同容器内的其他使用方拿到就能绕过本 key 的隔离。
	if bound != nil {
		runningMu.Lock()
		rp := runningProfiles[bound.profileID]
		runningMu.Unlock()
		if rp == nil {
			return map[string]any{"running": []entry{}, "count": 0}, nil
		}
		e := entry{ID: rp.profileID, Name: bound.name}
		if rp.cmd != nil && rp.cmd.Process != nil {
			e.PID = rp.cmd.Process.Pid
		}
		return map[string]any{"running": []entry{e}, "count": 1}, nil
	}

	runningMu.Lock()
	snapshot := make([]*runningProfile, 0, len(runningProfiles))
	for _, rp := range runningProfiles {
		snapshot = append(snapshot, rp)
	}
	runningMu.Unlock()

	out := make([]entry, 0, len(snapshot))
	for _, rp := range snapshot {
		e := entry{ID: rp.profileID, DevTools: rp.getDevToolsURL()}
		if rp.cmd != nil && rp.cmd.Process != nil {
			e.PID = rp.cmd.Process.Pid
		}
		if raw := decodeID(rp.profileID); raw > 0 {
			if p, err := loadProfile(raw); err == nil {
				e.Name = p.Name
			}
		}
		out = append(out, e)
	}
	return map[string]any{"running": out, "count": len(out)}, nil
}

func toolListGroups(ctx context.Context, args json.RawMessage) (any, error) {
	bound, err := bindingProfile(ctx)
	if err != nil {
		return nil, err
	}
	if bound != nil {
		// 绑定 key 只需要知道自己的配置属于哪个分组，返回全部组名等于泄露运维结构。
		p, err := loadProfile(decodeID(bound.profileID))
		if err != nil {
			return nil, errors.New(errBindingStale)
		}
		var g Group
		var rawID int64
		if raw := decodeID(p.GroupID); raw > 0 {
			if err := db.QueryRow("SELECT id, name, sort, created_at, updated_at FROM groups WHERE id=?", raw).
				Scan(&rawID, &g.Name, &g.Sort, &g.CreatedAt, &g.UpdatedAt); err == nil {
				g.ID = encodeID(rawID)
			}
		}
		list := make([]Group, 0, 1)
		if g.ID != "" {
			list = append(list, g)
		}
		return map[string]any{"list": list, "total": len(list)}, nil
	}

	rows, err := db.Query("SELECT id, name, sort, created_at, updated_at FROM groups ORDER BY sort DESC, id ASC")
	if err != nil {
		return nil, fmt.Errorf("查询分组失败：%v", err)
	}
	defer rows.Close()

	list := make([]Group, 0, 8)
	for rows.Next() {
		var g Group
		var rawID int64
		if err := rows.Scan(&rawID, &g.Name, &g.Sort, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("读取分组失败：%v", err)
		}
		g.ID = encodeID(rawID)
		list = append(list, g)
	}
	return map[string]any{"list": list, "total": len(list)}, rows.Err()
}

// proxyPublic 是给绑定 key 看的代理视图：不含 url（里面有用户名密码）。
type proxyPublic struct {
	ID       string `json:"_id"`
	Name     string `json:"name"`
	IP       string `json:"ip,omitempty"`
	Lang     string `json:"lang,omitempty"`
	Timezone string `json:"timezone,omitempty"`
	Location string `json:"location,omitempty"`
}

func toolListProxies(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		Limit int `json:"limit"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	limit := clampListLimit(a.Limit)

	bound, err := bindingProfile(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := db.Query("SELECT id, name, url, ip, lang, timezone, location, created_at, updated_at FROM proxies ORDER BY id ASC LIMIT ?", limit)
	if err != nil {
		return nil, fmt.Errorf("查询代理失败：%v", err)
	}
	defer rows.Close()

	if bound != nil {
		list := make([]proxyPublic, 0, limit)
		for rows.Next() {
			var p Proxy
			var rawID int64
			if err := rows.Scan(&rawID, &p.Name, &p.URL, &p.IP, &p.Lang, &p.Timezone, &p.Location, &p.CreatedAt, &p.UpdatedAt); err != nil {
				return nil, fmt.Errorf("读取代理失败：%v", err)
			}
			list = append(list, proxyPublic{
				ID: encodeID(rawID), Name: p.Name, IP: p.IP,
				Lang: p.Lang, Timezone: p.Timezone, Location: p.Location,
			})
		}
		return map[string]any{"list": list, "returned": len(list), "limit": limit, "note": "已隐藏代理连接串"}, rows.Err()
	}

	list := make([]Proxy, 0, limit)
	for rows.Next() {
		var p Proxy
		var rawID int64
		if err := rows.Scan(&rawID, &p.Name, &p.URL, &p.IP, &p.Lang, &p.Timezone, &p.Location, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("读取代理失败：%v", err)
		}
		p.ID = encodeID(rawID)
		list = append(list, p)
	}
	return map[string]any{"list": list, "returned": len(list), "limit": limit}, rows.Err()
}

// ------------------------------------------------------------------ 写工具

func registerWriteTools(s *mcpServer) {
	s.tools["launch_profile"] = mcpTool{
		Name:        "launch_profile",
		Description: "启动指定配置的浏览器实例。已运行则返回提示。",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"id": map[string]any{"type": "string", "description": "配置 _id"}},
			"required":   []string{"id"},
		},
		Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
			var a struct {
				ID string `json:"id"`
			}
			if err := decodeArgs(args, &a); err != nil {
				return nil, err
			}
			id, err := resolveTarget(ctx, a.ID)
			if err != nil {
				return nil, err
			}
			if isRunning(id) {
				return nil, fmt.Errorf("配置 %s 已在运行", id)
			}
			if err := launchProfileByID(id); err != nil {
				return nil, err
			}
			return map[string]any{"launched": id}, nil
		},
	}

	s.tools["stop_profile"] = mcpTool{
		Name:        "stop_profile",
		Description: "停止指定配置的浏览器实例。",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"id": map[string]any{"type": "string", "description": "配置 _id"}},
			"required":   []string{"id"},
		},
		Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
			var a struct {
				ID string `json:"id"`
			}
			if err := decodeArgs(args, &a); err != nil {
				return nil, err
			}
			id, err := resolveTarget(ctx, a.ID)
			if err != nil {
				return nil, err
			}
			if err := stopProfileByID(id); err != nil {
				return nil, err
			}
			return map[string]any{"stopped": id}, nil
		},
	}

	s.tools["add_profile"] = mcpTool{
		Name:        "add_profile",
		Description: "新建配置。name 必填；groupId、proxy 传 _id；fingerprint 传完整对象（可选）。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":        map[string]any{"type": "string"},
				"groupId":     map[string]any{"type": "string"},
				"proxy":       map[string]any{"type": "string"},
				"args":        map[string]any{"type": "string", "description": "自定义启动参数，形如 --foo --bar=baz"},
				"notes":       map[string]any{"type": "string"},
				"fingerprint": map[string]any{"type": "object", "description": "指纹配置，留空则用默认值 + 随机 seed"},
			},
			"required": []string{"name"},
		},
		Handler: toolAddProfile,
	}

	s.tools["update_profile"] = mcpTool{
		Name:        "update_profile",
		Description: "按 _id 更新配置，只需传要改的字段；未传的保持原值。",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":          map[string]any{"type": "string"},
				"name":        map[string]any{"type": "string"},
				"groupId":     map[string]any{"type": "string"},
				"proxy":       map[string]any{"type": "string"},
				"args":        map[string]any{"type": "string"},
				"notes":       map[string]any{"type": "string"},
				"sort":        map[string]any{"type": "integer"},
				"fingerprint": map[string]any{"type": "object"},
			},
			"required": []string{"id"},
		},
		Handler: toolUpdateProfile,
	}
}

func toolAddProfile(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		Name        string             `json:"name"`
		GroupID     string             `json:"groupId"`
		Proxy       string             `json:"proxy"`
		Args        string             `json:"args"`
		Notes       string             `json:"notes"`
		Fingerprint *FingerprintConfig `json:"fingerprint"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.Name) == "" {
		return nil, errors.New("name 不能为空")
	}

	p := Profile{
		Name:    a.Name,
		GroupID: a.GroupID,
		Proxy:   a.Proxy,
		Args:    a.Args,
		Notes:   a.Notes,
	}
	if a.Fingerprint != nil {
		p.Fingerprint = *a.Fingerprint
	}
	enrichFingerprint(&p, true)

	p.CreatedAt = time.Now().UnixMilli()
	p.UpdatedAt = p.CreatedAt

	res, err := db.Exec(`INSERT INTO profiles (name, group_id, sort, proxy, args, fingerprint, notes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, decodeID(p.GroupID), p.Sort, decodeID(p.Proxy), p.Args, p.Fingerprint, p.Notes, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, errors.New("配置名称已存在")
		}
		return nil, fmt.Errorf("插入失败：%v", err)
	}
	rowID, _ := res.LastInsertId()
	p.ID = encodeID(rowID)
	return map[string]any{"created": p}, nil
}

func toolUpdateProfile(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		ID          string             `json:"id"`
		Name        *string            `json:"name"`
		GroupID     *string            `json:"groupId"`
		Proxy       *string            `json:"proxy"`
		Args        *string            `json:"args"`
		Notes       *string            `json:"notes"`
		Sort        *int               `json:"sort"`
		Fingerprint *FingerprintConfig `json:"fingerprint"`
	}
	if err := decodeArgs(args, &a); err != nil {
		return nil, err
	}
	raw := decodeID(a.ID)
	if raw <= 0 {
		return nil, errors.New("id 无效")
	}
	p, err := loadProfile(raw)
	if err != nil {
		return nil, err
	}
	if a.Name != nil {
		if strings.TrimSpace(*a.Name) == "" {
			return nil, errors.New("name 不能为空")
		}
		p.Name = *a.Name
	}
	if a.GroupID != nil {
		p.GroupID = *a.GroupID
	}
	if a.Proxy != nil {
		p.Proxy = *a.Proxy
	}
	if a.Args != nil {
		p.Args = *a.Args
	}
	if a.Notes != nil {
		p.Notes = *a.Notes
	}
	if a.Sort != nil {
		p.Sort = *a.Sort
	}
	if a.Fingerprint != nil {
		p.Fingerprint = *a.Fingerprint
	}
	p.UpdatedAt = time.Now().UnixMilli()

	_, err = db.Exec(`UPDATE profiles SET name=?, group_id=?, sort=?, proxy=?, args=?, fingerprint=?, notes=?, updated_at=? WHERE id=?`,
		p.Name, decodeID(p.GroupID), p.Sort, decodeID(p.Proxy), p.Args, p.Fingerprint, p.Notes, p.UpdatedAt, raw)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, errors.New("配置名称已存在")
		}
		return nil, fmt.Errorf("更新失败：%v", err)
	}
	return map[string]any{"updated": p}, nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE")
}

// ------------------------------------------------------------------ 破坏性工具

func registerDestructiveTools(s *mcpServer) {
	s.tools["delete_profile"] = mcpTool{
		Name:        "delete_profile",
		Description: "删除配置及其 user-data-dir（不可恢复）。需 MCP_ENABLE_DESTRUCTIVE=1。",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"id": map[string]any{"type": "string"}},
			"required":   []string{"id"},
		},
		Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
			var a struct {
				ID string `json:"id"`
			}
			if err := decodeArgs(args, &a); err != nil {
				return nil, err
			}
			if err := deleteProfileByID(a.ID); err != nil {
				return nil, err
			}
			return map[string]any{"deleted": a.ID}, nil
		},
	}

	s.tools["delete_proxy"] = mcpTool{
		Name:        "delete_proxy",
		Description: "删除代理记录（不可恢复）。需 MCP_ENABLE_DESTRUCTIVE=1。",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"id": map[string]any{"type": "string"}},
			"required":   []string{"id"},
		},
		Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
			var a struct {
				ID string `json:"id"`
			}
			if err := decodeArgs(args, &a); err != nil {
				return nil, err
			}
			raw := decodeID(a.ID)
			if raw <= 0 {
				return nil, errors.New("id 无效")
			}
			if _, err := db.Exec("DELETE FROM proxies WHERE id=?", raw); err != nil {
				return nil, fmt.Errorf("删除失败：%v", err)
			}
			return map[string]any{"deleted": a.ID}, nil
		},
	}

	s.tools["delete_group"] = mcpTool{
		Name:        "delete_group",
		Description: "删除分组，组内配置的 groupId 置为 0。需 MCP_ENABLE_DESTRUCTIVE=1。",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"id": map[string]any{"type": "string"}},
			"required":   []string{"id"},
		},
		Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
			var a struct {
				ID string `json:"id"`
			}
			if err := decodeArgs(args, &a); err != nil {
				return nil, err
			}
			if err := deleteGroupByID(a.ID); err != nil {
				return nil, err
			}
			return map[string]any{"deleted": a.ID}, nil
		},
	}

	s.tools["stop_all_profiles"] = mcpTool{
		Name:        "stop_all_profiles",
		Description: "停止全部运行中的实例。需 MCP_ENABLE_DESTRUCTIVE=1。",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
			ids := getRunningIDs()
			for _, id := range ids {
				if err := stopProfileByID(id); err != nil {
					logfMCP("停止 %s 失败：%v", id, err)
				}
			}
			return map[string]any{"stopped": ids, "count": len(ids)}, nil
		},
	}
}

func logfMCP(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "[MCP] "+format+"\n", a...)
}
