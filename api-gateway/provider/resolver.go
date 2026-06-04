package provider

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type ProviderFormat string

const (
	FormatAnthropic ProviderFormat = "anthropic"
	FormatOpenAI    ProviderFormat = "openai"
	FormatGemini    ProviderFormat = "gemini"
)

type RoutingDecision struct {
	ProviderID       string
	ProviderCfg      ProviderConfig
	Format           ProviderFormat
	UpstreamURL      string
	AuthMode         string // "api_key", "bearer"
	ExtraHeaders     map[string]string
	APIKey           string
	AccountID        string
	ModelOverride    string
	MaxTokens        int
	MaxContinuations int    // 0 = disabled, >0 = max auto-continuations
	ToolMode         string // "" = none, "native" = OpenAI function calling
}

type Resolver struct {
	registry   *Registry
	tokenStore *TokenStore
	glmMode    bool
	counters   sync.Map // map[string]*atomic.Uint64, keyed by providerID
	cooldowns  sync.Map // map[string]time.Time, providerID -> cooldown until
}

func NewResolver(registry *Registry, tokenStore *TokenStore, glmMode bool) *Resolver {
	return &Resolver{registry: registry, tokenStore: tokenStore, glmMode: glmMode}
}

// MarkCooldown marks a provider+model as rate-limited so subsequent requests skip it.
func (r *Resolver) MarkCooldown(providerID string, d time.Duration, model ...string) {
	key := providerID
	if len(model) > 0 && model[0] != "" {
		key = providerID + ":" + model[0]
	}
	r.cooldowns.Store(key, time.Now().Add(d))
}

func (r *Resolver) IsCoolingDown(providerID string, model ...string) bool {
	key := providerID
	if len(model) > 0 && model[0] != "" {
		key = providerID + ":" + model[0]
	}
	if v, ok := r.cooldowns.Load(key); ok {
		return time.Now().Before(v.(time.Time))
	}
	return false
}

// MarkAccountCooldown marks a specific account within a provider as rate-limited.
func (r *Resolver) MarkAccountCooldown(providerID, accountID string, d time.Duration) {
	key := providerID + ":account:" + accountID
	r.cooldowns.Store(key, time.Now().Add(d))
}

// IsAccountCoolingDown checks if a specific account within a provider is cooling down.
func (r *Resolver) IsAccountCoolingDown(providerID, accountID string) bool {
	key := providerID + ":account:" + accountID
	if v, ok := r.cooldowns.Load(key); ok {
		return time.Now().Before(v.(time.Time))
	}
	return false
}

type providerRoute struct {
	format        ProviderFormat
	authMode      string
	urlSuffix     string
	extraHeaders  map[string]string
	modelOverride string
	maxTokens     int
}

// providerContinuations maps provider IDs to max auto-continuation count.
// When a provider returns finish_reason:"length", the gateway automatically
// sends a continuation request up to this many times.
var providerContinuations = map[string]int{
	"lotuss": 3,
}

// providerToolMode maps provider IDs to tool handling mode.
// "native" = use OpenAI function calling format, convert tool_calls to Anthropic tool_use.
var providerToolMode = map[string]string{
	"lotuss": "native",
}

