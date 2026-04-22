package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type RefreshWorker struct {
	store    *TokenStore
	registry *Registry
	interval time.Duration
	stop     chan struct{}
}

func NewRefreshWorker(store *TokenStore, registry *Registry) *RefreshWorker {
	return &RefreshWorker{
		store:    store,
		registry: registry,
		interval: 30 * time.Minute,
		stop:     make(chan struct{}),
	}
}

func (w *RefreshWorker) Start(ctx context.Context) {
	slog.Info("token refresh worker started", "interval", w.interval)

	// Refresh immediately on start, then every interval.
	w.refreshAll(ctx)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case <-ticker.C:
			w.refreshAll(ctx)
		}
	}
}

func (w *RefreshWorker) Stop() {
	close(w.stop)
}

func (w *RefreshWorker) refreshAll(ctx context.Context) {
	tokens, err := w.store.ListAll()
	if err != nil {
		slog.Error("refresh worker: list tokens failed", "error", err)
		return
	}

	threshold := time.Now().Add(45 * time.Minute)
	refreshed, failed := 0, 0

	for _, t := range tokens {
		if t.Paused {
			continue
		}
		if t.RefreshToken == "" && t.AuthType() != AuthTypeAuthCode {
			continue
		}
		if t.ExpiryDate.After(threshold) {
			continue
		}

		pc, ok := w.registry.Get(t.Provider)
		if !ok || pc.AuthType != AuthTypeAuthCode {
			continue
		}

		if err := w.refreshToken(ctx, pc, t); err != nil {
			slog.Warn("token refresh failed",
				"provider", t.Provider,
				"account_id", t.AccountID,
				"error", err,
			)
			failed++
			continue
		}
		refreshed++
	}

	slog.Info("token refresh cycle completed", "refreshed", refreshed, "failed", failed)

	// Resolve missing project IDs for gemini-oauth tokens.
	w.resolveMissingProjects(ctx)
}

