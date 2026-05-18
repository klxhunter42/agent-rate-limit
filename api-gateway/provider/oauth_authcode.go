package provider

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	oauthClient     *http.Client
	oauthClientOnce sync.Once
)

func getOAuthClient() *http.Client {
	oauthClientOnce.Do(func() {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if os.Getenv("HTTPS_PROXY") != "" {
			transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		}
		oauthClient = &http.Client{Transport: transport}
	})
	return oauthClient
}

type AuthCodeStartResponse struct {
	AuthURL      string `json:"auth_url"`
	State        string `json:"state"`
	PKCEVerifier string `json:"-"`
	ClientID     string `json:"client_id"`
	RedirectURI  string `json:"redirect_uri"`
	Scopes       string `json:"scopes"`
}

type AuthCallbackResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Email        string `json:"email"`
}

type tokenExchangeResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token,omitempty"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	Error        string `json:"error,omitempty"`
	ErrorDesc    string `json:"error_description,omitempty"`
}

type userInfoResponse struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

type claudeProfileResponse struct {
	Account struct {
		UUID         string `json:"uuid"`
		FullName     string `json:"full_name"`
		DisplayName  string `json:"display_name"`
		Email        string `json:"email"`
		HasClaudeMax bool   `json:"has_claude_max"`
		HasClaudePro bool   `json:"has_claude_pro"`
		CreatedAt    string `json:"created_at"`
	} `json:"account"`
	Organization struct {
		UUID                  string `json:"uuid"`
		Name                  string `json:"name"`
		OrganizationType      string `json:"organization_type"`
		BillingType           string `json:"billing_type"`
		RateLimitTier         string `json:"rate_limit_tier"`
		SeatTier              string `json:"seat_tier"`
		HasExtraUsageEnabled  bool   `json:"has_extra_usage_enabled"`
		SubscriptionStatus    string `json:"subscription_status"`
		SubscriptionCreatedAt string `json:"subscription_created_at"`
	} `json:"organization"`
	Application struct {
		UUID string `json:"uuid"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"application"`
}

type claudeRolesResponse struct {
	OrganizationUUID string `json:"organization_uuid"`
	OrganizationName string `json:"organization_name"`
	OrganizationRole string `json:"organization_role"`
	WorkspaceUUID    string `json:"workspace_uuid"`
	WorkspaceName    string `json:"workspace_name"`
	WorkspaceRole    string `json:"workspace_role"`
}

// generateState creates a random 32-byte base64url state parameter.
func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// generatePKCE generates a code_verifier and its S256 code_challenge.
// code_verifier: 43-128 chars, [A-Z] / [a-z] / [0-9] / "-._~"
// code_challenge: BASE64URL(SHA256(code_verifier))
func generatePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)

	hash := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(hash[:])
	return verifier, challenge, nil
}

