package master

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type claims struct {
	UserID   int64  `json:"uid"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

func hashPassword(pw string) (string, error) {
	// Cap cost input length to avoid bcrypt DoS with huge passwords.
	if len(pw) > 72 {
		pw = pw[:72]
	}
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func checkPassword(hash, pw string) bool {
	if len(pw) > 72 {
		pw = pw[:72]
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

func (s *ServerApp) signToken(u User) (string, error) {
	c := claims{
		UserID:   u.ID,
		Username: u.Username,
		Role:     u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(72 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "bpanel",
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return t.SignedString([]byte(s.jwtSecret))
}

func (s *ServerApp) parseToken(tokenStr string) (*claims, error) {
	// Bound token size to mitigate header memory abuse.
	if len(tokenStr) > 8192 {
		return nil, errors.New("token too large")
	}
	t, err := jwt.ParseWithClaims(tokenStr, &claims{}, func(t *jwt.Token) (any, error) {
		// Reject alg=none / unexpected algorithms (JWT algorithm confusion).
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(s.jwtSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		return nil, err
	}
	c, ok := t.Claims.(*claims)
	if !ok || !t.Valid {
		return nil, errors.New("invalid token")
	}
	if c.UserID <= 0 || c.Username == "" {
		return nil, errors.New("invalid claims")
	}
	return c, nil
}

func (s *ServerApp) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if h == "" {
			// also allow cookie
			if c, err := r.Cookie("token"); err == nil {
				h = "Bearer " + c.Value
			}
		}
		if !strings.HasPrefix(h, "Bearer ") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		c, err := s.parseToken(strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		ctx := withUser(r.Context(), c)
		next(w, r.WithContext(ctx))
	}
}

// —— simple in-memory login rate limit (per IP) ——
type loginLimiter struct {
	mu    sync.Mutex
	hits  map[string][]time.Time
	block map[string]time.Time
}

var globalLoginLimit = &loginLimiter{
	hits:  map[string][]time.Time{},
	block: map[string]time.Time{},
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		// strip port; keep IPv6 bracket form intact enough
		return host[:i]
	}
	return host
}

func (l *loginLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if until, ok := l.block[ip]; ok {
		if now.Before(until) {
			return false
		}
		delete(l.block, ip)
	}
	window := now.Add(-2 * time.Minute)
	arr := l.hits[ip]
	kept := arr[:0]
	for _, t := range arr {
		if t.After(window) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= 12 {
		l.block[ip] = now.Add(5 * time.Minute)
		l.hits[ip] = nil
		return false
	}
	l.hits[ip] = append(kept, now)
	return true
}
