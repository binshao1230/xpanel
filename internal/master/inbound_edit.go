package master

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	xpcrypto "github.com/binshao1230/bpanel/internal/crypto"
	"github.com/binshao1230/bpanel/internal/xraycfg"
)

// inboundForm is the free-form create/update body for nodes (inbounds).
// UI form fields and raw JSON are both accepted; raw settings/stream win when provided.
type inboundForm struct {
	ServerID     string         `json:"server_id"`
	Tag          string         `json:"tag"`
	Protocol     string         `json:"protocol"`
	Port         int            `json:"port"`
	Settings     map[string]any `json:"settings"`
	Stream       map[string]any `json:"stream"`
	SettingsJSON string         `json:"settings_json"`
	StreamJSON   string         `json:"stream_json"`
	ClientID     string         `json:"client_id"`
	UUID         string         `json:"uuid"`
	Password     string         `json:"password"`
	Method       string         `json:"method"`
	Flow         string         `json:"flow"`
	CertID       int64          `json:"cert_id"`
	EnableTLS    bool           `json:"enable_tls"`
	Network      string         `json:"network"`
	Security     string         `json:"security"`
	Path         string         `json:"path"`
	Host         string         `json:"host"`
	ServiceName  string         `json:"service_name"`
	SNI          string         `json:"sni"`
	Dest         string         `json:"dest"`
	PublicKey    string         `json:"public_key"`
	PrivateKey   string         `json:"private_key"`
	ShortID      string         `json:"short_id"`
	Fingerprint  string         `json:"fingerprint"`
	ALPN         string         `json:"alpn"`
	Remark       string         `json:"remark"`
	ShareName    string         `json:"share_name"`
	Multiplier   *float64       `json:"multiplier"`
	Enabled      *bool          `json:"enabled"`
}

func parseInboundForm(r *http.Request) (inboundForm, error) {
	var body inboundForm
	if err := readJSON(r, &body); err != nil {
		return body, err
	}
	// raw JSON strings override map fields when present
	if strings.TrimSpace(body.SettingsJSON) != "" {
		var m map[string]any
		if err := json.Unmarshal([]byte(body.SettingsJSON), &m); err != nil {
			return body, fmt.Errorf("settings_json 无效: %w", err)
		}
		body.Settings = m
	}
	if strings.TrimSpace(body.StreamJSON) != "" {
		var m map[string]any
		if err := json.Unmarshal([]byte(body.StreamJSON), &m); err != nil {
			return body, fmt.Errorf("stream_json 无效: %w", err)
		}
		body.Stream = m
	}
	return body, nil
}

func (s *ServerApp) composeInboundSettings(body *inboundForm) map[string]any {
	if body.Settings != nil {
		// still fill empty secrets if caller left them blank
		s.fillProtocolDefaults(body)
		return body.Settings
	}
	body.Settings = map[string]any{}
	s.fillProtocolDefaults(body)
	return body.Settings
}

