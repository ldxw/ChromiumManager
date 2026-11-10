package main

import (
	"context"
	"database/sql/driver"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/sqids/sqids-go"
	_ "modernc.org/sqlite"
)

//go:embed web/dist
var webFS embed.FS

const serverAddr = "127.0.0.1:10101"

// appVersion is reported by the MCP serverInfo block. Release builds may inject
// it with -ldflags "-X main.appVersion=..."; the default keeps source builds valid.
var appVersion = "1.0.0"

var (
	sq      *sqids.Sqids
	dataDir string

	httpServer   *http.Server
	shutdownOnce sync.Once
)

// defaultDataDir returns the default data directory. On Windows it lives under
// the user's config dir so the packaged build needs no writable CWD.
func defaultDataDir() string {
	if runtime.GOOS == "windows" {
		if dir, err := os.UserConfigDir(); err == nil {
			return filepath.Join(dir, "ChromiumManager")
		}
	}
	return "data"
}

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmsgprefix)

	dataDir = defaultDataDir()
	if v := os.Getenv("DATA_DIR"); v != "" {
		dataDir = v
	}

	var err error
	sq, err = sqids.New(sqids.Options{MinLength: 8})
	if err != nil {
		log.Fatalf("[App] failed to init sqids: %v", err)
	}
}

func encodeID(id int64) string {
	s, _ := sq.Encode([]uint64{uint64(id)})
	return s
}

func decodeID(s string) int64 {
	if s == "" || s == "all" {
		return 0
	}
	nums := sq.Decode(s)
	if len(nums) == 0 {
		return 0
	}
	return int64(nums[0])
}

