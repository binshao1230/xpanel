package master

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/binshao1230/bpanel/internal/protocol"
)

var upgrader = websocket.Upgrader{
	// Agents are non-browser clients (no Origin). Reject cross-site browser sockets.
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		return originOK(r, origin)
	},
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

type agentConn struct {
	serverID string
	conn     *websocket.Conn
	send     chan []byte
}

type Hub struct {
	mu    sync.RWMutex
	conns map[string]*agentConn // serverID -> conn
}

func newHub() *Hub {
	return &Hub{conns: make(map[string]*agentConn)}
}

func (h *Hub) set(c *agentConn) {
	h.mu.Lock()
	if old, ok := h.conns[c.serverID]; ok {
		close(old.send)
		_ = old.conn.Close()
	}
	h.conns[c.serverID] = c
	h.mu.Unlock()
}

func (h *Hub) remove(serverID string, c *agentConn) {
	h.mu.Lock()
	if cur, ok := h.conns[serverID]; ok && cur == c {
		delete(h.conns, serverID)
	}
	h.mu.Unlock()
}

func (h *Hub) Online(serverID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.conns[serverID]
	return ok
}

func (h *Hub) PushCommand(serverID, name string) bool {
	payload, _ := json.Marshal(map[string]any{
		"type": protocol.WSTypeCommand,
		"data": protocol.WSCommand{Name: name},
	})
	return h.send(serverID, payload)
}

func (h *Hub) send(serverID string, raw []byte) bool {
	h.mu.RLock()
	c, ok := h.conns[serverID]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	select {
	case c.send <- raw:
		return true
	default:
		return false
	}
}

func (h *Hub) BroadcastCommand(name string) int {
	h.mu.RLock()
	ids := make([]string, 0, len(h.conns))
	for id := range h.conns {
		ids = append(ids, id)
	}
	h.mu.RUnlock()
	n := 0
	for _, id := range ids {
		if h.PushCommand(id, name) {
			n++
		}
	}
	return n
}

func (s *ServerApp) handleAgentWS(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get(protocol.HeaderAgentKey)
	if key == "" {
		key = r.URL.Query().Get("key")
	}
	if key == "" {
		http.Error(w, "missing agent key", 401)
		return
	}
	var sid string
	err := s.db.QueryRow(`SELECT id FROM servers WHERE agent_key=?`, key).Scan(&sid)
	if err != nil {
		http.Error(w, "invalid agent key", 401)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	ac := &agentConn{
		serverID: sid,
		conn:     conn,
		send:     make(chan []byte, 16),
	}
	s.hub.set(ac)
	_, _ = s.db.Exec(`UPDATE servers SET status='online', last_seen=?, conn_mode='websocket' WHERE id=?`, nowUnix(), sid)
	s.notifyServerStatus(sid, true)

	// writer
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case msg, ok := <-ac.send:
				if !ok {
					_ = conn.WriteMessage(websocket.CloseMessage, []byte{})
					return
				}
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					return
				}
			case <-ticker.C:
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				ping, _ := json.Marshal(map[string]string{"type": protocol.WSTypePing})
				if err := conn.WriteMessage(websocket.TextMessage, ping); err != nil {
					return
				}
			}
		}
	}()

	// reader
	defer func() {
		s.hub.remove(sid, ac)
		_ = conn.Close()
		// don't force offline immediately — heartbeat HTTP may still work
	}()

	hello, _ := json.Marshal(map[string]any{"type": protocol.WSTypeHello, "data": map[string]any{"server_id": sid}})
	ac.send <- hello

	for {
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
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
		case protocol.WSTypePong:
			// ok
		case protocol.WSTypeHeartbeat:
			var req protocol.HeartbeatRequest
			if err := json.Unmarshal(env.Data, &req); err != nil {
				continue
			}
			req.ServerID = sid
			s.applyHeartbeat(sid, &req)
			var desired int64
			_ = s.db.QueryRow(`SELECT config_version FROM servers WHERE id=?`, sid).Scan(&desired)
			var cmds []string
			if req.ConfigVersion < desired {
				cmds = append(cmds, protocol.CmdReloadConfig)
			}
			resp, _ := json.Marshal(map[string]any{
				"type": protocol.WSTypeHBResp,
				"data": protocol.HeartbeatResponse{
					OK: true, DesiredConfigVersion: desired, Commands: cmds,
				},
			})
			select {
			case ac.send <- resp:
			default:
			}
		}
	}
}