var providerRouteTable = map[string]providerRoute{
	"anthropic": {FormatAnthropic, "api_key", "/v1/messages", nil, "", 0},
	"claude-oauth": {FormatAnthropic, "api_key", "/v1/messages?beta=true", map[string]string{
		"anthropic-beta": "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,prompt-caching-scope-2026-01-05,advanced-tool-use-2025-11-20,effort-2025-11-24",
		"x-app":          "cli",
		"User-Agent":     "claude-cli/2.1.123 (external, cli)",
		"anthropic-dangerous-direct-browser-access": "true",
		"Accept":                      "application/json",
		"X-Stainless-Lang":            "js",
		"X-Stainless-Package-Version": "0.81.0",
		"X-Stainless-OS":              "MacOS",
		"X-Stainless-Arch":            "arm64",
		"X-Stainless-Runtime":         "node",
		"X-Stainless-Runtime-Version": "v24.3.0",
		"X-Stainless-Retry-Count":     "0",
		"X-Stainless-Timeout":         "3000",
	}, "", 0},
	"claude": {FormatAnthropic, "api_key", "/v1/messages?beta=true", map[string]string{
		"anthropic-beta": "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,prompt-caching-scope-2026-01-05,advanced-tool-use-2025-11-20,effort-2025-11-24",
		"x-app":          "cli",
		"User-Agent":     "claude-cli/2.1.123 (external, cli)",
		"anthropic-dangerous-direct-browser-access": "true",
		"Accept":                      "application/json",
		"X-Stainless-Lang":            "js",
		"X-Stainless-Package-Version": "0.81.0",
		"X-Stainless-OS":              "MacOS",
		"X-Stainless-Arch":            "arm64",
		"X-Stainless-Runtime":         "node",
		"X-Stainless-Runtime-Version": "v24.3.0",
		"X-Stainless-Retry-Count":     "0",
		"X-Stainless-Timeout":         "3000",
	}, "", 0}, // alias
	"zai":          {FormatAnthropic, "api_key", "/v1/messages", nil, "", 0},
	"openai":       {FormatOpenAI, "bearer", "/v1/chat/completions", nil, "", 0},
	"copilot":      {FormatOpenAI, "bearer", "/v1/chat/completions", nil, "", 0},
	"openrouter":   {FormatOpenAI, "bearer", "/v1/chat/completions", map[string]string{"HTTP-Referer": "https://github.com/klxhunter/agent-rate-limit"}, "", 0},
	"qwen":         {FormatOpenAI, "bearer", "/compatible-mode/v1/chat/completions", nil, "", 0},
	"gemini":       {FormatGemini, "api_key", "", nil, "", 0},
	"gemini-oauth": {FormatGemini, "bearer", "", nil, "", 0},
	"deepseek":     {FormatOpenAI, "bearer", "/v1/chat/completions", nil, "", 0},
	"kimi":         {FormatAnthropic, "api_key", "/v1/messages", nil, "", 0},
	"huggingface":  {FormatOpenAI, "bearer", "/v1/chat/completions", nil, "", 0},
	"ollama":       {FormatOpenAI, "bearer", "/v1/chat/completions", nil, "", 0},
	"agy":          {FormatAnthropic, "api_key", "/v1/messages", nil, "", 0},
	"cursor":       {FormatOpenAI, "bearer", "/v1/chat/completions", nil, "", 0},
	"codebuddy":    {FormatOpenAI, "bearer", "/v1/chat/completions", nil, "", 0},
	"kilo":         {FormatOpenAI, "bearer", "/v1/chat/completions", nil, "", 0},
	"lotuss":       {FormatOpenAI, "bearer", "/v1/chat/completions", nil, "default", 4096},
}

// RegisterProviderRoute adds a dynamic route entry for custom providers.
func RegisterProviderRoute(providerID string, format ProviderFormat, models []string) {
	var route providerRoute
	modelOverride := ""
	if len(models) > 0 {
		modelOverride = models[0]
	}
	switch format {
	case FormatOpenAI:
		route = providerRoute{FormatOpenAI, "bearer", "/v1/chat/completions", nil, modelOverride, 0}
	default:
		route = providerRoute{FormatAnthropic, "api_key", "/v1/messages", nil, modelOverride, 0}
	}
	providerRouteTable[providerID] = route
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
	{"lotuss-", []string{"lotuss"}},
}

// ModelBelongsToProvider checks if a model name routes to the given provider
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

// ResolveProviderByModel returns the first provider ID that matches the model prefix,
// or "glm" as fallback.
func ResolveProviderByModel(model string) string {
	for _, rule := range modelRules {
		if strings.HasPrefix(model, rule.prefix) {
			return rule.providers[0]
		}
	}
	return "glm"
}

// ResolveFallback returns the next routing decision for a model, skipping providers in excluded
func (r *Resolver) ResolveFallback(model string, exclude []string) *RoutingDecision {
	excludeMap := make(map[string]bool, len(exclude))
	for _, p := range exclude {
		excludeMap[p] = true
	}
	for _, rule := range modelRules {
		if strings.HasPrefix(model, rule.prefix) {
			for _, pid := range rule.providers {
				if excludeMap[pid] {
					continue
				}
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
	return nil
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
			// Model matched a rule but no provider had a token
			// In GLM mode: fall back to Z.AI for any model (key from pool or client token)
			if r.glmMode {
				return r.buildDecision("zai", model, "", "")
			}
			return nil
		}
	}

	// No rule matched: Z.AI fallback in GLM mode for unknown models
	if r.glmMode {
		decision := r.tryResolve("zai", model)
		if decision != nil {
			return decision
		}
		return r.buildDecision("zai", model, "", "")
	}
	return nil
}

// ResolveByProvider creates a routing decision for a specific provider ID,
// looking up its token and route config
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
	return r.buildDecision(providerID, "", apiKey, ""), true
}

