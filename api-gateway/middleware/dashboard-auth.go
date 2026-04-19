package middleware

import (
	"encoding/json"
	"net/http"
)

// DashboardAuth returns middleware that checks for dashboard authentication.
// If apiKey is empty, all requests pass through (auth disabled).
// Checks x-api-key header first, then arl_session cookie.
func DashboardAuth(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if apiKey == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Check header.
			if r.Header.Get("x-api-key") == apiKey {
				next.ServeHTTP(w, r)
				return
			}

			// Check cookie.
			if cookie, err := r.Cookie("arl_session"); err == nil && cookie.Value == apiKey {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "unauthorized",
			})
		})
	}
}
