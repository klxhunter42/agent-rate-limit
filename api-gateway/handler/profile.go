package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/klxhunter/agent-rate-limit/api-gateway/provider"
	"github.com/redis/go-redis/v9"
)

const profileTokenPrefix = "profile_token:"

const profilePrefix = "profile:"

const profileTokensPrefix = "profile_tokens:"

// Profile represents a configuration for connecting to an AI provider.
// ProfileTarget represents a single target in a multi-target profile.
type ProfileTarget struct {
	ID              string   `json:"id,omitempty"`
	Target          string   `json:"target"`
	BaseURL         string   `json:"baseUrl,omitempty"`
	APIKey          string   `json:"apiKey,omitempty"`
	AccountIDs      []string `json:"accountIds,omitempty"`
	AccountEmails   []string `json:"accountEmails,omitempty"` // emails for each account ID
	PassthroughAuth bool     `json:"passthroughAuth,omitempty"`
}

type Profile struct {
	Name               string          `json:"name"`
	BaseURL            string          `json:"baseUrl"`
	APIKey             string          `json:"apiKey"`
	Model              string          `json:"model"`
	OpusModel          string          `json:"opusModel,omitempty"`
	SonnetModel        string          `json:"sonnetModel,omitempty"`
	HaikuModel         string          `json:"haikuModel,omitempty"`
	Target             string          `json:"target"`
	Provider           string          `json:"provider,omitempty"`
	AccountIDs         []string        `json:"accountIds"`
	AccountEmails      []string        `json:"accountEmails,omitempty"` // emails for each account ID
	Targets            []ProfileTarget `json:"targets,omitempty"`
	PassthroughAuth    bool            `json:"passthroughAuth,omitempty"`
	OptimizerOverrides map[string]bool `json:"optimizerOverrides,omitempty"`
	MaxThinkingTokens  int             `json:"maxThinkingTokens,omitempty"`
	CreatedAt          string          `json:"createdAt"`
	UpdatedAt          string          `json:"updatedAt"`
}

