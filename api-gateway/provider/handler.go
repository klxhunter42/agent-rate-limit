package provider

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

type AuthSession struct {
	Provider     string     `json:"provider"`
	State        string     `json:"state"`
	DeviceCode   string     `json:"device_code,omitempty"`
	PKCEVerifier string     `json:"pkce_verifier,omitempty"`
	Type         string     `json:"type"` // "device_code" or "auth_code"
	Token        *TokenInfo `json:"token,omitempty"`
	StartedAt    time.Time  `json:"started_at"`
}

type AuthHandler struct {
	store    *TokenStore
	registry *Registry
	sessions map[string]*AuthSession
	mu       sync.Mutex
	apiKey   string
}

func NewAuthHandler(store *TokenStore, registry *Registry) *AuthHandler {
	return &AuthHandler{
		store:    store,
		registry: registry,
		sessions: make(map[string]*AuthSession),
		apiKey:   os.Getenv("DASHBOARD_API_KEY"),
	}
}

func (h *AuthHandler) StartAuth(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "provider")
	pc, ok := h.registry.Get(providerID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown provider: " + providerID})
		return
	}

	switch pc.AuthType {
	case AuthTypeDeviceCode:
		h.startDeviceCode(w, r, pc)
	case AuthTypeAuthCode:
		h.startAuthCode(w, r, pc)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider does not support interactive OAuth"})
	}
}

func (h *AuthHandler) startDeviceCode(w http.ResponseWriter, r *http.Request, pc ProviderConfig) {
	dcr, err := StartDeviceCode(r.Context(), pc)
	if err != nil {
		slog.Error("start device code failed", "provider", pc.ID, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to start device code flow"})
		return
	}

	state := dcr.UserCode
	session := &AuthSession{
		Provider:   pc.ID,
		State:      state,
		DeviceCode: dcr.DeviceCode,
		Type:       "device_code",
		StartedAt:  time.Now(),
	}

	h.mu.Lock()
	h.sessions[state] = session
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"user_code":        dcr.UserCode,
		"verification_url": dcr.VerificationURL,
		"device_code":      dcr.DeviceCode,
		"state":            state,
		"expires_in":       dcr.ExpiresIn,
		"interval":         dcr.Interval,
	})
}

func (h *AuthHandler) startAuthCode(w http.ResponseWriter, r *http.Request, pc ProviderConfig) {
	if pc.ClientID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "missing Client ID: set " + strings.ToUpper(pc.ID) + "_CLIENT_ID or GOOGLE_CLIENT_ID env var",
		})
		return
	}
	// redirect_uri for OAuth: must use 127.0.0.1 for Google installed-app OAuth.
	callbackBase := envOr("OAUTH_CALLBACK_BASE", "http://127.0.0.1:8080")
	resp, err := StartAuthCode(r.Context(), pc, callbackBase)
	if err != nil {
		slog.Error("start auth code failed", "provider", pc.ID, "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to start auth code flow"})
		return
	}

	session := &AuthSession{
		Provider:     pc.ID,
		State:        resp.State,
		PKCEVerifier: resp.PKCEVerifier,
		Type:         "auth_code",
		StartedAt:    time.Now(),
	}

	h.mu.Lock()
	h.sessions[resp.State] = session
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"auth_url": resp.AuthURL,
		"state":    resp.State,
	})
}

func (h *AuthHandler) StartAuthURL(w http.ResponseWriter, r *http.Request) {
	h.StartAuth(w, r)
}

func (h *AuthHandler) HandleCallback(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "provider")
	pc, ok := h.registry.Get(providerID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown provider"})
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "code and state required"})
		return
	}

	h.mu.Lock()
	session, exists := h.sessions[state]
	if exists {
		session.Token = nil // clear any previous
	}
	h.mu.Unlock()

	if !exists {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown state"})
		return
	}

	callbackBase := envOr("OAUTH_CALLBACK_BASE", "http://127.0.0.1:8080")
	dashboardURL := envOr("DASHBOARD_URL", "http://localhost:8082")
	token, err := HandleCallbackWithPKCE(r.Context(), pc, code, state, callbackBase, session.PKCEVerifier)
	if err != nil {
		slog.Error("callback failed", "provider", pc.ID, "error", err)
		http.Redirect(w, r, dashboardURL+"/admin?auth_error=callback_failed", http.StatusTemporaryRedirect)
		return
	}

	if err := h.store.Store(*token); err != nil {
		slog.Error("store token from callback failed", "error", err)
		http.Redirect(w, r, dashboardURL+"/admin?auth_error=store_failed", http.StatusTemporaryRedirect)
		return
	}

	h.mu.Lock()
	session.Token = token
	h.mu.Unlock()

	slog.Info("auth callback success", "provider", pc.ID, "account_id", token.AccountID)
	http.Redirect(w, r, dashboardURL+"/admin?auth_success=1", http.StatusTemporaryRedirect)
}

