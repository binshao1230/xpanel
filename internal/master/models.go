package master

type User struct {
	ID             int64  `json:"id"`
	Username       string `json:"username"`
	Role           string `json:"role"`
	SubscribeToken string `json:"subscribe_token"`
	PlanID         int64  `json:"plan_id"`
	TrafficLimit   int64  `json:"traffic_limit"`
	TrafficUsed    int64  `json:"traffic_used"`
	SpeedLimit     int64  `json:"speed_limit"`
	ExpireAt       int64  `json:"expire_at"`
	Enabled        bool   `json:"enabled"`
	Remark         string `json:"remark"`
	CreatedAt      int64  `json:"created_at"`
}

type Plan struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	TrafficLimit int64  `json:"traffic_limit"`
	SpeedLimit   int64  `json:"speed_limit"`
	DurationDays int    `json:"duration_days"`
	PriceNote    string `json:"price_note"`
	Enabled      bool   `json:"enabled"`
	CreatedAt    int64  `json:"created_at"`
}

type Server struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	InstallToken  string `json:"install_token,omitempty"`
	AgentKey      string `json:"-"`
	Hostname      string `json:"hostname"`
	PublicIP      string `json:"public_ip"`
	Status        string `json:"status"`
	LastSeen      int64  `json:"last_seen"`
	ConfigVersion int64  `json:"config_version"`
	AgentVersion  string `json:"agent_version"`
	XrayRunning   bool   `json:"xray_running"`
	TrafficUp     int64  `json:"traffic_up"`
	TrafficDown   int64  `json:"traffic_down"`
	ConnMode      string `json:"conn_mode"`
	CreatedAt     int64  `json:"created_at"`
	Online        bool   `json:"online"`
}

type Inbound struct {
	ID           int64   `json:"id"`
	ServerID     string  `json:"server_id"`
	Tag          string  `json:"tag"`
	Protocol     string  `json:"protocol"`
	Port         int     `json:"port"`
	SettingsJSON string  `json:"settings_json"`
	StreamJSON   string  `json:"stream_json"`
	Multiplier   float64 `json:"multiplier"`
	Remark       string  `json:"remark"`
	CertID       int64   `json:"cert_id"`
	Enabled      bool    `json:"enabled"`
	CreatedAt    int64   `json:"created_at"`
}

type Outbound struct {
	ID           int64  `json:"id"`
	ServerID     string `json:"server_id"`
	Tag          string `json:"tag"`
	Protocol     string `json:"protocol"`
	SettingsJSON string `json:"settings_json"`
	StreamJSON   string `json:"stream_json"`
	Enabled      bool   `json:"enabled"`
	CreatedAt    int64  `json:"created_at"`
}

type RouteRule struct {
	ID          int64  `json:"id"`
	ServerID    string `json:"server_id"`
	Name        string `json:"name"`
	OutboundTag string `json:"outbound_tag"`
	DomainJSON  string `json:"domain_json"`
	IPJSON      string `json:"ip_json"`
	Port        string `json:"port"`
	Network     string `json:"network"`
	ProtocolJSON string `json:"protocol_json"`
	Priority    int    `json:"priority"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   int64  `json:"created_at"`
}

type ExternalNode struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Protocol  string `json:"protocol"`
	Address   string `json:"address"`
	Port      int    `json:"port"`
	ShareLink string `json:"share_link"`
	RawJSON   string `json:"raw_json"`
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"created_at"`
}

type Certificate struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Domain      string `json:"domain"`
	CertPEM     string `json:"cert_pem,omitempty"`
	KeyPEM      string `json:"key_pem,omitempty"`
	Provider    string `json:"provider"`
	ExpireAt    int64  `json:"expire_at"`
	CreatedAt   int64  `json:"created_at"`
	Email       string `json:"email,omitempty"`
	Challenge   string `json:"challenge,omitempty"`
	DNSProvider string `json:"dns_provider,omitempty"`
	Status      string `json:"status"`
	LastError   string `json:"last_error,omitempty"`
	AutoRenew   bool   `json:"auto_renew"`
	ServerID    string `json:"server_id,omitempty"` // empty = all agents
}

type TrafficDay struct {
	Day      string `json:"day"`
	UserID   int64  `json:"user_id"`
	ServerID string `json:"server_id"`
	Up       int64  `json:"up"`
	Down     int64  `json:"down"`
}
