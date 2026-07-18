package agent

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/binshao1230/bpanel/internal/protocol"
)

func (a *Agent) runWebSocket() error {
	a.mu.Lock()
	key := a.agentKey
	master := a.cfg.MasterURL
	a.mu.Unlock()
	if key == "" {
		return errNoKey
	}

	u, err := url.Parse(strings.TrimRight(master, "/"))
	if err != nil {
		return err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = "/api/agent/ws"
	q := u.Query()
	q.Set("key", key)
	u.RawQuery = q.Encode()

	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	header := http.Header{}
	header.Set(protocol.HeaderAgentKey, key)
	conn, _, err := dialer.Dial(u.String(), header)
	if err != nil {
		return err
	}
	defer conn.Close()
	log.Printf("websocket connected %s", u.String())

	// reader loop + heartbeat ticker
	errCh := make(chan error, 1)
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			var env struct {
				Type string          `json:"type"`
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(data, &env); err != nil {
				continue
			}
			switch env.Type {
			case protocol.WSTypePing:
				_ = conn.WriteJSON(map[string]string{"type": protocol.WSTypePong})
			case protocol.WSTypeCommand:
				var cmd protocol.WSCommand
				_ = json.Unmarshal(env.Data, &cmd)
				if cmd.Name == protocol.CmdReloadConfig || cmd.Name == "reload_config" {
					log.Printf("ws command: reload_config")
					if err := a.pullAndApply(); err != nil {
						log.Printf("ws reload: %v", err)
					}
				}
				if cmd.Name == "restart_xray" && a.xray != nil {
					_ = a.xray.Restart()
				}
			case protocol.WSTypeHBResp:
				var resp protocol.HeartbeatResponse
				if json.Unmarshal(env.Data, &resp) == nil {
					a.mu.Lock()
					local := a.configVersion
					a.mu.Unlock()
					if resp.DesiredConfigVersion > local {
						if err := a.pullAndApply(); err != nil {
							log.Printf("ws hb reload: %v", err)
						}
					}
				}
			}
		}
	}()

	hb := time.NewTicker(15 * time.Second)
	defer hb.Stop()
	wd := time.NewTicker(10 * time.Second)
	defer wd.Stop()
	// send first hb immediately
	if err := a.wsHeartbeat(conn); err != nil {
		return err
	}
	for {
		select {
		case <-a.stopCh:
			return nil
		case err := <-errCh:
			return err
		case <-wd.C:
			a.ensureXray()
		case <-hb.C:
			if err := a.wsHeartbeat(conn); err != nil {
				return err
			}
		}
	}
}

func (a *Agent) wsHeartbeat(conn *websocket.Conn) error {
	running := a.xray != nil && a.xray.IsRunning()
	bin := ""
	if a.xray != nil {
		bin = a.xray.Bin()
	}
	traf, users := queryXrayStats(bin, protocol.DefaultAPIPort)
	a.mu.Lock()
	req := protocol.HeartbeatRequest{
		ServerID:      a.serverID,
		PublicIP:      detectPublicIP(a.client),
		XrayRunning:   running,
		ConfigVersion: a.configVersion,
		UptimeSec:     int64(time.Since(a.startedAt).Seconds()),
		Traffic:       traf,
		UserTraffic:   users,
		LastError:     a.lastApplyErr,
	}
	a.mu.Unlock()
	raw, _ := json.Marshal(req)
	return conn.WriteJSON(map[string]any{
		"type": protocol.WSTypeHeartbeat,
		"data": json.RawMessage(raw),
	})
}

var errNoKey = errString("no agent key")

type errString string

func (e errString) Error() string { return string(e) }
