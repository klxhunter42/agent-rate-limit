package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/klxhunter/agent-rate-limit/api-gateway/proxy"
	"github.com/redis/go-redis/v9"
)

const (
	configOverridesKey  = "config:overrides"
	thinkingConfigKey   = "config:thinking"
	globalEnvKey        = "config:global-env"
	redactedPlaceholder = "[redacted]"
)

// ConfigResponse is the redacted view of the gateway config.
type ConfigResponse struct {
	UpstreamURL           string                       `json:"upstreamUrl"`
	ModelLimits           map[string]int               `json:"modelLimits"`
	DefaultLimit          int                          `json:"defaultLimit"`
	GlobalLimit           int                          `json:"globalLimit"`
	RoutingStrategy       string                       `json:"routingStrategy"`
	EnablePromptInjection bool                         `json:"enablePromptInjection"`
	EnableSmartMaxTokens  bool                         `json:"enableSmartMaxTokens"`
	EnableResponseTrim    bool                         `json:"enableResponseTrim"`
	StreamTimeout         string                       `json:"streamTimeout"`
	UpstreamRPMLimit      int                          `json:"upstreamRpmLimit"`
	UpstreamMaxRetries    int                          `json:"upstreamMaxRetries"`
	ProbeMultiplier       int                          `json:"probeMultiplier"`
	PrivacyEnabled        bool                         `json:"privacyEnabled"`
	ModelPricing          map[string]config.ModelPrice `json:"modelPricing"`
	NumAPIKeys            int                          `json:"numApiKeys"`
}

// ThinkingConfig holds thinking budget settings.
type ThinkingConfig struct {
	DefaultBudget int            `json:"defaultBudget"`
	ModelBudgets  map[string]int `json:"modelBudgets"`
	Enabled       bool           `json:"enabled"`
}

// GlobalEnv holds global environment variable overrides.
type GlobalEnv struct {
	Enabled bool              `json:"enabled"`
	Env     map[string]string `json:"env"`
}

// ConfigHandler provides config/settings API endpoints.
type ConfigHandler struct {
	cfg   *config.Config
	redis *redis.Client
}

// NewConfigHandler creates a ConfigHandler with the given config and Redis connection.
func NewConfigHandler(cfg *config.Config, redisAddr string) *ConfigHandler {
	opt, err := redis.ParseURL(fmt.Sprintf("redis://%s", redisAddr))
	if err != nil {
		opt = &redis.Options{Addr: redisAddr}
	}
	rdb := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return &ConfigHandler{cfg: cfg, redis: nil}
	}
	return &ConfigHandler{cfg: cfg, redis: rdb}
}

// Routes returns chi routes for config endpoints.
func (ch *ConfigHandler) Routes(r chi.Router) {
	r.Get("/v1/config", ch.GetConfig)
	r.Get("/v1/config/raw", ch.GetConfigRaw)
	r.Put("/v1/config", ch.UpdateConfig)
	r.Get("/v1/thinking", ch.GetThinking)
	r.Put("/v1/thinking", ch.UpdateThinking)
	r.Get("/v1/global-env", ch.GetGlobalEnv)
	r.Put("/v1/global-env", ch.UpdateGlobalEnv)
}

// GetConfig returns the current config with secrets redacted.
func (ch *ConfigHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	resp := ch.buildConfigResponse()
	writeJSON(w, http.StatusOK, resp)
}

// GetConfigRaw returns the current config as plain text with secrets redacted.
func (ch *ConfigHandler) GetConfigRaw(w http.ResponseWriter, r *http.Request) {
	resp := ch.buildConfigResponse()
	pretty, _ := json.MarshalIndent(resp, "", "  ")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(pretty)
	w.Write([]byte("\n"))
}

// UpdateConfig merges incoming fields into the stored overrides.
// Fields sent as [redacted] preserve the current value.
func (ch *ConfigHandler) UpdateConfig(w http.ResponseWriter, r *http.Request) {
	if ch.redis == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "redis not available"})
		return
	}

	var incoming map[string]any
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	// Load existing overrides
	existing, err := ch.loadOverrides()
	if err != nil {
		existing = make(map[string]any)
	}

	// Merge: walk incoming, keep originals for [redacted] values
	for k, v := range incoming {
		if sv, ok := v.(string); ok && sv == redactedPlaceholder {
			// Keep existing value
			continue
		}
		existing[k] = v
	}

	if err := ch.saveOverrides(existing); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetThinking returns the current thinking budget configuration.
func (ch *ConfigHandler) GetThinking(w http.ResponseWriter, r *http.Request) {
	tc := ch.loadThinkingConfig()
	writeJSON(w, http.StatusOK, tc)
}

// UpdateThinking stores thinking budget configuration.
func (ch *ConfigHandler) UpdateThinking(w http.ResponseWriter, r *http.Request) {
	if ch.redis == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "redis not available"})
		return
	}

	var tc ThinkingConfig
	if err := json.NewDecoder(r.Body).Decode(&tc); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	data, _ := json.Marshal(tc)
	if err := ch.redis.Set(r.Context(), thinkingConfigKey, data, 0).Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, tc)
}