func (s *ServerApp) fillProtocolDefaults(body *inboundForm) {
	if body.Settings == nil {
		body.Settings = map[string]any{}
	}
	proto := strings.ToLower(body.Protocol)
	// normalize aliases
	switch proto {
	case "ss":
		body.Protocol = "shadowsocks"
		proto = "shadowsocks"
	case "dokodemo", "tunnel":
		body.Protocol = "dokodemo-door"
		proto = "dokodemo-door"
	case "hy2", "hysteria2":
		body.Protocol = "hysteria"
		proto = "hysteria"
	}

	switch proto {
	case "vless", "vmess":
		cid := body.UUID
		if cid == "" {
			cid = body.ClientID
		}
		if cid == "" {
			cid = uuid.NewString()
		}
		flow := body.Flow
		if flow == "" && proto == "vless" && strings.EqualFold(body.Security, "reality") {
			flow = "xtls-rprx-vision"
		}
		if _, ok := body.Settings["clients"]; !ok {
			c := map[string]any{"id": cid, "email": "default@bpanel"}
			if flow != "" {
				c["flow"] = flow
			} else if proto == "vless" {
				c["flow"] = ""
			}
			body.Settings["clients"] = []map[string]any{c}
		}
		if proto == "vless" {
			if _, ok := body.Settings["decryption"]; !ok {
				body.Settings["decryption"] = "none"
			}
		}
	case "trojan":
		if _, ok := body.Settings["clients"]; !ok {
			pw := body.Password
			if pw == "" {
				pw = randomHex(8)
			}
			body.Settings["clients"] = []map[string]any{
				{"password": pw, "email": "trojan@bpanel"},
			}
		}
	case "shadowsocks":
		if _, ok := body.Settings["method"]; !ok {
			m := body.Method
			if m == "" {
				m = "aes-256-gcm"
			}
			body.Settings["method"] = m
		}
		if pw, _ := body.Settings["password"].(string); pw == "" {
			if body.Password != "" {
				body.Settings["password"] = body.Password
			} else {
				body.Settings["password"] = randomHex(8)
			}
		}
		if _, ok := body.Settings["network"]; !ok {
			body.Settings["network"] = "tcp,udp"
		}
	case "socks", "mixed":
		// mixed uses same settings shape as socks in Xray
		// default password auth (safer for public panels); set auth=noauth via 高级 JSON 可关
		if _, ok := body.Settings["auth"]; !ok {
			body.Settings["auth"] = "password"
		}
		if body.Settings["auth"] == "password" {
			if _, ok := body.Settings["accounts"]; !ok {
				user := body.UUID
				if user == "" {
					user = "user"
				}
				pw := body.Password
				if pw == "" {
					pw = randomHex(8)
				}
				body.Settings["accounts"] = []map[string]any{{"user": user, "pass": pw}}
			}
		}
		if _, ok := body.Settings["udp"]; !ok {
			body.Settings["udp"] = true
		}
	case "http":
		// default with account
		if _, ok := body.Settings["accounts"]; !ok {
			user := body.UUID
			if user == "" {
				user = "user"
			}
			pw := body.Password
			if pw == "" {
				pw = randomHex(8)
			}
			body.Settings["accounts"] = []map[string]any{{"user": user, "pass": pw}}
		}
		if _, ok := body.Settings["allowTransparent"]; !ok {
			body.Settings["allowTransparent"] = false
		}
	case "dokodemo-door":
		if _, ok := body.Settings["address"]; !ok {
			addr := body.Host
			if addr == "" {
				addr = "127.0.0.1"
			}
			body.Settings["address"] = addr
		}
		if _, ok := body.Settings["port"]; !ok {
			p := body.Port
			if p <= 0 {
				p = 80
			}
			// target port (not listen) — use path field as target port if numeric
			if body.Path != "" {
				if n, err := strconv.Atoi(strings.TrimPrefix(body.Path, "/")); err == nil && n > 0 {
					body.Settings["port"] = n
				} else {
					body.Settings["port"] = 80
				}
			} else {
				body.Settings["port"] = 80
			}
		}
		if _, ok := body.Settings["network"]; !ok {
			body.Settings["network"] = "tcp,udp"
		}
		if _, ok := body.Settings["followRedirect"]; !ok {
			body.Settings["followRedirect"] = false
		}
	case "hysteria":
		if _, ok := body.Settings["version"]; !ok {
			body.Settings["version"] = 2
		}
		if _, ok := body.Settings["users"]; !ok {
			if _, ok2 := body.Settings["clients"]; !ok2 {
				auth := body.Password
				if auth == "" {
					auth = randomHex(12)
				}
				body.Settings["users"] = []map[string]any{
					{"auth": auth, "email": "hy2@bpanel"},
				}
			}
		}
		// hysteria prefers its own transport
		if body.Network == "" {
			body.Network = "hysteria"
		}
		if body.Security == "" || body.Security == "none" {
			body.Security = "tls"
		}
	case "wireguard":
		// server-mode inbound (IsClient false by default in conf)
		if _, ok := body.Settings["secretKey"]; !ok {
			// placeholder — operator should replace with real WG private key
			// generate random 32-byte base64 for convenience
			body.Settings["secretKey"] = randomWGKey()
		}
		if _, ok := body.Settings["address"]; !ok {
			body.Settings["address"] = []string{"10.0.0.1/24"}
		}
		if _, ok := body.Settings["peers"]; !ok {
			body.Settings["peers"] = []map[string]any{}
		}
	case "anytls":
		// AnyTLS (sing-box 风格字段；需支持 anytls 的 Xray/兼容内核)
		pw := body.Password
		if pw == "" {
			if p, _ := body.Settings["password"].(string); p != "" {
				pw = p
			} else {
				pw = randomHex(12)
			}
		}
		if _, ok := body.Settings["users"]; !ok {
			name := body.UUID
			if name == "" {
				name = "default"
			}
			body.Settings["users"] = []map[string]any{
				{"name": name, "password": pw},
			}
		}
		if _, ok := body.Settings["password"]; !ok {
			body.Settings["password"] = pw
		}
		// store for share links
		if meta, _ := body.Settings["bpanelMeta"].(map[string]any); meta == nil {
			body.Settings["bpanelMeta"] = map[string]any{"password": pw}
		}
		if body.Security == "" || body.Security == "none" {
			body.Security = "tls"
		}
		if body.Network == "" {
			body.Network = "tcp"
		}
	}
}

