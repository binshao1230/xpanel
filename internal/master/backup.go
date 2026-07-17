package master

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

func (s *ServerApp) handleBackupExport(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"version":   1,
		"exported":  time.Now().Unix(),
		"tables":    map[string]any{},
	}
	tables := []string{"users", "plans", "servers", "inbounds", "outbounds", "route_rules", "external_nodes", "certificates", "settings", "traffic_daily"}
	tm := out["tables"].(map[string]any)
	for _, t := range tables {
		rows, err := exportTable(s.db, t)
		if err != nil {
			continue
		}
		tm[t] = rows
	}
	// scrub agent keys? keep them for restore; strip password hashes is bad for restore
	w.Header().Set("Content-Disposition", "attachment; filename=xpanel-backup.json")
	writeJSON(w, 200, out)
}

func exportTable(db *sql.DB, table string) ([]map[string]any, error) {
	rows, err := db.Query("SELECT * FROM " + table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var list []map[string]any
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		m := map[string]any{}
		for i, c := range cols {
			v := raw[i]
			switch t := v.(type) {
			case []byte:
				m[c] = string(t)
			default:
				m[c] = t
			}
		}
		// never export password hashes in plain API for non-admin — already adminOnly
		list = append(list, m)
	}
	if list == nil {
		list = []map[string]any{}
	}
	return list, nil
}

func (s *ServerApp) handleBackupImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Tables map[string][]map[string]any `json:"tables"`
		Mode   string                      `json:"mode"` // merge | replace (replace not full wipe)
	}
	if err := readJSON(r, &body); err != nil || body.Tables == nil {
		writeJSON(w, 400, map[string]string{"error": "tables required"})
		return
	}
	// only import settings + external_nodes + plans safely by default; full restore is dangerous
	imported := map[string]int{}
	if rows, ok := body.Tables["settings"]; ok {
		n := 0
		for _, row := range rows {
			k, _ := row["key"].(string)
			v, _ := row["value"].(string)
			if k == "" {
				continue
			}
			_, _ = s.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v)
			n++
		}
		imported["settings"] = n
	}
	if rows, ok := body.Tables["plans"]; ok {
		n := 0
		for _, row := range rows {
			name, _ := row["name"].(string)
			if name == "" {
				continue
			}
			tl := asInt64(row["traffic_limit"])
			sl := asInt64(row["speed_limit"])
			days := int(asInt64(row["duration_days"]))
			note, _ := row["price_note"].(string)
			_, err := s.db.Exec(
				`INSERT INTO plans(name,traffic_limit,speed_limit,duration_days,price_note,enabled,created_at) VALUES(?,?,?,?,?,1,?)
				 ON CONFLICT(name) DO UPDATE SET traffic_limit=excluded.traffic_limit, speed_limit=excluded.speed_limit, duration_days=excluded.duration_days, price_note=excluded.price_note`,
				name, tl, sl, days, note, nowUnix(),
			)
			if err == nil {
				n++
			}
		}
		imported["plans"] = n
	}
	if rows, ok := body.Tables["external_nodes"]; ok {
		n := 0
		for _, row := range rows {
			name, _ := row["name"].(string)
			link, _ := row["share_link"].(string)
			if link == "" {
				continue
			}
			proto, _ := row["protocol"].(string)
			addr, _ := row["address"].(string)
			port := int(asInt64(row["port"]))
			raw, _ := json.Marshal(row)
			_, err := s.db.Exec(
				`INSERT INTO external_nodes(name,protocol,address,port,share_link,raw_json,enabled,created_at) VALUES(?,?,?,?,?,?,1,?)`,
				name, proto, addr, port, link, string(raw), nowUnix(),
			)
			if err == nil {
				n++
			}
		}
		imported["external_nodes"] = n
	}
	writeJSON(w, 200, map[string]any{"ok": true, "imported": imported})
}

func asInt64(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	case json.Number:
		i, _ := t.Int64()
		return i
	default:
		return 0
	}
}
