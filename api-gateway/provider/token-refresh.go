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
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", t.RefreshToken)
	form.Set("client_id", pc.ClientID)
	if pc.ClientSecret != "" {
		form.Set("client_secret", pc.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pc.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("build refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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

// AuthType returns the auth type for a TokenInfo based on whether it has a refresh token.
// This is a convenience method to avoid needing the provider config.
func (t *TokenInfo) AuthType() AuthType {
	if t.RefreshToken != "" {
		return AuthTypeAuthCode
	}
	return AuthTypeDeviceCode
}
