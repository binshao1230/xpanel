package master

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xpanel/xpanel/internal/acme"
)

func (s *ServerApp) handleIssueACME(w http.ResponseWriter, r *http.Request) {
	if s.acme == nil {
		writeJSON(w, 500, map[string]string{"error": "acme manager not initialized"})
		return
	}
	var body struct {
		Name         string `json:"name"`
		Domain       string `json:"domain"`
		Domains      string `json:"domains"` // comma-separated SANs
		Email        string `json:"email"`
		Challenge    string `json:"challenge"`    // http-01 | dns-01
		DNSProvider  string `json:"dns_provider"` // cloudflare|alidns|tencentcloud
		DNSAPIToken  string `json:"dns_api_token"`
		DNSAPIKey    string `json:"dns_api_key"`
		DNSAPISecret string `json:"dns_api_secret"`
		ServerID     string `json:"server_id"` // empty = all agents
		Staging      bool   `json:"staging"`
		AutoRenew    *bool  `json:"auto_renew"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad json"})
		return
	}
	domain := strings.TrimSpace(strings.ToLower(body.Domain))
	if domain == "" {
		writeJSON(w, 400, map[string]string{"error": "domain required"})
		return
	}
	if body.Email == "" {
		// try settings
		_ = s.db.QueryRow(`SELECT value FROM settings WHERE key='acme_email'`).Scan(&body.Email)
	}
	if body.Email == "" {
		writeJSON(w, 400, map[string]string{"error": "email required (or set acme_email in settings)"})
		return
	}
	if body.Name == "" {
		body.Name = domain
	}
	if body.Challenge == "" {
		body.Challenge = acme.ChallengeHTTP01
	}
	if body.Challenge == acme.ChallengeDNS01 && body.DNSProvider == "" {
		body.DNSProvider = acme.ProviderCloudflare
	}
	autoRenew := true
	if body.AutoRenew != nil {
		autoRenew = *body.AutoRenew
	}

	domains := []string{domain}
	for _, d := range strings.Split(body.Domains, ",") {
		d = strings.TrimSpace(strings.ToLower(d))
		if d != "" && d != domain {
			domains = append(domains, d)
		}
	}

	// insert pending row
	res, err := s.db.Exec(
		`INSERT INTO certificates(name,domain,cert_pem,key_pem,provider,expire_at,created_at,email,challenge,dns_provider,status,last_error,auto_renew,server_id)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		body.Name, domain, "", "", body.Challenge, 0, nowUnix(),
		body.Email, body.Challenge, body.DNSProvider, "pending", "", boolToInt(autoRenew), body.ServerID,
	)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()

	req := acme.IssueRequest{
		Email:        body.Email,
		Domains:      domains,
		Challenge:    body.Challenge,
		DNSProvider:  body.DNSProvider,
		DNSAPIToken:  body.DNSAPIToken,
		DNSAPIKey:    body.DNSAPIKey,
		DNSAPISecret: body.DNSAPISecret,
		Staging:      body.Staging,
		Bundle:       true,
	}
	s.fillDNSCredsFromSettings(&req)

	result, err := s.acme.Obtain(req)
	if err != nil {
		_, _ = s.db.Exec(`UPDATE certificates SET status='error', last_error=? WHERE id=?`, err.Error(), id)
		writeJSON(w, 502, map[string]any{"id": id, "error": err.Error()})
		return
	}
	if err := s.acme.WriteFiles(domain, result.CertPEM, result.KeyPEM); err != nil {
		log.Printf("acme write files: %v", err)
	}
	provider := body.Challenge
	if body.DNSProvider != "" {
		provider = body.Challenge + ":" + body.DNSProvider
	}
	if body.Staging {
		provider += ":staging"
	}
	_, err = s.db.Exec(
		`UPDATE certificates SET cert_pem=?, key_pem=?, provider=?, expire_at=?, status='active', last_error='', email=?, challenge=?, dns_provider=?, auto_renew=?, server_id=? WHERE id=?`,
		result.CertPEM, result.KeyPEM, provider, result.ExpireAt.Unix(),
		body.Email, body.Challenge, body.DNSProvider, boolToInt(autoRenew), body.ServerID, id,
	)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	s.persistACMESettings(body.Email, req)
	// push certs to agents
	s.bumpServer(body.ServerID)

	writeJSON(w, 200, map[string]any{
		"id":        id,
		"domain":    domain,
		"expire_at": result.ExpireAt.Unix(),
		"status":    "active",
		"path":      "data/certs/" + domain + "/",
		"deployed":  true,
	})
}

