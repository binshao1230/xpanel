package master

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	xpcrypto "github.com/binshao1230/bpanel/internal/crypto"
	"github.com/binshao1230/bpanel/internal/version"
	"github.com/binshao1230/bpanel/internal/xraycfg"
)

func (s *ServerApp) audit(actor, action, detail string) {
	_, _ = s.db.Exec(`INSERT INTO audit_logs(actor,action,detail,created_at) VALUES(?,?,?,?)`,
		actor, action, detail, nowUnix())
}

// ---- Reality with real x25519 ----

func (s *ServerApp) handleQuickRealityV5(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ServerID string `json:"server_id"`
		Port     int    `json:"port"`
		Dest     string `json:"dest"`
		SNI      string `json:"sni"`
		Flow     string `json:"flow"`
		Name     string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil || body.ServerID == "" {
		writeJSON(w, 400, map[string]string{"error": "server_id required"})
		return
	}
	// verify server exists
	var srvName string
	if err := s.db.QueryRow(`SELECT name FROM servers WHERE id=?`, body.ServerID).Scan(&srvName); err != nil {
		writeJSON(w, 404, map[string]string{"error": "server not found"})
		return
	}
	if body.Port <= 0 {
		body.Port = 443
	}
	body.SNI = strings.TrimSpace(body.SNI)
	body.Dest = strings.TrimSpace(body.Dest)
	if body.SNI == "" {
		body.SNI = "www.microsoft.com"
	}
	if body.Dest == "" {
		body.Dest = body.SNI + ":443"
	} else if !strings.Contains(body.Dest, ":") {
		body.Dest = body.Dest + ":443"
	}
	// dest host should match SNI family when operator only filled one side
	if body.SNI == "" {
		host := body.Dest
		if i := strings.LastIndex(host, ":"); i > 0 {
			host = host[:i]
		}
		body.SNI = host
	}
	if body.Flow == "" {
		body.Flow = "xtls-rprx-vision"
	}
	priv, pub, err := xpcrypto.X25519Pair()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	// double-check pair consistency (client pbk must match server privateKey)
	if derived, err := xpcrypto.PublicFromPrivate(priv); err == nil && derived != "" {
		pub = derived
	}
	shortID, _ := xpcrypto.RandomShortID()
	clientID := uuid.NewString()
	settings := map[string]any{
		"clients":    []map[string]any{{"id": clientID, "email": "reality@bpanel", "flow": body.Flow}},
		"decryption": "none",
		// panel meta (stripped before agent deploy; kept for share links)
		"bpanelMeta": map[string]any{
			"publicKey":   pub,
			"shortId":     shortID,
			"fingerprint": "chrome",
		},
	}
	stream := map[string]any{
		"network":  "tcp",
		"security": "reality",
		"realitySettings": map[string]any{
			"show":        false,
			"dest":        body.Dest,
			"xver":        0,
			"serverNames": []string{body.SNI},
			"privateKey":  priv,
			"shortIds":    []string{shortID, ""},
		},
	}
	// validate deployable before insert
	if _, _, skip := xraycfg.SanitizeInbound("vless", body.Port, settings, stream); skip != "" {
		writeJSON(w, 400, map[string]string{"error": "reality 配置无效: " + skip})
		return
	}
	sj, _ := json.Marshal(settings)
	st, _ := json.Marshal(stream)
	tag := strings.TrimSpace(body.Name)
	if tag == "" {
		tag = fmt.Sprintf("vless-reality-%d", body.Port)
	}
	res, err := s.db.Exec(
		`INSERT INTO inbounds(server_id,tag,protocol,port,settings_json,stream_json,multiplier,enabled,created_at,share_name) VALUES(?,?,?,?,?,?,1,1,?,?)`,
		body.ServerID, tag, "vless", body.Port, string(sj), string(st), nowUnix(), tag,
	)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	s.bumpServer(body.ServerID)
	s.audit("admin", "create_reality", tag)

	// prefer domain over public_ip for share links (same as list links)
	var domain, ip string
	_ = s.db.QueryRow(`SELECT COALESCE(domain,''), COALESCE(public_ip,'') FROM servers WHERE id=?`, body.ServerID).Scan(&domain, &ip)
	addr := strings.TrimSpace(domain)
	if addr == "" {
		addr = strings.TrimSpace(ip)
	}
	if addr == "" {
		addr = "YOUR_IP"
	}
	link := buildVLESSRealityLink(clientID, addr, body.Port, body.Flow, body.SNI, pub, shortID, "chrome", tag)
	writeJSON(w, 200, map[string]any{
		"id": id, "tag": tag, "client_id": clientID,
		"private_key": priv, "public_key": pub, "short_id": shortID,
		"share_link": link,
		"sni":        body.SNI,
		"dest":       body.Dest,
		"server":     srvName,
		"address":    addr,
		"note":       "请确认服务器防火墙已放行端口，且 Agent 在线已拉取配置。dest 站点需可被节点机器访问。",
	})
}

