package provider

import (
	"fmt"
	"strings"
)

type ProviderFormat string

const (
	FormatAnthropic ProviderFormat = "anthropic"
	FormatOpenAI    ProviderFormat = "openai"
	FormatGemini    ProviderFormat = "gemini"
)

type RoutingDecision struct {
	ProviderID   string
	ProviderCfg  ProviderConfig
	Format       ProviderFormat
	UpstreamURL  string
	AuthMode     string // "api_key", "bearer"
	ExtraHeaders map[string]string
	APIKey       string
}

type Resolver struct {
	registry   *Registry
	tokenStore *TokenStore
	glmMode    bool
}

func NewResolver(registry *Registry, tokenStore *TokenStore, glmMode bool) *Resolver {
	return &Resolver{registry: registry, tokenStore: tokenStore, glmMode: glmMode}
}

type providerRoute struct {
	format       ProviderFormat
	authMode     string
	urlSuffix    string
	extraHeaders map[string]string
}

var providerRouteTable = map[string]providerRoute{
	"anthropic":    {FormatAnthropic, "api_key", "/v1/messages", nil},
	"claude-oauth": {FormatAnthropic, "bearer", "/v1/messages", map[string]string{"anthropic-beta": "oauth-2025-04-20"}},
		"claude":       {FormatAnthropic, "bearer", "/v1/messages", map[string]string{"anthropic-beta": "oauth-2025-04-20"}}, // alias
	"zai":          {FormatAnthropic, "api_key", "/v1/messages", nil},
	"openai":       {FormatOpenAI, "bearer", "/v1/chat/completions", nil},
	"copilot":      {FormatOpenAI, "bearer", "/v1/chat/completions", nil},
	"openrouter":   {FormatOpenAI, "bearer", "/v1/chat/completions", map[string]string{"HTTP-Referer": "https://github.com/klxhunter/agent-rate-limit"}},
	"qwen":         {FormatOpenAI, "bearer", "/compatible-mode/v1/chat/completions", nil},
	"gemini":       {FormatGemini, "api_key", "", nil},
	"gemini-oauth": {FormatGemini, "bearer", "", nil},
	"deepseek":     {FormatOpenAI, "bearer", "/v1/chat/completions", nil},
	"kimi":         {FormatOpenAI, "bearer", "/v1/chat/completions", nil},
	"huggingface":  {FormatOpenAI, "bearer", "/v1/chat/completions", nil},
	"ollama":       {FormatOpenAI, "bearer", "/v1/chat/completions", nil},
	"agy":          {FormatAnthropic, "api_key", "/v1/messages", nil},
	"cursor":       {FormatOpenAI, "bearer", "/v1/chat/completions", nil},
	"codebuddy":    {FormatOpenAI, "bearer", "/v1/chat/completions", nil},
	"kilo":         {FormatOpenAI, "bearer", "/v1/chat/completions", nil},
}

type modelRule struct {
	prefix    string
	providers []string // ordered by priority
}

var modelRules = []modelRule{
	{"claude-", []string{"claude-oauth", "anthropic"}},
	{"gpt-", []string{"openai"}},
	{"o1-", []string{"openai"}},
	{"o3-", []string{"openai"}},
	{"o4-", []string{"openai"}},
	{"gemini-", []string{"gemini-oauth", "gemini"}},
	{"glm-", []string{"zai"}},
	{"qwen-", []string{"qwen"}},
	{"or-", []string{"openrouter"}},
	{"anthropic/", []string{"anthropic", "openrouter"}},
	{"openai/", []string{"openrouter"}},
	{"google/", []string{"openrouter"}},
	{"meta/", []string{"openrouter"}},
	{"deepseek/", []string{"openrouter"}},
	{"qwen/", []string{"openrouter"}},
	{"deepseek-", []string{"deepseek"}},
	{"kimi-", []string{"kimi"}},
	{"huggingface/", []string{"huggingface"}},
	{"ollama", []string{"ollama"}},
	{"agy-", []string{"agy"}},
}

func (r *Resolver) Resolve(model string) *RoutingDecision {
	for _, rule := range modelRules {
		if strings.HasPrefix(model, rule.prefix) {
			for _, pid := range rule.providers {
				decision := r.tryResolve(pid, model)
				if decision != nil {
					return decision
				}
			}
			break
		}
	}

	// Default fallback: Z.AI only in GLM mode.
	if r.glmMode {
		decision := r.tryResolve("zai", model)
		if decision != nil {
			return decision
		}
		return r.buildDecision("zai", model, "")
	}
	return nil
}

// ResolveByProvider creates a routing decision for a specific provider ID,
// looking up its token and route config.
func (r *Resolver) ResolveByProvider(providerID string) (*RoutingDecision, bool) {
	if _, ok := r.registry.Get(providerID); !ok {
		return nil, false
	}
	var apiKey string
	if r.tokenStore != nil {
		if tok, err := r.tokenStore.GetDefault(providerID); err == nil && tok != nil {
			apiKey = tok.AccessToken
		}
	}
	return r.buildDecision(providerID, "", apiKey), true
}

func (r *Resolver) tryResolve(providerID, model string) *RoutingDecision {
	if r.tokenStore == nil {
		return nil
	}
	token, err := r.tokenStore.GetDefault(providerID)
	if err != nil {
		return nil
	}
	if token == nil {
		return nil
	}
	return r.buildDecision(providerID, model, token.AccessToken)
}

func (r *Resolver) buildDecision(providerID, model, apiKey string) *RoutingDecision {
	cfg, ok := r.registry.Get(providerID)
	if !ok {
		return nil
	}

	route, ok := providerRouteTable[providerID]
	if !ok {
		route = providerRoute{FormatAnthropic, "api_key", "/v1/messages", nil}
	}

	upstreamURL := cfg.UpstreamBase + route.urlSuffix

	// Gemini API key: endpoint includes model name and key as query param.
	if providerID == "gemini" && apiKey != "" {
		upstreamURL = fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?key=%s", cfg.UpstreamBase, model, apiKey)
	}

	return &RoutingDecision{
		ProviderID:   providerID,
		ProviderCfg:  cfg,
		Format:       route.format,
		UpstreamURL:  upstreamURL,
		AuthMode:     route.authMode,
		ExtraHeaders: route.extraHeaders,
		APIKey:       apiKey,
	}
}