func (s *ServerApp) fillDNSCredsFromSettings(req *acme.IssueRequest) {
	if req.DNSAPIToken == "" {
		_ = s.db.QueryRow(`SELECT value FROM settings WHERE key='cf_dns_api_token'`).Scan(&req.DNSAPIToken)
	}
	if req.DNSAPIKey == "" {
		_ = s.db.QueryRow(`SELECT value FROM settings WHERE key='dns_api_key'`).Scan(&req.DNSAPIKey)
	}
	if req.DNSAPISecret == "" {
		_ = s.db.QueryRow(`SELECT value FROM settings WHERE key='dns_api_secret'`).Scan(&req.DNSAPISecret)
	}
}

func (s *ServerApp) persistACMESettings(email string, req acme.IssueRequest) {
	_, _ = s.db.Exec(`INSERT INTO settings(key,value) VALUES('acme_email',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, email)
	if req.DNSAPIToken != "" {
		_, _ = s.db.Exec(`INSERT INTO settings(key,value) VALUES('cf_dns_api_token',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, req.DNSAPIToken)
	}
	if req.DNSAPIKey != "" {
		_, _ = s.db.Exec(`INSERT INTO settings(key,value) VALUES('dns_api_key',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, req.DNSAPIKey)
	}
	if req.DNSAPISecret != "" {
		_, _ = s.db.Exec(`INSERT INTO settings(key,value) VALUES('dns_api_secret',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, req.DNSAPISecret)
	}
}

func (s *ServerApp) handleACMEProviders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"challenges":    []string{acme.ChallengeHTTP01, acme.ChallengeDNS01},
		"dns_providers": acme.SupportedDNSProviders(),
	})
}

