package master

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xpanel/xpanel/internal/acme"
	"github.com/xpanel/xpanel/internal/protocol"
	"github.com/xpanel/xpanel/internal/sub"
	"github.com/xpanel/xpanel/internal/version"
	"github.com/xpanel/xpanel/internal/xraycfg"
)

type Config struct {
	Addr      string
	DataDir   string
	JWTSecret string
	PublicURL string // used in install command & subscribe base
	WebFS     fs.FS  // optional embedded UI
}

type ServerApp struct {
	db        *sql.DB
	jwtSecret string
	publicURL string
	dataDir   string
	webFS     fs.FS
	mux       *http.ServeMux
	acme      *acme.Manager
	hub       *Hub
}

func New(cfg Config) (*ServerApp, error) {
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "./data"
	}
	if cfg.JWTSecret == "" {
		cfg.JWTSecret = randomHex(32)
		log.Printf("generated ephemeral JWT secret; set JWT_SECRET for stability")
	}
	dbPath := strings.TrimRight(cfg.DataDir, `/\`) + "/xpanel.db"
	db, err := openDB(dbPath)
	if err != nil {
		return nil, err
	}
	acmeMgr, err := acme.NewManager(cfg.DataDir)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &ServerApp{
		db:        db,
		jwtSecret: cfg.JWTSecret,
		publicURL: strings.TrimRight(cfg.PublicURL, "/"),
		dataDir:   cfg.DataDir,
		webFS:     cfg.WebFS,
		mux:       http.NewServeMux(),
		acme:      acmeMgr,
		hub:       newHub(),
	}
	s.routes()
	s.startACMERenewer()
	s.startOfflineWatcher()
	return s, nil
}

func (s *ServerApp) Handler() http.Handler {
	return cors(s.mux)
}

func (s *ServerApp) Close() error { return s.db.Close() }

func (s *ServerApp) routes() {
	// auth / meta
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/meta", s.handleMeta)
	s.mux.HandleFunc("POST /api/auth/setup", s.handleSetup)
	s.mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	s.mux.HandleFunc("GET /api/auth/me", s.authMiddleware(s.handleMe))
	s.mux.HandleFunc("GET /api/dashboard", s.authMiddleware(s.handleDashboardV5))

	// servers
	s.mux.HandleFunc("GET /api/servers", s.authMiddleware(s.handleListServers))
	s.mux.HandleFunc("POST /api/servers", s.adminOnly(s.handleCreateServer))
	s.mux.HandleFunc("PUT /api/servers/{id}", s.adminOnly(s.handleUpdateServer))
	s.mux.HandleFunc("GET /api/servers/{id}/install-cmd", s.adminOnly(s.handleInstallCmd))
	s.mux.HandleFunc("DELETE /api/servers/{id}", s.adminOnly(s.handleDeleteServer))
	s.mux.HandleFunc("POST /api/servers/{id}/bump-config", s.adminOnly(s.handleBumpConfig))

	// inbounds / quick reality / links
	s.mux.HandleFunc("GET /api/inbounds", s.authMiddleware(s.handleListInbounds))
	s.mux.HandleFunc("POST /api/inbounds", s.adminOnly(s.handleCreateInbound))
	s.mux.HandleFunc("DELETE /api/inbounds/{id}", s.adminOnly(s.handleDeleteInbound))
	s.mux.HandleFunc("POST /api/inbounds/quick-reality", s.adminOnly(s.handleQuickRealityV5))
	s.mux.HandleFunc("GET /api/inbounds/links", s.authMiddleware(s.handleInboundLinks))
	s.mux.HandleFunc("POST /api/outbounds/quick-warp", s.adminOnly(s.handleQuickWARP))

	// tunnels / invites / nginx / audit
	s.mux.HandleFunc("GET /api/tunnels", s.authMiddleware(s.handleListTunnels))
	s.mux.HandleFunc("POST /api/tunnels", s.adminOnly(s.handleCreateTunnel))
	s.mux.HandleFunc("DELETE /api/tunnels/{id}", s.adminOnly(s.handleDeleteTunnel))
	s.mux.HandleFunc("GET /api/invites", s.adminOnly(s.handleListInvites))
	s.mux.HandleFunc("POST /api/invites", s.adminOnly(s.handleCreateInvite))
	s.mux.HandleFunc("POST /api/auth/register", s.handleRegisterInvite)
	s.mux.HandleFunc("GET /api/nginx", s.authMiddleware(s.handleGetNginx))
	s.mux.HandleFunc("PUT /api/nginx", s.adminOnly(s.handlePutNginx))
	s.mux.HandleFunc("GET /api/audit", s.adminOnly(s.handleAuditLogs))

	// outbounds / routes
	s.mux.HandleFunc("GET /api/outbounds", s.authMiddleware(s.handleListOutbounds))
	s.mux.HandleFunc("POST /api/outbounds", s.adminOnly(s.handleCreateOutbound))
	s.mux.HandleFunc("PUT /api/outbounds/{id}", s.adminOnly(s.handleUpdateOutbound))
	s.mux.HandleFunc("DELETE /api/outbounds/{id}", s.adminOnly(s.handleDeleteOutbound))
	s.mux.HandleFunc("GET /api/routes", s.authMiddleware(s.handleListRoutes))
	s.mux.HandleFunc("POST /api/routes", s.adminOnly(s.handleCreateRoute))
	s.mux.HandleFunc("DELETE /api/routes/{id}", s.adminOnly(s.handleDeleteRoute))

	// plans / users
	s.mux.HandleFunc("GET /api/plans", s.authMiddleware(s.handleListPlans))
	s.mux.HandleFunc("POST /api/plans", s.adminOnly(s.handleCreatePlan))
	s.mux.HandleFunc("DELETE /api/plans/{id}", s.adminOnly(s.handleDeletePlan))
	s.mux.HandleFunc("GET /api/users", s.adminOnly(s.handleListUsers))
	s.mux.HandleFunc("POST /api/users", s.adminOnly(s.handleCreateUser))
	s.mux.HandleFunc("PUT /api/users/{id}", s.adminOnly(s.handleUpdateUser))
	s.mux.HandleFunc("DELETE /api/users/{id}", s.adminOnly(s.handleDeleteUser))

	// external nodes / certs / traffic / settings
	s.mux.HandleFunc("GET /api/nodes/external", s.authMiddleware(s.handleListExtNodes))
	s.mux.HandleFunc("POST /api/nodes/external", s.adminOnly(s.handleImportExtNode))
	s.mux.HandleFunc("DELETE /api/nodes/external/{id}", s.adminOnly(s.handleDeleteExtNode))
	s.mux.HandleFunc("GET /api/certs", s.authMiddleware(s.handleListCerts))
	s.mux.HandleFunc("POST /api/certs", s.adminOnly(s.handleCreateCert))
	s.mux.HandleFunc("POST /api/certs/acme", s.adminOnly(s.handleIssueACME))
	s.mux.HandleFunc("GET /api/certs/acme/providers", s.authMiddleware(s.handleACMEProviders))
	s.mux.HandleFunc("POST /api/certs/{id}/renew", s.adminOnly(s.handleRenewCert))
	s.mux.HandleFunc("POST /api/certs/{id}/deploy", s.adminOnly(s.handleDeployCert))
	s.mux.HandleFunc("GET /api/certs/{id}", s.adminOnly(s.handleGetCert))
	s.mux.HandleFunc("DELETE /api/certs/{id}", s.adminOnly(s.handleDeleteCert))
	// ACME HTTP-01 challenge (must be reachable on domain:80 via reverse proxy or host network)
	if s.acme != nil {
		s.mux.HandleFunc("GET /.well-known/acme-challenge/{token}", s.acme.HTTP01.Handler())
	}
	s.mux.HandleFunc("GET /api/traffic", s.authMiddleware(s.handleTrafficSummary))
	s.mux.HandleFunc("GET /api/settings", s.authMiddleware(s.handleGetSettings))
	s.mux.HandleFunc("PUT /api/settings", s.adminOnly(s.handlePutSettings))

	// agent protocol
	s.mux.HandleFunc("POST /api/agent/register", s.handleAgentRegister)
	s.mux.HandleFunc("POST /api/agent/heartbeat", s.handleAgentHeartbeat)
	s.mux.HandleFunc("GET /api/agent/config", s.handleAgentConfig)
	s.mux.HandleFunc("GET /api/agent/ws", s.handleAgentWS)

	// speedtest / backup / mcp / webhook test
	s.mux.HandleFunc("POST /api/speedtest", s.authMiddleware(s.handleSpeedTest))
	s.mux.HandleFunc("POST /api/speedtest/batch", s.authMiddleware(s.handleSpeedTestBatch))
	s.mux.HandleFunc("GET /api/backup/export", s.adminOnly(s.handleBackupExport))
	s.mux.HandleFunc("POST /api/backup/import", s.adminOnly(s.handleBackupImport))
	s.mux.HandleFunc("POST /api/mcp", s.adminOnly(s.handleMCP))

	// public subscribe
	s.mux.HandleFunc("GET /sub/{token}", s.handleSubscribe)
	s.mux.HandleFunc("GET /sub/{token}/clash", s.handleSubscribeClash)
	s.mux.HandleFunc("GET /sub/{token}/singbox", s.handleSubscribeSingBox)
	s.mux.HandleFunc("GET /sub/{token}/surge", s.handleSubscribeSurge)
	s.mux.HandleFunc("GET /s/{code}", s.handleSubscribeShort)
	// disguise probe homepage when enabled
	s.mux.HandleFunc("GET /probe", s.handleProbe)

	// static UI (WebFS rooted at static/ contents)
	if s.webFS != nil {
		fileServer := http.FileServer(http.FS(s.webFS))
		s.mux.Handle("GET /static/", http.StripPrefix("/static/", fileServer))
		s.mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
			b, err := fs.ReadFile(s.webFS, "index.html")
			if err != nil {
				http.Error(w, "ui missing", 500)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(b)
		})
	}
}

func (s *ServerApp) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "version": version.Version})
}

func (s *ServerApp) handleMeta(w http.ResponseWriter, r *http.Request) {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&n)
	writeJSON(w, 200, map[string]any{
		"version":     version.Version,
		"initialized": n > 0,
	})
}

func (s *ServerApp) handleSetup(w http.ResponseWriter, r *http.Request) {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&n)
	if n > 0 {
		writeJSON(w, 400, map[string]string{"error": "already initialized"})
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil || body.Username == "" || len(body.Password) < 6 {
		writeJSON(w, 400, map[string]string{"error": "username and password(>=6) required"})
		return
	}
	hash, err := hashPassword(body.Password)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "hash failed"})
		return
	}
	tok := randomHex(16)
	res, err := s.db.Exec(
		`INSERT INTO users(username,password_hash,role,subscribe_token,created_at) VALUES(?,?,?,?,?)`,
		body.Username, hash, "admin", tok, nowUnix(),
	)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	u := User{ID: id, Username: body.Username, Role: "admin", SubscribeToken: tok}
	jwtStr, err := s.signToken(u)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "token failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"token": jwtStr, "user": u})
}

func (s *ServerApp) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad json"})
		return
	}
	var u User
	var hash string
	var en int
	err := s.db.QueryRow(
		`SELECT id,username,password_hash,role,subscribe_token,COALESCE(plan_id,0),traffic_limit,traffic_used,COALESCE(speed_limit,0),expire_at,COALESCE(enabled,1),COALESCE(remark,''),created_at FROM users WHERE username=?`,
		body.Username,
	).Scan(&u.ID, &u.Username, &hash, &u.Role, &u.SubscribeToken, &u.PlanID, &u.TrafficLimit, &u.TrafficUsed, &u.SpeedLimit, &u.ExpireAt, &en, &u.Remark, &u.CreatedAt)
	if err != nil || !checkPassword(hash, body.Password) {
		writeJSON(w, 401, map[string]string{"error": "invalid credentials"})
		return
	}
	u.Enabled = en == 1
	if !u.Enabled {
		writeJSON(w, 403, map[string]string{"error": "user disabled"})
		return
	}
	jwtStr, err := s.signToken(u)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "token failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"token": jwtStr, "user": u})
}

func (s *ServerApp) handleMe(w http.ResponseWriter, r *http.Request) {
	c := userFrom(r.Context())
	var u User
	var en int
	var shortCode string
	err := s.db.QueryRow(
		`SELECT id,username,role,subscribe_token,COALESCE(plan_id,0),traffic_limit,traffic_used,COALESCE(speed_limit,0),expire_at,COALESCE(enabled,1),COALESCE(remark,''),created_at,COALESCE(short_code,'') FROM users WHERE id=?`,
		c.UserID,
	).Scan(&u.ID, &u.Username, &u.Role, &u.SubscribeToken, &u.PlanID, &u.TrafficLimit, &u.TrafficUsed, &u.SpeedLimit, &u.ExpireAt, &en, &u.Remark, &u.CreatedAt, &shortCode)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	u.Enabled = en == 1
	if shortCode == "" {
		shortCode = randomHex(4)
		_, _ = s.db.Exec(`UPDATE users SET short_code=? WHERE id=?`, shortCode, u.ID)
	}
	base := s.publicURL
	if base == "" {
		// leave relative; frontend fills origin
	}
	writeJSON(w, 200, map[string]any{
		"user":              u,
		"short_code":        shortCode,
		"subscribe_url":     base + "/sub/" + u.SubscribeToken,
		"subscribe_clash":   base + "/sub/" + u.SubscribeToken + "/clash",
		"subscribe_singbox": base + "/sub/" + u.SubscribeToken + "/singbox",
		"subscribe_surge":   base + "/sub/" + u.SubscribeToken + "/surge",
		"subscribe_short":   base + "/s/" + shortCode,
	})
}

func (s *ServerApp) handleListServers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id,name,install_token,hostname,public_ip,status,last_seen,config_version,agent_version,COALESCE(xray_running,0),COALESCE(traffic_up,0),COALESCE(traffic_down,0),COALESCE(conn_mode,'http'),created_at,COALESCE(domain,''),COALESCE(remark,''),COALESCE(tags,''),COALESCE(speed_up,0),COALESCE(speed_down,0),COALESCE(agent_error,'') FROM servers ORDER BY created_at DESC`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	list := []Server{}
	now := time.Now().Unix()
	for rows.Next() {
		var srv Server
		var xrayRun int
		var domain, remark, tags, agentErr string
		var speedUp, speedDown int64
		if err := rows.Scan(&srv.ID, &srv.Name, &srv.InstallToken, &srv.Hostname, &srv.PublicIP, &srv.Status, &srv.LastSeen, &srv.ConfigVersion, &srv.AgentVersion, &xrayRun, &srv.TrafficUp, &srv.TrafficDown, &srv.ConnMode, &srv.CreatedAt, &domain, &remark, &tags, &speedUp, &speedDown, &agentErr); err != nil {
			continue
		}
		srv.XrayRunning = xrayRun == 1
		srv.Domain = domain
		srv.Remark = remark
		srv.Tags = tags
		srv.SpeedUp = speedUp
		srv.SpeedDown = speedDown
		srv.AgentError = agentErr
		srv.Online = srv.LastSeen > 0 && now-srv.LastSeen < 45
		if srv.Online {
			srv.Status = "online"
		} else if srv.LastSeen > 0 {
			srv.Status = "offline"
		}
		if s.hub != nil && s.hub.Online(srv.ID) {
			srv.ConnMode = "websocket"
			srv.Online = true
			srv.Status = "online"
		}
		list = append(list, srv)
	}
	writeJSON(w, 200, map[string]any{"servers": list})
}

