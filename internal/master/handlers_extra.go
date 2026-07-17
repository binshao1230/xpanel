package master

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xpanel/xpanel/internal/sub"
)

func (s *ServerApp) adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return s.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		c := userFrom(r.Context())
		if c == nil || c.Role != "admin" {
			writeJSON(w, 403, map[string]string{"error": "admin only"})
			return
		}
		next(w, r)
	})
}

// ---- Plans ----

func (s *ServerApp) handleListPlans(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id,name,traffic_limit,speed_limit,duration_days,price_note,enabled,created_at FROM plans ORDER BY id`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	list := []Plan{}
	for rows.Next() {
		var p Plan
		var en int
		if err := rows.Scan(&p.ID, &p.Name, &p.TrafficLimit, &p.SpeedLimit, &p.DurationDays, &p.PriceNote, &en, &p.CreatedAt); err != nil {
			continue
		}
		p.Enabled = en == 1
		list = append(list, p)
	}
	writeJSON(w, 200, map[string]any{"plans": list})
}

func (s *ServerApp) handleCreatePlan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name         string `json:"name"`
		TrafficLimit int64  `json:"traffic_limit"`
		SpeedLimit   int64  `json:"speed_limit"`
		DurationDays int    `json:"duration_days"`
		PriceNote    string `json:"price_note"`
	}
	if err := readJSON(r, &body); err != nil || body.Name == "" {
		writeJSON(w, 400, map[string]string{"error": "name required"})
		return
	}
	if body.DurationDays <= 0 {
		body.DurationDays = 30
	}
	res, err := s.db.Exec(
		`INSERT INTO plans(name,traffic_limit,speed_limit,duration_days,price_note,enabled,created_at) VALUES(?,?,?,?,?,1,?)`,
		body.Name, body.TrafficLimit, body.SpeedLimit, body.DurationDays, body.PriceNote, nowUnix(),
	)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *ServerApp) handleDeletePlan(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_, err := s.db.Exec(`DELETE FROM plans WHERE id=?`, id)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---- Users ----

func (s *ServerApp) handleListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id,username,role,subscribe_token,plan_id,traffic_limit,traffic_used,speed_limit,expire_at,enabled,remark,created_at FROM users ORDER BY id`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	list := []User{}
	for rows.Next() {
		var u User
		var en int
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.SubscribeToken, &u.PlanID, &u.TrafficLimit, &u.TrafficUsed, &u.SpeedLimit, &u.ExpireAt, &en, &u.Remark, &u.CreatedAt); err != nil {
			continue
		}
		u.Enabled = en == 1
		list = append(list, u)
	}
	writeJSON(w, 200, map[string]any{"users": list})
}

func (s *ServerApp) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
		PlanID   int64  `json:"plan_id"`
		Remark   string `json:"remark"`
	}
	if err := readJSON(r, &body); err != nil || body.Username == "" || len(body.Password) < 6 {
		writeJSON(w, 400, map[string]string{"error": "username and password(>=6) required"})
		return
	}
	if body.Role == "" {
		body.Role = "user"
	}
	hash, err := hashPassword(body.Password)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "hash failed"})
		return
	}
	var trafficLimit, speedLimit int64
	var expireAt int64
	if body.PlanID > 0 {
		var days int
		_ = s.db.QueryRow(`SELECT traffic_limit,speed_limit,duration_days FROM plans WHERE id=?`, body.PlanID).
			Scan(&trafficLimit, &speedLimit, &days)
		if days > 0 {
			expireAt = time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix()
		}
	}
	tok := randomHex(16)
	res, err := s.db.Exec(
		`INSERT INTO users(username,password_hash,role,subscribe_token,plan_id,traffic_limit,traffic_used,speed_limit,expire_at,enabled,remark,created_at)
		 VALUES(?,?,?,?,?,?,0,?,?,1,?,?)`,
		body.Username, hash, body.Role, tok, body.PlanID, trafficLimit, speedLimit, expireAt, body.Remark, nowUnix(),
	)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, 200, map[string]any{"id": id, "subscribe_token": tok})
}