// randomWGKey returns a random base64 wireguard-style private key (32 bytes).
func randomWGKey() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return randomHex(32)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func (s *ServerApp) composeInboundStream(body *inboundForm) (map[string]any, error) {
	if body.Stream != nil {
		// still apply cert if requested
		if err := s.applyCertToStream(body); err != nil {
			return nil, err
		}
		return body.Stream, nil
	}

	network := strings.ToLower(strings.TrimSpace(body.Network))
	if network == "" {
		network = "tcp"
	}
	security := strings.ToLower(strings.TrimSpace(body.Security))
	if security == "" {
		if body.CertID > 0 || body.EnableTLS {
			security = "tls"
		} else {
			security = "none"
		}
	}

	stream := map[string]any{
		"network":  network,
		"security": security,
	}

	// transport-specific
	switch network {
	case "ws", "websocket":
		stream["network"] = "ws"
		ws := map[string]any{}
		if body.Path != "" {
			ws["path"] = body.Path
		} else {
			ws["path"] = "/"
		}
		if body.Host != "" {
			ws["headers"] = map[string]any{"Host": body.Host}
		}
		stream["wsSettings"] = ws
	case "grpc":
		gs := map[string]any{}
		if body.ServiceName != "" {
			gs["serviceName"] = body.ServiceName
		} else if body.Path != "" {
			gs["serviceName"] = strings.TrimPrefix(body.Path, "/")
		} else {
			gs["serviceName"] = "GunService"
		}
		stream["grpcSettings"] = gs
	case "h2", "http":
		stream["network"] = "h2"
		hs := map[string]any{}
		if body.Path != "" {
			hs["path"] = body.Path
		}
		if body.Host != "" {
			hs["host"] = []string{body.Host}
		}
		stream["httpSettings"] = hs
	case "httpupgrade":
		hu := map[string]any{}
		if body.Path != "" {
			hu["path"] = body.Path
		}
		if body.Host != "" {
			hu["host"] = body.Host
		}
		stream["httpupgradeSettings"] = hu
	case "splithttp", "xhttp":
		stream["network"] = "splithttp"
		sh := map[string]any{}
		if body.Path != "" {
			sh["path"] = body.Path
		}
		if body.Host != "" {
			sh["host"] = body.Host
		}
		stream["splithttpSettings"] = sh
	case "hysteria":
		// protocol-native transport for hysteria v2
		stream["network"] = "hysteria"
		if body.Security == "" {
			stream["security"] = "tls"
		}
	case "tcp":
		// optional http header camouflage left to advanced JSON
	}

	switch security {
	case "tls":
		tls := map[string]any{}
		if body.SNI != "" {
			tls["serverName"] = body.SNI
		}
		if body.ALPN != "" {
			parts := strings.Split(body.ALPN, ",")
			alpn := make([]string, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					alpn = append(alpn, p)
				}
			}
			if len(alpn) > 0 {
				tls["alpn"] = alpn
			}
		}
		if body.Fingerprint != "" {
			tls["fingerprint"] = body.Fingerprint
		}
		stream["tlsSettings"] = tls
		body.Stream = stream
		if err := s.applyCertToStream(body); err != nil {
			return nil, err
		}
		return body.Stream, nil

	case "reality":
		priv := body.PrivateKey
		pub := body.PublicKey
		// Only generate a fresh pair when both keys are missing.
		// If only private is present, derive public — never pair old priv with a new pub.
		if priv == "" && pub == "" {
			p, u, err := xpcrypto.X25519Pair()
			if err != nil {
				return nil, err
			}
			priv, pub = p, u
		} else if priv != "" && pub == "" {
			if u, err := xpcrypto.PublicFromPrivate(priv); err == nil {
				pub = u
			}
		}
		shortID := body.ShortID
		if shortID == "" {
			shortID, _ = xpcrypto.RandomShortID()
		}
		dest := body.Dest
		if dest == "" {
			dest = "www.microsoft.com:443"
		}
		sni := body.SNI
		if sni == "" {
			// dest host without port
			sni = dest
			if i := strings.LastIndex(sni, ":"); i > 0 {
				sni = sni[:i]
			}
		}
		fp := body.Fingerprint
		if fp == "" {
			fp = "chrome"
		}
		stream["realitySettings"] = map[string]any{
			"show":        false,
			"dest":        dest,
			"xver":        0,
			"serverNames": []string{sni},
			"privateKey":  priv,
			"shortIds":    []string{shortID, ""},
			"fingerprint": fp,
		}
		// store public key for share links
		if body.Settings == nil {
			body.Settings = map[string]any{}
		}
		meta, _ := body.Settings["bpanelMeta"].(map[string]any)
		if meta == nil {
			meta, _ = body.Settings["xpanelMeta"].(map[string]any)
		}
		if meta == nil {
			meta = map[string]any{}
		}
		meta["publicKey"] = pub
		meta["shortId"] = shortID
		body.Settings["bpanelMeta"] = meta
		delete(body.Settings, "xpanelMeta")

	case "none", "":
		stream["security"] = "none"
	}

	body.Stream = stream
	return stream, nil
}

