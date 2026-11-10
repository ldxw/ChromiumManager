package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// countryToLang maps 2-letter ISO 3166-1 alpha-2 country codes to BCP 47 language codes.
var countryToLang = map[string]string{
	"AF": "prs-AF", "AL": "sq-AL", "AM": "hy-AM", "PT": "pt-PT", "US": "en-US", "AR": "es-AR",
	"AT": "de-AT", "AE": "ar-AE", "SE": "sv-SE", "FI": "fi-FI", "AZ": "az-Latn-AZ", "BA": "bs-BA",
	"HR": "hr-HR", "RS": "sr-Latn-RS", "NL": "nl-NL", "BD": "bn-BD", "AU": "en-AU", "FR": "fr-FR",
	"BG": "bg-BG", "BH": "ar-BH", "BO": "quz-BO", "BE": "fr-BE", "BR": "pt-BR", "ZA": "en-ZA",
	"BY": "be-BY", "BZ": "en-BZ", "CA": "en-CA", "BN": "ms-BN", "CL": "arn-CL", "CO": "es-CO",
	"CR": "es-CR", "ES": "es-ES", "GR": "el-GR", "TR": "tr-TR", "CZ": "cs-CZ", "DE": "de-DE",
	"SA": "ar-SA", "DK": "da-DK", "DO": "es-DO", "DZ": "ar-DZ", "EC": "quz-EC", "EE": "et-EE",
	"EG": "ar-EG", "CH": "de-CH", "FO": "fo-FO", "GE": "ka-GE", "GL": "kl-GL", "GT": "qut-GT",
	"HK": "zh-HK", "HN": "es-HN", "HU": "hu-HU", "IL": "he-IL", "IE": "en-IE", "ID": "id-ID",
	"GB": "en-GB", "IN": "hi-IN", "IS": "is-IS", "IR": "fa-IR", "IQ": "ar-IQ", "IT": "it-IT",
	"JM": "en-JM", "JO": "ar-JO", "JP": "ja-JP", "KH": "km-KH", "KW": "ar-KW", "LI": "de-LI",
	"LK": "si-LK", "LB": "ar-LB", "LT": "lt-LT", "LU": "de-LU", "LV": "lv-LV", "LY": "ar-LY",
	"MA": "ar-MA", "MC": "fr-MC", "RO": "ro-RO", "ME": "sr-Latn-ME", "MK": "mk-MK", "MT": "mt-MT",
	"MV": "dv-MV", "MX": "es-MX", "MY": "ms-MY", "NG": "ha-Latn-NG", "NI": "es-NI", "NO": "nn-no",
	"NP": "ne-NP", "OM": "ar-OM", "PA": "es-PA", "PE": "quz-PE", "MO": "zh-MO", "PH": "fil-PH",
	"PL": "pl-PL", "PK": "ur-PK", "PR": "es-PR", "QA": "ar-QA", "PY": "es-PY", "RU": "ru-RU",
	"SG": "en-SG", "SI": "sl-SI", "SK": "sk-SK", "SN": "wo-SN", "SV": "es-SV", "SY": "ar-SY",
	"TH": "th-TH", "TN": "ar-TN", "KE": "sw-KE", "UA": "uk-UA", "UY": "es-UY", "VE": "es-VE",
	"VN": "vi-VN", "YE": "ar-YE", "ZW": "en-ZW", "CN": "zh-CN", "ET": "am-ET", "TT": "en-TT",
	"TW": "zh-TW", "NZ": "en-NZ", "KR": "ko-KR", "LA": "lo-LA", "KG": "ky-KG", "KZ": "kk-KZ",
	"TJ": "tg-Cyrl-TJ", "TM": "tk-TM", "UZ": "uz-Latn-UZ", "MN": "mn-Mong", "RW": "rw-RW",
}

