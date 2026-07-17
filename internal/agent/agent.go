package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/xpanel/xpanel/internal/protocol"
	"github.com/xpanel/xpanel/internal/version"
	"github.com/xpanel/xpanel/internal/xrayproc"
)

type Config struct {
	MasterURL      string
	InstallToken   string
	DataDir        string
	XrayConfigPath string
	XrayBin        string
	DisableXray    bool
	// Mode: auto | http | websocket | pull
	// auto tries websocket then falls back to http/pull loop.
	Mode       string
	HTTPClient *http.Client
}

type Agent struct {
	cfg           Config
	client        *http.Client
	xray          *xrayproc.Manager
	mu            sync.Mutex
	serverID      string
	agentKey      string
	configVersion int64
	startedAt     time.Time
	stopCh        chan struct{}
	lastApplyErr  string
}

func New(cfg Config) *Agent {
	if cfg.DataDir == "" {
		cfg.DataDir = "./agent-data"
	}
	if cfg.XrayConfigPath == "" {
		cfg.XrayConfigPath = filepath.Join(cfg.DataDir, "xray.json")
	}
	if cfg.Mode == "" {
		cfg.Mode = "auto"
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	bin := xrayproc.ResolveBin(cfg.XrayBin, cfg.DataDir, filepath.Dir(cfg.XrayConfigPath))
	a := &Agent{
		cfg:       cfg,
		client:    cfg.HTTPClient,
		startedAt: time.Now(),
		stopCh:    make(chan struct{}),
	}
	if !cfg.DisableXray {
		a.xray = xrayproc.New(bin, cfg.XrayConfigPath)
		log.Printf("xray binary: %s (available=%v)", bin, a.xray.Available())
	}
	return a
}

func (a *Agent) Run() error {
	if err := os.MkdirAll(a.cfg.DataDir, 0o755); err != nil {
		return err
	}
	if err := a.loadState(); err != nil {
		log.Printf("no local state, will register: %v", err)
	}
	if a.agentKey == "" {
		if err := a.register(); err != nil {
			return fmt.Errorf("register: %w", err)
		}
	}
	if err := a.pullAndApply(); err != nil {
		log.Printf("initial config pull: %v", err)
		a.ensureXray()
	}
	// immediate status report so panel shows xray_running without waiting a full poll interval
	if err := a.heartbeat(); err != nil {
		log.Printf("initial heartbeat: %v", err)
	}

	mode := strings.ToLower(a.cfg.Mode)
	switch mode {
	case "websocket", "ws":
		return a.loopWS()
	case "http", "pull":
		return a.loopHTTP()
	default: // auto
		return a.loopAuto()
	}
}

func (a *Agent) loopAuto() error {
	for {
		select {
		case <-a.stopCh:
			if a.xray != nil {
				_ = a.xray.Stop()
			}
			return nil
		default:
		}
		log.Printf("connection mode: trying websocket")
		if err := a.runWebSocket(); err != nil {
			log.Printf("websocket ended: %v — fallback http 30s", err)
		}
		// fallback http for a while then retry ws
		if err := a.runHTTPFor(30 * time.Second); err != nil {
			return err
		}
	}
}

func (a *Agent) loopWS() error {
	for {
		select {
		case <-a.stopCh:
			if a.xray != nil {
				_ = a.xray.Stop()
			}
			return nil
		default:
		}
		if err := a.runWebSocket(); err != nil {
			log.Printf("websocket: %v; retry in 5s", err)
			select {
			case <-a.stopCh:
				return nil
			case <-time.After(5 * time.Second):
			}
		}
	}
}

func (a *Agent) loopHTTP() error {
	return a.runHTTPFor(0) // forever
}

func (a *Agent) runHTTPFor(max time.Duration) error {
	deadline := time.Time{}
	if max > 0 {
		deadline = time.Now().Add(max)
	}
	hb := time.NewTicker(15 * time.Second)
	wd := time.NewTicker(10 * time.Second)
	defer hb.Stop()
	defer wd.Stop()
	for {
		if !deadline.IsZero() && time.Now().After(deadline) {
			return nil
		}
		select {
		case <-a.stopCh:
			if a.xray != nil {
				_ = a.xray.Stop()
			}
			return nil
		case <-wd.C:
			a.ensureXray()
		case <-hb.C:
			if err := a.heartbeat(); err != nil {
				log.Printf("heartbeat: %v", err)
				if err := a.register(); err != nil {
					log.Printf("re-register: %v", err)
				}
			}
		}
	}
}

func (a *Agent) Stop() {
	select {
	case <-a.stopCh:
	default:
		close(a.stopCh)
	}
}

func (a *Agent) ensureXray() {
	if a.xray == nil {
		return
	}
	if a.xray.IsRunning() {
		return
	}
	if _, err := os.Stat(a.cfg.XrayConfigPath); err != nil {
		return
	}
	if !a.xray.Available() {
		return
	}
	if err := a.xray.EnsureRunning(); err != nil {
		log.Printf("xray watchdog restart: %v", err)
		a.mu.Lock()
		a.lastApplyErr = err.Error()
		a.mu.Unlock()
	} else {
		log.Printf("xray process started (watchdog)")
	}
}

func (a *Agent) statePath() string {
	return filepath.Join(a.cfg.DataDir, "agent-state.json")
}

type stateFile struct {
	ServerID      string `json:"server_id"`
	AgentKey      string `json:"agent_key"`
	ConfigVersion int64  `json:"config_version"`
}

func (a *Agent) loadState() error {
	b, err := os.ReadFile(a.statePath())
	if err != nil {
		return err
	}
	var st stateFile
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	a.serverID = st.ServerID
	a.agentKey = st.AgentKey
	a.configVersion = st.ConfigVersion
	return nil
}

func (a *Agent) saveState() error {
	st := stateFile{
		ServerID:      a.serverID,
		AgentKey:      a.agentKey,
		ConfigVersion: a.configVersion,
	}
	b, _ := json.MarshalIndent(st, "", "  ")
	return os.WriteFile(a.statePath(), b, 0o600)
}

func (a *Agent) register() error {
	host, _ := os.Hostname()
	req := protocol.RegisterRequest{
		Token:        a.cfg.InstallToken,
		Hostname:     host,
		AgentVersion: version.Version,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
	}
	var resp protocol.RegisterResponse
	if err := a.postJSON("/api/agent/register", "", req, &resp); err != nil {
		return err
	}
	a.mu.Lock()
	a.serverID = resp.ServerID
	a.agentKey = resp.AgentKey
	a.mu.Unlock()
	if err := a.saveState(); err != nil {
		return err
	}
	log.Printf("registered as server %s", resp.ServerID)
	return nil
}

func (a *Agent) heartbeat() error {
	running := false
	bin := ""
	if a.xray != nil {
		running = a.xray.IsRunning()
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
	key := a.agentKey
	a.mu.Unlock()

	var resp protocol.HeartbeatResponse
	if err := a.postJSON("/api/agent/heartbeat", key, req, &resp); err != nil {
		return err
	}
	if resp.DesiredConfigVersion > req.ConfigVersion {
		log.Printf("config outdated local=%d desired=%d, pulling", req.ConfigVersion, resp.DesiredConfigVersion)
		if err := a.pullAndApply(); err != nil {
			return err
		}
	}
	return nil
}

func (a *Agent) pullAndApply() error {
	a.mu.Lock()
	key := a.agentKey
	a.mu.Unlock()
	if key == "" {
		return fmt.Errorf("no agent key")
	}
	var bundle protocol.ConfigBundle
	if err := a.getJSON("/api/agent/config", key, &bundle); err != nil {
		return err
	}

	certDir := filepath.Join(a.cfg.DataDir, "certs")
	if err := a.writeCerts(certDir, bundle.Certs); err != nil {
		return fmt.Errorf("write certs: %w", err)
	}
	if err := a.writeNginx(bundle.Nginx); err != nil {
		log.Printf("write nginx: %v", err)
	}

	absCertDir, _ := filepath.Abs(certDir)
	expanded := expandCertPlaceholders(bundle.XrayJSON, absCertDir)

	raw, err := json.MarshalIndent(expanded, "", "  ")
	if err != nil {
		return err
	}

	if a.xray != nil && a.xray.Available() {
		if err := a.xray.ApplyConfigBytes(raw); err != nil {
			a.mu.Lock()
			a.lastApplyErr = err.Error()
			// do NOT advance configVersion — next heartbeat will retry after operator fixes,
			// but throttle: remember failed version
			a.mu.Unlock()
			_ = writeFileAtomic(a.cfg.XrayConfigPath+".bad.json", raw)
			_ = a.saveState()
			log.Printf("apply xray FAILED (will retry): %v", err)
			return fmt.Errorf("apply xray: %w", err)
		}
		log.Printf("xray running=%v config version=%d certs=%d checksum=%s",
			a.xray.IsRunning(), bundle.Version, len(bundle.Certs), bundle.Checksum)
	} else {
		if err := writeFileAtomic(a.cfg.XrayConfigPath, raw); err != nil {
			return err
		}
		log.Printf("wrote xray config only version=%d certs=%d", bundle.Version, len(bundle.Certs))
	}

	a.mu.Lock()
	a.configVersion = bundle.Version
	a.lastApplyErr = ""
	a.mu.Unlock()
	return a.saveState()
}

func (a *Agent) writeNginx(files []protocol.NginxFile) error {
	if len(files) == 0 {
		return nil
	}
	dir := filepath.Join(a.cfg.DataDir, "nginx")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, f := range files {
		name := f.Name
		if name == "" {
			name = "default"
		}
		// safe filename
		name = strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
				return r
			}
			return '_'
		}, name)
		path := filepath.Join(dir, name+".conf")
		if err := os.WriteFile(path, []byte(f.Content), 0o644); err != nil {
			return err
		}
		log.Printf("wrote nginx draft %s", path)
	}
	return nil
}

