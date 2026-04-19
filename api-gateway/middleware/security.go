package middleware

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

type contextKey string

const (
	correlationIDKey contextKey = "correlation_id"
	realIPKey        contextKey = "real_ip"
)

// SecurityHeaders adds standard security headers to every response.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		if strings.HasPrefix(r.URL.Path, "/v1/") {
			w.Header().Set("Cache-Control", "no-store")
		}

		next.ServeHTTP(w, r)
	})
}

// CorrelationID propagates or generates a correlation ID for each request.
func CorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Correlation-ID")
		if id == "" {
			id = uuid.New().String()
		}

		w.Header().Set("X-Correlation-ID", id)
		ctx := context.WithValue(r.Context(), correlationIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RealIP extracts the real client IP from proxy headers.
func RealIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := extractIP(r)
		ctx := context.WithValue(r.Context(), realIPKey, ip)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func extractIP(r *http.Request) string {
	if v := r.Header.Get("CF-Connecting-IP"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if idx := strings.Index(v, ","); idx != -1 {
			return strings.TrimSpace(v[:idx])
		}
		return strings.TrimSpace(v)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// GetRealIP returns the real IP stored in context by RealIP middleware.
func GetRealIP(ctx context.Context) string {
	if v, ok := ctx.Value(realIPKey).(string); ok {
		return v
	}
	return ""
}

// IPFilterConfig holds whitelist/blacklist configuration for IPFilter.
type IPFilterConfig struct {
	Whitelist []string // if non-empty, only these IPs/CIDRs are allowed
	Blacklist []string // these IPs/CIDRs are blocked
}

// NewIPFilter creates a middleware that blocks requests based on IP rules.
func NewIPFilter(cfg IPFilterConfig) func(http.Handler) http.Handler {
	whitelistNets := parseNets(cfg.Whitelist)
	blacklistNets := parseNets(cfg.Blacklist)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := net.ParseIP(GetRealIP(r.Context()))

			if len(whitelistNets) > 0 && !ipInNets(ip, whitelistNets) {
				writeForbidden(w)
				return
			}

			if ipInNets(ip, blacklistNets) {
				writeForbidden(w)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func parseNets(entries []string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(entries))
	for _, e := range entries {
		if strings.Contains(e, "/") {
			_, cidr, err := net.ParseCIDR(e)
			if err != nil {
				continue
			}
			nets = append(nets, cidr)
		} else {
			parsed := net.ParseIP(e)
			if parsed == nil {
				continue
			}
			if parsed.To4() != nil {
				_, cidr, _ := net.ParseCIDR(e + "/32")
				nets = append(nets, cidr)
			} else {
				_, cidr, _ := net.ParseCIDR(e + "/128")
				nets = append(nets, cidr)
			}
		}
	}
	return nets
}

func ipInNets(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func writeForbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(map[string]string{"error": "access denied"})
}
