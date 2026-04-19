package middleware

import (
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"
)

type loginAttempt struct {
	count     int
	expiresAt time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginAttempt
}

const (
	loginMaxAttempts     = 5
	loginWindow          = 15 * time.Minute
	loginCleanupInterval = 5 * time.Minute
)

// NewLoginLimiter returns middleware that rate-limits login attempts per IP.
// Max 5 attempts per 15-minute window. Expired entries cleaned every 5 minutes.
func NewLoginLimiter() func(http.Handler) http.Handler {
	ll := &loginLimiter{
		attempts: make(map[string]*loginAttempt),
	}

	// Background cleanup of expired entries.
	go func() {
		ticker := time.NewTicker(loginCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			ll.mu.Lock()
			now := time.Now()
			for ip, a := range ll.attempts {
				if now.After(a.expiresAt) {
					delete(ll.attempts, ip)
				}
			}
			ll.mu.Unlock()
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}

			ll.mu.Lock()
			now := time.Now()
			a, exists := ll.attempts[ip]
			if !exists || now.After(a.expiresAt) {
				ll.attempts[ip] = &loginAttempt{count: 1, expiresAt: now.Add(loginWindow)}
				ll.mu.Unlock()
				next.ServeHTTP(w, r)
				return
			}

			if a.count >= loginMaxAttempts {
				ll.mu.Unlock()
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("Retry-After", "900")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{
					"error": "too many login attempts, try again later",
				})
				return
			}

			a.count++
			ll.mu.Unlock()
			next.ServeHTTP(w, r)
		})
	}
}