func (s *ServerApp) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil || body.Name == "" {
		writeJSON(w, 400, map[string]string{"error": "name required"})
		return
	}
	id := uuid.NewString()
	token := randomHex(24)
	_, err := s.db.Exec(
		`INSERT INTO servers(id,name,install_token,status,created_at) VALUES(?,?,?,?,?)`,
		id, body.Name, token, "pending", nowUnix(),
	)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{
		"server": Server{
			ID: id, Name: body.Name, InstallToken: token, Status: "pending", CreatedAt: nowUnix(),
		},
	})
}

func (s *ServerApp) handleInstallCmd(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var name, token string
	err := s.db.QueryRow(`SELECT name, install_token FROM servers WHERE id=?`, id).Scan(&name, &token)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	base := s.publicURL
	if base == "" {
		// best-effort from request
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		base = fmt.Sprintf("%s://%s", scheme, r.Host)
	}
	cmd := fmt.Sprintf(
		`docker run -d --name xpanel-agent --restart unless-stopped --network host `+
			`-e MASTER_URL=%q -e INSTALL_TOKEN=%q -e XRAY_CONFIG=/data/xray.json `+
			`-v xpanel-agent-data:/data ghcr.io/binshao1230/xpanel-agent:latest`,
		base, token,
	)
	oneClick := fmt.Sprintf(
		`curl -sL https://raw.githubusercontent.com/binshao1230/xpanel/main/install-agent.sh | sudo bash -s -- -m %s -t %s --with-xray`,
		base, token,
	)
	bin := fmt.Sprintf(`./xpanel-agent -master %s -token %s -data ./agent-data -mode auto`, base, token)
	writeJSON(w, 200, map[string]any{
		"server_id":    id,
		"name":         name,
		"token":        token,
		"master_url":   base,
		"docker_cmd":   cmd,
		"binary_cmd":   bin,
		"install_cmd":  oneClick,
		"one_click_cmd": oneClick,
	})
}

