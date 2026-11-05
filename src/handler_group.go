package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func getGroups(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, name, sort, created_at, updated_at FROM groups ORDER BY sort DESC, created_at DESC")
	if err != nil {
		writeJSON(w, Response[any]{Code: 500, Message: err.Error()})
		return
	}
	defer rows.Close()

	var list []Group
	for rows.Next() {
		var g Group
		var rawID int64
		if err := rows.Scan(&rawID, &g.Name, &g.Sort, &g.CreatedAt, &g.UpdatedAt); err == nil {
			g.ID = encodeID(rawID)
			list = append(list, g)
		}
	}
	writeJSON(w, Response[any]{Code: 200, Message: "success", Data: list})
}

func addGroup(w http.ResponseWriter, r *http.Request) {
	var g Group
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}
	g.CreatedAt = time.Now().UnixMilli()
	g.UpdatedAt = g.CreatedAt

	res, err := db.Exec("INSERT INTO groups (name, sort, created_at, updated_at) VALUES (?, ?, ?, ?)",
		g.Name, g.Sort, g.CreatedAt, g.UpdatedAt)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "UNIQUE") {
			msg = "分组名称已存在！"
		}
		writeJSON(w, Response[any]{Code: 500, Message: msg})
		return
	}
	rowID, _ := res.LastInsertId()
	g.ID = encodeID(rowID)
	writeJSON(w, Response[any]{Code: 200, Message: "success", Data: g})
}

func updateGroup(w http.ResponseWriter, r *http.Request) {
	var g Group
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}
	g.UpdatedAt = time.Now().UnixMilli()

	rawID := decodeID(g.ID)
	_, err := db.Exec("UPDATE groups SET name=?, sort=?, updated_at=? WHERE id=?",
		g.Name, g.Sort, g.UpdatedAt, rawID)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "UNIQUE") {
			msg = "分组名称已存在！"
		}
		writeJSON(w, Response[any]{Code: 500, Message: msg})
		return
	}
	writeJSON(w, Response[any]{Code: 200, Message: "success", Data: g})
}

func deleteGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}
	if err := deleteGroupByID(req.ID); err != nil {
		writeJSON(w, Response[any]{Code: 500, Message: err.Error()})
		return
	}
	writeJSON(w, Response[any]{Code: 200, Message: "success"})
}

// deleteGroupByID removes a group and moves its profiles to the default group.
func deleteGroupByID(idStr string) error {
	rawID := decodeID(idStr)

	// Use transaction to delete group and move its profiles to default group
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM groups WHERE id=?", rawID); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE profiles SET group_id=0 WHERE group_id=?", rawID); err != nil {
		return err
	}
	return tx.Commit()
}
