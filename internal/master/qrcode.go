package master

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// handleQRCode generates a PNG QR code for share links / text.
// GET  /api/qr?text=...&size=256
// POST /api/qr  { "text": "...", "size": 256 }
func (s *ServerApp) handleQRCode(w http.ResponseWriter, r *http.Request) {
	text := ""
	size := 280
	if r.Method == http.MethodPost {
		var body struct {
			Text string `json:"text"`
			Size int    `json:"size"`
		}
		if err := readJSON(r, &body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad json"})
			return
		}
		text = strings.TrimSpace(body.Text)
		if body.Size > 0 {
			size = body.Size
		}
	} else {
		text = strings.TrimSpace(r.URL.Query().Get("text"))
		if text == "" {
			text = strings.TrimSpace(r.URL.Query().Get("data"))
		}
		if v := r.URL.Query().Get("size"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				size = n
			}
		}
	}
	if text == "" {
		writeJSON(w, 400, map[string]string{"error": "text required"})
		return
	}
	if len(text) > 4096 {
		writeJSON(w, 400, map[string]string{"error": "text too long (max 4096)"})
		return
	}
	if size < 128 {
		size = 128
	}
	if size > 1024 {
		size = 1024
	}

	png, err := qrcode.Encode(text, qrcode.Medium, size)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "qr encode: " + err.Error()})
		return
	}

	// support raw image via Accept or format=png
	if r.URL.Query().Get("format") == "png" || strings.Contains(r.Header.Get("Accept"), "image/png") {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(200)
		_, _ = w.Write(png)
		return
	}

	writeJSON(w, 200, map[string]any{
		"ok":         true,
		"png_base64": base64.StdEncoding.EncodeToString(png),
		"data_url":   "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		"size":       size,
		"bytes":      len(png),
	})
}

// handleInboundQR returns QR for a single inbound share link.
func (s *ServerApp) handleInboundQR(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var tag, proto, sj, st, shareName string
	var port int
	var serverID string
	err := s.db.QueryRow(`
SELECT server_id, tag, protocol, port, settings_json, stream_json, COALESCE(share_name,'')
FROM inbounds WHERE id=?`, id).Scan(&serverID, &tag, &proto, &port, &sj, &st, &shareName)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	var ip, domain string
	_ = s.db.QueryRow(`SELECT public_ip, COALESCE(domain,'') FROM servers WHERE id=?`, serverID).Scan(&ip, &domain)
	addr := domain
	if addr == "" {
		addr = ip
	}
	if addr == "" {
		addr = "YOUR_IP"
	}
	name := shareName
	if name == "" {
		name = tag
	}
	link := buildShareLink(proto, name, addr, port, sj, st)
	if link == "" {
		writeJSON(w, 400, map[string]string{"error": "该协议暂无分享链接，无法生成二维码"})
		return
	}
	size := 280
	if v := r.URL.Query().Get("size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 128 && n <= 1024 {
			size = n
		}
	}
	png, err := qrcode.Encode(link, qrcode.Medium, size)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	b64s := base64.StdEncoding.EncodeToString(png)
	writeJSON(w, 200, map[string]any{
		"ok":         true,
		"id":         id,
		"name":       name,
		"protocol":   proto,
		"link":       link,
		"png_base64": b64s,
		"data_url":   "data:image/png;base64," + b64s,
		"size":       size,
	})
}