// ProfileToken represents a named API token tied to a profile.
type ProfileToken struct {
	KeyName   string `json:"keyName"`
	Token     string `json:"token"`
	Profile   string `json:"profile"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	CreatedAt string `json:"createdAt"`
}

// ProfileHandler manages profile CRUD against Dragonfly/Redis.
type ProfileHandler struct {
	redis *redis.Client
}

// NewProfileHandler connects to Redis at redisAddr and returns a ready handler.
func NewProfileHandler(redisAddr string) *ProfileHandler {
	opt, err := redis.ParseURL(fmt.Sprintf("redis://%s", redisAddr))
	if err != nil {
		slog.Error("failed to parse profile redis url", "addr", redisAddr, "error", err)
		return nil
	}
	opt.DialTimeout = 3 * time.Second
	opt.ReadTimeout = 3 * time.Second
	opt.WriteTimeout = 3 * time.Second

	rdb := redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("profile handler redis ping failed", "addr", redisAddr, "error", err)
		return nil
	}

	return &ProfileHandler{redis: rdb}
}

// Redis returns the underlying Redis client for profile lookups.
func (h *ProfileHandler) Redis() *redis.Client {
	if h == nil {
		return nil
	}
	return h.redis
}

// Close releases the Redis connection.
func (h *ProfileHandler) Close() error {
	if h.redis != nil {
		return h.redis.Close()
	}
	return nil
}

// Routes registers all profile endpoints on a chi router.
func (h *ProfileHandler) Routes() func(r chi.Router) {
	return func(r chi.Router) {
		r.Post("/v1/profiles/delete", h.DeleteByName)
		r.Route("/v1/profiles", func(r chi.Router) {
			r.Get("/", h.List)
			r.Post("/", h.Create)
			r.Post("/import", h.Import)
			r.Get("/recommended-models", h.RecommendedModels)
			r.Get("/optimizer-settings", h.OptimizerSettings)
			r.Route("/{name}", func(r chi.Router) {
				r.Get("/", h.Get)
				r.Put("/", h.Update)
				r.Delete("/", h.Delete)
				r.Post("/copy", h.Copy)
				r.Post("/export", h.Export)
				r.Get("/export", h.Export)
				r.Get("/tokens", h.ListTokens)
				r.Post("/tokens", h.GenerateToken)
				r.Delete("/tokens/{keyName}", h.RevokeToken)
			})
		})
	}
}

// --- provider recommended models ---

// providerModels maps provider ID to its available models with tier info.
var providerModels = map[string][]map[string]string{
	"claude-oauth": {
		{"name": "claude-opus-4-7", "tier": "flagship", "description": "Most capable, complex tasks"},
		{"name": "claude-sonnet-4-20250514", "tier": "standard", "description": "Balanced performance and cost"},
		{"name": "claude-haiku-4-5-20251001", "tier": "light", "description": "Fast and affordable"},
	},
	"gemini-oauth": {
		{"name": "gemini-2.5-pro", "tier": "flagship", "description": "Most capable, thinking model"},
		{"name": "gemini-2.5-flash", "tier": "fast", "description": "Fast and versatile"},
		{"name": "gemini-2.0-flash", "tier": "light", "description": "Fastest, most affordable"},
	},
	"anthropic": {
		{"name": "claude-opus-4-7", "tier": "flagship", "description": "Most capable, complex tasks"},
		{"name": "claude-sonnet-4-20250514", "tier": "standard", "description": "Balanced performance and cost"},
	},
	"openai": {
		{"name": "o3", "tier": "flagship", "description": "Most capable reasoning"},
		{"name": "gpt-4o", "tier": "standard", "description": "Versatile flagship"},
		{"name": "gpt-4o-mini", "tier": "light", "description": "Fast and affordable"},
	},
	"gemini": {
		{"name": "gemini-2.5-pro", "tier": "flagship", "description": "Most capable"},
		{"name": "gemini-2.5-flash", "tier": "fast", "description": "Fast and versatile"},
	},
	"zai": {
		{"name": "glm-5.1", "tier": "flagship", "description": "Most capable"},
		{"name": "glm-4.5", "tier": "standard", "description": "Balanced performance"},
		{"name": "glm-4.5-air", "tier": "fast", "description": "Fast and affordable"},
	},
	"deepseek": {
		{"name": "deepseek-r1", "tier": "flagship", "description": "Reasoning model"},
		{"name": "deepseek-chat", "tier": "standard", "description": "General purpose"},
	},
	"copilot": {
		{"name": "claude-sonnet-4-6", "tier": "flagship", "description": "Via GitHub Copilot"},
		{"name": "gpt-4o", "tier": "standard", "description": "Via GitHub Copilot"},
	},
	"openrouter": {
		{"name": "or-anthropic/claude-sonnet-4-6", "tier": "flagship", "description": "Claude via OpenRouter"},
		{"name": "or-openai/gpt-4o", "tier": "standard", "description": "GPT-4o via OpenRouter"},
	},
	"qwen": {
		{"name": "qwen-max", "tier": "flagship", "description": "Most capable"},
		{"name": "qwen-plus", "tier": "standard", "description": "Balanced"},
		{"name": "qwen-turbo", "tier": "light", "description": "Fast"},
	},
}

// providerOptimizerDefaults defines which optimizers should be enabled by default for each provider.
// true = enabled by default, false = disabled by default, missing = use global default.
//
// Optimizer stages:
// - semantic_dedup: F7 semantic dedup (tokenizer-based, always available)
// - chunker: F1 chunker (reorder system prompt)
// - delta: F8 delta encoding (metrics only)
// - sketch: F9 sketch dedup
// - summarizer: F6 summarizer (red budget only)
// - textcomp: F17 text compression (regex-based)
// - caveman: F16 caveman compression (LLM-based)
// - pordee: F17 Thai terse injection
// - desctrim: tool description trimming
// - toolcomp: tool result compression
// - toolfilter: tool result filtering
// - disclosure: PII disclosure detection
// - packer: F2 chunker-based packing
// - prefetcher: F4 prefetcher
// - bandit: F5 bandit MAB
// - waste: F11 waste detection
// - cache: F14 cache manager
// - warmstart: warm start
// - compcache: compiler cache
var providerOptimizerDefaults = map[string]map[string]bool{
	"claude-oauth": {
		"semantic_dedup": true,
		"chunker":        true,
		"delta":          true,
		"sketch":         true,
		"summarizer":     true,
		"textcomp":       true,
		"caveman":        false, // LLM-based, may cause issues with OAuth
		"pordee":         true,
		"desctrim":       true,
		"toolcomp":       true,
		"toolfilter":     true,
		"disclosure":     true,
		"packer":         true,
		"prefetcher":     true,
		"bandit":         true,
		"waste":          true,
		"cache":          true,
		"warmstart":      true,
		"compcache":      true,
	},
	"gemini-oauth": {
		"semantic_dedup": true,
		"chunker":        true,
		"delta":          true,
		"sketch":         true,
		"summarizer":     true,
		"textcomp":       true,
		"caveman":        false, // LLM-based, may cause issues with OAuth
		"pordee":         true,
		"desctrim":       true,
		"toolcomp":       true,
		"toolfilter":     true,
		"disclosure":     true,
		"packer":         true,
		"prefetcher":     true,
		"bandit":         true,
		"waste":          true,
		"cache":          true,
		"warmstart":      true,
		"compcache":      true,
	},
	"zai": {
		// All disabled for Z.AI - handled via isZAIProvider check in handler
	},
	"anthropic": {
		// Full optimization for Anthropic API key
		"semantic_dedup": true,
		"chunker":        true,
		"delta":          true,
		"sketch":         true,
		"summarizer":     true,
		"textcomp":       true,
		"caveman":        true,
		"pordee":         true,
		"desctrim":       true,
		"toolcomp":       true,
		"toolfilter":     true,
		"disclosure":     true,
		"packer":         true,
		"prefetcher":     true,
		"bandit":         true,
		"waste":          true,
		"cache":          true,
		"warmstart":      true,
		"compcache":      true,
	},
	"openai": {
		"semantic_dedup": true,
		"chunker":        true,
		"delta":          true,
		"sketch":         true,
		"summarizer":     true,
		"textcomp":       true,
		"caveman":        false, // May cause issues with OpenAI
		"pordee":         true,
		"desctrim":       true,
		"toolcomp":       true,
		"toolfilter":     true,
		"disclosure":     true,
		"packer":         true,
		"prefetcher":     true,
		"bandit":         true,
		"waste":          true,
		"cache":          true,
		"warmstart":      true,
		"compcache":      true,
	},
}

// GetProviderOptimizerDefaults returns the default optimizer overrides for a provider.
// Returns nil if the provider has no specific defaults (use global defaults).
func GetProviderOptimizerDefaults(provider string) map[string]bool {
	if defaults, ok := providerOptimizerDefaults[provider]; ok {
		// Return a copy to avoid modifying the original
		copy := make(map[string]bool, len(defaults))
		for k, v := range defaults {
			copy[k] = v
		}
		return copy
	}
	return nil
}

// MergeOptimizerOverrides merges profile overrides with provider defaults.
// Profile overrides take precedence over provider defaults.
func MergeOptimizerOverrides(provider string, profileOverrides map[string]bool) map[string]bool {
	providerDefaults := GetProviderOptimizerDefaults(provider)
	if providerDefaults == nil && profileOverrides == nil {
		return nil
	}
	if providerDefaults == nil {
		return profileOverrides
	}
	if profileOverrides == nil {
		return providerDefaults
	}
	// Merge: profile overrides take precedence
	merged := make(map[string]bool, len(providerDefaults))
	for k, v := range providerDefaults {
		merged[k] = v
	}
	for k, v := range profileOverrides {
		merged[k] = v
	}
	return merged
}

// RecommendedModels returns available models for a target provider.
func (h *ProfileHandler) RecommendedModels(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	customModels := h.loadCustomProviderModels()

	if target == "" {
		all := make(map[string][]map[string]string)
		for k, v := range providerModels {
			all[k] = v
		}
		for k, v := range customModels {
			all[k] = v
		}
		writeJSON(w, http.StatusOK, map[string]any{"models": all})
		return
	}
	if models, ok := providerModels[target]; ok {
		writeJSON(w, http.StatusOK, map[string]any{"target": target, "models": models})
		return
	}
	if models, ok := customModels[target]; ok {
		writeJSON(w, http.StatusOK, map[string]any{"target": target, "models": models})
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "no models for provider: " + target})
}

// OptimizerSettings returns optimizer configuration for a specific provider.
// Query param "target" specifies the provider ID. Returns all providers if empty.
// Response includes which optimizers are enabled/disabled by default for each provider.
func (h *ProfileHandler) OptimizerSettings(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")

	type optimizerInfo struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Category    string `json:"category"`
	}

	// All available optimizers with metadata
	allOptimizers := map[string]optimizerInfo{
		"semantic_dedup": {ID: "semantic_dedup", Name: "Semantic Dedup", Description: "Deduplicate semantically similar sentences", Category: "input"},
		"chunker":        {ID: "chunker", Name: "Chunker", Description: "Reorder system prompt for cache hit", Category: "input"},
		"delta":          {ID: "delta", Name: "Delta Encoding", Description: "Track potential savings via delta encoding", Category: "metrics"},
		"sketch":         {ID: "sketch", Name: "Sketch Dedup", Description: "Detect duplicate system prompts via sketch", Category: "input"},
		"summarizer":     {ID: "summarizer", Name: "Summarizer", Description: "Summarize long system prompts (red budget)", Category: "input"},
		"textcomp":       {ID: "textcomp", Name: "Text Compression", Description: "Remove filler and verbose text", Category: "input"},
		"caveman":        {ID: "caveman", Name: "Caveman Compression", Description: "LLM-based input/output compression", Category: "input_output"},
		"pordee":         {ID: "pordee", Name: "Pordee Thai", Description: "Inject Thai terse output rules", Category: "output"},
		"desctrim":       {ID: "desctrim", Name: "Description Trim", Description: "Trim verbose tool descriptions", Category: "input"},
		"toolcomp":       {ID: "toolcomp", Name: "Tool Compression", Description: "Compress tool result outputs", Category: "input"},
		"toolfilter":     {ID: "toolfilter", Name: "Tool Filter", Description: "Filter irrelevant tool results", Category: "input"},
		"disclosure":     {ID: "disclosure", Name: "Disclosure Detection", Description: "Detect PII disclosure attempts", Category: "security"},
		"packer":         {ID: "packer", Name: "Packer", Description: "Chunker-based message packing", Category: "input"},
		"prefetcher":     {ID: "prefetcher", Name: "Prefetcher", Description: "Prefetch common completions", Category: "performance"},
		"bandit":         {ID: "bandit", Name: "Bandit MAB", Description: "Multi-armed bandit for routing", Category: "performance"},
		"waste":          {ID: "waste", Name: "Waste Detection", Description: "Detect wasteful token usage", Category: "metrics"},
		"cache":          {ID: "cache", Name: "Cache Manager", Description: "Prompt cache eviction management", Category: "performance"},
		"warmstart":      {ID: "warmstart", Name: "Warm Start", Description: "Session warm-start from cache", Category: "performance"},
		"compcache":      {ID: "compcache", Name: "Compiler Cache", Description: "Cache compiler outputs", Category: "performance"},
	}

	if target != "" {
		// Return settings for specific provider
		defaults := GetProviderOptimizerDefaults(target)
		if defaults == nil {
			// No specific defaults = all enabled (use global defaults)
			defaults = make(map[string]bool)
			for id := range allOptimizers {
				defaults[id] = true
			}
		}
		result := make([]map[string]any, 0, len(defaults))
		for id, info := range allOptimizers {
			enabled, hasOverride := defaults[id]
			if !hasOverride {
				enabled = true // default to enabled if not specified
			}
			result = append(result, map[string]any{
				"id":          id,
				"name":        info.Name,
				"description": info.Description,
				"category":    info.Category,
				"enabled":     enabled,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"provider": target, "optimizers": result})
		return
	}

	// Return all providers with their optimizer settings
	result := make(map[string]any)
	for provider := range providerModels {
		defaults := GetProviderOptimizerDefaults(provider)
		optimizers := make([]map[string]any, 0)
		for id, info := range allOptimizers {
			enabled := true
			if defaults != nil {
				if e, ok := defaults[id]; ok {
					enabled = e
				}
			}
			optimizers = append(optimizers, map[string]any{
				"id":          id,
				"name":        info.Name,
				"description": info.Description,
				"category":    info.Category,
				"enabled":     enabled,
			})
		}
		result[provider] = map[string]any{
			"optimizers": optimizers,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": result})
}

// loadCustomProviderModels reads custom provider configs from Redis and returns
// a map of provider ID -> model entries compatible with the recommended-models response.
func (h *ProfileHandler) loadCustomProviderModels() map[string][]map[string]string {
	result := make(map[string][]map[string]string)
	if h.redis == nil {
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	keys, err := h.redis.Keys(ctx, "arl:providers:custom:*").Result()
	if err != nil {
		return result
	}
	for _, key := range keys {
		data, err := h.redis.Get(ctx, key).Bytes()
		if err != nil {
			continue
		}
		var cfg provider.ProviderConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			continue
		}
		if len(cfg.Models) == 0 {
			result[cfg.ID] = []map[string]string{
				{"name": "default", "tier": "standard", "description": cfg.Name},
			}
			continue
		}
		models := make([]map[string]string, 0, len(cfg.Models))
		for i, m := range cfg.Models {
			tier := "standard"
			if i == 0 {
				tier = "flagship"
			}
			models = append(models, map[string]string{
				"name":        m,
				"tier":        tier,
				"description": cfg.Name,
			})
		}
		result[cfg.ID] = models
	}
	return result
}

// --- handlers ---

// List returns all stored profiles.
func (h *ProfileHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	keys, err := scanKeys(ctx, h.redis, profilePrefix+"*")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list profiles"})
		return
	}

	profiles := make([]Profile, 0, len(keys))
	for _, key := range keys {
		val, err := h.redis.Get(ctx, key).Result()
		if err != nil {
			continue
		}
		var p Profile
		if err := json.Unmarshal([]byte(val), &p); err != nil {
			continue
		}
		enrichProfileWithEmails(ctx, h.redis, &p)
		profiles = append(profiles, p)
	}

	writeJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
}

// Create stores a new profile. Returns 409 if name already exists.
func (h *ProfileHandler) Create(w http.ResponseWriter, r *http.Request) {
	var p Profile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if msg := validateProfile(&p); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	key := profilePrefix + p.Name
	exists, err := h.redis.Exists(ctx, key).Result()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "redis error"})
		return
	}
	if exists > 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "profile already exists: " + p.Name})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	p.CreatedAt = now
	p.UpdatedAt = now

	// Apply provider-specific optimizer defaults if not set
	if p.OptimizerOverrides == nil && p.Provider != "" {
		p.OptimizerOverrides = GetProviderOptimizerDefaults(p.Provider)
	}

	if err := setProfile(ctx, h.redis, &p); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save profile"})
		return
	}

	writeJSON(w, http.StatusCreated, p)
}

// enrichProfileWithEmails fetches emails from token store for account IDs.
func enrichProfileWithEmails(ctx context.Context, rdb *redis.Client, p *Profile) {
	slog.Info("enrichProfileWithEmails called", "profile", p.Name, "provider", p.Provider, "account_ids_count", len(p.AccountIDs), "account_ids", p.AccountIDs)
	if len(p.AccountIDs) == 0 {
		slog.Debug("no account IDs to enrich", "profile", p.Name, "provider", p.Provider)
		return
	}
	slog.Info("enriching profile with emails", "profile", p.Name, "provider", p.Provider, "account_ids", p.AccountIDs)
	emails := make([]string, 0, len(p.AccountIDs))
	for _, accountID := range p.AccountIDs {
		tokenKey := "arl:tokens:" + p.Provider + ":" + accountID
		slog.Debug("fetching token", "key", tokenKey)
		data, err := rdb.Get(ctx, tokenKey).Result()
		if err != nil {
			slog.Warn("token lookup failed", "key", tokenKey, "error", err)
			emails = append(emails, "")
			continue
		}
		var tokenInfo struct {
			Email         string          `json:"email"`
			ClaudeProfile *map[string]any `json:"claude_profile,omitempty"`
			AccountEmail  string          `json:"account_email,omitempty"`
		}
		if err := json.Unmarshal([]byte(data), &tokenInfo); err == nil {
			email := ""
			if tokenInfo.Email != "" {
				email = tokenInfo.Email
			} else if tokenInfo.AccountEmail != "" {
				email = tokenInfo.AccountEmail
			} else if tokenInfo.ClaudeProfile != nil {
				if ae, ok := (*tokenInfo.ClaudeProfile)["account_email"].(string); ok && ae != "" {
					email = ae
				}
			}
			if email != "" {
				emails = append(emails, email)
				slog.Debug("found email", "account_id", accountID, "email", email)
			} else {
				emails = append(emails, "")
			}
		} else {
			emails = append(emails, "")
		}
	}
	p.AccountEmails = emails
	slog.Info("enriched profile emails", "profile", p.Name, "emails", emails)

	// Also enrich targets with emails
	for i := range p.Targets {
		t := &p.Targets[i]
		if len(t.AccountIDs) == 0 {
			continue
		}
		slog.Info("enriching target with emails", "target", t.Target, "account_ids", t.AccountIDs)
		emails := make([]string, 0, len(t.AccountIDs))
		for _, accountID := range t.AccountIDs {
			tokenKey := "arl:tokens:" + t.Target + ":" + accountID
			slog.Debug("fetching target token", "key", tokenKey)
			data, err := rdb.Get(ctx, tokenKey).Result()
			if err != nil {
				slog.Warn("target token lookup failed", "key", tokenKey, "error", err)
				emails = append(emails, "")
				continue
			}
			var tokenInfo struct {
				Email         string          `json:"email"`
				ClaudeProfile *map[string]any `json:"claude_profile,omitempty"`
				AccountEmail  string          `json:"account_email,omitempty"`
			}
			if err := json.Unmarshal([]byte(data), &tokenInfo); err == nil {
				email := ""
				if tokenInfo.Email != "" {
					email = tokenInfo.Email
				} else if tokenInfo.AccountEmail != "" {
					email = tokenInfo.AccountEmail
				} else if tokenInfo.ClaudeProfile != nil {
					if ae, ok := (*tokenInfo.ClaudeProfile)["account_email"].(string); ok && ae != "" {
						email = ae
					}
				}
				if email != "" {
					emails = append(emails, email)
					slog.Debug("found target email", "account_id", accountID, "email", email)
				} else {
					emails = append(emails, "")
				}
			} else {
				emails = append(emails, "")
			}
		}
		t.AccountEmails = emails
		slog.Info("enriched target emails", "target", t.Target, "emails", emails)
	}
}

// Get returns a single profile by name.
func (h *ProfileHandler) Get(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	p, err := getProfile(ctx, h.redis, name)
	if err == redis.Nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "profile not found: " + name})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get profile"})
		return
	}

	enrichProfileWithEmails(ctx, h.redis, p)

	writeJSON(w, http.StatusOK, p)
}

// Update replaces an existing profile. Returns 404 if not found.
func (h *ProfileHandler) Update(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	var p Profile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if msg := validateProfile(&p); msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	key := profilePrefix + name
	exists, err := h.redis.Exists(ctx, key).Result()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "redis error"})
		return
	}
	if exists == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "profile not found: " + name})
		return
	}

	// Preserve creation time, update name from URL param.
	p.Name = name
	p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	// Fetch existing to preserve CreatedAt and sensitive fields not sent by frontend.
	existing, err := getProfile(ctx, h.redis, name)
	if err == nil {
		p.CreatedAt = existing.CreatedAt
		if p.APIKey == "" {
			p.APIKey = existing.APIKey
		}
		for i := range p.Targets {
			if p.Targets[i].APIKey == "" && i < len(existing.Targets) {
				p.Targets[i].APIKey = existing.Targets[i].APIKey
			}
		}
		if p.AccountIDs == nil {
			p.AccountIDs = existing.AccountIDs
		}
		if p.AccountEmails == nil {
			p.AccountEmails = existing.AccountEmails
		}
		// Preserve OptimizerOverrides if not provided in update
		if p.OptimizerOverrides == nil && existing.OptimizerOverrides != nil {
			p.OptimizerOverrides = existing.OptimizerOverrides
		}
	}

	// Apply provider-specific optimizer defaults if still not set
	if p.OptimizerOverrides == nil && p.Provider != "" {
		p.OptimizerOverrides = GetProviderOptimizerDefaults(p.Provider)
	}

	if err := setProfile(ctx, h.redis, &p); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update profile"})
		return
	}

	writeJSON(w, http.StatusOK, p)
}

// Delete removes a profile and all its tokens. Returns 404 if not found.
func (h *ProfileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	// Revoke all tokens for this profile.
	tokens, _ := h.redis.HGetAll(ctx, profileTokensPrefix+name).Result()
	for _, val := range tokens {
		var pt ProfileToken
		if json.Unmarshal([]byte(val), &pt) == nil {
			h.redis.Del(ctx, profileTokenPrefix+pt.Token)
		}
	}
	h.redis.Del(ctx, profileTokensPrefix+name)

	deleted, err := h.redis.Del(ctx, profilePrefix+name).Result()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete profile"})
		return
	}
	if deleted == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "profile not found: " + name})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": name})
}

// DeleteByName removes a profile by name in JSON body. Works around URL path
// issues with special characters like brackets in profile names.
func (h *ProfileHandler) DeleteByName(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required in JSON body"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	tokens, _ := h.redis.HGetAll(ctx, profileTokensPrefix+req.Name).Result()
	for _, val := range tokens {
		var pt ProfileToken
		if json.Unmarshal([]byte(val), &pt) == nil {
			h.redis.Del(ctx, profileTokenPrefix+pt.Token)
		}
	}
	h.redis.Del(ctx, profileTokensPrefix+req.Name)

	deleted, err := h.redis.Del(ctx, profilePrefix+req.Name).Result()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete profile"})
		return
	}
	if deleted == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "profile not found: " + req.Name})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": req.Name})
}

// Copy duplicates a profile under a new name. Body: {"destination": "new-name"}.
func (h *ProfileHandler) Copy(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	var req struct {
		Destination string `json:"destination"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.Destination == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "destination is required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	src, err := getProfile(ctx, h.redis, name)
	if err == redis.Nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "source profile not found: " + name})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read source profile"})
		return
	}

	destKey := profilePrefix + req.Destination
	exists, err := h.redis.Exists(ctx, destKey).Result()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "redis error"})
		return
	}
	if exists > 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "destination profile already exists: " + req.Destination})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	src.Name = req.Destination
	src.APIKey = ""
	src.CreatedAt = now
	src.UpdatedAt = now

	if err := setProfile(ctx, h.redis, src); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to copy profile"})
		return
	}

	writeJSON(w, http.StatusCreated, src)
}