type Group struct {
	ID        string `json:"_id"`
	Name      string `json:"name"`
	Sort      int    `json:"sort"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type Proxy struct {
	ID        string `json:"_id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	IP        string `json:"ip"`
	Lang      string `json:"lang"`
	Timezone  string `json:"timezone"`
	Location  string `json:"location"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type FingerprintConfig struct {
	Seed                int32    `json:"seed"`
	Platform            string   `json:"platform"`
	Brand               string   `json:"brand"`
	HardwareConcurrency string   `json:"hardwareConcurrency"`
	DeviceMemory        string   `json:"deviceMemory"`
	DisableFeatures     []string `json:"disableFeatures"`
	Screen              string   `json:"screen"`
	Lang                string   `json:"lang"`
	Timezone            string   `json:"timezone"`
	IP                  string   `json:"ip"`
	Location            string   `json:"location"`
	DisableFingerprint  []string `json:"disableFingerprint"`
	RandomFingerprint   bool     `json:"randomFingerprint"`
	ProxyLang           bool     `json:"proxyLang"`
	ProxyTimezone       bool     `json:"proxyTimezone"`
	ProxyLocation       bool     `json:"proxyLocation"`
}

func (f *FingerprintConfig) Scan(src interface{}) error {
	if src == nil {
		return nil
	}
	var data []byte
	switch v := src.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		return fmt.Errorf("unsupported type for FingerprintConfig: %T", src)
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, f)
}

func (f FingerprintConfig) Value() (driver.Value, error) {
	b, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

type Profile struct {
	ID          string            `json:"_id"`
	Name        string            `json:"name"`
	GroupID     string            `json:"groupId"`
	Sort        int               `json:"sort"`
	Proxy       string            `json:"proxy"`
	Fingerprint FingerprintConfig `json:"fingerprint"`
	Args        string            `json:"args"`
	Notes       string            `json:"notes"`
	CreatedAt   int64             `json:"createdAt"`
	UpdatedAt   int64             `json:"updatedAt"`
}

type Response[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

// writeJSON writes a JSON-encoded response to the response writer.
func writeJSON(w http.ResponseWriter, resp any) {
	data, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// withMiddleware wraps the handler.
func withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle preflight
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Panic recovery
		defer func() {
			if err := recover(); err != nil {
				log.Printf("[Server] panic recovered: %v", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

func main() {
	if hasArg(os.Args[1:], "--mcp-stdio") {
		runMCPStdio()
		return
	}

	initDB()
	defer db.Close()

	startServer()
	runUI()
	stopAllBrowsers()
	stopServer()
}

// hasArg reports whether name appears in args. Used instead of the flag package
// so unrelated flags meant for a wrapped binary are never rejected.
func hasArg(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

// runMCPStdio serves the MCP protocol on stdin/stdout. stdout is the protocol
// channel, so logging must stay on stderr; launching browsers is still allowed
// and their output is discarded by exec.Cmd's nil writers.
func runMCPStdio() {
	log.SetOutput(os.Stderr)
	initDB()
	defer db.Close()

	log.Printf("[MCP] stdio server starting (version %s)", appVersion)
	if err := newMCPServer().serveStdio(os.Stdin, os.Stdout); err != nil {
		log.Printf("[MCP] stdio server exited: %v", err)
	}
	stopAllBrowsers()
	dropAllBrowserConns()
}

// startServer starts the HTTP server and returns immediately. The process keeps
// running until runUI returns.
func startServer() {
	mux := http.NewServeMux()

	// Group routes
	mux.HandleFunc("GET /get_groups", getGroups)
	mux.HandleFunc("POST /add_group", addGroup)
	mux.HandleFunc("POST /update_group", updateGroup)
	mux.HandleFunc("POST /delete_group", deleteGroup)

	// Profile routes
	mux.HandleFunc("GET /get_profiles", getProfiles)
	mux.HandleFunc("GET /get_profile", getProfile)
	mux.HandleFunc("POST /add_profile", addProfile)
	mux.HandleFunc("POST /update_profile", updateProfile)
	mux.HandleFunc("POST /delete_profile", deleteProfile)
	mux.HandleFunc("POST /launch_profile", launchProfile)
	mux.HandleFunc("POST /stop_profile", stopProfile)
	mux.HandleFunc("GET /show_profile", showProfile)
	mux.HandleFunc("GET /export_cookies", exportCookies)
	mux.HandleFunc("POST /import_cookies", importCookies)

	// Proxy routes
	mux.HandleFunc("GET /get_proxies", getProxies)
	mux.HandleFunc("GET /get_proxy", getProxy)
	mux.HandleFunc("POST /add_proxy", addProxy)
	mux.HandleFunc("POST /update_proxy", updateProxy)
	mux.HandleFunc("POST /delete_proxy", deleteProxy)

	// SSE
	mux.HandleFunc("GET /events", eventsHandler)

	// MCP over Streamable HTTP。不限定方法，由 handler 自己回 405 + Allow，
	// 这样 GET/DELETE 也能走到鉴权和 Allow 逻辑。
	// 同一个实例也挂到外部监听端口，两处共用工具注册结果。
	if err := initMCPKeys(); err != nil {
		log.Fatalf("[MCP] 初始化 key 绑定失败：%v", err)
	}
	mcpSrv := newMCPServer()
	mux.Handle("/mcp", mcpHTTPHandler(mcpSrv))

	// 浏览器伪装脚本的下载入口（/chrome-mcp 与 /chrome-mcp-<slug>），供手机/Aurabot
	// 侧的自动化模块取走当「浏览器路径」。带 slug 的那条没法用 ServeMux 模式表达，
	// 由 shimHandler 在 mux 前面按前缀分流，见 src/shim.go。

	// DevTools 反向代理：Chromium 只把调试端口绑在 127.0.0.1 上，
	// --remote-debugging-address 在 156 上不生效（实测 headless/headful 都只监听
	// loopback），容器外拿不到。这里把 /cdp/<配置 _id>/... 转发到该实例的随机端口。
	// 同样挂到外部监听端口。
	registerCDPProxy(mux)

	// Static files
	fsSub, _ := fs.Sub(webFS, "web/dist")
	mux.Handle("/", http.FileServer(http.FS(fsSub)))

	handler := withMiddleware(shimHandler(mux))
	httpServer = &http.Server{Addr: serverAddr, Handler: handler}

	go func() {
		log.Printf("[Server] listening on %s", serverAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[Server] listen error: %v", err)
		}
	}()

	// 外部直连端口，供容器外的 AI 客户端使用。
	startMCPExternalServer(mcpSrv)
}

// stopServer shuts the HTTP server down once, giving in-flight requests 5s.
func stopServer() {
	shutdownOnce.Do(func() {
		if httpServer == nil {
			return
		}
		log.Printf("[Server] shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Printf("[Server] forced shutdown: %v", err)
		}
		stopMCPExternalServer()
		log.Printf("[Server] exited gracefully")
	})
}
