package main

import (
	"log"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
)

type runningProfile struct {
	cmd        *exec.Cmd
	done       chan struct{}
	profileID  string
	cookiePath string
	// devtoolsURL is filled in by watchDevToolsPort once Chromium is listening.
	// Guarded by devtoolsMu because the watcher goroutine writes it while
	// tools/list_running reads it.
	devtoolsMu  sync.Mutex
	devtoolsURL string
}

func (rp *runningProfile) getDevToolsURL() string {
	rp.devtoolsMu.Lock()
	defer rp.devtoolsMu.Unlock()
	return rp.devtoolsURL
}

func (rp *runningProfile) setDevToolsURL(u string) {
	rp.devtoolsMu.Lock()
	rp.devtoolsURL = u
	rp.devtoolsMu.Unlock()
}

var (
	runningMu       sync.Mutex
	runningProfiles = map[string]*runningProfile{}
)

// getRunningIDs returns a snapshot of currently running profile IDs.
// Caller must NOT hold runningMu.
func getRunningIDs() []string {
	runningMu.Lock()
	ids := make([]string, 0, len(runningProfiles))
	for id := range runningProfiles {
		ids = append(ids, id)
	}
	runningMu.Unlock()
	slices.Sort(ids)
	return ids
}

// bundledBrowserName is the binary name inside the packaged Chromium folder.
func bundledBrowserName() string {
	if runtime.GOOS == "windows" {
		return "chrome.exe"
	}
	return "chrome-wrapper"
}

// findBrowserPath returns the Chromium binary to launch.
//
// The packaged Chromium (bin/chromium next to the executable) is checked first:
// the fingerprint flags these builds rely on only exist in the patched build, so
// a system Chrome in PATH must never win over it. PATH is only a fallback.
func findBrowserPath() string {
	if exe, err := os.Executable(); err == nil {
		local := filepath.Join(filepath.Dir(exe), "bin", "chromium", bundledBrowserName())
		if st, err := os.Stat(local); err == nil && !st.IsDir() {
			abs, _ := filepath.Abs(local)
			return abs
		}
	}

	// The container image ships Chromium itself as `chrome`.
	if p, err := exec.LookPath("chrome"); err == nil {
		abs, _ := filepath.Abs(p)
		return abs
	}
	return ""
}

// stopAllBrowsers asks every running profile to close, killing stragglers after
// 5s. Used on application exit so no Chromium windows are left behind.
func stopAllBrowsers() {
	runningMu.Lock()
	snapshot := make([]*runningProfile, 0, len(runningProfiles))
	for _, rp := range runningProfiles {
		snapshot = append(snapshot, rp)
	}
	runningMu.Unlock()

	for _, rp := range snapshot {
		if rp.cmd.Process == nil {
			continue
		}
		if runtime.GOOS == "windows" {
			closeWindowsByPID(uint32(rp.cmd.Process.Pid))
		} else {
			rp.cmd.Process.Signal(syscall.SIGTERM)
		}
	}

	// Wait for all of them concurrently; kill whatever is still alive after 5s.
	allDone := make(chan struct{})
	go func() {
		for _, rp := range snapshot {
			<-rp.done
		}
		close(allDone)
	}()
	select {
	case <-allDone:
	case <-time.After(5 * time.Second):
		for _, rp := range snapshot {
			if rp.cmd.Process != nil {
				rp.cmd.Process.Kill()
			}
		}
	}
}

// proxyInfo holds the fields we need from a proxy record.
type proxyInfo struct {
	URL      string
	Timezone string
	Lang     string
	Location string
}

// getProxyInfo fetches proxy info (url, timezone, lang) by proxy rowid.
// Returns zero-value struct when proxyRawID <= 0.
func getProxyInfo(proxyRawID int64) proxyInfo {
	if proxyRawID <= 0 {
		return proxyInfo{}
	}
	var info proxyInfo
	if err := db.QueryRow("SELECT url, timezone, lang, location FROM proxies WHERE id=?", proxyRawID).
		Scan(&info.URL, &info.Timezone, &info.Lang, &info.Location); err != nil {
		log.Printf("[DB] getProxyInfo(%d) error: %v", proxyRawID, err)
	}
	return info
}

// defaultFingerprint returns the same defaults the Web UI uses for a new
// profile, so profiles created through the API or by MCP_KEYS auto-creation
// start with a usable hardware/host fingerprint instead of the container's
// real values.
func defaultFingerprint() FingerprintConfig {
	return FingerprintConfig{
		Platform:            "windows",
		Brand:               "Chrome",
		HardwareConcurrency: "8",
		DeviceMemory:        "8",
		Screen:              "1920x1080",
		DisableFeatures:     []string{"webrtc"},
		DisableFingerprint:  []string{},
		RandomFingerprint:   true,
		ProxyLang:           true,
		ProxyTimezone:       true,
		ProxyLocation:       true,
	}
}

// isEmptyFingerprint reports whether nothing at all was configured, which is
// what callers that omit the fingerprint field end up with.
func isEmptyFingerprint(f FingerprintConfig) bool {
	return f.Seed == 0 &&
		f.Platform == "" && f.Brand == "" &&
		f.HardwareConcurrency == "" && f.DeviceMemory == "" &&
		f.Screen == "" && f.Lang == "" && f.Timezone == "" &&
		f.IP == "" && f.Location == "" &&
		len(f.DisableFeatures) == 0 && len(f.DisableFingerprint) == 0 &&
		!f.RandomFingerprint && !f.ProxyLang && !f.ProxyTimezone && !f.ProxyLocation
}

// enrichFingerprint fills in defaults and a random Seed on new profiles.
func enrichFingerprint(p *Profile, isNew bool) {
	if isNew {
		if isEmptyFingerprint(p.Fingerprint) {
			p.Fingerprint = defaultFingerprint()
		}
		p.Fingerprint.Seed = rand.Int32()
	}
}

// splitArgs splits a command-line string into arguments.
func splitArgs(s string) []string {
	parts := strings.Split(s, "--")
	var args []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			args = append(args, "--"+p)
		}
	}
	return args
}
