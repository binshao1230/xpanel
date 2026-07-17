package master

import (
	"time"

	"github.com/xpanel/xpanel/internal/protocol"
)

// applyHeartbeat updates DB from agent heartbeat (HTTP or WS).
func (s *ServerApp) applyHeartbeat(sid string, req *protocol.HeartbeatRequest) {
	xrayRun := 0
	if req.XrayRunning {
		xrayRun = 1
	}
	var prevStatus string
	_ = s.db.QueryRow(`SELECT status FROM servers WHERE id=?`, sid).Scan(&prevStatus)

	_, _ = s.db.Exec(
		`UPDATE servers SET public_ip=?, status='online', last_seen=?, xray_running=?, traffic_up=?, traffic_down=? WHERE id=?`,
		req.PublicIP, nowUnix(), xrayRun, req.Traffic.Up, req.Traffic.Down, sid,
	)
	if prevStatus != "online" {
		s.notifyServerStatus(sid, true)
	}

	day := dayKey(time.Now())
	_, _ = s.db.Exec(
		`INSERT INTO traffic_daily(day,user_id,server_id,up,down) VALUES(?,0,?,?,?)
		 ON CONFLICT(day,user_id,server_id) DO UPDATE SET up=excluded.up, down=excluded.down`,
		day, sid, req.Traffic.Up, req.Traffic.Down,
	)
	for _, ut := range req.UserTraffic {
		email := ut.Email
		if email == "" {
			continue
		}
		name := email
		for i := 0; i < len(email); i++ {
			if email[i] == '@' {
				name = email[:i]
				break
			}
		}
		total := ut.Up + ut.Down
		if total <= 0 {
			continue
		}
		_, _ = s.db.Exec(`UPDATE users SET traffic_used=? WHERE username=? OR username=?`, total, name, email)
	}
}
