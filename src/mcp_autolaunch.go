package main

import (
	"log"
	"os"
	"time"
)

// mcpAutolaunchEnabled 与 MCP_ENABLE_DESTRUCTIVE 同样只认 "1"，其余取值一律当关闭。
func mcpAutolaunchEnabled() bool {
	return os.Getenv("MCP_AUTOLAUNCH") == "1"
}

// startMCPAutolaunch 在 HTTP 服务已就绪后调用，启动后立即返回。
// 开启时在后台逐个 launch 数据库里已存在的配置：客户端连上就能直接 navigate，
// 不必先调 launch_profile。单个配置启动失败只记日志，不影响其他配置与进程本身。
func startMCPAutolaunch() {
	if !mcpAutolaunchEnabled() {
		return
	}
	go autolaunchAllProfiles()
}

// autolaunchAllProfiles 先取完配置列表再逐个启动，避免启动过程中长时间持有 db 连接
// （launchProfileByID 自己也要查库）。
func autolaunchAllProfiles() {
	rows, err := db.Query("SELECT id, name FROM profiles ORDER BY group_id ASC, sort DESC, id ASC")
	if err != nil {
		log.Printf("[MCP] MCP_AUTOLAUNCH=1 但读取配置失败：%v", err)
		return
	}

	type target struct {
		id   string
		name string
	}
	var targets []target
	for rows.Next() {
		var rawID int64
		var name string
		if err := rows.Scan(&rawID, &name); err != nil {
			log.Printf("[MCP] 读取配置失败：%v", err)
			continue
		}
		targets = append(targets, target{encodeID(rawID), name})
	}
	rows.Close()

	if len(targets) == 0 {
		log.Printf("[MCP] MCP_AUTOLAUNCH=1，但数据库里没有配置，无可自动启动的对象")
		return
	}

	log.Printf("[MCP] MCP_AUTOLAUNCH=1，自动启动 %d 个配置", len(targets))
	for i, t := range targets {
		if i > 0 {
			time.Sleep(time.Second)
		}
		if isRunning(t.id) {
			continue
		}
		if err := launchProfileByID(t.id); err != nil {
			log.Printf("[MCP] 自动启动配置「%s」失败：%v", t.name, err)
			continue
		}
		log.Printf("[MCP] 已自动启动配置「%s」", t.name)
	}
}