func (a *Agent) writeCerts(certDir string, certs []protocol.CertFile) error {
	if len(certs) == 0 {
		return nil
	}
	if err := os.MkdirAll(certDir, 0o755); err != nil {
		return err
	}
	for _, c := range certs {
		if c.Domain == "" || c.CertPEM == "" || c.KeyPEM == "" {
			continue
		}
		dir := filepath.Join(certDir, sanitizeDomain(c.Domain))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "fullchain.pem"), []byte(c.CertPEM), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "privkey.pem"), []byte(c.KeyPEM), 0o600); err != nil {
			return err
		}
		log.Printf("wrote cert domain=%s", c.Domain)
	}
	return nil
}

func sanitizeDomain(d string) string {
	d = strings.TrimSpace(strings.ToLower(d))
	d = strings.ReplaceAll(d, "*", "_wildcard_")
	return d
}

func expandCertPlaceholders(v any, absCertDir string) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = expandCertPlaceholders(val, absCertDir)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = expandCertPlaceholders(val, absCertDir)
		}
		return out
	case string:
		if strings.Contains(t, protocol.CertPlaceholder) {
			p := strings.ReplaceAll(t, protocol.CertPlaceholder, absCertDir)
			return filepath.FromSlash(filepath.ToSlash(p))
		}
		return t
	default:
		return v
	}
}

