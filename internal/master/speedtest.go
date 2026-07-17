package master

import (
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"time"
)

type SpeedResult struct {
	Target string  `json:"target"`
	TCPMs  float64 `json:"tcp_ms"`
	TLSMs  float64 `json:"tls_ms,omitempty"`
	Error  string  `json:"error,omitempty"`
	At     int64   `json:"at"`
}

func (s *ServerApp) handleSpeedTest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Host      string `json:"host"`
		Port      int    `json:"port"`
		TLS       bool   `json:"tls"`
		ServerID  string `json:"server_id"`
		InboundID int64  `json:"inbound_id"`
		TimeoutMs int    `json:"timeout_ms"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad json"})
		return
	}
	if body.TimeoutMs <= 0 {
		body.TimeoutMs = 5000
	}
	if body.Host == "" && body.InboundID > 0 {
		var port int
		var sid string
		_ = s.db.QueryRow(`SELECT server_id, port FROM inbounds WHERE id=?`, body.InboundID).Scan(&sid, &port)
		var ip string
		_ = s.db.QueryRow(`SELECT public_ip FROM servers WHERE id=?`, sid).Scan(&ip)
		body.Host = ip
		body.Port = port
	}
	if body.Host == "" && body.ServerID != "" {
		_ = s.db.QueryRow(`SELECT public_ip FROM servers WHERE id=?`, body.ServerID).Scan(&body.Host)
	}
	if body.Host == "" || body.Port <= 0 {
		writeJSON(w, 400, map[string]string{"error": "host and port required (or inbound_id)"})
		return
	}

	res := probeTarget(body.Host, body.Port, body.TLS, time.Duration(body.TimeoutMs)*time.Millisecond)
	raw, _ := json.Marshal(res)
	key := "speedtest:" + body.Host + ":" + strconv.Itoa(body.Port)
	_, _ = s.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, string(raw))
	writeJSON(w, 200, res)
}

func (s *ServerApp) handleSpeedTestBatch(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
SELECT i.id, i.port, s.public_ip, i.tag, s.name FROM inbounds i
JOIN servers s ON s.id=i.server_id WHERE i.enabled=1 AND s.public_ip!='' LIMIT 50`)
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
		if err := rows.Scan(&id, &port, &ip, &tag, &sname); err != nil {
			continue
		}
		res := probeTarget(ip, port, false, 3*time.Second)
		results = append(results, map[string]any{
			"inbound_id": id, "tag": tag, "server": sname, "result": res,
		})
	}
	if results == nil {
		results = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"results": results})
}

func probeTarget(host string, port int, doTLS bool, timeout time.Duration) SpeedResult {
	target := net.JoinHostPort(host, strconv.Itoa(port))
	res := SpeedResult{Target: target, At: time.Now().Unix()}
	start := time.Now()
	conn, err := net.DialTimeout("tcp", target, timeout)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.TCPMs = float64(time.Since(start).Microseconds()) / 1000.0
	if !doTLS {
		_ = conn.Close()
		return res
	}
	tlsStart := time.Now()
	tconn := tls.Client(conn, &tls.Config{InsecureSkipVerify: true, ServerName: host, MinVersion: tls.VersionTLS12})
	_ = tconn.SetDeadline(time.Now().Add(timeout))
	if err := tconn.Handshake(); err != nil {
		res.Error = "tls: " + err.Error()
		_ = conn.Close()
		return res
	}
	res.TLSMs = float64(time.Since(tlsStart).Microseconds()) / 1000.0
	_ = tconn.Close()
	return res
}