func (r *Resolver) tryResolve(providerID, model string) *RoutingDecision {
	if r.IsCoolingDown(providerID, model) {
		return nil
	}
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
	// Skip expired tokens so fallback to next provider can trigger
	if !token.ExpiryDate.IsZero() && token.ExpiryDate.Before(time.Now()) {
		return nil
	}
	return r.buildDecision(providerID, model, token.AccessToken, token.AccountID)
}

// tryResolveRoundRobin cycles through all active tokens for a provider
// Prefers accounts with low 5h utilization; falls back to all if all are high
func (r *Resolver) tryResolveRoundRobin(providerID, model string) *RoutingDecision {
	if r.IsCoolingDown(providerID, model) {
		return nil
	}
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
			// Skip expired tokens so fallback to next provider can trigger
			if !t.ExpiryDate.IsZero() && t.ExpiryDate.Before(time.Now()) {
				continue
			}
			if r.IsAccountCoolingDown(providerID, t.AccountID) {
				continue
			}
			active = append(active, t)
		}
	}
	if len(active) == 0 {
		return nil
	}

	// If multiple accounts, try to pick one with lowest utilization
	if len(active) > 1 {
		ids := make([]string, len(active))
		for i, t := range active {
			ids[i] = t.AccountID
		}
		rls := r.tokenStore.GetRateLimits(providerID, ids)

		// Partition into low-util (<0.8) and high-util
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
	return r.buildDecision(providerID, model, active[idx].AccessToken, active[idx].AccountID)
}

// ResolveTransparent returns a claude-oauth routing decision for transparent passthrough,
// bypassing token expiry checks. The client provides its own valid Bearer token.
func (r *Resolver) ResolveTransparent(model string) *RoutingDecision {
	return r.buildDecision("claude-oauth", model, "", "")
}

func (r *Resolver) buildDecision(providerID, model, apiKey, accountID string) *RoutingDecision {
	cfg, ok := r.registry.Get(providerID)
	if !ok {
		return nil
	}

	route, ok := providerRouteTable[providerID]
	if !ok {
		route = providerRoute{FormatAnthropic, "api_key", "/v1/messages", nil, "", 0}
	}

	upstreamURL := cfg.UpstreamBase + route.urlSuffix

	// Gemini API key: endpoint includes model name and key as query param
	if providerID == "gemini" && apiKey != "" {
		upstreamURL = fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?key=%s", cfg.UpstreamBase, model, apiKey)
	}

	return &RoutingDecision{
		ProviderID:       providerID,
		ProviderCfg:      cfg,
		Format:           route.format,
		UpstreamURL:      upstreamURL,
		AuthMode:         route.authMode,
		ExtraHeaders:     route.extraHeaders,
		APIKey:           apiKey,
		AccountID:        accountID,
		ModelOverride:    route.modelOverride,
		MaxTokens:        route.maxTokens,
		MaxContinuations: providerContinuations[providerID],
		ToolMode:         providerToolMode[providerID],
	}
}

// GetProviderUpstream returns the upstream URL for a provider, or empty string if not found.
func (r *Resolver) GetProviderUpstream(providerID string) (string, bool) {
	cfg, ok := r.registry.Get(providerID)
	if !ok {
		return "", false
	}
	return cfg.UpstreamBase, true
}

// UpdateProviderUpstream updates the upstream URL for a provider in memory.
func (r *Resolver) UpdateProviderUpstream(providerID, upstream string) bool {
	return r.registry.UpdateUpstream(providerID, upstream)
}

// ProviderExists checks if a provider ID exists in the registry.
func (r *Resolver) ProviderExists(providerID string) bool {
	_, ok := r.registry.Get(providerID)
	return ok
}