func buildVLESSRealityLink(uuidStr, addr string, port int, flow, sni, pbk, sid, fp, name string) string {
	q := url.Values{}
	q.Set("encryption", "none")
	q.Set("security", "reality")
	q.Set("type", "tcp")
	if flow != "" {
		q.Set("flow", flow)
	}
	if sni != "" {
		q.Set("sni", sni)
	}
	if fp == "" {
		fp = "chrome"
	}
	q.Set("fp", fp)
	if pbk != "" {
		q.Set("pbk", pbk)
	}
	if sid != "" {
		q.Set("sid", sid)
	}
	frag := url.PathEscape(name)
	if frag == "" {
		frag = "reality"
	}
	return fmt.Sprintf("vless://%s@%s:%d?%s#%s", uuidStr, addr, port, q.Encode(), frag)
}

func urlQueryEscape(s string) string {
	return url.QueryEscape(s)
}

// ---- Share links for all inbounds ----

func (s *ServerApp) handleInboundLinks(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
SELECT i.id, i.tag, i.protocol, i.port, i.settings_json, i.stream_json, s.public_ip, s.domain, s.name, COALESCE(i.share_name,'')
FROM inbounds i JOIN servers s ON s.id=i.server_id WHERE i.enabled=1 ORDER BY i.id DESC`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	var list []map[string]any
	for rows.Next() {
		var id int64
		var tag, proto, sj, st, ip, domain, sname, shareName string
		var port int
		if err := rows.Scan(&id, &tag, &proto, &port, &sj, &st, &ip, &domain, &sname, &shareName); err != nil {
			continue
		}
		addr := domain
		if addr == "" {
			addr = ip
		}
		if addr == "" {
			addr = "0.0.0.0"
		}
		name := shareName
		if name == "" {
			name = sname + "-" + tag
		}
		link := buildShareLink(proto, name, addr, port, sj, st)
		list = append(list, map[string]any{
			"id": id, "name": name, "protocol": proto, "address": addr, "port": port, "link": link,
		})
	}
	writeJSON(w, 200, map[string]any{"links": list})
}

func buildShareLink(proto, name, addr string, port int, settingsJSON, streamJSON string) string {
	var settings, stream map[string]any
	_ = json.Unmarshal([]byte(settingsJSON), &settings)
	_ = json.Unmarshal([]byte(streamJSON), &stream)
	uuidStr, password, flow := "", "", ""
	if clients, ok := settings["clients"].([]any); ok && len(clients) > 0 {
		if c0, ok := clients[0].(map[string]any); ok {
			uuidStr, _ = c0["id"].(string)
			password, _ = c0["password"].(string)
			flow, _ = c0["flow"].(string)
		}
	}
	if password == "" {
		password, _ = settings["password"].(string)
	}
	sec, _ := stream["security"].(string)
	netw, _ := stream["network"].(string)
	if netw == "" {
		netw = "tcp"
	}
	sni := ""
	pbk, sid, fp := "", "", "chrome"
	if sec == "reality" {
		if rs, ok := stream["realitySettings"].(map[string]any); ok {
			if names, ok := rs["serverNames"].([]any); ok && len(names) > 0 {
				sni, _ = names[0].(string)
			}
			// pick first non-empty shortId
			if shortIds, ok := rs["shortIds"].([]any); ok {
				for _, x := range shortIds {
					if s, _ := x.(string); strings.TrimSpace(s) != "" {
						sid = s
						break
					}
				}
			}
		}
		if meta, ok := settings["bpanelMeta"].(map[string]any); ok {
			pbk, _ = meta["publicKey"].(string)
			if sid == "" {
				sid, _ = meta["shortId"].(string)
			}
			if f, _ := meta["fingerprint"].(string); f != "" {
				fp = f
			}
		} else if meta, ok := settings["xpanelMeta"].(map[string]any); ok {
			// legacy XPanel key
			pbk, _ = meta["publicKey"].(string)
			if sid == "" {
				sid, _ = meta["shortId"].(string)
			}
		}
	}
	// extract socks/http user/pass from accounts
	accUser, accPass := "", ""
	if accs, ok := settings["accounts"].([]any); ok && len(accs) > 0 {
		if a0, ok := accs[0].(map[string]any); ok {
			accUser, _ = a0["user"].(string)
			accPass, _ = a0["pass"].(string)
		}
	}
	// hysteria auth
	hyAuth := password
	if users, ok := settings["users"].([]any); ok && len(users) > 0 {
		if u0, ok := users[0].(map[string]any); ok {
			if a, _ := u0["auth"].(string); a != "" {
				hyAuth = a
			}
			if p, _ := u0["password"].(string); p != "" && hyAuth == "" {
				hyAuth = p
			}
		}
	}
	if hyAuth == "" {
		if meta, ok := settings["bpanelMeta"].(map[string]any); ok {
			hyAuth, _ = meta["password"].(string)
		}
	}

	switch proto {
	case "vless":
		if strings.EqualFold(sec, "reality") {
			return buildVLESSRealityLink(uuidStr, addr, port, flow, sni, pbk, sid, fp, name)
		}
		q := url.Values{}
		q.Set("encryption", "none")
		q.Set("type", netw)
		if sec != "" {
			q.Set("security", sec)
		}
		if sni != "" {
			q.Set("sni", sni)
		}
		if flow != "" {
			q.Set("flow", flow)
		}
		return fmt.Sprintf("vless://%s@%s:%d?%s#%s", uuidStr, addr, port, q.Encode(), url.PathEscape(name))
	case "vmess":
		obj := map[string]any{"v": "2", "ps": name, "add": addr, "port": port, "id": uuidStr, "aid": 0, "net": netw, "type": "none", "tls": ""}
		if sec == "tls" {
			obj["tls"] = "tls"
		}
		raw, _ := json.Marshal(obj)
		return "vmess://" + b64(raw)
	case "trojan":
		q := ""
		if sec != "" {
			q = "security=" + sec
		}
		if sni != "" {
			if q != "" {
				q += "&"
			}
			q += "sni=" + sni
		}
		if q != "" {
			return fmt.Sprintf("trojan://%s@%s:%d?%s#%s", password, addr, port, q, urlQueryEscape(name))
		}
		return fmt.Sprintf("trojan://%s@%s:%d#%s", password, addr, port, urlQueryEscape(name))
	case "shadowsocks":
		method, _ := settings["method"].(string)
		if method == "" {
			method = "aes-256-gcm"
		}
		userinfo := b64([]byte(method + ":" + password))
		return fmt.Sprintf("ss://%s@%s:%d#%s", userinfo, addr, port, urlQueryEscape(name))
	case "socks", "mixed":
		if accUser != "" {
			return fmt.Sprintf("socks5://%s:%s@%s:%d#%s", accUser, accPass, addr, port, urlQueryEscape(name))
		}
		return fmt.Sprintf("socks5://%s:%d#%s", addr, port, urlQueryEscape(name))
	case "http":
		if accUser != "" {
			return fmt.Sprintf("http://%s:%s@%s:%d#%s", accUser, accPass, addr, port, urlQueryEscape(name))
		}
		return fmt.Sprintf("http://%s:%d#%s", addr, port, urlQueryEscape(name))
	case "hysteria", "hysteria2":
		// hy2://auth@host:port?sni=...#name
		q := "insecure=0"
		if sni != "" {
			q += "&sni=" + sni
		}
		return fmt.Sprintf("hy2://%s@%s:%d?%s#%s", hyAuth, addr, port, q, urlQueryEscape(name))
	case "anytls":
		// anytls://password@host:port?security=tls&sni=...#name
		pw := hyAuth
		if pw == "" {
			pw = password
		}
		q := "security=tls"
		if sni != "" {
			q += "&sni=" + sni
		}
		return fmt.Sprintf("anytls://%s@%s:%d?%s#%s", pw, addr, port, q, urlQueryEscape(name))
	case "wireguard":
		return fmt.Sprintf("wireguard://%s:%d#%s", addr, port, urlQueryEscape(name))
	default:
		return ""
	}
}

func b64(b []byte) string {
	const table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	// use std
	return stdB64(b)
}

// ---- Tunnels (port forward) ----

func (s *ServerApp) handleListTunnels(w http.ResponseWriter, r *http.Request) {
	sid := r.URL.Query().Get("server_id")
	var (
		rows interface {
			Next() bool
			Scan(dest ...any) error
			Close() error
		}
		err error
	)
	base := `SELECT id,server_id,name,listen_port,target_host,target_port,protocol,enabled,created_at FROM tunnels`
	if sid != "" {
		rows, err = s.db.Query(base+` WHERE server_id=? ORDER BY id DESC`, sid)
	} else {
		rows, err = s.db.Query(base + ` ORDER BY id DESC`)
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	list := []map[string]any{}
	for rows.Next() {
		var id, lp, tp, en, ca int64
		var serverID, name, th, proto string
		if err := rows.Scan(&id, &serverID, &name, &lp, &th, &tp, &proto, &en, &ca); err != nil {
			continue
		}
		list = append(list, map[string]any{
			"id": id, "server_id": serverID, "name": name, "listen_port": lp,
			"target_host": th, "target_port": tp, "protocol": proto, "enabled": en == 1, "created_at": ca,
		})
	}
	writeJSON(w, 200, map[string]any{"tunnels": list})
}

func (s *ServerApp) handleCreateTunnel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ServerID   string `json:"server_id"`
		Name       string `json:"name"`
		ListenPort int    `json:"listen_port"`
		TargetHost string `json:"target_host"`
		TargetPort int    `json:"target_port"`
		Protocol   string `json:"protocol"`
	}
	if err := readJSON(r, &body); err != nil || body.ServerID == "" || body.ListenPort <= 0 || body.TargetHost == "" || body.TargetPort <= 0 {
		writeJSON(w, 400, map[string]string{"error": "server_id, listen_port, target_host, target_port required"})
		return
	}
	if body.Name == "" {
		body.Name = fmt.Sprintf("fwd-%d", body.ListenPort)
	}
	if body.Protocol == "" {
		body.Protocol = "tcp"
	}
	res, err := s.db.Exec(
		`INSERT INTO tunnels(server_id,name,listen_port,target_host,target_port,protocol,enabled,created_at) VALUES(?,?,?,?,?,?,1,?)`,
		body.ServerID, body.Name, body.ListenPort, body.TargetHost, body.TargetPort, body.Protocol, nowUnix(),
	)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	// also create dokodemo-door style inbound via outbounds freedom - inject as inbound dokodemo
	settings := map[string]any{
		"address": body.TargetHost,
		"port":    body.TargetPort,
		"network": body.Protocol,
	}
	if body.Protocol == "tcp,udp" || body.Protocol == "tcp" {
		settings["network"] = "tcp,udp"
	}
	sj, _ := json.Marshal(settings)
	st, _ := json.Marshal(map[string]any{"network": "tcp"})
	_, _ = s.db.Exec(
		`INSERT INTO inbounds(server_id,tag,protocol,port,settings_json,stream_json,enabled,created_at,remark) VALUES(?,?,?,?,?,?,1,?,?)`,
		body.ServerID, body.Name, "dokodemo-door", body.ListenPort, string(sj), string(st), nowUnix(), "tunnel",
	)
	id, _ := res.LastInsertId()
	s.bumpServer(body.ServerID)
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *ServerApp) handleDeleteTunnel(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var sid, name string
	_ = s.db.QueryRow(`SELECT server_id,name FROM tunnels WHERE id=?`, id).Scan(&sid, &name)
	_, _ = s.db.Exec(`DELETE FROM tunnels WHERE id=?`, id)
	if name != "" {
		_, _ = s.db.Exec(`DELETE FROM inbounds WHERE server_id=? AND tag=? AND remark='tunnel'`, sid, name)
	}
	if sid != "" {
		s.bumpServer(sid)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---- Invite codes ----

func (s *ServerApp) handleListInvites(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id,code,plan_id,max_uses,used_count,expire_at,enabled,created_at FROM invite_codes ORDER BY id DESC`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	list := []map[string]any{}
	for rows.Next() {
		var id, planID, maxU, used, exp, en, ca int64
		var code string
		if err := rows.Scan(&id, &code, &planID, &maxU, &used, &exp, &en, &ca); err != nil {
			continue
		}
		list = append(list, map[string]any{
			"id": id, "code": code, "plan_id": planID, "max_uses": maxU, "used_count": used,
			"expire_at": exp, "enabled": en == 1, "created_at": ca,
		})
	}
	writeJSON(w, 200, map[string]any{"invites": list})
}

