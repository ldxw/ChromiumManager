package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// watchDevToolsPort waits for Chromium to write DevToolsActivePort inside its
// user-data-dir and records the resulting DevTools websocket URL on rp.
//
// With --remote-debugging-port=0 Chromium picks a free port itself, so the port
// is only knowable after startup: DevToolsActivePort holds the port on line 1
// and the browser ws path on line 2. Returns when the profile exits, when the
// URL is recorded, or after devtoolsWaitTimeout.
const (
	devtoolsPollInterval = 100 * time.Millisecond
	devtoolsWaitTimeout  = 60 * time.Second
)

func watchDevToolsPort(rp *runningProfile, userDataDir string) {
	portFile := filepath.Join(userDataDir, "DevToolsActivePort")

	ticker := time.NewTicker(devtoolsPollInterval)
	defer ticker.Stop()
	deadline := time.NewTimer(devtoolsWaitTimeout)
	defer deadline.Stop()

	for {
		select {
		case <-rp.done:
			return
		case <-deadline.C:
			log.Printf("[CDP] profile %s: DevToolsActivePort not written within %s", rp.profileID, devtoolsWaitTimeout)
			return
		case <-ticker.C:
		}

		url, ok := readDevToolsURL(portFile)
		if !ok {
			continue
		}
		rp.setDevToolsURL(url)
		log.Printf("[CDP] profile %s listening at %s", rp.profileID, url)
		return
	}
}

// readDevToolsURL parses DevToolsActivePort. ok is false while the file is
// absent, empty, or not yet complete (Chromium may write it in two passes).
func readDevToolsURL(portFile string) (string, bool) {
	data, err := os.ReadFile(portFile)
	if err != nil {
		return "", false
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) < 2 {
		return "", false
	}
	port := strings.TrimSpace(lines[0])
	path := strings.TrimSpace(lines[1])
	if port == "" || path == "" {
		return "", false
	}
	return "ws://127.0.0.1:" + port + path, true
}
