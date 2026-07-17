package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/xpanel/xpanel/internal/agent"
	"github.com/xpanel/xpanel/internal/version"
	"github.com/xpanel/xpanel/internal/xrayproc"
)

func main() {
	masterURL := flag.String("master", envOr("MASTER_URL", "http://127.0.0.1:8080"), "master base URL")
	token := flag.String("token", envOr("INSTALL_TOKEN", ""), "install token from master")
	data := flag.String("data", envOr("DATA_DIR", "./agent-data"), "agent data dir")
	xrayCfg := flag.String("xray-config", envOr("XRAY_CONFIG", ""), "path to write xray config")
	xrayBin := flag.String("xray-bin", envOr("XRAY_BIN", ""), "xray binary path (auto-detect if empty)")
	disableXray := flag.Bool("disable-xray", envOr("DISABLE_XRAY", "") == "1", "only write config, do not manage process")
	mode := flag.String("mode", envOr("AGENT_MODE", "auto"), "connection mode: auto|websocket|http|pull")
	flag.Parse()

	if *token == "" {
		if b, err := os.ReadFile(filepath.Join(*data, "install_token")); err == nil {
			*token = string(bytesTrimSpace(b))
		}
	}
	if *token == "" {
		log.Fatal("INSTALL_TOKEN / -token is required")
	}

	cfgPath := *xrayCfg
	if cfgPath == "" {
		cfgPath = filepath.Join(*data, "xray.json")
	}

	resolved := xrayproc.ResolveBin(*xrayBin, *data, filepath.Dir(cfgPath))
	if !*disableXray {
		if st, err := os.Stat(resolved); err != nil || st.IsDir() {
			log.Printf("WARNING: xray binary not found (%q). Agent will write config only until binary is available.", resolved)
		} else {
			log.Printf("using xray: %s", resolved)
		}
	}

	a := agent.New(agent.Config{
		MasterURL:      *masterURL,
		InstallToken:   *token,
		DataDir:        *data,
		XrayConfigPath: cfgPath,
		XrayBin:        resolved,
		DisableXray:    *disableXray,
		Mode:           *mode,
	})

	// graceful stop on Ctrl+C / SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Printf("signal received, stopping agent & xray...")
		a.Stop()
	}()

	log.Printf("xpanel agent %s → master %s", version.Version, *masterURL)
	if err := a.Run(); err != nil {
		b, _ := json.Marshal(map[string]string{"error": err.Error()})
		log.Fatal(string(b))
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func bytesTrimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\n' || b[i] == '\r' || b[i] == '\t') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\n' || b[j-1] == '\r' || b[j-1] == '\t') {
		j--
	}
	return b[i:j]
}
