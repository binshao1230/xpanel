package agent

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/binshao1230/bpanel/internal/protocol"
	"github.com/binshao1230/bpanel/internal/xrayproc"
)

// handleCommand executes a master command string.
// Forms: "reload_config", "restart_xray", "install_xray:latest", "install_xray:v26.3.27"
func (a *Agent) handleCommand(cmd string) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return
	}
	name, arg, _ := strings.Cut(cmd, ":")
	name = strings.TrimSpace(name)
	arg = strings.TrimSpace(arg)
	switch name {
	case protocol.CmdReloadConfig:
		log.Printf("command: reload_config")
		if err := a.pullAndApply(); err != nil {
			log.Printf("reload_config: %v", err)
			a.noteLog("agent", "reload failed: "+err.Error())
		}
	case protocol.CmdRestartXray:
		log.Printf("command: restart_xray")
		if a.xray == nil {
			return
		}
		if err := a.xray.Restart(); err != nil {
			log.Printf("restart_xray: %v", err)
			a.noteLog("agent", "restart failed: "+err.Error())
		} else {
			a.noteLog("agent", "xray restarted")
		}
	case protocol.CmdInstallXray:
		if arg == "" {
			arg = "latest"
		}
		log.Printf("command: install_xray %s", arg)
		a.noteLog("agent", "installing xray "+arg+" …")
		if err := a.installXray(arg); err != nil {
			log.Printf("install_xray: %v", err)
			a.noteLog("agent", "install failed: "+err.Error())
			a.mu.Lock()
			a.lastApplyErr = "install xray: " + err.Error()
			a.mu.Unlock()
		} else {
			a.invalidateXrayVerCache()
			ver := a.xrayVersion()
			a.noteLog("agent", "xray installed: "+ver)
			a.mu.Lock()
			a.lastApplyErr = ""
			a.mu.Unlock()
			// re-apply current config with new binary
			if err := a.pullAndApply(); err != nil {
				log.Printf("post-install apply: %v", err)
			}
		}
	default:
		log.Printf("unknown command: %s", cmd)
	}
}

func (a *Agent) noteLog(src, msg string) {
	a.pushAgentLog(src + ": " + msg)
}

func (a *Agent) xrayVersion() string {
	if a.xray == nil {
		return ""
	}
	a.mu.Lock()
	if a.cachedXrayVer != "" && time.Since(a.cachedVerAt) < 5*time.Minute {
		v := a.cachedXrayVer
		a.mu.Unlock()
		return v
	}
	a.mu.Unlock()
	v := a.xray.Version()
	a.mu.Lock()
	a.cachedXrayVer = v
	a.cachedVerAt = time.Now()
	a.mu.Unlock()
	return v
}

func (a *Agent) invalidateXrayVerCache() {
	a.mu.Lock()
	a.cachedXrayVer = ""
	a.cachedVerAt = time.Time{}
	a.mu.Unlock()
}

func (a *Agent) logTail(n int) []string {
	if a.xray == nil {
		return nil
	}
	return a.xray.Logs(n)
}

func (a *Agent) installXray(tag string) error {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		tag = "latest"
	}
	asset, err := xrayAssetName()
	if err != nil {
		return err
	}
	url, err := resolveXrayDownloadURL(tag, asset)
	if err != nil {
		return err
	}
	log.Printf("download xray: %s", url)

	tmpDir, err := os.MkdirTemp("", "bpanel-xray-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	zipPath := filepath.Join(tmpDir, "xray.zip")
	if err := downloadFile(url, zipPath); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	outDir := filepath.Join(tmpDir, "out")
	if err := unzip(zipPath, outDir); err != nil {
		return fmt.Errorf("unzip: %w", err)
	}

	binName := "xray"
	if runtime.GOOS == "windows" {
		binName = "xray.exe"
	}
	src := findFile(outDir, binName)
	if src == "" {
		return fmt.Errorf("zip 中未找到 %s", binName)
	}

	destDir := filepath.Join(a.cfg.DataDir, "bin")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(destDir, binName)

	// stop old process before replace on Windows
	if a.xray != nil {
		_ = a.xray.Stop()
	}
	if err := copyFile(src, dest, 0o755); err != nil {
		return fmt.Errorf("install binary: %w", err)
	}
	// geo files
	for _, g := range []string{"geoip.dat", "geosite.dat"} {
		if p := findFile(outDir, g); p != "" {
			_ = copyFile(p, filepath.Join(destDir, g), 0o644)
		}
	}

	abs, _ := filepath.Abs(dest)
	a.cfg.XrayBin = abs
	if a.cfg.DisableXray {
		log.Printf("xray binary installed at %s (process management disabled)", abs)
		return nil
	}
	if a.xray != nil {
		a.xray.SetBin(abs)
	} else {
		a.xray = xrayproc.New(abs, a.cfg.XrayConfigPath)
	}
	log.Printf("xray binary installed at %s", abs)
	return nil
}

func xrayAssetName() (string, error) {
	switch runtime.GOOS {
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return "Xray-linux-64.zip", nil
		case "arm64":
			return "Xray-linux-arm64-v8a.zip", nil
		}
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			return "Xray-windows-64.zip", nil
		case "arm64":
			return "Xray-windows-arm64-v8a.zip", nil
		}
	case "darwin":
		switch runtime.GOARCH {
		case "amd64":
			return "Xray-macos-64.zip", nil
		case "arm64":
			return "Xray-macos-arm64-v8a.zip", nil
		}
	}
	return "", fmt.Errorf("unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
}

func resolveXrayDownloadURL(tag, asset string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	api := "https://api.github.com/repos/XTLS/Xray-core/releases/latest"
	if tag != "latest" && tag != "" {
		if !strings.HasPrefix(tag, "v") {
			tag = "v" + tag
		}
		api = "https://api.github.com/repos/XTLS/Xray-core/releases/tags/" + tag
	}
	req, err := http.NewRequest(http.MethodGet, api, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "BPanel-Agent")
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if res.StatusCode != 200 {
		return "", fmt.Errorf("GitHub API %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	var rel struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", err
	}
	for _, a := range rel.Assets {
		if a.Name == asset && a.BrowserDownloadURL != "" {
			return a.BrowserDownloadURL, nil
		}
	}
	// fallback direct URL
	t := rel.TagName
	if t == "" {
		t = tag
		if t == "latest" {
			return "", fmt.Errorf("release 中没有资源 %s", asset)
		}
		if !strings.HasPrefix(t, "v") {
			t = "v" + t
		}
	}
	return fmt.Sprintf("https://github.com/XTLS/Xray-core/releases/download/%s/%s", t, asset), nil
}

func downloadFile(url, dest string) error {
	client := &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "BPanel-Agent")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, io.LimitReader(res.Body, 200<<20))
	return err
}

func unzip(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	cleanDest := filepath.Clean(dest)
	for _, f := range r.File {
		// zip slip guard
		target := filepath.Join(dest, f.Name)
		rel, err := filepath.Rel(cleanDest, filepath.Clean(target))
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("illegal path in zip: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(target, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, io.LimitReader(rc, 200<<20))
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func findFile(root, name string) string {
	var found string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if strings.EqualFold(info.Name(), name) {
			found = path
			return io.EOF
		}
		return nil
	})
	return found
}

func copyFile(src, dest string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dest + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	cerr := out.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if cerr != nil {
		_ = os.Remove(tmp)
		return cerr
	}
	_ = os.Remove(dest)
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Chmod(dest, mode)
	return nil
}