func (s *ServerApp) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PlanID   int64 `json:"plan_id"`
		MaxUses  int   `json:"max_uses"`
		Days     int   `json:"days"`
		Count    int   `json:"count"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad json"})
		return
	}
	if body.MaxUses <= 0 {
		body.MaxUses = 1
	}
	if body.Count <= 0 {
		body.Count = 1
	}
	if body.Count > 50 {
		body.Count = 50
	}
	var exp int64
	if body.Days > 0 {
		exp = time.Now().Add(time.Duration(body.Days) * 24 * time.Hour).Unix()
	}
	codes := []string{}
	for i := 0; i < body.Count; i++ {
		code := strings.ToUpper(randomHex(4))
		_, err := s.db.Exec(
			`INSERT INTO invite_codes(code,plan_id,max_uses,used_count,expire_at,enabled,created_at) VALUES(?,?,?,0,?,1,?)`,
			code, body.PlanID, body.MaxUses, exp, nowUnix(),
		)
		if err == nil {
			codes = append(codes, code)
		}
	}
	writeJSON(w, 200, map[string]any{"codes": codes})
}

func (s *ServerApp) handleRegisterInvite(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := readJSON(r, &body); err != nil || body.Username == "" || len(body.Password) < 6 || body.Code == "" {
		writeJSON(w, 400, map[string]string{"error": "username, password(>=6), code required"})
		return
	}
	var id, planID, maxU, used, exp, en int64
	err := s.db.QueryRow(
		`SELECT id,plan_id,max_uses,used_count,expire_at,enabled FROM invite_codes WHERE code=?`,
		strings.ToUpper(strings.TrimSpace(body.Code)),
	).Scan(&id, &planID, &maxU, &used, &exp, &en)
	if err != nil || en != 1 {
		writeJSON(w, 400, map[string]string{"error": "invalid invite code"})
		return
	}
	if exp > 0 && nowUnix() > exp {
		writeJSON(w, 400, map[string]string{"error": "invite expired"})
		return
	}
	if used >= maxU {
		writeJSON(w, 400, map[string]string{"error": "invite exhausted"})
		return
	}
	hash, err := hashPassword(body.Password)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "hash failed"})
		return
	}
	var tl, sl int64
	var days int
	if planID > 0 {
		_ = s.db.QueryRow(`SELECT traffic_limit,speed_limit,duration_days FROM plans WHERE id=?`, planID).Scan(&tl, &sl, &days)
	}
	var expireAt int64
	if days > 0 {
		expireAt = time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix()
	}
	tok := randomHex(16)
	short := randomHex(4)
	_, err = s.db.Exec(
		`INSERT INTO users(username,password_hash,role,subscribe_token,plan_id,traffic_limit,traffic_used,speed_limit,expire_at,enabled,remark,created_at,short_code,invite_code)
		 VALUES(?,?,?,?,?,?,0,?,?,1,?,?,?,?)`,
		body.Username, hash, "user", tok, planID, tl, sl, expireAt, "invite", nowUnix(), short, strings.ToUpper(body.Code),
	)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	_, _ = s.db.Exec(`UPDATE invite_codes SET used_count = used_count + 1 WHERE id=?`, id)
	writeJSON(w, 200, map[string]any{"ok": true, "subscribe_token": tok, "short_code": short})
}

// ---- Nginx config store ----

func (s *ServerApp) handleGetNginx(w http.ResponseWriter, r *http.Request) {
	sid := r.URL.Query().Get("server_id")
	var id int64
	var name, content string
	var updated int64
	err := s.db.QueryRow(
		`SELECT id,name,content,updated_at FROM nginx_configs WHERE server_id=? ORDER BY id DESC LIMIT 1`, sid,
	).Scan(&id, &name, &content, &updated)
	if err != nil {
		writeJSON(w, 200, map[string]any{"content": defaultNginxTemplate(), "server_id": sid})
		return
	}
	writeJSON(w, 200, map[string]any{"id": id, "name": name, "content": content, "updated_at": updated, "server_id": sid})
}

func (s *ServerApp) handlePutNginx(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ServerID string `json:"server_id"`
		Name     string `json:"name"`
		Content  string `json:"content"`
	}
	if err := readJSON(r, &body); err != nil || body.ServerID == "" {
		writeJSON(w, 400, map[string]string{"error": "server_id required"})
		return
	}
	if body.Name == "" {
		body.Name = "default"
	}
	var id int64
	_ = s.db.QueryRow(`SELECT id FROM nginx_configs WHERE server_id=? AND name=?`, body.ServerID, body.Name).Scan(&id)
	if id > 0 {
		_, _ = s.db.Exec(`UPDATE nginx_configs SET content=?, updated_at=? WHERE id=?`, body.Content, nowUnix(), id)
	} else {
		_, _ = s.db.Exec(`INSERT INTO nginx_configs(server_id,name,content,enabled,updated_at) VALUES(?,?,?,1,?)`,
			body.ServerID, body.Name, body.Content, nowUnix())
	}
	s.bumpServer(body.ServerID)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func defaultNginxTemplate() string {
	return `# managed by BPanel — 下发到 Agent 的 nginx 配置草稿