func (w *RefreshWorker) refreshToken(ctx context.Context, pc ProviderConfig, t TokenInfo) error {
	for attempt := 0; attempt < 3; attempt++ {
		err := w.doRefresh(ctx, pc, &t)
		if err == nil {
			return nil
		}

		backoff := time.Duration(1<<uint(attempt)) * 5 * time.Second
		slog.Warn("refresh retry",
			"provider", t.Provider,
			"account_id", t.AccountID,
			"attempt", attempt+1,
			"backoff", backoff,
			"error", err,
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
	return fmt.Errorf("exceeded max refresh attempts")
}

func (w *RefreshWorker) doRefresh(ctx context.Context, pc ProviderConfig, t *TokenInfo) error {
	var req *http.Request
	var err error

	// Claude OAuth requires JSON body, others use form-urlencoded
	if t.Provider == "claude-oauth" {
		payload := map[string]string{
			"grant_type":    "refresh_token",
			"refresh_token": t.RefreshToken,
			"client_id":     pc.ClientID,
		}
		body, _ := json.Marshal(payload)
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, pc.TokenURL, strings.NewReader(string(body)))
		if err != nil {
			return fmt.Errorf("build refresh request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
	} else {
		form := url.Values{}
		form.Set("grant_type", "refresh_token")
		form.Set("refresh_token", t.RefreshToken)
		form.Set("client_id", pc.ClientID)
		if pc.ClientSecret != "" {
			form.Set("client_secret", pc.ClientSecret)
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, pc.TokenURL, strings.NewReader(form.Encode()))
		if err != nil {
			return fmt.Errorf("build refresh request: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("refresh failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokResp); err != nil {
		return fmt.Errorf("decode refresh response: %w", err)
	}

	t.AccessToken = tokResp.AccessToken
	if tokResp.RefreshToken != "" {
		t.RefreshToken = tokResp.RefreshToken
	}
	if tokResp.ExpiresIn > 0 {
		t.ExpiryDate = time.Now().Add(time.Duration(tokResp.ExpiresIn) * time.Second)
	}

	if err := w.store.Store(*t); err != nil {
		return fmt.Errorf("store refreshed token: %w", err)
	}

	slog.Info("token refreshed",
		"provider", t.Provider,
		"account_id", t.AccountID,
		"expires", t.ExpiryDate,
	)
	return nil
}

// RefreshOne refreshes a single token by provider and accountID.
// Returns error if the token or provider is not found, or if refresh fails.
func (w *RefreshWorker) RefreshOne(providerID, accountID string) error {
	t, err := w.store.Get(providerID, accountID)
	if err != nil {
		return fmt.Errorf("get token: %w", err)
	}
	if t == nil {
		return fmt.Errorf("token not found: %s/%s", providerID, accountID)
	}

	pc, ok := w.registry.Get(providerID)
	if !ok {
		return fmt.Errorf("provider not found: %s", providerID)
	}

	return w.doRefresh(context.Background(), pc, t)
}

// AuthType returns the auth type for a TokenInfo based on whether it has a refresh token.
// This is a convenience method to avoid needing the provider config.
func (t *TokenInfo) AuthType() AuthType {
	if t.RefreshToken != "" {
		return AuthTypeAuthCode
	}
	return AuthTypeDeviceCode
}

// resolveMissingProjects resolves Google CodeAssist project IDs for gemini-oauth
// tokens that don't have one stored yet. Called during each refresh cycle.
func (w *RefreshWorker) resolveMissingProjects(ctx context.Context) {
	tokens, err := w.store.ListByProvider("gemini-oauth")
	if err != nil || len(tokens) == 0 {
		return
	}

	pc, ok := w.registry.Get("gemini-oauth")
	if !ok {
		return
	}

	for _, t := range tokens {
		if t.Paused || t.ProjectID != "" || t.AccessToken == "" {
			continue
		}
		pid, err := w.loadCodeAssistProject(ctx, pc, t.AccessToken)
		if err != nil {
			slog.Warn("failed to resolve gemini project", "account_id", t.AccountID, "error", err)
			continue
		}
		t.ProjectID = pid
		if err := w.store.Store(t); err != nil {
			slog.Warn("failed to store project ID", "account_id", t.AccountID, "error", err)
		} else {
			slog.Info("resolved gemini project ID", "account_id", t.AccountID, "project_id", pid)
		}
	}
}

// loadCodeAssistProject calls the CodeAssist loadCodeAssist endpoint to obtain
// the cloudaicompanion projectId for the authenticated user.
func (w *RefreshWorker) loadCodeAssistProject(ctx context.Context, pc ProviderConfig, accessToken string) (string, error) {
	endpoint := pc.UpstreamBase + "/v1internal:loadCodeAssist"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader("{}"))
	if err != nil {
		return "", fmt.Errorf("build loadCodeAssist request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("loadCodeAssist request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("loadCodeAssist failed (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		ProjectID string `json:"cloudaicompanionProject"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode loadCodeAssist response: %w", err)
	}
	if result.ProjectID == "" {
		slog.Warn("loadCodeAssist returned no project ID, attempting onboardUser",
			"status", resp.StatusCode,
			"body", string(body),
		)
		// Try onboarding first, then load again.
		if pid, err := w.onboardAndLoad(ctx, pc, accessToken); err == nil {
			return pid, nil
		}
		return "", fmt.Errorf("loadCodeAssist returned empty project ID: %s", string(body))
	}
	return result.ProjectID, nil
}

func (w *RefreshWorker) onboardAndLoad(ctx context.Context, pc ProviderConfig, accessToken string) (string, error) {
	onboardEndpoint := pc.UpstreamBase + "/v1internal:onboardUser"
	reqBody := `{"tierId":"free-tier","metadata":{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, onboardEndpoint, strings.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("build onboardUser request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("onboardUser request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	slog.Info("onboardUser response", "status", resp.StatusCode, "body", string(body))

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("onboardUser failed (%d): %s", resp.StatusCode, string(body))
	}

	// onboardUser returns a Long Running Operation. Check if done.
	var lro struct {
		Name     string `json:"name"`
		Done     bool   `json:"done"`
		Response struct {
			Project struct {
				ID string `json:"id"`
			} `json:"cloudaicompanionProject"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &lro); err != nil {
		return "", fmt.Errorf("decode onboardUser response: %w", err)
	}

	// If LRO is not done, poll up to 3 times with 5s interval.
	if !lro.Done {
		for i := 0; i < 3; i++ {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(5 * time.Second):
			}
			pid, err := w.pollOperation(ctx, pc, accessToken, lro.Name)
			if err != nil {
				slog.Warn("poll operation failed", "attempt", i+1, "error", err)
				continue
			}
			if pid != "" {
				return pid, nil
			}
		}
	}

	if lro.Response.Project.ID != "" {
		return lro.Response.Project.ID, nil
	}

	// Fallback: call loadCodeAssist again after onboarding.
	return w.loadCodeAssistProject(ctx, pc, accessToken)
}

func (w *RefreshWorker) pollOperation(ctx context.Context, pc ProviderConfig, accessToken, opName string) (string, error) {
	endpoint := pc.UpstreamBase + "/v1internal/" + opName
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("build poll request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("poll request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("poll failed (%d): %s", resp.StatusCode, string(body))
	}

	var lro struct {
		Done     bool `json:"done"`
		Response struct {
			Project struct {
				ID string `json:"id"`
			} `json:"cloudaicompanionProject"`
		} `json:"response"`
	}
	if err := json.Unmarshal(body, &lro); err != nil {
		return "", fmt.Errorf("decode poll response: %w", err)
	}
	if lro.Done && lro.Response.Project.ID != "" {
		return lro.Response.Project.ID, nil
	}
	return "", nil
}
