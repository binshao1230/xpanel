package master

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/binshao1230/bpanel/internal/version"
)

const defaultUpdateRepo = "binshao1230/xpanel"

var updateMu sync.Mutex

type ghRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Body    string `json:"body"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

func updateRepo() string {
	if v := strings.TrimSpace(os.Getenv("BPANEL_REPO")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("XPANEL_REPO")); v != "" {
		return v
	}
	return defaultUpdateRepo
}

func normalizeTag(tag string) string {
	return strings.TrimPrefix(strings.TrimSpace(tag), "v")
}

// compareSemver returns 1 if a>b, -1 if a<b, 0 if equal/unknown.
func compareSemver(a, b string) int {
	pa := strings.Split(normalizeTag(a), ".")
	pb := strings.Split(normalizeTag(b), ".")
	for len(pa) < 3 {
		pa = append(pa, "0")
	}
	for len(pb) < 3 {
		pb = append(pb, "0")
	}
	for i := 0; i < 3; i++ {
		var na, nb int
		fmt.Sscanf(pa[i], "%d", &na)
		fmt.Sscanf(pb[i], "%d", &nb)
		if na > nb {
			return 1
		}
		if na < nb {
			return -1
		}
	}
	return 0
}

func masterAssetName() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	name := fmt.Sprintf("bpanel-master-%s-%s", goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

func fetchLatestRelease() (*ghRelease, error) {
	repo := updateRepo()
	url := "https://api.github.com/repos/" + repo + "/releases/latest"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "BPanel-SelfUpdate/"+version.Version)
	client := &http.Client{Timeout: 20 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return nil, fmt.Errorf("GitHub API %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	var rel ghRelease
	if err := json.NewDecoder(res.Body).Decode(&rel); err != nil {
		return nil, err
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("empty release tag")
	}
	return &rel, nil
}

func (s *ServerApp) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	rel, err := fetchLatestRelease()
	if err != nil {
		writeJSON(w, 502, map[string]any{
			"error":          err.Error(),
			"current":        version.Version,
			"latest":         "",
			"update_available": false,
			"repo":           updateRepo(),
		})
		return
	}
	cur := version.Version
	latest := normalizeTag(rel.TagName)
	cmp := compareSemver(latest, cur)
	asset := masterAssetName()
	var downloadURL string
	var size int64
	for _, a := range rel.Assets {
		if a.Name == asset {
			downloadURL = a.BrowserDownloadURL
			size = a.Size
			break
		}
	}
	// also accept legacy name during transition
	if downloadURL == "" {
		legacy := strings.Replace(asset, "bpanel-master", "xpanel-master", 1)
		for _, a := range rel.Assets {
			if a.Name == legacy {
				downloadURL = a.BrowserDownloadURL
				size = a.Size
				asset = legacy
				break
			}
		}
	}
	writeJSON(w, 200, map[string]any{
		"current":          cur,
		"latest":           latest,
		"latest_tag":       rel.TagName,
		"update_available": cmp > 0,
		"notes":            rel.Body,
		"html_url":         rel.HTMLURL,
		"asset":            asset,
		"download_url":     downloadURL,
		"size":             size,
		"repo":             updateRepo(),
		"goos":             runtime.GOOS,
		"goarch":           runtime.GOARCH,
	})
}

func (s *ServerApp) handleSelfUpdate(w http.ResponseWriter, r *http.Request) {
	if !updateMu.TryLock() {
		writeJSON(w, 409, map[string]string{"error": "已有更新任务在进行"})
		return
	}
	// unlock after async start? hold until download done before restart
	defer updateMu.Unlock()

	var body struct {
		Tag string `json:"tag"` // optional specific tag, default latest
	}
	_ = readJSON(r, &body)

	rel, err := fetchLatestRelease()
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "检查版本失败: " + err.Error()})
		return
	}
	latest := normalizeTag(rel.TagName)
	if compareSemver(latest, version.Version) <= 0 && body.Tag == "" {
		writeJSON(w, 200, map[string]any{
			"ok":      true,
			"updated": false,
			"message": "已是最新版本 " + version.Version,
			"current": version.Version,
			"latest":  latest,
		})
		return
	}

	asset := masterAssetName()
	var url string
	for _, a := range rel.Assets {
		if a.Name == asset {
			url = a.BrowserDownloadURL
			break
		}
	}
	if url == "" {
		writeJSON(w, 404, map[string]string{
			"error": fmt.Sprintf("Release %s 中没有资源 %s（当前平台 %s/%s）", rel.TagName, asset, runtime.GOOS, runtime.GOARCH),
		})
		return
	}
	if !strings.HasPrefix(url, "https://github.com/") && !strings.HasPrefix(url, "https://objects.githubusercontent.com/") {
		// GitHub may redirect to objects.githubusercontent.com; we follow redirects via http.Get
		if !strings.Contains(url, "github.com") && !strings.Contains(url, "githubusercontent.com") {
			writeJSON(w, 400, map[string]string{"error": "拒绝非 GitHub 下载地址"})
			return
		}
	}

	exe, err := os.Executable()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "无法定位当前程序: " + err.Error()})
		return
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		// keep original path
		exe, _ = os.Executable()
	}

	tmp := exe + ".new"
	bak := exe + ".bak"
	if err := downloadBinary(url, tmp); err != nil {
		_ = os.Remove(tmp)
		writeJSON(w, 502, map[string]string{"error": "下载失败: " + err.Error()})
		return
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		writeJSON(w, 500, map[string]string{"error": "chmod 失败: " + err.Error()})
		return
	}

	// backup current
	_ = os.Remove(bak)
	if err := os.Rename(exe, bak); err != nil {
		// Windows may lock; try copy-overwrite style on unix rename is fine
		_ = os.Remove(tmp)
		writeJSON(w, 500, map[string]string{"error": "备份当前二进制失败: " + err.Error()})
		return
	}
	if err := os.Rename(tmp, exe); err != nil {
		// rollback
		_ = os.Rename(bak, exe)
		_ = os.Remove(tmp)
		writeJSON(w, 500, map[string]string{"error": "替换二进制失败: " + err.Error()})
		return
	}

	s.audit("admin", "self_update", fmt.Sprintf("%s → %s", version.Version, latest))

	// respond first, then restart asynchronously
	writeJSON(w, 200, map[string]any{
		"ok":      true,
		"updated": true,
		"message": fmt.Sprintf("已更新到 %s，服务即将重启", latest),
		"from":    version.Version,
		"to":      latest,
		"path":    exe,
	})

	go func() {
		time.Sleep(800 * time.Millisecond)
		restartMaster()
	}()
}

const maxUpdateBytes = 120 << 20 // 120 MiB hard cap for self-update binary

func isGitHubDownloadHost(host string) bool {
	host = strings.ToLower(host)
	return host == "github.com" ||
		host == "www.github.com" ||
		strings.HasSuffix(host, ".github.com") ||
		host == "objects.githubusercontent.com" ||
		strings.HasSuffix(host, ".githubusercontent.com") ||
		host == "release-assets.githubusercontent.com"
}

func downloadBinary(url, dest string) error {
	client := &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return fmt.Errorf("too many redirects")
			}
			if !isGitHubDownloadHost(req.URL.Host) {
				return fmt.Errorf("redirect to non-GitHub host blocked: %s", req.URL.Host)
			}
			return nil
		},
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "BPanel-SelfUpdate/"+version.Version)
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if cl := res.ContentLength; cl > maxUpdateBytes {
		return fmt.Errorf("asset too large (%d bytes)", cl)
	}
	// reject HTML error pages
	buf := make([]byte, 32)
	n, _ := io.ReadFull(res.Body, buf)
	head := string(buf[:n])
	if strings.Contains(strings.ToLower(head), "<!doctype") || strings.Contains(strings.ToLower(head), "<html") {
		return fmt.Errorf("下载内容不是二进制")
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer f.Close()
	if n > 0 {
		if _, err := f.Write(buf[:n]); err != nil {
			return err
		}
	}
	// Cap total bytes read (head + rest).
	rest, err := io.Copy(f, io.LimitReader(res.Body, maxUpdateBytes-int64(n)+1))
	if err != nil {
		return err
	}
	if int64(n)+rest > maxUpdateBytes {
		return fmt.Errorf("asset exceeds size limit")
	}
	return f.Sync()
}

func restartMaster() {
	// Prefer systemd service names (new then legacy)
	for _, unit := range []string{"bpanel-master", "xpanel-master"} {
		if err := exec.Command("systemctl", "restart", unit).Run(); err == nil {
			return
		}
	}
	// fallback: exit and let process manager restart; if none, panel stops
	// try re-exec
	exe, err := os.Executable()
	if err == nil {
		cmd := exec.Command(exe, os.Args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = os.Environ()
		if err := cmd.Start(); err == nil {
			os.Exit(0)
		}
	}
	os.Exit(0)
}