server {
    listen 80;
    server_name _;
    location /.well-known/acme-challenge/ {
        root /var/www/html;
    }
    location / {
        return 200 'ok';
        add_header Content-Type text/plain;
    }
}
`
}

// ---- WARP outbound helper ----

func (s *ServerApp) handleQuickWARP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ServerID string `json:"server_id"`
		Tag      string `json:"tag"`
	}
	if err := readJSON(r, &body); err != nil || body.ServerID == "" {
		writeJSON(w, 400, map[string]string{"error": "server_id required"})
		return
	}
	if body.Tag == "" {
		body.Tag = "warp"
	}
	// WireGuard placeholder — disabled until keys filled (enabled=0 prevents xray -test failure)
	settings := map[string]any{
		"secretKey": "REPLACE_WG_PRIVATE_KEY",
		"address":   []string{"172.16.0.2/32"},
		"peers": []map[string]any{
			{
				"publicKey":  "REPLACE_WG_PEER_PUBLIC",
				"endpoint":   "engage.cloudflareclient.com:2408",
				"allowedIPs": []string{"0.0.0.0/0", "::/0"},
			},
		},
	}
	sj, _ := json.Marshal(settings)
	_, err := s.db.Exec(
		`INSERT INTO outbounds(server_id,tag,protocol,settings_json,stream_json,enabled,created_at) VALUES(?,?,?,?,?,0,?)`,
		body.ServerID, body.Tag, "wireguard", string(sj), "{}", nowUnix(),
	)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	// domain rules without geosite.dat dependency
	dj, _ := json.Marshal([]string{
		"domain:openai.com", "domain:chatgpt.com", "domain:ai.com",
		"domain:netflix.com", "domain:nflxvideo.net",
	})
	_, _ = s.db.Exec(
		`INSERT INTO route_rules(server_id,name,outbound_tag,domain_json,ip_json,port,network,protocol_json,priority,enabled,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,0,?)`,
		body.ServerID, "to-warp", body.Tag, string(dj), "[]", "", "", "[]", 50, nowUnix(),
	)
	// do not bump until user enables — avoids breaking live nodes
	writeJSON(w, 200, map[string]any{
		"ok":   true,
		"tag":  body.Tag,
		"note": "WARP 出站与路由已创建但默认【未启用】。请填入 WireGuard 密钥后，在数据库/后续编辑中启用并下发。当前不会破坏现有 Xray 配置。",
	})
}

// ---- Audit ----

func (s *ServerApp) handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	rows, err := s.db.Query(`SELECT id,actor,action,detail,created_at FROM audit_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	list := []map[string]any{}
	for rows.Next() {
		var id, ca int64
		var actor, action, detail string
		if err := rows.Scan(&id, &actor, &action, &detail, &ca); err != nil {
			continue
		}
		list = append(list, map[string]any{"id": id, "actor": actor, "action": action, "detail": detail, "created_at": ca})
	}
	writeJSON(w, 200, map[string]any{"logs": list})
}