func writeFileAtomic(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(path)
		return os.Rename(tmp, path)
	}
	return nil
}

func (a *Agent) postJSON(path, agentKey string, body any, out any) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, stringsTrimRight(a.cfg.MasterURL)+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if agentKey != "" {
		req.Header.Set(protocol.HeaderAgentKey, agentKey)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("POST %s: %s %s", path, resp.Status, string(data))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

func (a *Agent) getJSON(path, agentKey string, out any) error {
	req, err := http.NewRequest(http.MethodGet, stringsTrimRight(a.cfg.MasterURL)+path, nil)
	if err != nil {
		return err
	}
	if agentKey != "" {
		req.Header.Set(protocol.HeaderAgentKey, agentKey)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("GET %s: %s %s", path, resp.Status, string(data))
	}
	return json.Unmarshal(data, out)
}

func stringsTrimRight(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func detectPublicIP(c *http.Client) string {
	urls := []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
	}
	cli := &http.Client{Timeout: 5 * time.Second}
	for _, u := range urls {
		req, _ := http.NewRequest(http.MethodGet, u, nil)
		resp, err := cli.Do(req)
		if err != nil {
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		if resp.StatusCode == 200 {
			ip := string(bytes.TrimSpace(b))
			if ip != "" {
				return ip
			}
		}
	}
	return ""
}
