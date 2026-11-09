package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"
)

// 多 key 绑定：每个 MCP key 对应一个配置，外部使用方只能操作自己那一个，
// 看不到也动不了别人的配置。
//
//	MCP_KEYS={"key1":"窗口1","key2":"窗口2"}
//	MCP_KEYS_FILE=/config/mcp-keys.json   # 与 MCP_KEYS 二选一，MCP_KEYS 优先
//	MCP_GROUP_NAME=mcp                    # 自动创建配置时用的分组名
//
// 启动时按配置建好分组与配置，运行期只做 token → 配置 的内存查表。

const (
	defaultMCPGroupName = "mcp"

	errBindingStale = "绑定的配置已不存在或被重命名，请重启容器以按 MCP_KEYS 重新创建"
)

type mcpBinding struct {
	token     string
	profileID string // encodeID 之后的配置 _id
	name      string // 配置名，即 MCP_KEYS 的 value
}

type mcpKeyRegistry struct {
	bindings map[string]*mcpBinding
}

var mcpKeys = &mcpKeyRegistry{bindings: map[string]*mcpBinding{}}

func (r *mcpKeyRegistry) enabled() bool { return len(r.bindings) > 0 }

// lookup 用常量时间比较遍历全部 token，避免按比较耗时逐字符试探。
func (r *mcpKeyRegistry) lookup(token string) (*mcpBinding, bool) {
	var hit *mcpBinding
	for t, b := range r.bindings {
		if subtle.ConstantTimeCompare([]byte(t), []byte(token)) == 1 {
			hit = b
		}
	}
	if hit == nil {
		return nil, false
	}
	return hit, true
}

type mcpBindingCtxKey struct{}

func bindingFrom(ctx context.Context) *mcpBinding {
	if ctx == nil {
		return nil
	}
	b, _ := ctx.Value(mcpBindingCtxKey{}).(*mcpBinding)
	return b
}

// ensureValid 校验绑定配置仍然存在且没被改名。改名后 key 就指不到原配置了，
// 与其猜一个同组同名配置，不如要求重启让启动逻辑重新按 MCP_KEYS 校正。
func (b *mcpBinding) ensureValid() error {
	p, err := loadProfile(decodeID(b.profileID))
	if err != nil || p.Name != b.name {
		return errors.New(errBindingStale)
	}
	return nil
}

// bindingProfile 返回当前请求的绑定，已校验配置仍然有效。
func bindingProfile(ctx context.Context) (*mcpBinding, error) {
	b := bindingFrom(ctx)
	if b == nil {
		return nil, nil
	}
	if err := b.ensureValid(); err != nil {
		return nil, err
	}
	return b, nil
}

// resolveTarget 决定本次操作的目标配置 _id：有绑定时缺省取绑定，
// 显式传别的 id 直接拒绝；无绑定时 id 必填。
func resolveTarget(ctx context.Context, id string) (string, error) {
	b, err := bindingProfile(ctx)
	if err != nil {
		return "", err
	}
	id = strings.TrimSpace(id)
	if b == nil {
		if id == "" {
			return "", errors.New("id 不能为空")
		}
		return id, nil
	}
	if id == "" {
		return b.profileID, nil
	}
	if id != b.profileID {
		return "", fmt.Errorf("当前 key 已绑定到配置「%s」，不能操作其他配置", b.name)
	}
	return b.profileID, nil
}

// bindingAllowedTools 是绑定 key 可调用的工具白名单。不含 add_profile /
// update_profile 与全部破坏性工具：外部使用方只能操作被分配的那个配置。
var bindingAllowedTools = map[string]bool{
	"list_profiles":  true,
	"get_profile":    true,
	"list_running":   true,
	"list_groups":    true,
	"list_proxies":   true,
	"launch_profile": true,
	"stop_profile":   true,
	"navigate":       true,
	"read_page":      true,
	"click":          true,
	"fill":           true,
	"screenshot":     true,
	"eval_js":        true,
}

func bindingAllows(tool string) bool { return bindingAllowedTools[tool] }

// bindingIDOptional 是绑定时可以省略 id 的工具。
var bindingIDOptional = map[string]bool{
	"get_profile":    true,
	"launch_profile": true,
	"stop_profile":   true,
	"navigate":       true,
	"read_page":      true,
	"click":          true,
	"fill":           true,
	"screenshot":     true,
	"eval_js":        true,
}

// boundToolPresentation 返回绑定时对外展示的 schema 与描述：去掉 id 必填、
// 去掉 screenshot 的 path（绑定时不落盘），并改掉截图描述。
func boundToolPresentation(name string, t mcpTool) (map[string]any, string) {
	desc := t.Description
	if name == "screenshot" {
		desc = "对整页或指定元素截图，直接以 PNG base64 图片内容返回，不写文件。"
	}
	if t.InputSchema == nil {
		return t.InputSchema, desc
	}

	schema := make(map[string]any, len(t.InputSchema))
	for k, v := range t.InputSchema {
		schema[k] = v
	}

	if props, ok := t.InputSchema["properties"].(map[string]any); ok {
		newProps := make(map[string]any, len(props))
		for k, v := range props {
			if name == "screenshot" && k == "path" {
				continue
			}
			newProps[k] = v
		}
		schema["properties"] = newProps
	}

	if required, ok := t.InputSchema["required"].([]string); ok {
		kept := make([]string, 0, len(required))
		for _, r := range required {
			if r == "id" && bindingIDOptional[name] {
				continue
			}
			kept = append(kept, r)
		}
		if len(kept) == 0 {
			delete(schema, "required")
		} else {
			schema["required"] = kept
		}
	}
	return schema, desc
}

