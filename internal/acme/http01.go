package acme

import (
	"net/http"
	"strings"
	"sync"
)

// HTTP01Provider implements lego's challenge.Provider for HTTP-01,
// serving tokens from the panel's own HTTP server.
type HTTP01Provider struct {
	mu     sync.RWMutex
	tokens map[string]string // token -> keyAuth
}

func NewHTTP01Provider() *HTTP01Provider {
	return &HTTP01Provider{tokens: make(map[string]string)}
}

func (p *HTTP01Provider) Present(domain, token, keyAuth string) error {
	p.mu.Lock()
	p.tokens[token] = keyAuth
	p.mu.Unlock()
	return nil
}

func (p *HTTP01Provider) CleanUp(domain, token, keyAuth string) error {
	p.mu.Lock()
	delete(p.tokens, token)
	p.mu.Unlock()
	return nil
}

// Handler serves GET /.well-known/acme-challenge/{token}
func (p *HTTP01Provider) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.URL.Path, "/.well-known/acme-challenge/")
		token = strings.Trim(token, "/")
		if token == "" || strings.Contains(token, "/") {
			http.NotFound(w, r)
			return
		}
		p.mu.RLock()
		keyAuth, ok := p.tokens[token]
		p.mu.RUnlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(keyAuth))
	}
}
