package provider

import (
	"os"
)

type AuthType string

const (
	AuthTypeAPIKey        AuthType = "api_key"
	AuthTypeDeviceCode    AuthType = "device_code"
	AuthTypeAuthCode      AuthType = "auth_code"
	AuthTypeSessionCookie AuthType = "session_cookie"
)

type ProviderConfig struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	AuthType      AuthType `json:"auth_type"`
	TokenURL      string   `json:"token_url,omitempty"`
	AuthURL       string   `json:"auth_url,omitempty"`
	DeviceCodeURL string   `json:"device_code_url,omitempty"`
	UserInfoURL   string   `json:"user_info_url,omitempty"`
	CallbackPort  int      `json:"callback_port,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
	ClientID      string   `json:"client_id,omitempty"`
	ClientSecret  string   `json:"client_secret,omitempty"`
	UpstreamBase  string   `json:"upstream_base"`
}

type Registry struct {
	providers map[string]ProviderConfig
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func NewRegistry() *Registry {
	r := &Registry{providers: make(map[string]ProviderConfig)}

	r.providers["anthropic"] = ProviderConfig{
		ID:           "anthropic",
		Name:         "Anthropic",
		AuthType:     AuthTypeAPIKey,
		UpstreamBase: envOr("ANTHROPIC_UPSTREAM_BASE", "https://api.anthropic.com"),
	}

	// API Key auth - get key at https://aistudio.google.com/apikey
	r.providers["gemini"] = ProviderConfig{
		ID:           "gemini",
		Name:         "Google Gemini",
		AuthType:     AuthTypeAPIKey,
		UpstreamBase: envOr("GEMINI_UPSTREAM_BASE", "https://generativelanguage.googleapis.com"),
	}

	// OAuth via Code Assist proxy - uses bundled Gemini CLI Client ID.
	// Ref: https://github.com/google-gemini/gemini-cli/blob/main/packages/core/src/code_assist/oauth2.ts
	// Routes through cloudcode-pa.googleapis.com (not generativelanguage.googleapis.com).
	r.providers["gemini-oauth"] = ProviderConfig{
		ID:           "gemini-oauth",
		Name:         "Google Gemini (OAuth)",
		AuthType:     AuthTypeAuthCode,
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		UserInfoURL:  "https://www.googleapis.com/oauth2/v2/userinfo",
		Scopes:       []string{"https://www.googleapis.com/auth/cloud-platform", "https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile"},
		ClientID:     envOr("GEMINI_OAUTH_CLIENT_ID", ""),
		ClientSecret: envOr("GEMINI_OAUTH_CLIENT_SECRET", ""),
		UpstreamBase: envOr("GEMINI_CODEASSIST_BASE", "https://cloudcode-pa.googleapis.com"),
	}

	r.providers["openai"] = ProviderConfig{
		ID:           "openai",
		Name:         "OpenAI",
		AuthType:     AuthTypeAPIKey,
		UpstreamBase: envOr("OPENAI_UPSTREAM_BASE", "https://api.openai.com"),
	}

	r.providers["copilot"] = ProviderConfig{
		ID:            "copilot",
		Name:          "GitHub Copilot",
		AuthType:      AuthTypeDeviceCode,
		DeviceCodeURL: "https://github.com/login/device/code",
		TokenURL:      "https://github.com/login/oauth/access_token",
		Scopes:        []string{},
		ClientID:      envOr("COPILOT_CLIENT_ID", "Iv1.b507a08c87ecfe98"),
		ClientSecret:  "",
		UpstreamBase:  "https://api.github.com/copilot",
	}

	r.providers["zai"] = ProviderConfig{
		ID:           "zai",
		Name:         "Z.AI",
		AuthType:     AuthTypeAPIKey,
		UpstreamBase: envOr("ZAI_UPSTREAM_BASE", "https://api.z.ai/api/anthropic"),
	}

	r.providers["openrouter"] = ProviderConfig{
		ID:           "openrouter",
		Name:         "OpenRouter",
		AuthType:     AuthTypeAPIKey,
		UpstreamBase: envOr("OPENROUTER_UPSTREAM_BASE", "https://openrouter.ai/api"),
	}

	r.providers["qwen"] = ProviderConfig{
		ID:            "qwen",
		Name:          "Qwen (Aliyun)",
		AuthType:      AuthTypeDeviceCode,
		DeviceCodeURL: envOr("QWEN_DEVICE_CODE_URL", ""),
		TokenURL:      envOr("QWEN_TOKEN_URL", ""),
		Scopes:        []string{},
		ClientID:      envOr("QWEN_CLIENT_ID", ""),
		ClientSecret:  envOr("QWEN_CLIENT_SECRET", ""),
		UpstreamBase:  envOr("QWEN_UPSTREAM_BASE", "https://dashscope.aliyuncs.com"),
	}

	// OAuth via Claude Code CLI pattern - PKCE only, no client_secret.
	// Ref: https://github.com/anthropics/claude-code (Client ID extracted from CLI)
	// Uses Bearer token with api.anthropic.com/v1/messages.
	// Auth: platform.claude.com/oauth/authorize (CONSOLE_AUTHORIZE_URL)
	// Token: platform.claude.com/v1/oauth/token (JSON body, not form-urlencoded)
	r.providers["claude"] = ProviderConfig{
		ID:           "claude",
		Name:         "Claude (OAuth)",
		AuthType:     AuthTypeAuthCode,
		AuthURL:      "https://platform.claude.com/oauth/authorize",
		TokenURL:     "https://platform.claude.com/v1/oauth/token",
		Scopes:       []string{"org:create_api_key", "user:profile", "user:inference", "user:sessions:claude_code", "user:mcp_servers", "user:file_upload"},
		ClientID:     envOr("CLAUDE_OAUTH_CLIENT_ID", "9d1c250a-e61b-44d9-88ed-5944d1962f5e"),
		ClientSecret: "",
		UpstreamBase: envOr("CLAUDE_UPSTREAM_BASE", "https://api.anthropic.com"),
	}

	r.providers["deepseek"] = ProviderConfig{
		ID:           "deepseek",
		Name:         "DeepSeek",
		AuthType:     AuthTypeAPIKey,
		UpstreamBase: envOr("DEEPSEEK_UPSTREAM_BASE", "https://api.deepseek.com"),
	}

	r.providers["kimi"] = ProviderConfig{
		ID:           "kimi",
		Name:         "Kimi (Moonshot)",
		AuthType:     AuthTypeAPIKey,
		UpstreamBase: envOr("KIMI_UPSTREAM_BASE", "https://api.moonshot.cn/v1"),
	}

	r.providers["huggingface"] = ProviderConfig{
		ID:           "huggingface",
		Name:         "Hugging Face",
		AuthType:     AuthTypeAPIKey,
		UpstreamBase: envOr("HUGGINGFACE_UPSTREAM_BASE", "https://api-inference.huggingface.co/models"),
	}

	r.providers["ollama"] = ProviderConfig{
		ID:           "ollama",
		Name:         "Ollama",
		AuthType:     AuthTypeAPIKey,
		UpstreamBase: envOr("OLLAMA_UPSTREAM_BASE", "http://localhost:11434"),
	}

	r.providers["agy"] = ProviderConfig{
		ID:           "agy",
		Name:         "Antigravity",
		AuthType:     AuthTypeAPIKey,
		UpstreamBase: envOr("AGY_UPSTREAM_BASE", "https://antigravity.com"),
	}

	r.providers["cursor"] = ProviderConfig{
		ID:           "cursor",
		Name:         "Cursor",
		AuthType:     AuthTypeAPIKey,
		UpstreamBase: envOr("CURSOR_UPSTREAM_BASE", "https://api2.cursor.sh"),
	}

	r.providers["codebuddy"] = ProviderConfig{
		ID:           "codebuddy",
		Name:         "CodeBuddy",
		AuthType:     AuthTypeAPIKey,
		UpstreamBase: envOr("CODEBUDDY_UPSTREAM_BASE", "https://api.codebuddy.io"),
	}

	r.providers["kilo"] = ProviderConfig{
		ID:           "kilo",
		Name:         "Kilo",
		AuthType:     AuthTypeAPIKey,
		UpstreamBase: envOr("KILO_UPSTREAM_BASE", "https://api.kilo.ai"),
	}

	return r
}

func (r *Registry) Get(id string) (ProviderConfig, bool) {
	p, ok := r.providers[id]
	return p, ok
}

func (r *Registry) List() []ProviderConfig {
	out := make([]ProviderConfig, 0, len(r.providers))
	for _, p := range r.providers {
		out = append(out, p)
	}
	return out
}
