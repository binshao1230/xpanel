package protocol

// WebSocket message envelope between Master and Agent.
type WSEnvelope struct {
	Type string          `json:"type"`
	Data jsonRawMessage  `json:"data,omitempty"`
}

// jsonRawMessage avoids importing encoding/json in docs; real type is json.RawMessage in code.
// Use map for flexibility in this package without circular deps.

const (
	WSTypeHello     = "hello"
	WSTypePing      = "ping"
	WSTypePong      = "pong"
	WSTypeHeartbeat = "heartbeat"
	WSTypeHBResp    = "heartbeat_resp"
	WSTypeCommand   = "command"
	WSTypeConfig    = "config" // full config push optional
	WSTypeError     = "error"
)

// Command payload
type WSCommand struct {
	Name string `json:"name"` // reload_config, restart_xray
}

// Prefer encoding/json.RawMessage at call sites.
type jsonRawMessage = []byte