func (s *ServerApp) applyCertToStream(body *inboundForm) error {
	if body.CertID <= 0 && !body.EnableTLS {
		return nil
	}
	// Reality uses its own security; don't override with cert files
	if body.Stream != nil {
		if sec, _ := body.Stream["security"].(string); sec == "reality" {
			return nil
		}
	}
	var domain, certPEM string
	if body.CertID > 0 {
		_ = s.db.QueryRow(
			`SELECT domain, cert_pem FROM certificates WHERE id=? AND status='active' AND cert_pem!='' AND key_pem!=''`,
			body.CertID,
		).Scan(&domain, &certPEM)
	}
	if domain == "" || !strings.Contains(certPEM, "BEGIN CERTIFICATE") {
		return fmt.Errorf("请选择有效证书（需含完整 PEM）。可先在「证书 ACME」申请或上传真实证书")
	}
	body.Stream = xraycfg.ApplyTLSFiles(body.Stream, domain)
	if body.SNI != "" {
		if tls, ok := body.Stream["tlsSettings"].(map[string]any); ok {
			tls["serverName"] = body.SNI
		}
	}
	return nil
}

func (s *ServerApp) handleGetInbound(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var in Inbound
	var en int
	var shareName string
	err := s.db.QueryRow(`
SELECT id,server_id,tag,protocol,port,settings_json,stream_json,
       COALESCE(multiplier,1),COALESCE(remark,''),COALESCE(cert_id,0),enabled,created_at,
       COALESCE(share_name,'')
FROM inbounds WHERE id=?`, id).Scan(
		&in.ID, &in.ServerID, &in.Tag, &in.Protocol, &in.Port, &in.SettingsJSON, &in.StreamJSON,
		&in.Multiplier, &in.Remark, &in.CertID, &en, &in.CreatedAt, &shareName,
	)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	in.Enabled = en == 1
	in.ShareName = shareName
	writeJSON(w, 200, map[string]any{"inbound": in})
}

