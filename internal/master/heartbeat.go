package master

import (
	"time"

	"github.com/binshao1230/bpanel/internal/protocol"
)

// applyHeartbeat updates DB from agent heartbeat (HTTP or WS).
func (s *ServerApp) applyHeartbeat(sid string, req *protocol.HeartbeatRequest) {
	xrayRun := 0
	if req.XrayRunning {
		xrayRun = 1
	}
	var prevStatus string
	var prevUp, prevDown, prevSeen int64
	_ = s.db.QueryRow(`SELECT status, COALESCE(traffic_up,0), COALESCE(traffic_down,0), COALESCE(last_seen,0) FROM servers WHERE id=?`, sid).
		Scan(&prevStatus, &prevUp, &prevDown, &prevSeen)

	// estimate bytes/s
	var speedUp, speedDown int64
	now := nowUnix()
	if prevSeen > 0 && now > prevSeen && req.Traffic.Up >= prevUp {
		dt := now - prevSeen
		if dt > 0 {
			speedUp = (req.Traffic.Up - prevUp) / dt
			speedDown = (req.Traffic.Down - prevDown) / dt
		}
	}

	_, _ = s.db.Exec(
		`UPDATE servers SET public_ip=?, status='online', last_seen=?, xray_running=?, traffic_up=?, traffic_down=?, speed_up=?, speed_down=?, agent_error=?, xray_version=COALESCE(NULLIF(?,''), xray_version) WHERE id=?`,
		req.PublicIP, now, xrayRun, req.Traffic.Up, req.Traffic.Down, speedUp, speedDown, req.LastError, req.XrayVersion, sid,
	)
	if s.rt != nil && len(req.LogTail) > 0 {
		s.rt.StoreLogs(sid, req.LogTail)
	}
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