// ---- Dashboard v5 ----

func (s *ServerApp) handleDashboardV5(w http.ResponseWriter, r *http.Request) {
	var users, servers, online, inbounds, plans, offline, pending int
	var inEnabled, xrayRunning, certs, extNodes, invites int
	now := nowUnix()
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&users)
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM servers`).Scan(&servers)
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM servers WHERE last_seen > ?`, now-45).Scan(&online)
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM servers WHERE last_seen > 0 AND last_seen <= ?`, now-45).Scan(&offline)
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM servers WHERE last_seen = 0 OR status='pending'`).Scan(&pending)
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM inbounds`).Scan(&inbounds)
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM inbounds WHERE enabled=1`).Scan(&inEnabled)
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM plans`).Scan(&plans)
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM servers WHERE COALESCE(xray_running,0)=1`).Scan(&xrayRunning)
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM certificates`).Scan(&certs)
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM external_nodes`).Scan(&extNodes)
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM invite_codes WHERE enabled=1`).Scan(&invites)

	var up, down, speedUp, speedDown int64
	_ = s.db.QueryRow(`SELECT COALESCE(SUM(traffic_up),0), COALESCE(SUM(traffic_down),0), COALESCE(SUM(speed_up),0), COALESCE(SUM(speed_down),0) FROM servers`).Scan(&up, &down, &speedUp, &speedDown)

	// today traffic from traffic_daily (YYYY-MM-DD in local server date)
	today := time.Now().Format("2006-01-02")
	var todayUp, todayDown int64
	_ = s.db.QueryRow(`SELECT COALESCE(SUM(up),0), COALESCE(SUM(down),0) FROM traffic_daily WHERE day=?`, today).Scan(&todayUp, &todayDown)

	// 14-day traffic series
	rows, err := s.db.Query(`SELECT day, SUM(up), SUM(down) FROM traffic_daily GROUP BY day ORDER BY day DESC LIMIT 14`)
	series := []map[string]any{}
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var day string
			var u, d int64
			if rows.Scan(&day, &u, &d) == nil {
				series = append(series, map[string]any{"day": day, "up": u, "down": d})
			}
		}
		for i, j := 0, len(series)-1; i < j; i, j = i+1, j-1 {
			series[i], series[j] = series[j], series[i]
		}
	}

	// protocol distribution
	protocols := []map[string]any{}
	if prows, err := s.db.Query(`SELECT protocol, COUNT(1) FROM inbounds GROUP BY protocol ORDER BY COUNT(1) DESC`); err == nil {
		defer prows.Close()
		for prows.Next() {
			var proto string
			var cnt int
			if prows.Scan(&proto, &cnt) == nil {
				protocols = append(protocols, map[string]any{"protocol": proto, "count": cnt})
			}
		}
	}

	// server preview (recent / status)
	serverPreview := []map[string]any{}
	if srows, err := s.db.Query(`