func (s *ServerApp) handleUpdateInbound(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var curServer, curTag, curProto, curSJ, curST, curRemark, curShare string
	var curPort int
	var curMult float64
	var curCert int64
	var curEn int
	err := s.db.QueryRow(`
SELECT server_id,tag,protocol,port,settings_json,stream_json,
       COALESCE(multiplier,1),COALESCE(remark,''),COALESCE(cert_id,0),enabled,COALESCE(share_name,'')
FROM inbounds WHERE id=?`, id).Scan(
		&curServer, &curTag, &curProto, &curPort, &curSJ, &curST,
		&curMult, &curRemark, &curCert, &curEn, &curShare,
	)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}

	body, err := parseInboundForm(r)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	// Track whether client supplied full JSON/maps (must not be wiped by form fields).
	settingsExplicit := body.Settings != nil || strings.TrimSpace(body.SettingsJSON) != ""
	streamExplicit := body.Stream != nil || strings.TrimSpace(body.StreamJSON) != ""
	// Transport form fields (fingerprint alone is too common — require real transport intent).
	formTransport := body.Network != "" || body.Security != "" || body.Path != "" || body.Host != "" ||
		body.SNI != "" || body.Dest != "" || body.PrivateKey != "" || body.PublicKey != "" ||
		body.ServiceName != "" || body.ShortID != "" || body.ALPN != ""
	// Only rebuild clients when identity secrets are explicitly provided.
	// Do NOT treat method/flow alone as rebuild triggers (UI always sends method/default flow).
	rebuildClients := body.UUID != "" || body.ClientID != "" || body.Password != ""
	patchFlow := body.Flow != "" && !rebuildClients
	patchMethod := body.Method != "" && !rebuildClients

	if body.ServerID == "" {
		body.ServerID = curServer
	}
	if body.Tag == "" {
		body.Tag = curTag
	}
	if body.Protocol == "" {
		body.Protocol = curProto
	}
	if body.Port <= 0 {
		body.Port = curPort
	}

	// Load current blobs when not explicitly replaced.
	if !settingsExplicit {
		_ = json.Unmarshal([]byte(curSJ), &body.Settings)
	}
	if !streamExplicit {
		_ = json.Unmarshal([]byte(curST), &body.Stream)
	}

	// Preserve Reality keys from current stream when form rebuilds without keys
	// (prevents accidental key rotation on every save).
	var curPriv, curPub, curSid string
	if curStream := map[string]any{}; json.Unmarshal([]byte(curST), &curStream) == nil {
		if rs, ok := curStream["realitySettings"].(map[string]any); ok {
			curPriv, _ = rs["privateKey"].(string)
			if ids, ok := rs["shortIds"].([]any); ok && len(ids) > 0 {
				curSid, _ = ids[0].(string)
			}
		}
		if curSettings := map[string]any{}; json.Unmarshal([]byte(curSJ), &curSettings) == nil {
			meta, _ := curSettings["bpanelMeta"].(map[string]any)
			if meta == nil {
				meta, _ = curSettings["xpanelMeta"].(map[string]any)
			}
			if meta != nil {
				curPub, _ = meta["publicKey"].(string)
				if curSid == "" {
					curSid, _ = meta["shortId"].(string)
				}
			}
		}
	}
	if formTransport && !streamExplicit {
		if body.PrivateKey == "" && curPriv != "" {
			body.PrivateKey = curPriv
		}
		if body.PublicKey == "" && curPub != "" {
			body.PublicKey = curPub
		}
		if body.ShortID == "" && curSid != "" {
			body.ShortID = curSid
		}
		// force rebuild from form fields
		body.Stream = nil
	}

	// Rebuild clients only when UUID/password explicitly provided.
	if rebuildClients && !settingsExplicit && body.Settings != nil {
		delete(body.Settings, "clients")
		if body.Password != "" {
			delete(body.Settings, "password")
		}
		if body.Method != "" {
			delete(body.Settings, "method")
		}
	}

	var settings map[string]any
	var stream map[string]any
	if settingsExplicit {
		settings = body.Settings
		if settings == nil {
			settings = map[string]any{}
		}
	} else {
		settings = s.composeInboundSettings(&body)
	}
	// Patch flow/method in place without regenerating clients/UUID.
	if !settingsExplicit && !rebuildClients {
		if patchMethod {
			settings["method"] = body.Method
		}
		if patchFlow {
			if clients, ok := settings["clients"].([]any); ok {
				for _, c := range clients {
					if m, ok := c.(map[string]any); ok {
						m["flow"] = body.Flow
					}
				}
			}
		}
	}
	// Keep settings in body for reality meta injection.
	body.Settings = settings

	if streamExplicit {
		stream = body.Stream
		if stream == nil {
			stream = map[string]any{"network": "tcp"}
		}
		body.Stream = stream
		if err := s.applyCertToStream(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		stream = body.Stream
	} else if formTransport {
		stream, err = s.composeInboundStream(&body)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
	} else {
		// pure partial (e.g. enabled-only): keep current stream, maybe cert
		stream = body.Stream
		if stream == nil {
			_ = json.Unmarshal([]byte(curST), &stream)
		}
		body.Stream = stream
		if body.CertID > 0 || body.EnableTLS {
			if err := s.applyCertToStream(&body); err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			stream = body.Stream
		}
	}

	certID := body.CertID
	if certID == 0 && curCert > 0 && !body.EnableTLS {
		if sec, _ := stream["security"].(string); sec == "tls" && body.Security != "none" && body.Security != "reality" {
			certID = curCert
		}
	}
	// Explicit none/reality clears cert binding
	if body.Security == "none" || body.Security == "reality" {
		if body.CertID == 0 {
			certID = 0
		}
	}

	mult := curMult
	if body.Multiplier != nil {
		mult = *body.Multiplier
	}
	if mult <= 0 {
		mult = 1
	}
	en := curEn
	if body.Enabled != nil {
		if *body.Enabled {
			en = 1
		} else {
			en = 0
		}
	}

	// remark / share_name: keep previous when omitted on partial updates
	remark := body.Remark
	if remark == "" && !formTransport && !rebuildClients && !settingsExplicit && body.Tag == curTag {
		remark = curRemark
	}
	shareName := body.ShareName
	if shareName == "" {
		// Prefer existing custom share name over falling back to tag on full form edits.
		if curShare != "" {
			shareName = curShare
		} else if body.Tag != "" {
			shareName = body.Tag
		} else {
			shareName = curShare
		}
	}

	sj, _ := json.Marshal(settings)
	st, _ := json.Marshal(stream)
	_, err = s.db.Exec(`
UPDATE inbounds SET server_id=?, tag=?, protocol=?, port=?, settings_json=?, stream_json=?,
  multiplier=?, remark=?, cert_id=?, enabled=?, share_name=? WHERE id=?`,
		body.ServerID, body.Tag, body.Protocol, body.Port, string(sj), string(st),
		mult, remark, certID, en, shareName, id,
	)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	s.bumpServer(body.ServerID)
	if curServer != body.ServerID {
		s.bumpServer(curServer)
	}
	s.audit("admin", "update_inbound", fmt.Sprintf("#%d %s", id, body.Tag))

	var ip, domain string
	_ = s.db.QueryRow(`SELECT public_ip, COALESCE(domain,'') FROM servers WHERE id=?`, body.ServerID).Scan(&ip, &domain)
	addr := domain
	if addr == "" {
		addr = ip
	}
	if addr == "" {
		addr = "YOUR_IP"
	}
	name := shareName
	if name == "" {
		name = body.Tag
	}
	link := buildShareLink(body.Protocol, name, addr, body.Port, string(sj), string(st))
	writeJSON(w, 200, map[string]any{
		"ok": true, "id": id, "tag": body.Tag,
		"settings": settings, "stream": stream, "share_link": link,
	})
}
