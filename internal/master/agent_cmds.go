package master

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/binshao1230/bpanel/internal/protocol"
)

// pending agent commands + in-memory log cache

type agentRuntime struct {
	mu       sync.Mutex
	pending  map[string][]string // serverID -> commands
	logs     map[string][]string // serverID -> log lines
	maxLines int
}

func newAgentRuntime() *agentRuntime {
	return &agentRuntime{
		pending:  map[string][]string{},
		logs:     map[string][]string{},
		maxLines: 500,
	}
}

func (r *agentRuntime) Enqueue(serverID, cmd string) {
	if serverID == "" || cmd == "" {
		return
	}
	r.mu.Lock()
	r.pending[serverID] = append(r.pending[serverID], cmd)
	// cap queue
	if len(r.pending[serverID]) > 20 {
		r.pending[serverID] = r.pending[serverID][len(r.pending[serverID])-20:]
	}
	r.mu.Unlock()
}

func (r *agentRuntime) TakePending(serverID string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	cmds := r.pending[serverID]
	if len(cmds) == 0 {
		return nil
	}
	delete(r.pending, serverID)
	return cmds
}

func (r *agentRuntime) StoreLogs(serverID string, lines []string) {
	if serverID == "" || len(lines) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// replace with latest tail from agent
	out := make([]string, len(lines))
	copy(out, lines)
	if len(out) > r.maxLines {
		out = out[len(out)-r.maxLines:]
	}
	r.logs[serverID] = out
}

func (r *agentRuntime) GetLogs(serverID string, n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	lines := r.logs[serverID]
	if len(lines) == 0 {
		return []string{}
	}
	if n <= 0 || n > len(lines) {
		n = len(lines)
	}
	from := len(lines) - n
	out := make([]string, n)
	copy(out, lines[from:])
	return out
}

// —— GitHub Xray releases (cached) ——

type xrayReleaseCache struct {
	mu      sync.Mutex
	at      time.Time
	tags    []map[string]any
	latest  string
	ttl     time.Duration
}

var globalXrayReleases = &xrayReleaseCache{ttl: 10 * time.Minute}

func (c *xrayReleaseCache) List() (latest string, tags []map[string]any, err error) {
	c.mu.Lock()
	if time.Since(c.at) < c.ttl && len(c.tags) > 0 {
		latest, tags = c.latest, append([]map[string]any{}, c.tags...)
		c.mu.Unlock()
		return latest, tags, nil
	}
	c.mu.Unlock()

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/XTLS/Xray-core/releases?per_page=20", nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "BPanel-Master")
	res, err := client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode != 200 {
		return "", nil, fmt.Errorf("GitHub API %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	var raw []struct {
		TagName     string    `json:"tag_name"`
		Name        string    `json:"name"`
		PublishedAt time.Time `json:"published_at"`
		Prerelease  bool      `json:"prerelease"`
		Draft       bool      `json:"draft"`
		HTMLURL     string    `json:"html_url"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", nil, err
	}
	out := make([]map[string]any, 0, len(raw))
	var lat string
	for _, r := range raw {
		if r.Draft {
			continue
		}
		if lat == "" && !r.Prerelease {
			lat = r.TagName
		}
		out = append(out, map[string]any{
			"tag":          r.TagName,
			"name":         r.Name,
			"published_at": r.PublishedAt.Unix(),
			"prerelease":   r.Prerelease,
			"html_url":     r.HTMLURL,
		})
	}
	if lat == "" && len(out) > 0 {
		lat, _ = out[0]["tag"].(string)
	}
	c.mu.Lock()
	c.at = time.Now()
	c.latest = lat
	c.tags = out
	c.mu.Unlock()
	return lat, out, nil
}

func (s *ServerApp) dispatchAgentCmd(serverID, cmd string) {
	s.rt.Enqueue(serverID, cmd)
	// best-effort live push over websocket
	name, arg, _ := strings.Cut(cmd, ":")
	if s.hub != nil {
		payload, _ := json.Marshal(map[string]any{
			"type": protocol.WSTypeCommand,
			"data": protocol.WSCommand{Name: name, Arg: arg},
		})
		_ = s.hub.send(serverID, payload)
	}
}

func (s *ServerApp) handleXrayVersions(w http.ResponseWriter, r *http.Request) {
	latest, tags, err := globalXrayReleases.List()
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{
		"latest":   latest,
		"releases": tags,
		"repo":     "XTLS/Xray-core",
	})
}

func (s *ServerApp) handleServerLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	n := 200
	if v := r.URL.Query().Get("lines"); v != "" {
		if x, err := parsePositiveInt(v); err == nil && x > 0 {
			n = x
			if n > 500 {
				n = 500
			}
		}
	}
	var name, xver string
	var xrayRun int
	_ = s.db.QueryRow(`SELECT name, COALESCE(xray_version,''), COALESCE(xray_running,0) FROM servers WHERE id=?`, id).
		Scan(&name, &xver, &xrayRun)
	if name == "" {
		// check exists
		var cnt int
		_ = s.db.QueryRow(`SELECT COUNT(1) FROM servers WHERE id=?`, id).Scan(&cnt)
		if cnt == 0 {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
	}
	lines := s.rt.GetLogs(id, n)
	writeJSON(w, 200, map[string]any{
		"server_id":    id,
		"name":         name,
		"xray_version": xver,
		"xray_running": xrayRun == 1,
		"lines":        lines,
		"count":        len(lines),
	})
}

func (s *ServerApp) handleInstallXray(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Version string `json:"version"`
	}
	_ = readJSON(r, &body)
	ver := strings.TrimSpace(body.Version)
	if ver == "" {
		ver = "latest"
	}
	// basic sanitize
	if strings.ContainsAny(ver, " \t\n\r;|&$`") {
		writeJSON(w, 400, map[string]string{"error": "invalid version"})
		return
	}
	var name string
	if err := s.db.QueryRow(`SELECT name FROM servers WHERE id=?`, id).Scan(&name); err != nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	cmd := protocol.CmdInstallXray + ":" + ver
	s.dispatchAgentCmd(id, cmd)
	s.audit("admin", "install_xray", fmt.Sprintf("%s %s", id, ver))
	writeJSON(w, 200, map[string]any{
		"ok":      true,
		"message": "已通知 Agent 安装 Xray " + ver + "（需 Agent 在线，约 15s 内执行）",
		"command": cmd,
		"server":  name,
	})
}

func (s *ServerApp) handleRestartXray(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var name string
	if err := s.db.QueryRow(`SELECT name FROM servers WHERE id=?`, id).Scan(&name); err != nil {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	s.dispatchAgentCmd(id, protocol.CmdRestartXray)
	s.audit("admin", "restart_xray", id)
	writeJSON(w, 200, map[string]any{"ok": true, "message": "已通知 Agent 重启 Xray", "server": name})
}

func parsePositiveInt(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("bad")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}