// HandleClaudeCallback handles the /callback route for Claude OAuth loopback redirect.
func (h *AuthHandler) HandleClaudeCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "code and state required"})
		return
	}

	h.mu.Lock()
	session, exists := h.sessions[state]
	if exists {
		session.Token = nil
	}
	h.mu.Unlock()

	if !exists {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown state"})
		return
	}

	callbackBase := envOr("OAUTH_CALLBACK_BASE", "http://127.0.0.1:8080")
	dashboardURL := envOr("DASHBOARD_URL", "http://localhost:8082")
	pc, ok := h.registry.Get("claude")
	if !ok {
		http.Redirect(w, r, dashboardURL+"/admin?auth_error=no_claude_provider", http.StatusTemporaryRedirect)
		return
	}

	token, err := HandleCallbackWithPKCE(r.Context(), pc, code, state, callbackBase, session.PKCEVerifier)
	if err != nil {
		slog.Error("claude callback failed", "error", err)
		http.Redirect(w, r, dashboardURL+"/admin?auth_error=callback_failed", http.StatusTemporaryRedirect)
		return
	}

	if err := h.store.Store(*token); err != nil {
		slog.Error("store claude token failed", "error", err)
		http.Redirect(w, r, dashboardURL+"/admin?auth_error=store_failed", http.StatusTemporaryRedirect)
		return
	}

	h.mu.Lock()
	session.Token = token
	h.mu.Unlock()

	slog.Info("claude auth callback success", "account_id", token.AccountID)
	http.Redirect(w, r, dashboardURL+"/admin?auth_success=1", http.StatusTemporaryRedirect)
}

func (h *AuthHandler) PollStatus(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "provider")
	state := r.URL.Query().Get("state")
	deviceCode := r.URL.Query().Get("device_code")

	if state == "" && deviceCode == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "state or device_code required"})
		return
	}

	lookupKey := state
	if lookupKey == "" {
		lookupKey = deviceCode
	}

	h.mu.Lock()
	session, exists := h.sessions[lookupKey]
	h.mu.Unlock()

	if !exists {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	if session.Token != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "complete",
			"account": session.Token,
		})
		return
	}

	if session.Type == "device_code" && session.DeviceCode != "" {
		pc, ok := h.registry.Get(providerID)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown provider"})
			return
		}

		token, err := PollDeviceToken(r.Context(), pc, session.DeviceCode)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"status": "error",
				"error":  err.Error(),
			})
			return
		}
		if token != nil {
			if err := h.store.Store(*token); err != nil {
				slog.Error("store device token failed", "error", err)
			}
			h.mu.Lock()
			session.Token = token
			h.mu.Unlock()

			writeJSON(w, http.StatusOK, map[string]any{
				"status":  "complete",
				"account": token,
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "pending",
	})
}

func (h *AuthHandler) CancelAuth(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if state == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "state required"})
		return
	}

	h.mu.Lock()
	delete(h.sessions, state)
	h.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (h *AuthHandler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "provider")

	if providerID != "" {
		tokens, err := h.store.ListByProvider(providerID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list accounts"})
			return
		}
		if tokens == nil {
			tokens = []TokenInfo{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"accounts": tokens})
		return
	}

	tokens, err := h.store.ListAll()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list accounts"})
		return
	}
	if tokens == nil {
		tokens = []TokenInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": tokens})
}

func (h *AuthHandler) RemoveAccount(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "provider")
	accountID := chi.URLParam(r, "accountId")

	if providerID == "" || accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider and accountId required"})
		return
	}

	if err := h.store.Delete(providerID, accountID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to remove account"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (h *AuthHandler) PauseAccount(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "provider")
	accountID := chi.URLParam(r, "accountId")

	if err := h.store.Pause(providerID, accountID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (h *AuthHandler) ResumeAccount(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "provider")
	accountID := chi.URLParam(r, "accountId")

	if err := h.store.Resume(providerID, accountID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

func (h *AuthHandler) SetDefaultAccount(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "provider")
	accountID := chi.URLParam(r, "accountId")

	if err := h.store.SetDefault(providerID, accountID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "default_set"})
}

func (h *AuthHandler) DashboardLogin(w http.ResponseWriter, r *http.Request) {
	if h.apiKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dashboard auth not configured"})
		return
	}

	var req struct {
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	if req.APIKey != h.apiKey {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid api key"})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "arl_session",
		Value:    h.apiKey,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 30,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "authenticated"})
}