// GetGlobalEnv returns global environment variables with secrets redacted.
func (ch *ConfigHandler) GetGlobalEnv(w http.ResponseWriter, r *http.Request) {
	ge := ch.loadGlobalEnv()
	if ge.Env != nil {
		redacted := make(map[string]string, len(ge.Env))
		for k, v := range ge.Env {
			if isSensitiveKey(k) {
				redacted[k] = redactedPlaceholder
			} else {
				redacted[k] = v
			}
		}
		ge.Env = redacted
	}
	writeJSON(w, http.StatusOK, ge)
}

// UpdateGlobalEnv stores global environment variable overrides.
// Values sent as [redacted] preserve the current value.
func (ch *ConfigHandler) UpdateGlobalEnv(w http.ResponseWriter, r *http.Request) {
	if ch.redis == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "redis not available"})
		return
	}

	var incoming GlobalEnv
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	existing := ch.loadGlobalEnv()

	// Merge enabled flag
	existing.Enabled = incoming.Enabled

	// Merge env vars, keeping originals for [redacted] values
	if existing.Env == nil {
		existing.Env = make(map[string]string)
	}
	for k, v := range incoming.Env {
		if v == redactedPlaceholder {
			continue
		}
		existing.Env[k] = v
	}

	data, _ := json.Marshal(existing)
	if err := ch.redis.Set(r.Context(), globalEnvKey, data, 0).Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- internal helpers ---

func (ch *ConfigHandler) buildConfigResponse() ConfigResponse {
	return ConfigResponse{
		UpstreamURL:           ch.cfg.UpstreamURL,
		ModelLimits:           copyIntMap(ch.cfg.ModelLimits),
		DefaultLimit:          ch.cfg.DefaultLimit,
		GlobalLimit:           ch.cfg.GlobalLimit,
		RoutingStrategy:       proxy.GetStrategy(),
		EnablePromptInjection: ch.cfg.EnablePromptInjection,
		EnableSmartMaxTokens:  ch.cfg.EnableSmartMaxTokens,
		EnableResponseTrim:    ch.cfg.EnableResponseTrim,
		StreamTimeout:         ch.cfg.StreamTimeout.String(),
		UpstreamRPMLimit:      ch.cfg.UpstreamRPMLimit,
		UpstreamMaxRetries:    ch.cfg.UpstreamMaxRetries,
		ProbeMultiplier:       ch.cfg.ProbeMultiplier,
		PrivacyEnabled:        os.Getenv("PRIVACY_ENABLED") == "true",
		ModelPricing:          copyPricingMap(ch.cfg.ModelPricing),
		NumAPIKeys:            len(ch.cfg.UpstreamAPIKeys),
	}
}

func (ch *ConfigHandler) loadOverrides() (map[string]any, error) {
	if ch.redis == nil {
		return make(map[string]any), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	data, err := ch.redis.Get(ctx, configOverridesKey).Bytes()
	if err != nil {
		return make(map[string]any), nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return make(map[string]any), nil
	}
	return m, nil
}

func (ch *ConfigHandler) saveOverrides(m map[string]any) error {
	if ch.redis == nil {
		return fmt.Errorf("redis not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	data, _ := json.Marshal(m)
	return ch.redis.Set(ctx, configOverridesKey, data, 0).Err()
}

func (ch *ConfigHandler) loadThinkingConfig() ThinkingConfig {
	if ch.redis == nil {
		return ThinkingConfig{Enabled: false, DefaultBudget: 10000}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	data, err := ch.redis.Get(ctx, thinkingConfigKey).Bytes()
	if err != nil {
		return ThinkingConfig{Enabled: false, DefaultBudget: 10000}
	}
	var tc ThinkingConfig
	if err := json.Unmarshal(data, &tc); err != nil {
		return ThinkingConfig{Enabled: false, DefaultBudget: 10000}
	}
	if tc.ModelBudgets == nil {
		tc.ModelBudgets = make(map[string]int)
	}
	return tc
}

func (ch *ConfigHandler) loadGlobalEnv() GlobalEnv {
	if ch.redis == nil {
		return GlobalEnv{Env: make(map[string]string)}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	data, err := ch.redis.Get(ctx, globalEnvKey).Bytes()
	if err != nil {
		return GlobalEnv{Env: make(map[string]string)}
	}
	var ge GlobalEnv
	if err := json.Unmarshal(data, &ge); err != nil {
		return GlobalEnv{Env: make(map[string]string)}
	}
	if ge.Env == nil {
		ge.Env = make(map[string]string)
	}
	return ge
}

func isSensitiveKey(k string) bool {
	lower := strings.ToLower(k)
	return strings.Contains(lower, "key") ||
		strings.Contains(lower, "secret") ||
		strings.Contains(lower, "token") ||
		strings.Contains(lower, "password")
}

func copyIntMap(m map[string]int) map[string]int {
	if m == nil {
		return nil
	}
	c := make(map[string]int, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

func copyPricingMap(m map[string]config.ModelPrice) map[string]config.ModelPrice {
	if m == nil {
		return nil
	}
	c := make(map[string]config.ModelPrice, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}