SELECT id, name, public_ip, COALESCE(domain,''), status, last_seen,
  COALESCE(xray_running,0), COALESCE(traffic_up,0), COALESCE(traffic_down,0),
  COALESCE(speed_up,0), COALESCE(speed_down,0), COALESCE(agent_error,''), COALESCE(hostname,'')
FROM servers ORDER BY
  CASE WHEN last_seen > ? THEN 0 WHEN last_seen = 0 OR status='pending' THEN 1 ELSE 2 END,
  name COLLATE NOCASE
LIMIT 8`, now-45); err == nil {
		defer srows.Close()
		for srows.Next() {
			var id, name, ip, domain, status, host, agentErr string
			var lastSeen, tUp, tDown, sUp, sDown int64
			var xray int
			if srows.Scan(&id, &name, &ip, &domain, &status, &lastSeen, &xray, &tUp, &tDown, &sUp, &sDown, &agentErr, &host) != nil {
				continue
			}
			st := "offline"
			if lastSeen > now-45 {
				st = "online"
			} else if lastSeen == 0 || status == "pending" {
				st = "pending"
			}
			serverPreview = append(serverPreview, map[string]any{
				"id": id, "name": name, "public_ip": ip, "domain": domain, "hostname": host,
				"status": st, "xray_running": xray == 1,
				"traffic_up": tUp, "traffic_down": tDown,
				"speed_up": sUp, "speed_down": sDown,
				"agent_error": agentErr,
			})
		}
	}

	// alerts
	alerts := []map[string]any{}
	if offline > 0 {
		alerts = append(alerts, map[string]any{"level": "warn", "text": fmt.Sprintf("%d 台服务器离线", offline), "go": "servers"})
	}
	if pending > 0 {
		alerts = append(alerts, map[string]any{"level": "info", "text": fmt.Sprintf("%d 台待安装 Agent", pending), "go": "servers"})
	}
	// agent errors
	var errCount int
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM servers WHERE COALESCE(agent_error,'') != ''`).Scan(&errCount)
	if errCount > 0 {
		alerts = append(alerts, map[string]any{"level": "err", "text": fmt.Sprintf("%d 台 Agent 上报异常", errCount), "go": "servers"})
	}
	// certs expiring in 14 days
	var expiring int
	expireSoon := now + 14*24*3600
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM certificates WHERE expire_at > 0 AND expire_at <= ?`, expireSoon).Scan(&expiring)
	if expiring > 0 {
		alerts = append(alerts, map[string]any{"level": "warn", "text": fmt.Sprintf("%d 张证书将在 14 天内到期", expiring), "go": "certs"})
	}
	if inbounds > 0 && inEnabled == 0 {
		alerts = append(alerts, map[string]any{"level": "warn", "text": "所有入站节点均已禁用", "go": "inbounds"})
	}
	if servers == 0 {
		alerts = append(alerts, map[string]any{"level": "info", "text": "尚未接入服务器，先部署 Agent", "go": "servers"})
	}
	if len(alerts) == 0 {
		alerts = append(alerts, map[string]any{"level": "ok", "text": "运行正常，暂无告警", "go": ""})
	}

	writeJSON(w, 200, map[string]any{
		"users": users, "servers": servers, "online": online, "offline": offline, "pending": pending,
		"inbounds": inbounds, "inbounds_enabled": inEnabled, "plans": plans,
		"xray_running": xrayRunning, "certs": certs, "certs_expiring": expiring,
		"external_nodes": extNodes, "invites": invites,
		"traffic_up": up, "traffic_down": down,
		"speed_up": speedUp, "speed_down": speedDown,
		"today_up": todayUp, "today_down": todayDown,
		"series": series, "protocols": protocols,
		"servers_preview": serverPreview, "alerts": alerts,
		"version": version.Version,
	})
}

// ---- Server update (domain/remark/tags) ----

func (s *ServerApp) handleUpdateServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Name   *string `json:"name"`
		Domain *string `json:"domain"`
		Remark *string `json:"remark"`
		Tags   *string `json:"tags"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad json"})
		return
	}
	if body.Name != nil {
		_, _ = s.db.Exec(`UPDATE servers SET name=? WHERE id=?`, *body.Name, id)
	}
	if body.Domain != nil {
		_, _ = s.db.Exec(`UPDATE servers SET domain=? WHERE id=?`, *body.Domain, id)
	}
	if body.Remark != nil {
		_, _ = s.db.Exec(`UPDATE servers SET remark=? WHERE id=?`, *body.Remark, id)
	}
	if body.Tags != nil {
		_, _ = s.db.Exec(`UPDATE servers SET tags=? WHERE id=?`, *body.Tags, id)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ensure xraycfg import used
var _ = xraycfg.Build
