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

type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type DeviceCodePollResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope,omitempty"`
	Error       string `json:"error,omitempty"`
}

// StartDeviceCode initiates a device code flow for the given provider.
func StartDeviceCode(ctx context.Context, pc ProviderConfig) (*DeviceCodeResponse, error) {
	form := url.Values{}
	form.Set("client_id", pc.ClientID)
	form.Set("scope", strings.Join(pc.Scopes, " "))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pc.DeviceCodeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build device code request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device code request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device code failed (%d): %s", resp.StatusCode, string(body))
	}

	var dcr DeviceCodeResponse
	if err := json.Unmarshal(body, &dcr); err != nil {
		return nil, fmt.Errorf("decode device code response: %w", err)
	}

	slog.Info("device code initiated", "provider", pc.ID, "user_code", dcr.UserCode)
	return &dcr, nil
}

// PollDeviceToken polls the provider for a device code token exchange.
// Returns nil, nil if the authorization is still pending.
func PollDeviceToken(ctx context.Context, pc ProviderConfig, deviceCode string) (*TokenInfo, error) {
	form := url.Values{}
	form.Set("client_id", pc.ClientID)
	form.Set("device_code", deviceCode)
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

	if pc.ClientSecret != "" {
		form.Set("client_secret", pc.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pc.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build poll request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("poll request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var pollResp DeviceCodePollResponse
	if err := json.Unmarshal(body, &pollResp); err != nil {
		return nil, fmt.Errorf("decode poll response: %w", err)
	}

	if pollResp.Error != "" {
		if pollResp.Error == "authorization_pending" || pollResp.Error == "slow_down" {
			return nil, nil
		}
		return nil, fmt.Errorf("device poll error: %s", pollResp.Error)
	}

	if pollResp.AccessToken == "" {
		return nil, nil
	}

	accountID := deriveAccountID(pc.ID, pollResp.AccessToken)

	token := &TokenInfo{
		AccessToken: pollResp.AccessToken,
		Provider:    pc.ID,
		AccountID:   accountID,
		ExpiryDate:  time.Now().Add(90 * 24 * time.Hour), // device tokens typically long-lived
		CreatedAt:   time.Now(),
	}

	slog.Info("device token obtained", "provider", pc.ID, "account_id", accountID)
	return token, nil
}

// deriveAccountID creates a stable account ID from a provider token.
func deriveAccountID(provider, accessToken string) string {
	if len(accessToken) < 16 {
		return accessToken
	}
	// Use last 12 chars as a stable identifier.
	suffix := accessToken[len(accessToken)-12:]
	return provider + "_" + suffix
}
