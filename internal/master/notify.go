package master

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

func (s *ServerApp) getSetting(key string) string {
	var v string
	_ = s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	return v
}

func (s *ServerApp) notifyServerStatus(serverID string, online bool) {
	url := s.getSetting("webhook_url")
	if url == "" {
		return
	}
	var name, ip string
	_ = s.db.QueryRow(`SELECT name, public_ip FROM servers WHERE id=?`, serverID).Scan(&name, &ip)
	status := "offline"
	if online {
		status = "online"
	}
	body, _ := json.Marshal(map[string]any{
		"event":     "server_status",
		"server_id": serverID,
		"name":      name,
		"public_ip": ip,
		"status":    status,
		"time":      time.Now().Unix(),
	})
	go func() {
		cli := &http.Client{Timeout: 8 * time.Second}
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := cli.Do(req)
		if err != nil {
			log.Printf("webhook: %v", err)
			return
		}
		resp.Body.Close()
	}()
}

// watchOffline periodically marks long-silent servers offline and notifies.
func (s *ServerApp) startOfflineWatcher() {
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			cutoff := nowUnix() - 90
			rows, err := s.db.Query(`SELECT id FROM servers WHERE status='online' AND last_seen > 0 AND last_seen < ?`, cutoff)
			if err != nil {
				continue
			}
			var ids []string
			for rows.Next() {
				var id string
				if rows.Scan(&id) == nil {
					ids = append(ids, id)
				}
			}
			rows.Close()
			for _, id := range ids {
				// skip if still on websocket
				if s.hub != nil && s.hub.Online(id) {
					continue
				}
				_, _ = s.db.Exec(`UPDATE servers SET status='offline' WHERE id=?`, id)
				s.notifyServerStatus(id, false)
			}
		}
	}()
}