func (s *ServerApp) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var body struct {
		PlanID       *int64  `json:"plan_id"`
		TrafficLimit *int64  `json:"traffic_limit"`
		Enabled      *bool   `json:"enabled"`
		Remark       *string `json:"remark"`
		Password     *string `json:"password"`
		RenewDays    *int    `json:"renew_days"`
		ResetTraffic *bool   `json:"reset_traffic"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad json"})
		return
	}
	if body.PlanID != nil {
		_, _ = s.db.Exec(`UPDATE users SET plan_id=? WHERE id=?`, *body.PlanID, id)
		if *body.PlanID > 0 {
			var tl, sl int64
			var days int
			if err := s.db.QueryRow(`SELECT traffic_limit,speed_limit,duration_days FROM plans WHERE id=?`, *body.PlanID).
				Scan(&tl, &sl, &days); err == nil {
				_, _ = s.db.Exec(`UPDATE users SET traffic_limit=?, speed_limit=? WHERE id=?`, tl, sl, id)
			}
		}
	}
	if body.TrafficLimit != nil {
		_, _ = s.db.Exec(`UPDATE users SET traffic_limit=? WHERE id=?`, *body.TrafficLimit, id)
	}
	if body.Enabled != nil {
		en := 0
		if *body.Enabled {
			en = 1
		}
		_, _ = s.db.Exec(`UPDATE users SET enabled=? WHERE id=?`, en, id)
	}
	if body.Remark != nil {
		_, _ = s.db.Exec(`UPDATE users SET remark=? WHERE id=?`, *body.Remark, id)
	}
	if body.Password != nil && len(*body.Password) >= 6 {
		hash, err := hashPassword(*body.Password)
		if err == nil {
			_, _ = s.db.Exec(`UPDATE users SET password_hash=? WHERE id=?`, hash, id)
		}
	}
	if body.RenewDays != nil && *body.RenewDays > 0 {
		var exp int64
		_ = s.db.QueryRow(`SELECT expire_at FROM users WHERE id=?`, id).Scan(&exp)
		base := time.Now()
		if exp > base.Unix() {
			base = time.Unix(exp, 0)
		}
		_, _ = s.db.Exec(`UPDATE users SET expire_at=? WHERE id=?`, base.Add(time.Duration(*body.RenewDays)*24*time.Hour).Unix(), id)
	}
	if body.ResetTraffic != nil && *body.ResetTraffic {
		_, _ = s.db.Exec(`UPDATE users SET traffic_used=0 WHERE id=?`, id)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *ServerApp) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	c := userFrom(r.Context())
	if c != nil && c.UserID == id {
		writeJSON(w, 400, map[string]string{"error": "cannot delete self"})
		return
	}
	_, err := s.db.Exec(`DELETE FROM users WHERE id=?`, id)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---- Outbounds ----

func (s *ServerApp) handleListOutbounds(w http.ResponseWriter, r *http.Request) {
	serverID := r.URL.Query().Get("server_id")
	var rows *sql.Rows
	var err error
	if serverID != "" {
		rows, err = s.db.Query(`SELECT id,server_id,tag,protocol,settings_json,stream_json,enabled,created_at FROM outbounds WHERE server_id=? ORDER BY id`, serverID)
	} else {
		rows, err = s.db.Query(`SELECT id,server_id,tag,protocol,settings_json,stream_json,enabled,created_at FROM outbounds ORDER BY id`)
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	list := []Outbound{}
	for rows.Next() {
		var o Outbound
		var en int
		if err := rows.Scan(&o.ID, &o.ServerID, &o.Tag, &o.Protocol, &o.SettingsJSON, &o.StreamJSON, &en, &o.CreatedAt); err != nil {
			continue
		}
		o.Enabled = en == 1
		list = append(list, o)
	}
	writeJSON(w, 200, map[string]any{"outbounds": list})
}

func (s *ServerApp) handleCreateOutbound(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ServerID string         `json:"server_id"`
		Tag      string         `json:"tag"`
		Protocol string         `json:"protocol"`
		Settings map[string]any `json:"settings"`
		Stream   map[string]any `json:"stream"`
	}
	if err := readJSON(r, &body); err != nil || body.ServerID == "" || body.Tag == "" || body.Protocol == "" {
		writeJSON(w, 400, map[string]string{"error": "server_id, tag, protocol required"})
		return
	}
	if body.Settings == nil {
		body.Settings = map[string]any{}
	}
	if body.Stream == nil {
		body.Stream = map[string]any{}
	}
	sj, _ := json.Marshal(body.Settings)
	st, _ := json.Marshal(body.Stream)
	res, err := s.db.Exec(
		`INSERT INTO outbounds(server_id,tag,protocol,settings_json,stream_json,enabled,created_at) VALUES(?,?,?,?,?,1,?)`,
		body.ServerID, body.Tag, body.Protocol, string(sj), string(st), nowUnix(),
	)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_, _ = s.db.Exec(`UPDATE servers SET config_version = config_version + 1 WHERE id=?`, body.ServerID)
	id, _ := res.LastInsertId()
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *ServerApp) handleDeleteOutbound(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var sid string
	if err := s.db.QueryRow(`SELECT server_id FROM outbounds WHERE id=?`, id).Scan(&sid); err != nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	_, _ = s.db.Exec(`DELETE FROM outbounds WHERE id=?`, id)
	s.bumpServer(sid)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *ServerApp) handleUpdateOutbound(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var body struct {
		Enabled  *bool          `json:"enabled"`
		Settings map[string]any `json:"settings"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad json"})
		return
	}
	var sid string
	if err := s.db.QueryRow(`SELECT server_id FROM outbounds WHERE id=?`, id).Scan(&sid); err != nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	if body.Enabled != nil {
		en := 0
		if *body.Enabled {
			en = 1
		}
		_, _ = s.db.Exec(`UPDATE outbounds SET enabled=? WHERE id=?`, en, id)
	}
	if body.Settings != nil {
		sj, _ := json.Marshal(body.Settings)
		_, _ = s.db.Exec(`UPDATE outbounds SET settings_json=? WHERE id=?`, string(sj), id)
	}
	s.bumpServer(sid)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---- Routes ----

