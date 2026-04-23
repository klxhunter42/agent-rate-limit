package provider

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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
	counters   sync.Map // map[string]*atomic.Uint64, keyed by providerID
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
	"anthropic": {FormatAnthropic, "api_key", "/v1/messages", nil},
	"claude-oauth": {FormatAnthropic, "bearer", "/v1/messages?beta=true", map[string]string{
		"anthropic-beta": "oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,advanced-tool-use-2025-11-20,effort-2025-11-24",
		"x-app":          "cli",
		"User-Agent":     "claude-cli/2.1.94 (external, sdk-cli)",
		"anthropic-dangerous-direct-browser-access": "true",
		"Accept":                      "application/json",
		"accept-language":             "*",
		"sec-fetch-mode":              "cors",
		"X-Stainless-Lang":            "js",
		"X-Stainless-Package-Version": "0.81.0",
		"X-Stainless-OS":              "MacOS",
		"X-Stainless-Arch":            "arm64",
		"X-Stainless-Runtime":         "node",
		"X-Stainless-Runtime-Version": "v25.5.0",
		"X-Stainless-Retry-Count":     "0",
		"X-Stainless-Timeout":         "3000",
	}},
	"claude": {FormatAnthropic, "bearer", "/v1/messages?beta=true", map[string]string{
		"anthropic-beta": "oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05,advanced-tool-use-2025-11-20,effort-2025-11-24",
		"x-app":          "cli",
		"User-Agent":     "claude-cli/2.1.94 (external, sdk-cli)",
		"anthropic-dangerous-direct-browser-access": "true",
		"Accept":                      "application/json",
		"accept-language":             "*",
		"sec-fetch-mode":              "cors",
		"X-Stainless-Lang":            "js",
		"X-Stainless-Package-Version": "0.81.0",
		"X-Stainless-OS":              "MacOS",
		"X-Stainless-Arch":            "arm64",
		"X-Stainless-Runtime":         "node",
		"X-Stainless-Runtime-Version": "v25.5.0",
		"X-Stainless-Retry-Count":     "0",
		"X-Stainless-Timeout":         "3000",
	}}, // alias
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

// ModelBelongsToProvider checks if a model name routes to the given provider.
func ModelBelongsToProvider(model, providerID string) bool {
	for _, rule := range modelRules {
		if strings.HasPrefix(model, rule.prefix) {
			for _, pid := range rule.providers {
				if pid == providerID {
					return true
				}
			}
			return false
		}
	}
	return false
}

func (r *Resolver) Resolve(model string) *RoutingDecision {
	for _, rule := range modelRules {
		if strings.HasPrefix(model, rule.prefix) {
			for _, pid := range rule.providers {
				var decision *RoutingDecision
				if pid == "claude-oauth" {
					decision = r.tryResolveRoundRobin(pid, model)
				} else {
					decision = r.tryResolve(pid, model)
				}
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

// tryResolveRoundRobin cycles through all active tokens for a provider.
// Prefers accounts with low 5h utilization; falls back to all if all are high.
func (r *Resolver) tryResolveRoundRobin(providerID, model string) *RoutingDecision {
	if r.tokenStore == nil {
		return nil
	}
	tokens, err := r.tokenStore.ListByProvider(providerID)
	if err != nil || len(tokens) == 0 {
		return nil
	}
	var active []TokenInfo
	for _, t := range tokens {
		if !t.Paused {
			active = append(active, t)
		}
	}
	if len(active) == 0 {
		return nil
	}

	// If multiple accounts, try to pick one with lowest utilization.
	if len(active) > 1 {
		ids := make([]string, len(active))
		for i, t := range active {
			ids[i] = t.AccountID
		}
		rls := r.tokenStore.GetRateLimits(providerID, ids)

		// Partition into low-util (<0.8) and high-util.
		var low, high []TokenInfo
		for _, t := range active {
			if rl, ok := rls[t.AccountID]; ok && rl.Util5h >= 0.8 {
				high = append(high, t)
			} else {
				low = append(low, t)
			}
		}
		if len(low) > 0 {
			active = low
		} else if len(high) > 0 {
			active = high
		}
	}

	val, _ := r.counters.LoadOrStore(providerID, new(atomic.Uint64))
	counter := val.(*atomic.Uint64)
	idx := int(counter.Add(1)-1) % len(active)
	return r.buildDecision(providerID, model, active[idx].AccessToken)
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