func (s *ServerApp) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, _ = s.db.Exec(`DELETE FROM inbounds WHERE server_id=?`, id)
	res, err := s.db.Exec(`DELETE FROM servers WHERE id=?`, id)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *ServerApp) handleListInbounds(w http.ResponseWriter, r *http.Request) {
	serverID := r.URL.Query().Get("server_id")
	var rows *sql.Rows
	var err error
	if serverID != "" {
		rows, err = s.db.Query(`SELECT id,server_id,tag,protocol,port,settings_json,stream_json,COALESCE(multiplier,1),COALESCE(remark,''),COALESCE(cert_id,0),enabled,created_at FROM inbounds WHERE server_id=? ORDER BY id`, serverID)
	} else {
		rows, err = s.db.Query(`SELECT id,server_id,tag,protocol,port,settings_json,stream_json,COALESCE(multiplier,1),COALESCE(remark,''),COALESCE(cert_id,0),enabled,created_at FROM inbounds ORDER BY id`)
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	list := []Inbound{}
	for rows.Next() {
		var in Inbound
		var en int
		if err := rows.Scan(&in.ID, &in.ServerID, &in.Tag, &in.Protocol, &in.Port, &in.SettingsJSON, &in.StreamJSON, &in.Multiplier, &in.Remark, &in.CertID, &en, &in.CreatedAt); err != nil {
			continue
		}
		in.Enabled = en == 1
		list = append(list, in)
	}
	writeJSON(w, 200, map[string]any{"inbounds": list})
}

func (s *ServerApp) handleCreateInbound(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ServerID string         `json:"server_id"`
		Tag      string         `json:"tag"`
		Protocol string         `json:"protocol"`
		Port     int            `json:"port"`
		Settings map[string]any `json:"settings"`
		Stream   map[string]any `json:"stream"`
		ClientID string         `json:"client_id"`
		CertID   int64          `json:"cert_id"`
		EnableTLS bool          `json:"enable_tls"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad json"})
		return
	}
	if body.ServerID == "" || body.Protocol == "" || body.Port <= 0 {
		writeJSON(w, 400, map[string]string{"error": "server_id, protocol, port required"})
		return
	}
	if body.Tag == "" {
		body.Tag = fmt.Sprintf("%s-%d", body.Protocol, body.Port)
	}
	if body.Settings == nil {
		body.Settings = map[string]any{}
	}
	if body.Protocol == "vless" || body.Protocol == "vmess" {
		cid := body.ClientID
		if cid == "" {
			cid = uuid.NewString()
		}
		if _, ok := body.Settings["clients"]; !ok {
			body.Settings["clients"] = []map[string]any{
				xraycfg.DefaultVLESSClient(cid, "default@xpanel"),
			}
			if body.Protocol == "vless" {
				body.Settings["decryption"] = "none"
			}
		}
	}
	if body.Stream == nil {
		body.Stream = map[string]any{"network": "tcp"}
	}
	// bind TLS cert if requested
	if body.CertID > 0 || body.EnableTLS {
		var domain string
		if body.CertID > 0 {
			_ = s.db.QueryRow(`SELECT domain FROM certificates WHERE id=? AND status='active' AND cert_pem!=''`, body.CertID).Scan(&domain)
		}
		if domain != "" {
			body.Stream = xraycfg.ApplyTLSFiles(body.Stream, domain)
		} else if body.EnableTLS {
			writeJSON(w, 400, map[string]string{"error": "cert_id required for enable_tls (active cert with PEM)"})
			return
		}
	}
	sj, _ := json.Marshal(body.Settings)
	st, _ := json.Marshal(body.Stream)
	res, err := s.db.Exec(
		`INSERT INTO inbounds(server_id,tag,protocol,port,settings_json,stream_json,cert_id,enabled,created_at) VALUES(?,?,?,?,?,?,?,1,?)`,
		body.ServerID, body.Tag, body.Protocol, body.Port, string(sj), string(st), body.CertID, nowUnix(),
	)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	_, _ = s.db.Exec(`UPDATE servers SET config_version = config_version + 1 WHERE id=?`, body.ServerID)
	writeJSON(w, 200, map[string]any{"id": id, "tag": body.Tag, "cert_id": body.CertID})
}

func (s *ServerApp) handleDeleteInbound(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, _ := strconv.ParseInt(idStr, 10, 64)
	var serverID string
	err := s.db.QueryRow(`SELECT server_id FROM inbounds WHERE id=?`, id).Scan(&serverID)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	_, _ = s.db.Exec(`DELETE FROM inbounds WHERE id=?`, id)
	_, _ = s.db.Exec(`UPDATE servers SET config_version = config_version + 1 WHERE id=?`, serverID)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *ServerApp) handleBumpConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, err := s.db.Exec(`UPDATE servers SET config_version = config_version + 1 WHERE id=?`, id)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// --- Agent protocol ---

func (s *ServerApp) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	var req protocol.RegisterRequest
	if err := readJSON(r, &req); err != nil || req.Token == "" {
		writeJSON(w, 400, map[string]string{"error": "token required"})
		return
	}
	var id, existingKey string
	err := s.db.QueryRow(`SELECT id, agent_key FROM servers WHERE install_token=?`, req.Token).Scan(&id, &existingKey)
	if err != nil {
		writeJSON(w, 401, map[string]string{"error": "invalid install token"})
		return
	}
	key := existingKey
	if key == "" {
		key = randomHex(32)
	}
	_, err = s.db.Exec(
		`UPDATE servers SET agent_key=?, hostname=?, agent_version=?, status='online', last_seen=?, agent_error='' WHERE id=?`,
		key, req.Hostname, req.AgentVersion, nowUnix(), id,
	)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, protocol.RegisterResponse{
		ServerID:        id,
		AgentKey:        key,
		PollIntervalSec: 15,
		APIPort:         protocol.DefaultAPIPort,
	})
}

func (s *ServerApp) lookupAgent(r *http.Request) (serverID string, ok bool) {
	key := r.Header.Get(protocol.HeaderAgentKey)
	if key == "" {
		return "", false
	}
	err := s.db.QueryRow(`SELECT id FROM servers WHERE agent_key=?`, key).Scan(&serverID)
	return serverID, err == nil
}

func (s *ServerApp) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	sid, ok := s.lookupAgent(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "invalid agent key"})
		return
	}
	var req protocol.HeartbeatRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad json"})
		return
	}
	if req.ServerID != "" && req.ServerID != sid {
		writeJSON(w, 403, map[string]string{"error": "server_id mismatch"})
		return
	}
	s.applyHeartbeat(sid, &req)
	_, _ = s.db.Exec(`UPDATE servers SET conn_mode='http' WHERE id=? AND conn_mode!='websocket'`, sid)
	var desired int64
	_ = s.db.QueryRow(`SELECT config_version FROM servers WHERE id=?`, sid).Scan(&desired)
	var cmds []string
	if req.ConfigVersion < desired {
		cmds = append(cmds, protocol.CmdReloadConfig)
	}
	writeJSON(w, 200, protocol.HeartbeatResponse{
		OK:                   true,
		DesiredConfigVersion: desired,
		Commands:             cmds,
	})
}

func (s *ServerApp) handleAgentConfig(w http.ResponseWriter, r *http.Request) {
	sid, ok := s.lookupAgent(r)
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "invalid agent key"})
		return
	}
	bundle, err := s.buildConfigBundle(sid)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, bundle)
}

func (s *ServerApp) buildConfigBundle(serverID string) (*protocol.ConfigBundle, error) {
	var ver int64
	if err := s.db.QueryRow(`SELECT config_version FROM servers WHERE id=?`, serverID).Scan(&ver); err != nil {
		return nil, err
	}

	// certs for this agent: global (server_id='') or bound to this server
	certs, certDomainByID, err := s.loadCertsForServer(serverID)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(
		`SELECT tag,protocol,port,settings_json,stream_json,COALESCE(cert_id,0) FROM inbounds WHERE server_id=? AND enabled=1`,
		serverID,
	)
	if err != nil {
		return nil, err
	}
	var inSpecs []xraycfg.InboundSpec
	for rows.Next() {
		var tag, proto, sj, st string
		var port int
		var certID int64
		if err := rows.Scan(&tag, &proto, &port, &sj, &st, &certID); err != nil {
			continue
		}
		var settings, stream map[string]any
		_ = json.Unmarshal([]byte(sj), &settings)
		_ = json.Unmarshal([]byte(st), &stream)
		if settings != nil {
			delete(settings, "xpanelMeta") // not for xray core
		}
		if certID > 0 {
			if domain, ok := certDomainByID[certID]; ok {
				stream = xraycfg.MergeTLSIfNeeded(stream, domain)
			}
		}
		inSpecs = append(inSpecs, xraycfg.InboundSpec{
			Tag: tag, Protocol: proto, Port: port, Settings: settings, Stream: stream,
		})
	}
	rows.Close()

	orows, err := s.db.Query(`SELECT tag,protocol,settings_json,stream_json FROM outbounds WHERE server_id=? AND enabled=1`, serverID)
	if err != nil {
		return nil, err
	}
	var outSpecs []xraycfg.OutboundSpec
	enabledOutTags := map[string]bool{"direct": true, "block": true, "api": true}
	for orows.Next() {
		var tag, proto, sj, st string
		if err := orows.Scan(&tag, &proto, &sj, &st); err != nil {
			continue
		}
		var settings, stream map[string]any
		_ = json.Unmarshal([]byte(sj), &settings)
		_ = json.Unmarshal([]byte(st), &stream)
		// skip incomplete wireguard / placeholders that break xray -test
		if proto == "wireguard" && settingsIncomplete(settings) {
			continue
		}
		outSpecs = append(outSpecs, xraycfg.OutboundSpec{Tag: tag, Protocol: proto, Settings: settings, Stream: stream})
		enabledOutTags[tag] = true
	}
	orows.Close()

	rrows, err := s.db.Query(`SELECT outbound_tag,domain_json,ip_json,port,network,protocol_json FROM route_rules WHERE server_id=? AND enabled=1 ORDER BY priority,id`, serverID)
	if err != nil {
		return nil, err
	}
	var routes []xraycfg.RouteSpec
	for rrows.Next() {
		var ob, dj, ij, port, netw, pj string
		if err := rrows.Scan(&ob, &dj, &ij, &port, &netw, &pj); err != nil {
			continue
		}
		if !enabledOutTags[ob] {
			continue // skip routes pointing to disabled outbounds
		}
		var domain, ip, protos []string
		_ = json.Unmarshal([]byte(dj), &domain)
		_ = json.Unmarshal([]byte(ij), &ip)
		_ = json.Unmarshal([]byte(pj), &protos)
		// strip geosite:/geoip: rules that require .dat files on agent
		domain = filterGeoSiteRules(domain)
		ip = filterGeoIPRules(ip)
		if len(domain) == 0 && len(ip) == 0 && port == "" && netw == "" && len(protos) == 0 {
			continue
		}
		routes = append(routes, xraycfg.RouteSpec{
			OutboundTag: ob, Domain: domain, IP: ip, Port: port, Network: netw, Protocol: protos,
		})
	}
	rrows.Close()

	cfg, sum, err := xraycfg.Build(xraycfg.BuildOptions{
		Inbounds:  inSpecs,
		Outbounds: outSpecs,
		Routes:    routes,
		APIPort:   protocol.DefaultAPIPort,
	})
	if err != nil {
		return nil, err
	}
	return &protocol.ConfigBundle{
		Version:  ver,
		XrayJSON: cfg,
		Checksum: sum,
		APIPort:  protocol.DefaultAPIPort,
		Certs:    certs,
	}, nil
}

func (s *ServerApp) loadCertsForServer(serverID string) ([]protocol.CertFile, map[int64]string, error) {
	rows, err := s.db.Query(
		`SELECT id, domain, cert_pem, key_pem, COALESCE(server_id,'') FROM certificates
		 WHERE status='active' AND cert_pem!='' AND key_pem!='' AND (server_id='' OR server_id=?)`,
		serverID,
	)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var certs []protocol.CertFile
	byID := map[int64]string{}
	seenDomain := map[string]bool{}
	for rows.Next() {
		var id int64
		var domain, certPEM, keyPEM, sid string
		if err := rows.Scan(&id, &domain, &certPEM, &keyPEM, &sid); err != nil {
			continue
		}
		byID[id] = domain
		if seenDomain[domain] {
			continue
		}
		seenDomain[domain] = true
		certs = append(certs, protocol.CertFile{Domain: domain, CertPEM: certPEM, KeyPEM: keyPEM})
	}
	return certs, byID, nil
}

func settingsIncomplete(settings map[string]any) bool {
	if settings == nil {
		return true
	}
	if sk, ok := settings["secretKey"].(string); ok {
		if sk == "" || strings.Contains(sk, "REPLACE") {
			return true
		}
	}
	return false
}

func filterGeoSiteRules(in []string) []string {
	out := make([]string, 0, len(in))
	for _, d := range in {
		if strings.HasPrefix(d, "geosite:") {
			continue
		}
		out = append(out, d)
	}
	return out
}

func filterGeoIPRules(in []string) []string {
	out := make([]string, 0, len(in))
	for _, d := range in {
		if strings.HasPrefix(d, "geoip:") {
			continue
		}
		out = append(out, d)
	}
	return out
}

func (s *ServerApp) bumpAllServers() {
	_, _ = s.db.Exec(`UPDATE servers SET config_version = config_version + 1`)
	if s.hub != nil {
		s.hub.BroadcastCommand(protocol.CmdReloadConfig)
	}
}

func (s *ServerApp) bumpServer(id string) {
	if id == "" {
		s.bumpAllServers()
		return
	}
	_, _ = s.db.Exec(`UPDATE servers SET config_version = config_version + 1 WHERE id=?`, id)
	if s.hub != nil {
		s.hub.PushCommand(id, protocol.CmdReloadConfig)
	}
}

func (s *ServerApp) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.nodesForToken(r.PathValue("token"))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(sub.ToV2RayLinks(nodes)))
}

func (s *ServerApp) handleSubscribeClash(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.nodesForToken(r.PathValue("token"))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	_, _ = w.Write([]byte(sub.ToClashYAML(nodes)))
}

func (s *ServerApp) handleSubscribeSingBox(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.nodesForToken(r.PathValue("token"))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(sub.ToSingBox(nodes)))
}

func (s *ServerApp) handleSubscribeSurge(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.nodesForToken(r.PathValue("token"))
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(sub.ToSurge(nodes)))
}

func (s *ServerApp) handleSubscribeShort(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	var token string
	err := s.db.QueryRow(`SELECT subscribe_token FROM users WHERE short_code=? AND enabled=1`, code).Scan(&token)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	// redirect-style: serve base64 by default
	nodes, err := s.nodesForToken(token)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(sub.ToV2RayLinks(nodes)))
}

func (s *ServerApp) handleProbe(w http.ResponseWriter, r *http.Request) {
	mode := s.getSetting("probe_mode")
	if mode == "" || mode == "off" {
		http.NotFound(w, r)
		return
	}
	title := s.getSetting("site_name")
	if title == "" {
		title = "Welcome"
	}
	// disguise: simple static page (nginx-like / blog fake)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>%s</title>
<style>body{font-family:system-ui;max-width:720px;margin:4rem auto;padding:0 1rem;color:#334;background:#fafbff}
h1{font-weight:600}p{line-height:1.7;color:#556}</style></head>
<body><h1>%s</h1><p>This site is under construction. Please check back later.</p>
<p style="color:#99a;font-size:12px">Powered by open source.</p></body></html>`, title, title)
}