func fetchLocationInfo(proxyRawID int64) (timezone, lang, ip, location string) {
	var proxyURLStr string
	if proxyRawID > 0 {
		db.QueryRow("SELECT url FROM proxies WHERE id=?", proxyRawID).Scan(&proxyURLStr)
	}
	client := &http.Client{Timeout: 10 * time.Second}

	if proxyURLStr != "" {
		// 标准库的 Transport 只认 socks5 scheme，socks5h 是 Chromium 那边的写法，
		// 检测时归一成 socks5（两者都是把域名交给代理解析）。
		if strings.HasPrefix(proxyURLStr, "socks5h://") {
			proxyURLStr = "socks5://" + strings.TrimPrefix(proxyURLStr, "socks5h://")
		}
		if !strings.HasPrefix(proxyURLStr, "http://") && !strings.HasPrefix(proxyURLStr, "https://") && !strings.HasPrefix(proxyURLStr, "socks5://") {
			proxyURLStr = "http://" + proxyURLStr
		}
		if pURL, err := url.Parse(proxyURLStr); err == nil {
			client.Transport = &http.Transport{Proxy: http.ProxyURL(pURL)}
		}
	}

	req, _ := http.NewRequest("GET", "https://get.geojs.io/v1/ip/geo.json", nil)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[GeoJS] request failed (proxyID=%d): %v", proxyRawID, err)
		return "", "", "", ""
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		var result struct {
			Timezone    string `json:"timezone"`
			CountryCode string `json:"country_code"`
			IP          string `json:"ip"`
			Latitude    string `json:"latitude"`
			Longitude   string `json:"longitude"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
			timezone = result.Timezone
			ip = result.IP
			if result.Latitude != "" && result.Longitude != "" {
				location = result.Latitude + "," + result.Longitude
			}
			if mapped, ok := countryToLang[result.CountryCode]; ok {
				lang = mapped
			} else {
				lang = "en-US"
			}
			return timezone, lang, ip, location
		}
	}
	return "", "", "", ""
}

func getProxies(w http.ResponseWriter, r *http.Request) {
	scanProxies := func(rows interface {
		Next() bool
		Scan(...interface{}) error
	}) []Proxy {
		var list []Proxy
		for rows.Next() {
			var p Proxy
			var rawID int64
			if err := rows.Scan(&rawID, &p.Name, &p.URL, &p.Timezone, &p.Lang, &p.IP, &p.Location, &p.CreatedAt, &p.UpdatedAt); err == nil {
				p.ID = encodeID(rawID)
				list = append(list, p)
			}
		}
		return list
	}

	if r.URL.Query().Get("all") == "1" {
		rows, err := db.Query("SELECT id, name, url, timezone, lang, ip, location, created_at, updated_at FROM proxies ORDER BY created_at DESC")
		if err != nil {
			writeJSON(w, Response[any]{Code: 500, Message: err.Error()})
			return
		}
		defer rows.Close()
		writeJSON(w, Response[any]{Code: 200, Message: "success", Data: scanProxies(rows)})
		return
	}

	page, pageSize := 1, 10
	fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
	fmt.Sscanf(r.URL.Query().Get("pageSize"), "%d", &pageSize)
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 10
	}

	var total int
	var rows *sql.Rows
	var err error

	if keyword != "" {
		like := "%" + keyword + "%"
		db.QueryRow("SELECT COUNT(*) FROM proxies WHERE name LIKE ? OR url LIKE ? OR ip LIKE ? OR lang LIKE ? OR timezone LIKE ?",
			like, like, like, like, like).Scan(&total)
		rows, err = db.Query("SELECT id, name, url, timezone, lang, ip, location, created_at, updated_at FROM proxies WHERE name LIKE ? OR url LIKE ? OR ip LIKE ? OR lang LIKE ? OR timezone LIKE ? ORDER BY created_at DESC LIMIT ? OFFSET ?",
			like, like, like, like, like, pageSize, (page-1)*pageSize)
	} else {
		db.QueryRow("SELECT COUNT(*) FROM proxies").Scan(&total)
		rows, err = db.Query("SELECT id, name, url, timezone, lang, ip, location, created_at, updated_at FROM proxies ORDER BY created_at DESC LIMIT ? OFFSET ?",
			pageSize, (page-1)*pageSize)
	}

	if err != nil {
		writeJSON(w, Response[any]{Code: 500, Message: err.Error()})
		return
	}
	defer rows.Close()

	writeJSON(w, Response[any]{Code: 200, Message: "success", Data: map[string]interface{}{
		"list":  scanProxies(rows),
		"total": total,
	}})
}

func getProxy(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	rawID := decodeID(idStr)
	if rawID <= 0 {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}
	var p Proxy
	err := db.QueryRow("SELECT id, name, url, timezone, lang, ip, location, created_at, updated_at FROM proxies WHERE id=?", rawID).
		Scan(&rawID, &p.Name, &p.URL, &p.Timezone, &p.Lang, &p.IP, &p.Location, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		writeJSON(w, Response[any]{Code: 404, Message: "proxy not found"})
		return
	}
	p.ID = encodeID(rawID)
	writeJSON(w, Response[any]{Code: 200, Message: "success", Data: p})
}

func addProxy(w http.ResponseWriter, r *http.Request) {
	var p Proxy
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}
	p.CreatedAt = time.Now().UnixMilli()
	p.UpdatedAt = p.CreatedAt

	res, err := db.Exec("INSERT INTO proxies (name, url, timezone, lang, ip, location, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		p.Name, p.URL, p.Timezone, p.Lang, p.IP, p.Location, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "UNIQUE") {
			msg = "代理名称已存在！"
		}
		writeJSON(w, Response[any]{Code: 500, Message: msg})
		return
	}
	rowID, _ := res.LastInsertId()
	p.ID = encodeID(rowID)

	// Fetch location info
	if p.Lang == "" && p.Timezone == "" && p.Location == "" {
		if tz, lang, ip, loc := fetchLocationInfo(rowID); tz != "" {
			p.Timezone = tz
			p.Lang = lang
			p.IP = ip
			p.Location = loc
			db.Exec("UPDATE proxies SET timezone=?, lang=?, ip=?, location=?, updated_at=? WHERE id=?",
				tz, lang, ip, loc, time.Now().UnixMilli(), rowID)
		}
	}

	writeJSON(w, Response[any]{Code: 200, Message: "success", Data: p})
}

func updateProxy(w http.ResponseWriter, r *http.Request) {
	var p Proxy
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}
	p.UpdatedAt = time.Now().UnixMilli()

	rawID := decodeID(p.ID)

	// Fetch location info
	if p.Lang == "" && p.Timezone == "" && p.Location == "" {
		if tz, lang, ip, loc := fetchLocationInfo(rawID); tz != "" {
			p.Timezone = tz
			p.Lang = lang
			p.IP = ip
			p.Location = loc
		}
	}

	_, err := db.Exec("UPDATE proxies SET name=?, url=?, lang=?, timezone=?, ip=?, location=?, updated_at=? WHERE id=?",
		p.Name, p.URL, p.Lang, p.Timezone, p.IP, p.Location, p.UpdatedAt, rawID)
	if err != nil {
		writeJSON(w, Response[any]{Code: 500, Message: err.Error()})
		return
	}
	writeJSON(w, Response[any]{Code: 200, Message: "success", Data: p})
}

func deleteProxy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}
	rawID := decodeID(req.ID)

	// Use transaction to ensure atomic delete + cleanup
	tx, err := db.Begin()
	if err != nil {
		writeJSON(w, Response[any]{Code: 500, Message: err.Error()})
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM proxies WHERE id=?", rawID); err != nil {
		writeJSON(w, Response[any]{Code: 500, Message: err.Error()})
		return
	}
	if _, err := tx.Exec("UPDATE profiles SET proxy=0 WHERE proxy=?", rawID); err != nil {
		writeJSON(w, Response[any]{Code: 500, Message: err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, Response[any]{Code: 500, Message: err.Error()})
		return
	}
	writeJSON(w, Response[any]{Code: 200, Message: "success"})
}