// StartAuthCode initiates an authorization code flow with PKCE.
func StartAuthCode(ctx context.Context, pc ProviderConfig, redirectBase string) (*AuthCodeStartResponse, error) {
	state, err := generateState()
	if err != nil {
		return nil, fmt.Errorf("generate state: %w", err)
	}

	verifier, challenge, err := generatePKCE()
	if err != nil {
		return nil, fmt.Errorf("generate PKCE: %w", err)
	}

	// Localhost redirects: no Google Console registration needed.
	// Claude: http://localhost:8765/callback (Anthropic requirement)
	// Gemini: http://localhost (Google Desktop app auto-allows)
	redirectURI := fmt.Sprintf("%s/v1/auth/%s/callback", redirectBase, pc.ID)
	if pc.ClientSecret == "" && pc.ID != "gemini-oauth" {
		redirectURI = "http://localhost:8765/callback"
	}
	if pc.ID == "gemini-oauth" {
		redirectURI = "http://localhost"
	}

	params := url.Values{}
	params.Set("client_id", pc.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", "code")
	params.Set("scope", strings.Join(pc.Scopes, " "))
	params.Set("state", state)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")

	// Google-specific params.
	if pc.ID == "gemini-oauth" {
		params.Set("access_type", "offline")
		params.Set("prompt", "consent")
	}
	// Claude OAuth requires code=true to grant inference scopes.
	if pc.ID == "claude-oauth" {
		params.Set("code", "true")
	}

	authURL := pc.AuthURL + "?" + params.Encode()

	slog.Info("auth code flow started", "provider", pc.ID, "redirect_uri", redirectURI, "pkce", true)
	return &AuthCodeStartResponse{
		AuthURL:      authURL,
		State:        state,
		PKCEVerifier: verifier,
		ClientID:     pc.ClientID,
		RedirectURI:  redirectURI,
		Scopes:       strings.Join(pc.Scopes, " "),
	}, nil
}

// HandleCallbackWithPKCE exchanges an authorization code for tokens using PKCE.
func HandleCallbackWithPKCE(ctx context.Context, pc ProviderConfig, code, state, redirectBase, pkceVerifier string) (*TokenInfo, error) {
	// Build redirect_uri matching what was sent in StartAuthCode.
	redirectURI := fmt.Sprintf("%s/v1/auth/%s/callback", redirectBase, pc.ID)
	if pc.ClientSecret == "" && pc.ID != "gemini-oauth" {
		redirectURI = "http://localhost:8765/callback"
	}
	if pc.ID == "gemini-oauth" {
		redirectURI = "http://localhost"
	}

	// Claude uses JSON body, Google uses form-urlencoded.
	useJSON := pc.ClientSecret == "" && pc.ID != "gemini-oauth"

	var reqBody []byte
	var contentType string

	if useJSON {
		payload := map[string]any{
			"grant_type":    "authorization_code",
			"code":          code,
			"redirect_uri":  redirectURI,
			"client_id":     pc.ClientID,
			"code_verifier": pkceVerifier,
			"state":         state,
		}
		reqBody, _ = json.Marshal(payload)
		contentType = "application/json"
	} else {
		form := url.Values{}
		form.Set("code", code)
		form.Set("client_id", pc.ClientID)
		form.Set("redirect_uri", redirectURI)
		form.Set("grant_type", "authorization_code")

		// PKCE + client_secret: Google installed apps require both.
		if pkceVerifier != "" {
			form.Set("code_verifier", pkceVerifier)
		}
		if pc.ClientSecret != "" {
			form.Set("client_secret", pc.ClientSecret)
		}
		reqBody = []byte(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pc.TokenURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("build token exchange: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")

	resp, err := getOAuthClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokResp tokenExchangeResponse
	if err := json.Unmarshal(body, &tokResp); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if tokResp.Error != "" {
		return nil, fmt.Errorf("token error: %s: %s", tokResp.Error, tokResp.ErrorDesc)
	}

	email := ""
	if pc.UserInfoURL != "" && tokResp.AccessToken != "" {
		email = fetchUserInfo(ctx, pc.UserInfoURL, tokResp.AccessToken)
	}
	// Fallback: extract email from ID token JWT (for providers like Claude OAuth).
	if email == "" && tokResp.IDToken != "" {
		email = extractEmailFromIDToken(tokResp.IDToken)
	}

	accountID := email
	if accountID == "" {
		accountID = deriveAccountID(pc.ID, tokResp.AccessToken)
	}

	expiry := time.Now().Add(time.Duration(tokResp.ExpiresIn) * time.Second)
	if tokResp.ExpiresIn == 0 {
		expiry = time.Now().Add(24 * time.Hour)
	}

	token := &TokenInfo{
		AccessToken:  tokResp.AccessToken,
		RefreshToken: tokResp.RefreshToken,
		ExpiryDate:   expiry,
		Email:        email,
		Provider:     pc.ID,
		AccountID:    accountID,
		CreatedAt:    time.Now(),
		Scopes:       firstNonEmpty(tokResp.Scope, strings.Join(pc.Scopes, " ")),
	}

	// Fetch Claude profile for claude-oauth provider
	if pc.ID == "claude-oauth" && tokResp.AccessToken != "" {
		if profile, err := FetchClaudeProfile(ctx, tokResp.AccessToken); err == nil {
			token.ClaudeProfile = profile
			// Always use email from profile for Claude OAuth
			if profile.AccountEmail != "" {
				token.Email = profile.AccountEmail
				token.AccountID = profile.AccountEmail
			}
		} else {
			slog.Warn("failed to fetch claude profile", "error", err)
		}
	}

	slog.Info("auth code token obtained", "provider", pc.ID, "account_id", accountID, "email", email)
	return token, nil
}

// HandleCallback exchanges an authorization code for tokens (legacy, no PKCE).
func HandleCallback(ctx context.Context, pc ProviderConfig, code, state, redirectBase string) (*TokenInfo, error) {
	return HandleCallbackWithPKCE(ctx, pc, code, state, redirectBase, "")
}

func fetchUserInfo(ctx context.Context, userInfoURL, accessToken string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userInfoURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := getOAuthClient().Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var info userInfoResponse
	if err := json.Unmarshal(body, &info); err != nil {
		return ""
	}
	return info.Email
}

// extractEmailFromIDToken parses an unverified JWT id_token to extract the email claim.
// No signature verification since we only use it for display purposes (account identification).
func extractEmailFromIDToken(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return ""
	}
	// Decode base64url payload (2nd part).
	payload := parts[1]
	// Fix padding.
	if l := len(payload) % 4; l > 0 {
		payload += strings.Repeat("=", 4-l)
	}
	data, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		// Try raw base64url.
		data, err = base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			return ""
		}
	}
	var claims map[string]any
	if err := json.Unmarshal(data, &claims); err != nil {
		return ""
	}
	if email, ok := claims["email"].(string); ok && email != "" {
		return email
	}
	return ""
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// FetchClaudeProfile retrieves user profile, organization, and role information
// from Claude's /api/oauth/profile and /api/oauth/claude_cli/roles endpoints.
func FetchClaudeProfile(ctx context.Context, accessToken string) (*ClaudeProfile, error) {
	profileURL := "https://api.anthropic.com/api/oauth/profile"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, profileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build profile request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := getOAuthClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("profile request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("profile failed (%d): %s", resp.StatusCode, string(body))
	}

	var profileResp claudeProfileResponse
	if err := json.Unmarshal(body, &profileResp); err != nil {
		return nil, fmt.Errorf("decode profile response: %w", err)
	}

	profile := &ClaudeProfile{
		AccountUUID:   profileResp.Account.UUID,
		AccountName:   profileResp.Account.FullName,
		AccountEmail:  profileResp.Account.Email,
		HasClaudeMax:  profileResp.Account.HasClaudeMax,
		HasClaudePro:  profileResp.Account.HasClaudePro,
		OrgUUID:       profileResp.Organization.UUID,
		OrgName:       profileResp.Organization.Name,
		OrgType:       profileResp.Organization.OrganizationType,
		BillingType:   profileResp.Organization.BillingType,
		RateLimitTier: profileResp.Organization.RateLimitTier,
		SeatTier:      profileResp.Organization.SeatTier,
		SubStatus:     profileResp.Organization.SubscriptionStatus,
		HasExtraUsage: profileResp.Organization.HasExtraUsageEnabled,
		AppUUID:       profileResp.Application.UUID,
		AppName:       profileResp.Application.Name,
	}

	// Fetch roles separately
	rolesURL := "https://api.anthropic.com/api/oauth/claude_cli/roles"
	req2, err := http.NewRequestWithContext(ctx, http.MethodGet, rolesURL, nil)
	if err == nil {
		req2.Header.Set("Authorization", "Bearer "+accessToken)
		req2.Header.Set("Accept", "application/json")
		resp2, err := getOAuthClient().Do(req2)
		if err == nil {
			defer resp2.Body.Close()
			body2, _ := io.ReadAll(resp2.Body)
			if resp2.StatusCode == http.StatusOK {
				var rolesResp claudeRolesResponse
				if json.Unmarshal(body2, &rolesResp) == nil {
					profile.OrgRole = rolesResp.OrganizationRole
					profile.WorkspaceUUID = rolesResp.WorkspaceUUID
					profile.WorkspaceName = rolesResp.WorkspaceName
					profile.WorkspaceRole = rolesResp.WorkspaceRole
				}
			}
		}
	}

	slog.Info("claude profile fetched",
		"email", profile.AccountEmail,
		"org", profile.OrgName,
		"tier", profile.RateLimitTier,
		"role", profile.OrgRole,
	)

	return profile, nil
}
