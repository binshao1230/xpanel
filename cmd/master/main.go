package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/binshao1230/bpanel/internal/master"
	"github.com/binshao1230/bpanel/internal/version"
	"github.com/binshao1230/bpanel/web"
)

func main() {
	addr := flag.String("addr", envOr("ADDR", ":8080"), "listen address")
	data := flag.String("data", envOr("DATA_DIR", "./data"), "data directory")
	publicURL := flag.String("public-url", envOr("PUBLIC_URL", ""), "public base URL for install/subscribe links")
	jwtSecret := flag.String("jwt-secret", envOr("JWT_SECRET", ""), "JWT signing secret")
	flag.Parse()

	if err := os.MkdirAll(*data, 0o755); err != nil {
		log.Fatal(err)
	}

	app, err := master.New(master.Config{
		Addr:      *addr,
		DataDir:   *data,
		JWTSecret: *jwtSecret,
		PublicURL: *publicURL,
		WebFS:     web.Static(),
	})
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()

	log.Printf("bpanel master %s listening on %s (data=%s)", version.Version, *addr, filepath.Clean(*data))
	if err := http.ListenAndServe(*addr, app.Handler()); err != nil {
		log.Fatal(err)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
