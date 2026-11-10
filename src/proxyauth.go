package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Chromium 不解析 --proxy-server 里 URL 的 userinfo：带用户名密码的地址会被判成
// 不支持的代理，页面直接报 ERR_NO_SUPPORTED_PROXIES，请求一个都发不出去；代理端
// 则只看到 407。所以带凭据的代理要把地址与凭据拆开：地址照常给 --proxy-server，
// 凭据通过一个随配置生成的 MV3 扩展在 webRequest.onAuthRequired 里回给浏览器。
//
// 扩展必须用 ["blocking"] 并在监听器里同步 return authCredentials。用
// ["asyncBlocking"] + callback 的写法实测会得到 ERR_INVALID_AUTH_CREDENTIALS。

// splitProxyCredentials 从代理地址里剥掉 userinfo。
// 没有凭据时原样返回入参，保证无认证代理的启动参数与以前逐字相同。
func splitProxyCredentials(raw string) (clean, user, pass string, hasAuth bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", false
	}

	withScheme := raw
	if !strings.Contains(withScheme, "://") {
		withScheme = "http://" + withScheme
	}
	u, err := url.Parse(withScheme)
	if err != nil || u.User == nil {
		return raw, "", "", false
	}

	user = u.User.Username()
	pass, _ = u.User.Password()
	u.User = nil
	return strings.TrimSuffix(u.String(), "/"), user, pass, true
}

// proxyAuthExtDir 放在 dataDir 下而不是配置的 user-data-dir 里：
// 扩展目录在 user-data-dir 内会被 Chromium 当成配置数据的一部分。
func proxyAuthExtDir(seed int32) string {
	return filepath.Join(dataDir, "proxy-auth", encodeID(int64(seed)))
}

const proxyAuthManifest = `{
  "manifest_version": 3,
  "name": "Proxy Auth",
  "version": "1.0",
  "permissions": ["webRequest", "webRequestAuthProvider"],
  "host_permissions": ["<all_urls>"],
  "background": {"service_worker": "background.js"}
}
`

// ensureProxyAuthExtension 写出（或覆盖）一个带指定凭据的扩展目录，返回可传给
// --load-extension 的绝对路径。凭据用 JSON 编码成 JS 字符串字面量，避免引号、
// 反斜杠、换行把脚本写坏。
func ensureProxyAuthExtension(seed int32, user, pass string) (string, error) {
	dir := proxyAuthExtDir(seed)
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	if err := os.MkdirAll(abs, 0755); err != nil {
		return "", err
	}

	userJS, err := json.Marshal(user)
	if err != nil {
		return "", err
	}
	passJS, err := json.Marshal(pass)
	if err != nil {
		return "", err
	}

	script := fmt.Sprintf(`chrome.webRequest.onAuthRequired.addListener(
  function (details) {
    if (details.isProxy) {
      return {authCredentials: {username: %s, password: %s}};
    }
  },
  {urls: ["<all_urls>"]},
  ["blocking"]
);
`, userJS, passJS)

	if err := os.WriteFile(filepath.Join(abs, "manifest.json"), []byte(proxyAuthManifest), 0644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(abs, "background.js"), []byte(script), 0644); err != nil {
		return "", err
	}
	return abs, nil
}