// ------------------------------------------------------------------ 启动阶段

// initMCPKeys 解析 MCP_KEYS / MCP_KEYS_FILE，建立分组与配置，并填充内存映射。
// 返回错误时调用方直接退出：宁可起不来，也不要带着半配置的绑定对外服务。
func initMCPKeys() error {
	raw, source, err := readMCPKeysConfig()
	if err != nil {
		return fmt.Errorf("解析 %s 失败：%w", source, err)
	}
	if raw == nil {
		if mcpToken() != "" {
			log.Printf("[MCP] 未设置 MCP_KEYS，沿用单一 MCP_TOKEN 鉴权")
		}
		return nil
	}
	if mcpToken() != "" {
		log.Printf("[MCP] MCP_KEYS 与 MCP_TOKEN 同时设置，按 key 绑定处理，MCP_TOKEN 不生效")
	}

	groupName := strings.TrimSpace(os.Getenv("MCP_GROUP_NAME"))
	if groupName == "" {
		groupName = defaultMCPGroupName
	}
	groupID, err := ensureMCPGroup(groupName)
	if err != nil {
		return fmt.Errorf("准备分组「%s」失败：%w", groupName, err)
	}

	tokens := make([]string, 0, len(raw))
	for t := range raw {
		tokens = append(tokens, t)
	}
	sort.Strings(tokens)

	bindings := make(map[string]*mcpBinding, len(raw))
	for _, token := range tokens {
		name := raw[token]
		rawID, created, err := ensureMCPProfile(groupID, name)
		if err != nil {
			return fmt.Errorf("准备配置「%s」失败：%w", name, err)
		}
		bindings[token] = &mcpBinding{token: token, profileID: encodeID(rawID), name: name}
		state := "复用已有"
		if created {
			state = "新建"
		}
		log.Printf("[MCP] key %s → 配置「%s」（%s，分组 %s）", maskMCPKey(token), name, state, groupName)
	}
	mcpKeys.bindings = bindings
	log.Printf("[MCP] 已启用 %d 个 key 绑定，分组「%s」", len(bindings), groupName)
	return nil
}

func readMCPKeysConfig() (map[string]string, string, error) {
	if v := strings.TrimSpace(os.Getenv("MCP_KEYS")); v != "" {
		m, err := parseMCPKeys([]byte(v))
		return m, "MCP_KEYS", err
	}
	if path := strings.TrimSpace(os.Getenv("MCP_KEYS_FILE")); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, "MCP_KEYS_FILE", err
		}
		m, err := parseMCPKeys(b)
		return m, "MCP_KEYS_FILE", err
	}
	return nil, "", nil
}

func parseMCPKeys(b []byte) (map[string]string, error) {
	var raw map[string]string
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf(`需要 JSON 对象，形如 {"key1":"窗口1"}：%v`, err)
	}
	if len(raw) == 0 {
		return nil, errors.New("至少需要一个 key")
	}

	out := make(map[string]string, len(raw))
	byProfile := make(map[string]string, len(raw))
	for k, v := range raw {
		token := strings.TrimSpace(k)
		name := strings.TrimSpace(v)
		if token == "" {
			return nil, errors.New("存在空的 key")
		}
		if name == "" {
			return nil, fmt.Errorf("key %s 的配置名为空", maskMCPKey(token))
		}
		if prev, dup := byProfile[name]; dup {
			return nil, fmt.Errorf("key %s 与 %s 绑定了同一个配置「%s」，拒绝歧义",
				maskMCPKey(prev), maskMCPKey(token), name)
		}
		byProfile[name] = token
		out[token] = name
	}
	return out, nil
}

func maskMCPKey(k string) string {
	if len(k) <= 4 {
		return "****"
	}
	return k[:4] + "****"
}

// ensureMCPGroup 按名称取分组，没有就建。返回分组原始 rowid。
func ensureMCPGroup(name string) (int64, error) {
	var id int64
	err := db.QueryRow("SELECT id FROM groups WHERE name=?", name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	now := time.Now().UnixMilli()
	if _, err := db.Exec("INSERT INTO groups (name, sort, created_at, updated_at) VALUES (?, ?, ?, ?)",
		name, 0, now, now); err != nil {
		// 并发或历史数据下再查一次，避免把已存在的分组当失败。
		if e2 := db.QueryRow("SELECT id FROM groups WHERE name=?", name).Scan(&id); e2 == nil {
			return id, nil
		}
		return 0, err
	}
	if err := db.QueryRow("SELECT id FROM groups WHERE name=?", name).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// ensureMCPProfile 在指定分组下确保存在名为 name 的配置。返回 rowid 与是否新建。
func ensureMCPProfile(groupID int64, name string) (int64, bool, error) {
	var id int64
	err := db.QueryRow("SELECT id FROM profiles WHERE group_id=? AND name=?", groupID, name).Scan(&id)
	if err == nil {
		return id, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}

	p := Profile{Name: name, GroupID: encodeID(groupID)}
	enrichFingerprint(&p, true)
	p.CreatedAt = time.Now().UnixMilli()
	p.UpdatedAt = p.CreatedAt

	res, err := db.Exec(`INSERT INTO profiles (name, group_id, sort, proxy, args, fingerprint, notes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, groupID, 0, 0, "", p.Fingerprint, "", p.CreatedAt, p.UpdatedAt)
	if err != nil {
		// 并发创建后重查一次。
		if e2 := db.QueryRow("SELECT id FROM profiles WHERE group_id=? AND name=?", groupID, name).Scan(&id); e2 == nil {
			return id, false, nil
		}
		return 0, false, err
	}
	id, err = res.LastInsertId()
	return id, true, err
}