func (s *ServerApp) nodesForToken(token string) ([]sub.Node, error) {
	var uid int64
	var enabled int
	var expireAt, trafficLimit, trafficUsed int64
	err := s.db.QueryRow(
		`SELECT id, COALESCE(enabled,1), expire_at, traffic_limit, traffic_used FROM users WHERE subscribe_token=?`, token,
	).Scan(&uid, &enabled, &expireAt, &trafficLimit, &trafficUsed)
	if err != nil {
		return nil, err
	}
	if enabled != 1 {
		return nil, fmt.Errorf("disabled")
	}
	if expireAt > 0 && nowUnix() > expireAt {
		return nil, fmt.Errorf("expired")
	}
	if trafficLimit > 0 && trafficUsed >= trafficLimit {
		return nil, fmt.Errorf("traffic exceeded")
	}
	_ = uid

	rows, err := s.db.Query(`
SELECT i.tag, i.protocol, i.port, i.settings_json, i.stream_json, s.public_ip, s.name
FROM inbounds i JOIN servers s ON s.id = i.server_id
WHERE i.enabled=1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []sub.Node
	for rows.Next() {
		var tag, proto, sj, st, ip, sname string
		var port int
		if err := rows.Scan(&tag, &proto, &port, &sj, &st, &ip, &sname); err != nil {
			continue
		}
		if ip == "" {
			ip = "0.0.0.0"
		}
		var settings, stream map[string]any
		_ = json.Unmarshal([]byte(sj), &settings)
		_ = json.Unmarshal([]byte(st), &stream)
		n := sub.Node{
			Name:     fmt.Sprintf("%s-%s", sname, tag),
			Protocol: proto,
			Address:  ip,
			Port:     port,
			Network:  "tcp",
		}
		if stream != nil {
			if netw, ok := stream["network"].(string); ok {
				n.Network = netw
			}
			if sec, ok := stream["security"].(string); ok {
				n.Security = sec
			}
		}
		if clients, ok := settings["clients"].([]any); ok && len(clients) > 0 {
			if c0, ok := clients[0].(map[string]any); ok {
				if id, ok := c0["id"].(string); ok {
					n.UUID = id
				}
				if pw, ok := c0["password"].(string); ok {
					n.Password = pw
				}
				if flow, ok := c0["flow"].(string); ok {
					n.Flow = flow
				}
			}
		}
		if pw, ok := settings["password"].(string); ok {
			n.Password = pw
		}
		nodes = append(nodes, n)
	}
	// external imported nodes
	erows, err := s.db.Query(`SELECT name,protocol,address,port,share_link FROM external_nodes WHERE enabled=1`)
	if err == nil {
		defer erows.Close()
		for erows.Next() {
			var n sub.Node
			if err := erows.Scan(&n.Name, &n.Protocol, &n.Address, &n.Port, &n.ShareLink); err != nil {
				continue
			}
			nodes = append(nodes, n)
		}
	}
	return nodes, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