func (h *AuthHandler) DashboardLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "arl_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (h *AuthHandler) CheckAuth(w http.ResponseWriter, r *http.Request) {
	if h.apiKey == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": true,
			"auth_type":     "none",
		})
		return
	}

	// Check header.
	if r.Header.Get("x-api-key") == h.apiKey {
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": true,
			"auth_type":     "api_key",
		})
		return
	}

	// Check cookie.
	if cookie, err := r.Cookie("arl_session"); err == nil && cookie.Value == h.apiKey {
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": true,
			"auth_type":     "cookie",
		})
		return
	}

	writeJSON(w, http.StatusUnauthorized, map[string]any{
		"authenticated": false,
	})
}

func (h *AuthHandler) RegisterAPIKey(w http.ResponseWriter, r *http.Request) {
	providerID := chi.URLParam(r, "provider")
	pc, ok := h.registry.Get(providerID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown provider: " + providerID})
		return
	}
	if pc.AuthType != AuthTypeAPIKey && pc.AuthType != AuthTypeSessionCookie {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider does not use API key or session cookie auth"})
		return
	}

	var req struct {
		APIKey        string `json:"api_key"`
		SessionCookie string `json:"session_cookie"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	secret := req.APIKey
	if pc.AuthType == AuthTypeSessionCookie {
		secret = req.SessionCookie
	}
	if secret == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "api_key or session_cookie required"})
		return
	}

	accountID := secret[len(secret)-6:]
	if len(secret) < 6 {
		accountID = secret
	}

	tier := "unknown"
	if pc.AuthType == AuthTypeSessionCookie {
		tier = "browser_session"
	}

	token := TokenInfo{
		AccessToken: secret,
		AccountID:   accountID,
		Provider:    providerID,
		Tier:        tier,
		IsDefault:   false,
		CreatedAt:   time.Now(),
	}

	tokens, _ := h.store.ListByProvider(providerID)
	if len(tokens) == 0 {
		token.IsDefault = true
	}

	if err := h.store.Store(token); err != nil {
		slog.Error("store credential failed", "provider", providerID, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store credential"})
		return
	}

	slog.Info("credential registered", "provider", providerID, "auth_type", string(pc.AuthType), "account_id", accountID)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "registered",
		"account": token,
	})
}

// Routes returns all auth routes for mounting on a chi.Router.
func (h *AuthHandler) Routes() func(chi.Router) {
	return func(r chi.Router) {
		r.Post("/auth/{provider}/start", h.StartAuth)
		r.Post("/auth/{provider}/start-url", h.StartAuthURL)
		r.Post("/auth/{provider}/register", h.RegisterAPIKey)
		r.Get("/auth/{provider}/callback", h.HandleCallback)
		r.Get("/auth/{provider}/status", h.PollStatus)
		r.Post("/auth/{provider}/cancel", h.CancelAuth)
		r.Get("/auth/accounts", h.ListAccounts)
		r.Get("/auth/accounts/{provider}", h.ListAccounts)
		r.Delete("/auth/accounts/{provider}/{accountId}", h.RemoveAccount)
		r.Post("/auth/accounts/{provider}/{accountId}/pause", h.PauseAccount)
		r.Post("/auth/accounts/{provider}/{accountId}/resume", h.ResumeAccount)
		r.Post("/auth/accounts/{provider}/{accountId}/default", h.SetDefaultAccount)
		r.Post("/auth/login", h.DashboardLogin)
		r.Post("/auth/logout", h.DashboardLogout)
		r.Get("/auth/check", h.CheckAuth)
		r.Get("/providers", h.ListProviders)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	b, err := json.Marshal(v)
	if err != nil {
		json.NewEncoder(w).Encode(v)
		return
	}
	w.Write(b)
	w.Write([]byte("\n"))
}

// ListProviders returns all registered providers with their config.
func (h *AuthHandler) ListProviders(w http.ResponseWriter, r *http.Request) {
	if h.registry == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	providers := h.registry.List()
	type providerInfo struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		AuthType string `json:"authType"`
	}
	result := make([]providerInfo, 0, len(providers))
	for _, p := range providers {
		result = append(result, providerInfo{
			ID:       p.ID,
			Name:     p.Name,
			AuthType: string(p.AuthType),
		})
	}
	writeJSON(w, http.StatusOK, result)
}