func (s *ServerApp) handleListRoutes(w http.ResponseWriter, r *http.Request) {
	serverID := r.URL.Query().Get("server_id")
	var rows *sql.Rows
	var err error
	if serverID != "" {
		rows, err = s.db.Query(`SELECT id,server_id,name,outbound_tag,domain_json,ip_json,port,network,protocol_json,priority,enabled,created_at FROM route_rules WHERE server_id=? ORDER BY priority,id`, serverID)
	} else {
		rows, err = s.db.Query(`SELECT id,server_id,name,outbound_tag,domain_json,ip_json,port,network,protocol_json,priority,enabled,created_at FROM route_rules ORDER BY priority,id`)
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	list := []RouteRule{}
	for rows.Next() {
		var rr RouteRule
		var en int
		if err := rows.Scan(&rr.ID, &rr.ServerID, &rr.Name, &rr.OutboundTag, &rr.DomainJSON, &rr.IPJSON, &rr.Port, &rr.Network, &rr.ProtocolJSON, &rr.Priority, &en, &rr.CreatedAt); err != nil {
			continue
		}
		rr.Enabled = en == 1
		list = append(list, rr)
	}
	writeJSON(w, 200, map[string]any{"routes": list})
}

func (s *ServerApp) handleCreateRoute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ServerID    string   `json:"server_id"`
		Name        string   `json:"name"`
		OutboundTag string   `json:"outbound_tag"`
		Domain      []string `json:"domain"`
		IP          []string `json:"ip"`
		Port        string   `json:"port"`
		Network     string   `json:"network"`
		Protocol    []string `json:"protocol"`
		Priority    int      `json:"priority"`
	}
	if err := readJSON(r, &body); err != nil || body.ServerID == "" || body.OutboundTag == "" {
		writeJSON(w, 400, map[string]string{"error": "server_id and outbound_tag required"})
		return
	}
	if body.Name == "" {
		body.Name = body.OutboundTag
	}
	if body.Priority == 0 {
		body.Priority = 100
	}
	dj, _ := json.Marshal(body.Domain)
	ij, _ := json.Marshal(body.IP)
	pj, _ := json.Marshal(body.Protocol)
	res, err := s.db.Exec(
		`INSERT INTO route_rules(server_id,name,outbound_tag,domain_json,ip_json,port,network,protocol_json,priority,enabled,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,1,?)`,
		body.ServerID, body.Name, body.OutboundTag, string(dj), string(ij), body.Port, body.Network, string(pj), body.Priority, nowUnix(),
	)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_, _ = s.db.Exec(`UPDATE servers SET config_version = config_version + 1 WHERE id=?`, body.ServerID)
	id, _ := res.LastInsertId()
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *ServerApp) handleDeleteRoute(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var sid string
	if err := s.db.QueryRow(`SELECT server_id FROM route_rules WHERE id=?`, id).Scan(&sid); err != nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	_, _ = s.db.Exec(`DELETE FROM route_rules WHERE id=?`, id)
	_, _ = s.db.Exec(`UPDATE servers SET config_version = config_version + 1 WHERE id=?`, sid)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---- External nodes ----

func (s *ServerApp) handleListExtNodes(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id,name,protocol,address,port,share_link,raw_json,enabled,created_at FROM external_nodes ORDER BY id DESC`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	list := []ExternalNode{}
	for rows.Next() {
		var n ExternalNode
		var en int
		if err := rows.Scan(&n.ID, &n.Name, &n.Protocol, &n.Address, &n.Port, &n.ShareLink, &n.RawJSON, &en, &n.CreatedAt); err != nil {
			continue
		}
		n.Enabled = en == 1
		list = append(list, n)
	}
	writeJSON(w, 200, map[string]any{"nodes": list})
}

func (s *ServerApp) handleImportExtNode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Links string `json:"links"` // multi-line
		Name  string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil || strings.TrimSpace(body.Links) == "" {
		writeJSON(w, 400, map[string]string{"error": "links required"})
		return
	}
	var imported int
	for _, line := range strings.Split(body.Links, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		n, err := sub.ParseShareLink(line)
		if err != nil {
			continue
		}
		if body.Name != "" && imported == 0 {
			n.Name = body.Name
		}
		raw, _ := json.Marshal(n)
		_, err = s.db.Exec(
			`INSERT INTO external_nodes(name,protocol,address,port,share_link,raw_json,enabled,created_at) VALUES(?,?,?,?,?,?,1,?)`,
			n.Name, n.Protocol, n.Address, n.Port, n.ShareLink, string(raw), nowUnix(),
		)
		if err == nil {
			imported++
		}
	}
	writeJSON(w, 200, map[string]any{"imported": imported})
}

func (s *ServerApp) handleDeleteExtNode(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_, _ = s.db.Exec(`DELETE FROM external_nodes WHERE id=?`, id)
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---- Certificates ----

func (s *ServerApp) handleListCerts(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id,name,domain,provider,expire_at,created_at,
		COALESCE(email,''),COALESCE(challenge,''),COALESCE(dns_provider,''),COALESCE(status,'active'),COALESCE(last_error,''),COALESCE(auto_renew,1),COALESCE(server_id,'')
		FROM certificates ORDER BY id DESC`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	list := []Certificate{}
	for rows.Next() {
		var c Certificate
		var ar int
		if err := rows.Scan(&c.ID, &c.Name, &c.Domain, &c.Provider, &c.ExpireAt, &c.CreatedAt,
			&c.Email, &c.Challenge, &c.DNSProvider, &c.Status, &c.LastError, &ar, &c.ServerID); err != nil {
			continue
		}
		c.AutoRenew = ar == 1
		list = append(list, c)
	}
	writeJSON(w, 200, map[string]any{"certificates": list})
}

func (s *ServerApp) handleCreateCert(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Domain   string `json:"domain"`
		CertPEM  string `json:"cert_pem"`
		KeyPEM   string `json:"key_pem"`
		Provider string `json:"provider"`
	}
	if err := readJSON(r, &body); err != nil || body.Name == "" {
		writeJSON(w, 400, map[string]string{"error": "name required"})
		return
	}
	if body.Provider == "" {
		body.Provider = "manual"
	}
	res, err := s.db.Exec(
		`INSERT INTO certificates(name,domain,cert_pem,key_pem,provider,expire_at,created_at) VALUES(?,?,?,?,?,0,?)`,
		body.Name, body.Domain, body.CertPEM, body.KeyPEM, body.Provider, nowUnix(),
	)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *ServerApp) handleDeleteCert(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	_, _ = s.db.Exec(`DELETE FROM certificates WHERE id=?`, id)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *ServerApp) handleGetCert(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var c Certificate
	var ar int
	err := s.db.QueryRow(
		`SELECT id,name,domain,cert_pem,key_pem,provider,expire_at,created_at,
		 COALESCE(email,''),COALESCE(challenge,''),COALESCE(dns_provider,''),COALESCE(status,'active'),COALESCE(last_error,''),COALESCE(auto_renew,1)
		 FROM certificates WHERE id=?`, id,
	).Scan(&c.ID, &c.Name, &c.Domain, &c.CertPEM, &c.KeyPEM, &c.Provider, &c.ExpireAt, &c.CreatedAt,
		&c.Email, &c.Challenge, &c.DNSProvider, &c.Status, &c.LastError, &ar)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	c.AutoRenew = ar == 1
	writeJSON(w, 200, c)
}

