package master

import (
	"encoding/json"
	"net/http"
	"time"
)

// Minimal MCP-like JSON tool bridge for automation (OpenClaw/Hermes style).
// POST /api/mcp  { "method": "tools/list" | "tools/call", "params": {...} }

func (s *ServerApp) handleMCP(w http.ResponseWriter, r *http.Request) {
	// allow bearer or leave to auth middleware
	var body struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad json"})
		return
	}
	switch body.Method {
	case "tools/list", "list_tools":
		writeJSON(w, 200, map[string]any{
			"tools": []map[string]any{
				{"name": "list_servers", "description": "List managed servers"},
				{"name": "list_users", "description": "List users"},
				{"name": "dashboard", "description": "Dashboard counters"},
				{"name": "traffic", "description": "Traffic summary"},
				{"name": "speedtest_batch", "description": "TCP probe all inbounds"},
				{"name": "bump_config", "description": "params: {server_id}", "input": map[string]string{"server_id": "string"}},
			},
		})
	case "tools/call", "call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(body.Params, &p); err != nil {
			// also allow flat {name, server_id}
			_ = json.Unmarshal(body.Params, &p.Arguments)
			if n, ok := p.Arguments["name"].(string); ok {
				p.Name = n
			}
		}
		s.mcpCall(w, p.Name, p.Arguments)
	default:
		writeJSON(w, 400, map[string]string{"error": "unknown method"})
	}
}

func (s *ServerApp) mcpCall(w http.ResponseWriter, name string, args map[string]any) {
	switch name {
	case "list_servers":
		rows, err := s.db.Query(`SELECT id,name,public_ip,status,last_seen,xray_running,config_version FROM servers ORDER BY created_at DESC`)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		var list []map[string]any
		for rows.Next() {
			var id, name, ip, st string
			var ls, cv int64
			var xr int
			_ = rows.Scan(&id, &name, &ip, &st, &ls, &xr, &cv)
			list = append(list, map[string]any{"id": id, "name": name, "public_ip": ip, "status": st, "last_seen": ls, "xray_running": xr == 1, "config_version": cv})
		}
		writeJSON(w, 200, map[string]any{"content": list})
	case "list_users":
		rows, err := s.db.Query(`SELECT id,username,role,traffic_used,traffic_limit,expire_at,enabled FROM users`)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		var list []map[string]any
		for rows.Next() {
			var id int64
			var user, role string
			var used, lim, exp int64
			var en int
			_ = rows.Scan(&id, &user, &role, &used, &lim, &exp, &en)
			list = append(list, map[string]any{"id": id, "username": user, "role": role, "traffic_used": used, "traffic_limit": lim, "expire_at": exp, "enabled": en == 1})
		}
		writeJSON(w, 200, map[string]any{"content": list})
	case "dashboard":
		var users, servers, online int
		_ = s.db.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&users)
		_ = s.db.QueryRow(`SELECT COUNT(1) FROM servers`).Scan(&servers)
		_ = s.db.QueryRow(`SELECT COUNT(1) FROM servers WHERE last_seen > ?`, nowUnix()-45).Scan(&online)
		writeJSON(w, 200, map[string]any{"content": map[string]any{"users": users, "servers": servers, "online": online}})
	case "traffic":
		var up, down int64
		_ = s.db.QueryRow(`SELECT COALESCE(SUM(traffic_up),0), COALESCE(SUM(traffic_down),0) FROM servers`).Scan(&up, &down)
		writeJSON(w, 200, map[string]any{"content": map[string]any{"up": up, "down": down}})
	case "speedtest_batch":
		rows, err := s.db.Query(`
SELECT i.id, i.port, s.public_ip, i.tag, s.name FROM inbounds i
JOIN servers s ON s.id=i.server_id WHERE i.enabled=1 AND s.public_ip!='' LIMIT 30`)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		var results []map[string]any
		for rows.Next() {
			var id int64
			var port int
			var ip, tag, sname string
			if rows.Scan(&id, &port, &ip, &tag, &sname) != nil {
				continue
			}
			res := probeTarget(ip, port, false, 3*time.Second)
			results = append(results, map[string]any{"inbound_id": id, "tag": tag, "server": sname, "result": res})
		}
		writeJSON(w, 200, map[string]any{"content": results})
	case "bump_config":
		sid, _ := args["server_id"].(string)
		if sid == "" {
			s.bumpAllServers()
		} else {
			s.bumpServer(sid)
			if s.hub != nil {
				s.hub.PushCommand(sid, protocolCmdReload())
			}
		}
		writeJSON(w, 200, map[string]any{"content": map[string]any{"ok": true}})
	default:
		writeJSON(w, 400, map[string]string{"error": "unknown tool: " + name})
	}
}

func protocolCmdReload() string { return "reload_config" }
