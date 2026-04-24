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
	"github.com/redis/go-redis/v9"
)

const profileTokenPrefix = "profile_token:"

const profilePrefix = "profile:"

const profileTokensPrefix = "profile_tokens:"

// Profile represents a configuration for connecting to an AI provider.
type Profile struct {
	Name        string   `json:"name"`
	BaseURL     string   `json:"baseUrl"`
	APIKey      string   `json:"apiKey"`
	Model       string   `json:"model"`
	OpusModel   string   `json:"opusModel,omitempty"`
	SonnetModel string   `json:"sonnetModel,omitempty"`
	HaikuModel  string   `json:"haikuModel,omitempty"`
	Target      string   `json:"target"`
	Provider    string   `json:"provider,omitempty"`
	AccountIDs  []string `json:"accountIds"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
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
		r.Route("/v1/profiles", func(r chi.Router) {
			r.Get("/", h.List)
			r.Post("/", h.Create)
			r.Post("/import", h.Import)
			r.Get("/recommended-models", h.RecommendedModels)
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

// RecommendedModels returns available models for a target provider.
func (h *ProfileHandler) RecommendedModels(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	if target == "" {
		all := make(map[string][]map[string]string)
		for k, v := range providerModels {
			all[k] = v
		}
		writeJSON(w, http.StatusOK, map[string]any{"models": all})
		return
	}
	models, ok := providerModels[target]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no models for provider: " + target})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"target": target, "models": models})
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

	if err := setProfile(ctx, h.redis, &p); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save profile"})
		return
	}

	writeJSON(w, http.StatusCreated, p)
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

	// Fetch existing to preserve CreatedAt.
	existing, err := getProfile(ctx, h.redis, name)
	if err == nil {
		p.CreatedAt = existing.CreatedAt
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

// ListTokens returns all tokens for a profile.
func (h *ProfileHandler) ListTokens(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	data, err := h.redis.HGetAll(ctx, profileTokensPrefix+name).Result()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list tokens"})
		return
	}

	tokens := make([]ProfileToken, 0, len(data))
	for _, val := range data {
		var pt ProfileToken
		if json.Unmarshal([]byte(val), &pt) == nil {
			tokens = append(tokens, pt)
		}
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
