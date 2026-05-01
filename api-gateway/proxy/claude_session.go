package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	claudeProfileURL  = "https://api.anthropic.com/api/oauth/profile"
	claudeRolesURL    = "https://api.anthropic.com/api/oauth/claude_cli/roles"
	claudeSettingsURL = "https://api.anthropic.com/api/claude_code/settings"
	claudePolicyURL   = "https://api.anthropic.com/api/claude_code/policy_limits"
	cliUserAgent      = "claude-cli/2.1.123 (external, cli)"
)

type ClaudeSession struct {
	Token        string
	Profile      json.RawMessage
	Roles        json.RawMessage
	Settings     json.RawMessage
	PolicyLimits json.RawMessage
	Bootstrapped bool
	BootstrapAt  time.Time
}

type ClaudeSessionManager struct {
	sessions sync.Map
	client   *http.Client
}

func NewClaudeSessionManager() *ClaudeSessionManager {
	return &ClaudeSessionManager{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func cliBootstrapHeaders(token string) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	h.Set("anthropic-version", "2023-06-01")
	h.Set("user-agent", cliUserAgent)
	h.Set("x-app", "cli")
	h.Set("accept", "application/json")
	return h
}

func (m *ClaudeSessionManager) doGet(url, token string) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range cliBootstrapHeaders(token) {
		req.Header[k] = v
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GET %s: status %d: %s", url, resp.StatusCode, string(body[:min(200, len(body))]))
	}
	return json.RawMessage(body), nil
}

func (m *ClaudeSessionManager) BootstrapSession(token string) (*ClaudeSession, error) {
	sess := &ClaudeSession{Token: token}

	if profile, err := m.doGet(claudeProfileURL, token); err != nil {
		slog.Warn("claude session: profile failed", "error", err)
	} else {
		sess.Profile = profile
		slog.Info("claude session: profile OK", "size", len(profile))
	}

	roles, err := m.doGet(claudeRolesURL, token)
	if err != nil {
		return nil, fmt.Errorf("claude session: roles failed: %w", err)
	}
	sess.Roles = roles
	slog.Info("claude session: roles OK", "size", len(roles))

	if settings, err := m.doGet(claudeSettingsURL, token); err != nil {
		slog.Warn("claude session: settings failed", "error", err)
	} else {
		sess.Settings = settings
		slog.Info("claude session: settings OK", "size", len(settings))
	}

	if policy, err := m.doGet(claudePolicyURL, token); err != nil {
		slog.Warn("claude session: policy_limits failed", "error", err)
	} else {
		sess.PolicyLimits = policy
	}

	sess.Bootstrapped = true
	sess.BootstrapAt = time.Now()
	m.sessions.Store(token, sess)

	slog.Info("claude session bootstrapped", "token_prefix", token[:min(20, len(token))]+"...")
	return sess, nil
}

func (m *ClaudeSessionManager) GetOrCreateSession(token string) (*ClaudeSession, error) {
	if v, ok := m.sessions.Load(token); ok {
		sess := v.(*ClaudeSession)
		if sess.Bootstrapped {
			return sess, nil
		}
	}
	return m.BootstrapSession(token)
}

func (m *ClaudeSessionManager) InvalidateSession(token string) {
	m.sessions.Delete(token)
}

func (m *ClaudeSessionManager) BootstrapIfNeeded(token string) {
	if !strings.HasPrefix(token, "sk-ant-oat") {
		return
	}
	if v, ok := m.sessions.Load(token); ok {
		if s, ok := v.(*ClaudeSession); ok && s.Bootstrapped {
			return
		}
	}
	if _, err := m.BootstrapSession(token); err != nil {
		slog.Warn("claude session bootstrap failed", "error", err, "token_prefix", token[:min(20, len(token))]+"...")
	}
}