// ---- Traffic ----

func (s *ServerApp) handleTrafficSummary(w http.ResponseWriter, r *http.Request) {
	var totalUp, totalDown int64
	_ = s.db.QueryRow(`SELECT COALESCE(SUM(traffic_up),0), COALESCE(SUM(traffic_down),0) FROM servers`).Scan(&totalUp, &totalDown)
	var userUsed int64
	_ = s.db.QueryRow(`SELECT COALESCE(SUM(traffic_used),0) FROM users`).Scan(&userUsed)

	rows, err := s.db.Query(`SELECT day,user_id,server_id,up,down FROM traffic_daily WHERE day >= date('now','-30 day') ORDER BY day`)
	if err != nil {
		// sqlite date may differ; fallback query all recent
		rows, err = s.db.Query(`SELECT day,user_id,server_id,up,down FROM traffic_daily ORDER BY day DESC LIMIT 200`)
	}
	days := []TrafficDay{}
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var d TrafficDay
			if err := rows.Scan(&d.Day, &d.UserID, &d.ServerID, &d.Up, &d.Down); err == nil {
				days = append(days, d)
			}
		}
	}
	writeJSON(w, 200, map[string]any{
		"server_up":   totalUp,
		"server_down": totalDown,
		"user_used":   userUsed,
		"daily":       days,
	})
}