func (s *ServerApp) handleRenewCert(w http.ResponseWriter, r *http.Request) {
	if s.acme == nil {
		writeJSON(w, 500, map[string]string{"error": "acme not ready"})
		return
	}
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var domain, email, challenge, dnsProvider, certPEM string
	err := s.db.QueryRow(
		`SELECT domain, COALESCE(email,''), COALESCE(challenge,'http-01'), COALESCE(dns_provider,''), cert_pem FROM certificates WHERE id=?`, id,
	).Scan(&domain, &email, &challenge, &dnsProvider, &certPEM)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	if email == "" {
		_ = s.db.QueryRow(`SELECT value FROM settings WHERE key='acme_email'`).Scan(&email)
	}
	if email == "" {
		writeJSON(w, 400, map[string]string{"error": "no acme email on record"})
		return
	}
	staging := strings.Contains(challenge, "staging") || strings.Contains(dnsProvider, "staging")
	// normalize challenge field (may be stored as provider string historically)
	ch := challenge
	if strings.HasPrefix(ch, "dns") {
		ch = acme.ChallengeDNS01
	} else if strings.Contains(ch, "http") || ch == "" {
		ch = acme.ChallengeHTTP01
	}
	req := acme.IssueRequest{
		Email: email, Domains: []string{domain}, Challenge: ch,
		DNSProvider: dnsProvider, Staging: staging, Bundle: true,
	}
	s.fillDNSCredsFromSettings(&req)

	_, _ = s.db.Exec(`UPDATE certificates SET status='pending', last_error='' WHERE id=?`, id)
	result, err := s.acme.Renew(req)
	if err != nil {
		_, _ = s.db.Exec(`UPDATE certificates SET status='error', last_error=? WHERE id=?`, err.Error(), id)
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	_ = s.acme.WriteFiles(domain, result.CertPEM, result.KeyPEM)
	var sid string
	_ = s.db.QueryRow(`SELECT COALESCE(server_id,'') FROM certificates WHERE id=?`, id).Scan(&sid)
	_, _ = s.db.Exec(
		`UPDATE certificates SET cert_pem=?, key_pem=?, expire_at=?, status='active', last_error='' WHERE id=?`,
		result.CertPEM, result.KeyPEM, result.ExpireAt.Unix(), id,
	)
	s.bumpServer(sid)
	writeJSON(w, 200, map[string]any{"ok": true, "expire_at": result.ExpireAt.Unix(), "deployed": true})
}

func (s *ServerApp) startACMERenewer() {
	if s.acme == nil {
		return
	}
	go func() {
		// initial delay
		time.Sleep(2 * time.Minute)
		ticker := time.NewTicker(12 * time.Hour)
		defer ticker.Stop()
		s.renewExpiringCerts()
		for range ticker.C {
			s.renewExpiringCerts()
		}
	}()
}

func (s *ServerApp) renewExpiringCerts() {
	// renew if expire within 30 days and auto_renew
	deadline := time.Now().Add(30 * 24 * time.Hour).Unix()
	rows, err := s.db.Query(
		`SELECT id, domain, COALESCE(email,''), COALESCE(challenge,'http-01'), COALESCE(dns_provider,''), provider
		 FROM certificates WHERE auto_renew=1 AND status='active' AND expire_at>0 AND expire_at < ?`,
		deadline,
	)
	if err != nil {
		log.Printf("acme renewer query: %v", err)
		return
	}
	defer rows.Close()
	type item struct {
		id                         int64
		domain, email, ch, dns, pr string
	}
	var list []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.domain, &it.email, &it.ch, &it.dns, &it.pr); err == nil {
			list = append(list, it)
		}
	}
	for _, it := range list {
		email := it.email
		if email == "" {
			_ = s.db.QueryRow(`SELECT value FROM settings WHERE key='acme_email'`).Scan(&email)
		}
		if email == "" {
			continue
		}
		ch := it.ch
		if strings.Contains(it.pr, "dns") || strings.HasPrefix(ch, "dns") {
			ch = acme.ChallengeDNS01
		} else {
			ch = acme.ChallengeHTTP01
		}
		staging := strings.Contains(it.pr, "staging")
		req := acme.IssueRequest{
			Email: email, Domains: []string{it.domain}, Challenge: ch,
			DNSProvider: it.dns, Staging: staging, Bundle: true,
		}
		s.fillDNSCredsFromSettings(&req)
		log.Printf("acme auto-renew %s (id=%d)", it.domain, it.id)
		result, err := s.acme.Renew(req)
		if err != nil {
			log.Printf("acme renew %s: %v", it.domain, err)
			_, _ = s.db.Exec(`UPDATE certificates SET status='error', last_error=? WHERE id=?`, err.Error(), it.id)
			continue
		}
		_ = s.acme.WriteFiles(it.domain, result.CertPEM, result.KeyPEM)
		var sid string
		_ = s.db.QueryRow(`SELECT COALESCE(server_id,'') FROM certificates WHERE id=?`, it.id).Scan(&sid)
		_, _ = s.db.Exec(
			`UPDATE certificates SET cert_pem=?, key_pem=?, expire_at=?, status='active', last_error='' WHERE id=?`,
			result.CertPEM, result.KeyPEM, result.ExpireAt.Unix(), it.id,
		)
		s.bumpServer(sid)
		log.Printf("acme renewed %s until %s", it.domain, result.ExpireAt.Format(time.RFC3339))
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// handleDeployCert forces agents to re-pull config+certs (bump version).
func (s *ServerApp) handleDeployCert(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var sid, domain, status string
	err := s.db.QueryRow(`SELECT COALESCE(server_id,''), domain, COALESCE(status,'') FROM certificates WHERE id=?`, id).
		Scan(&sid, &domain, &status)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	if status != "active" {
		writeJSON(w, 400, map[string]string{"error": "certificate not active"})
		return
	}
	s.bumpServer(sid)
	writeJSON(w, 200, map[string]any{"ok": true, "domain": domain, "server_id": sid})
}
