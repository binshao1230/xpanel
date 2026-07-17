package protocol

// Agent registration
type RegisterRequest struct {
	Token        string `json:"token"`
	Hostname     string `json:"hostname"`
	AgentVersion string `json:"agent_version"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
}

type RegisterResponse struct {
	ServerID        string `json:"server_id"`
	AgentKey        string `json:"agent_key"`
	PollIntervalSec int    `json:"poll_interval_sec"`
	APIPort         int    `json:"api_port"`
}

// Heartbeat
type TrafficSnapshot struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

type UserTraffic struct {
	Email string `json:"email"`
	Up    int64  `json:"up"`
	Down  int64  `json:"down"`
}

type HeartbeatRequest struct {
	ServerID      string          `json:"server_id"`
	PublicIP      string          `json:"public_ip"`
	XrayRunning   bool            `json:"xray_running"`
	ConfigVersion int64           `json:"config_version"`
	UptimeSec     int64           `json:"uptime_sec"`
	Traffic       TrafficSnapshot `json:"traffic"`
	UserTraffic   []UserTraffic   `json:"user_traffic,omitempty"`
}

type HeartbeatResponse struct {
	OK                   bool     `json:"ok"`
	DesiredConfigVersion int64    `json:"desired_config_version"`
	Commands             []string `json:"commands,omitempty"`
}

// CertFile is pushed to agents and written under data/certs/<domain>/.
type CertFile struct {
	Domain  string `json:"domain"`
	CertPEM string `json:"cert_pem"`
	KeyPEM  string `json:"key_pem"`
}

// Config pull
type ConfigBundle struct {
	Version  int64          `json:"version"`
	XrayJSON map[string]any `json:"xray_json"`
	Checksum string         `json:"checksum"`
	APIPort  int            `json:"api_port"`
	Certs    []CertFile     `json:"certs,omitempty"`
}

const (
	HeaderAgentKey  = "X-Agent-Key"
	CmdReloadConfig = "reload_config"
	DefaultAPIPort  = 10085
	// CertPlaceholder must match acme.CertPlaceholder — agent expands to local certs dir.
	CertPlaceholder = "{{CERTS}}"
)