// Export returns a profile as a portable bundle.
// Body: {"includeSecrets": false} (default false).
func (h *ProfileHandler) Export(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	var req struct {
		IncludeSecrets bool `json:"includeSecrets"`
	}
	json.NewDecoder(r.Body).Decode(&req) // optional body, ignore errors

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	p, err := getProfile(ctx, h.redis, name)
	if err == redis.Nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "profile not found: " + name})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get profile"})
		return
	}

	if !req.IncludeSecrets {
		p.APIKey = "__CCS_REDACTED__"
	}

	writeJSON(w, http.StatusOK, map[string]any{"bundle": p})
}

// Import creates a profile from a previously exported bundle.
// Body: {"bundle": {...}, "name": "optional-override", "target": "optional-override"}.
func (h *ProfileHandler) Import(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Bundle Profile `json:"bundle"`
		Name   string  `json:"name,omitempty"`
		Target string  `json:"target,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	p := req.Bundle
	if req.Name != "" {
		p.Name = req.Name
	}
	if req.Target != "" {
		p.Target = req.Target
	}

	if p.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	warnings := make([]string, 0)
	if p.APIKey == "__CCS_REDACTED__" {
		p.APIKey = ""
		warnings = append(warnings, "apiKey was redacted in the export and has been cleared; set it manually")
	}

	if p.Target == "" {
		p.Target = "claude-oauth"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	key := profilePrefix + p.Name
	exists, err := h.redis.Exists(ctx, key).Result()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "redis error"})
		return
	}
	if exists > 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "profile already exists: " + p.Name})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	p.CreatedAt = now
	p.UpdatedAt = now

	if err := setProfile(ctx, h.redis, &p); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to import profile"})
		return
	}

	resp := map[string]any{"profile": p}
	if len(warnings) > 0 {
		resp["warnings"] = warnings
	}
	writeJSON(w, http.StatusCreated, resp)
}

// --- token handlers ---

// ListTokens returns all tokens for a profile. Expired tokens are auto-revoked.
func (h *ProfileHandler) ListTokens(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	tokensKey := profileTokensPrefix + name
	data, err := h.redis.HGetAll(ctx, tokensKey).Result()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list tokens"})
		return
	}

	now := time.Now().UTC()
	tokens := make([]ProfileToken, 0, len(data))
	for key, val := range data {
		var pt ProfileToken
		if json.Unmarshal([]byte(val), &pt) != nil {
			continue
		}
		if pt.ExpiresAt != "" {
			if exp, err := time.Parse(time.RFC3339, pt.ExpiresAt); err == nil && exp.Before(now) {
				h.redis.HDel(ctx, tokensKey, key)
				h.redis.Del(ctx, profileTokenPrefix+pt.Token)
				slog.Info("auto-revoked expired token", "profile", name, "keyName", pt.KeyName)
				continue
			}
		}
		tokens = append(tokens, pt)
	}

	writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

// GenerateToken creates a named API token tied to this profile.
// Body: {"keyName": "my-laptop", "expiresIn": 3600}
func (h *ProfileHandler) GenerateToken(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	var req struct {
		KeyName   string `json:"keyName"`
		ExpiresIn int    `json:"expiresIn"` // seconds, 0 = never
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.KeyName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "keyName is required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	// Check profile exists.
	if _, err := getProfile(ctx, h.redis, name); err == redis.Nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "profile not found: " + name})
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get profile"})
		return
	}

	// If keyName already exists, revoke old token first.
	tokensKey := profileTokensPrefix + name
	if existing, err := h.redis.HGet(ctx, tokensKey, req.KeyName).Result(); err == nil {
		var old ProfileToken
		if json.Unmarshal([]byte(existing), &old) == nil {
			h.redis.Del(ctx, profileTokenPrefix+old.Token)
		}
	}

	// Generate token: arl_<32 random hex chars>.
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate token"})
		return
	}
	token := "arl_" + hex.EncodeToString(b)

	// Determine TTL.
	var ttl time.Duration
	var expiresAt string
	if req.ExpiresIn > 0 {
		ttl = time.Duration(req.ExpiresIn) * time.Second
		expiresAt = time.Now().Add(ttl).UTC().Format(time.RFC3339)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	pt := ProfileToken{
		KeyName:   req.KeyName,
		Token:     token,
		Profile:   name,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}

	ptData, _ := json.Marshal(pt)

	// Store reverse mapping: token -> profile name (with optional TTL).
	if err := h.redis.Set(ctx, profileTokenPrefix+token, name, ttl).Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store token"})
		return
	}

	// Store token metadata in hash: profile_tokens:{name} -> {keyName: json}.
	if err := h.redis.HSet(ctx, tokensKey, req.KeyName, ptData).Err(); err != nil {
		h.redis.Del(ctx, profileTokenPrefix+token)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store token metadata"})
		return
	}

	slog.Info("profile token generated", "profile", name, "keyName", req.KeyName, "ttl", ttl)
	writeJSON(w, http.StatusOK, pt)
}

// RevokeToken removes a named token for a profile.
func (h *ProfileHandler) RevokeToken(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	keyName := chi.URLParam(r, "keyName")
	if name == "" || keyName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and keyName are required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	tokensKey := profileTokensPrefix + name
	existing, err := h.redis.HGet(ctx, tokensKey, keyName).Result()
	if err == redis.Nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "token not found: " + keyName})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get token"})
		return
	}

	var pt ProfileToken
	if json.Unmarshal([]byte(existing), &pt) == nil {
		h.redis.Del(ctx, profileTokenPrefix+pt.Token)
	}
	h.redis.HDel(ctx, tokensKey, keyName)

	slog.Info("profile token revoked", "profile", name, "keyName", keyName)
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked", "profile": name, "keyName": keyName})
}

// ResolveProfileToken looks up a token and returns the profile name.
func ResolveProfileToken(rdb *redis.Client, token string) (string, error) {
	if rdb == nil || !strings.HasPrefix(token, "arl_") {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	name, err := rdb.Get(ctx, profileTokenPrefix+token).Result()
	if err == redis.Nil {
		return "", nil
	}
	return name, err
}

// --- helpers ---

func validateProfile(p *Profile) string {
	if p.Name == "" {
		return "name is required"
	}
	if strings.ContainsAny(p.Name, "/%\\") {
		return "name must not contain /, %, or \\"
	}
	if p.Target == "" {
		p.Target = "claude-oauth"
	}
	return ""
}

func getProfile(ctx context.Context, rdb *redis.Client, name string) (*Profile, error) {
	val, err := rdb.Get(ctx, profilePrefix+name).Result()
	if err != nil {
		return nil, err
	}
	var p Profile
	if err := json.Unmarshal([]byte(val), &p); err != nil {
		return nil, fmt.Errorf("unmarshal profile: %w", err)
	}
	return &p, nil
}

func setProfile(ctx context.Context, rdb *redis.Client, p *Profile) error {
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}
	return rdb.Set(ctx, profilePrefix+p.Name, data, 0).Err()
}
