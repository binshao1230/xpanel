package master

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const maxJSONBody = 4 << 20 // 4 MiB

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return nil
	}
	defer r.Body.Close()
	// Limit request body size to mitigate DoS / memory abuse.
	dec := json.NewDecoder(io.LimitReader(r.Body, maxJSONBody+1))
	err := dec.Decode(dst)
	if err == nil {
		return nil
	}
	// empty / missing body is fine for optional payloads
	if err == io.EOF || err.Error() == "EOF" || err.Error() == "http: invalid Read on closed Body" {
		return nil
	}
	return err
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-XSS-Protection", "0")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		// Panel is self-hosted; allow inline styles from embedded UI, block external scripts.
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/sub/") || strings.HasPrefix(r.URL.Path, "/s/") {
			w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		} else {
			w.Header().Set("Content-Security-Policy",
				"default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; font-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		}
		next.ServeHTTP(w, r)
	})
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Same-origin panel does not need wide-open CORS. Allow only same host
		// or explicitly missing Origin (non-browser / agent clients).
		origin := r.Header.Get("Origin")
		if origin == "" {
			// non-browser
		} else if originOK(r, origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Agent-Key")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func originOK(r *http.Request, origin string) bool {
	// Accept if Origin host matches request Host (scheme-agnostic).
	o := strings.TrimPrefix(strings.TrimPrefix(origin, "https://"), "http://")
	host := r.Host
	if i := strings.IndexByte(o, '/'); i >= 0 {
		o = o[:i]
	}
	return strings.EqualFold(o, host)
}