// ---- Dashboard ----

func (s *ServerApp) handleDashboard(w http.ResponseWriter, r *http.Request) {
	var users, servers, online, inbounds, plans int
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&users)
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM servers`).Scan(&servers)
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM servers WHERE last_seen > ?`, nowUnix()-45).Scan(&online)
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM inbounds`).Scan(&inbounds)
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM plans`).Scan(&plans)
	var up, down int64
	_ = s.db.QueryRow(`SELECT COALESCE(SUM(traffic_up),0), COALESCE(SUM(traffic_down),0) FROM servers`).Scan(&up, &down)
	writeJSON(w, 200, map[string]any{
		"users": users, "servers": servers, "online": online,
		"inbounds": inbounds, "plans": plans,
		"traffic_up": up, "traffic_down": down,
		"version": "0.2.0",
	})
}

// ---- Settings ----

func (s *ServerApp) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT key,value FROM settings`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err == nil {
			m[k] = v
		}
	}
	// defaults — softer auto theme by default
	if _, ok := m["site_name"]; !ok {
		m["site_name"] = "XPanel"
	}
	if _, ok := m["theme"]; !ok {
		m["theme"] = "auto"
	}
	writeJSON(w, 200, map[string]any{"settings": m})
}

func (s *ServerApp) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad json"})
		return
	}
	for k, v := range body {
		_, _ = s.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---- Reality quick create helper ----

func (s *ServerApp) handleQuickReality(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ServerID string `json:"server_id"`
		Port     int    `json:"port"`
		Dest     string `json:"dest"`
		SNI      string `json:"sni"`
	}
	if err := readJSON(r, &body); err != nil || body.ServerID == "" {
		writeJSON(w, 400, map[string]string{"error": "server_id required"})
		return
	}
	if body.Port <= 0 {
		body.Port = 443
	}
	if body.Dest == "" {
		body.Dest = "www.cloudflare.com:443"
	}
	if body.SNI == "" {
		body.SNI = "www.cloudflare.com"
	}
	// placeholder keys — operator should replace with xray x25519 output
	priv := "REPLACE_WITH_X25519_PRIVATE"
	pub := "REPLACE_WITH_X25519_PUBLIC"
	shortID := randomHex(4)
	clientID := uuid.NewString()
	settings := map[string]any{
		"clients":    []map[string]any{{"id": clientID, "email": "reality@xpanel", "flow": "xtls-rprx-vision"}},
		"decryption": "none",
	}
	stream := map[string]any{
		"network":  "tcp",
		"security": "reality",
		"realitySettings": map[string]any{
			"show":        false,
			"dest":        body.Dest,
			"serverNames": []string{body.SNI},
			"privateKey":  priv,
			"shortIds":    []string{shortID},
		},
	}
	sj, _ := json.Marshal(settings)
	st, _ := json.Marshal(stream)
	tag := "vless-reality-" + strconv.Itoa(body.Port)
	res, err := s.db.Exec(
		`INSERT INTO inbounds(server_id,tag,protocol,port,settings_json,stream_json,multiplier,enabled,created_at) VALUES(?,?,?,?,?,?,1,1,?)`,
		body.ServerID, tag, "vless", body.Port, string(sj), string(st), nowUnix(),
	)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_, _ = s.db.Exec(`UPDATE servers SET config_version = config_version + 1 WHERE id=?`, body.ServerID)
	id, _ := res.LastInsertId()
	writeJSON(w, 200, map[string]any{
		"id": id, "tag": tag, "client_id": clientID,
		"note": "请用 xray x25519 生成密钥后编辑 inbound stream_json 替换 privateKey，并在客户端填 publicKey",
		"public_key_placeholder": pub,
		"short_id":               shortID,
	})
}
