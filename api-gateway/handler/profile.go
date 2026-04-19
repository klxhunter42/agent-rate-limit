package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

const profilePrefix = "profile:"

// Profile represents a configuration for connecting to an AI provider.
type Profile struct {
	Name        string `json:"name"`
	BaseURL     string `json:"baseUrl"`
	APIKey      string `json:"apiKey"`
	Model       string `json:"model"`
	OpusModel   string `json:"opusModel,omitempty"`
	SonnetModel string `json:"sonnetModel,omitempty"`
	HaikuModel  string `json:"haikuModel,omitempty"`
	Target      string `json:"target"`
	Provider    string `json:"provider,omitempty"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
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
			r.Route("/{name}", func(r chi.Router) {
				r.Get("/", h.Get)
				r.Put("/", h.Update)
				r.Delete("/", h.Delete)
				r.Post("/copy", h.Copy)
				r.Post("/export", h.Export)
			})
		})
	}
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

// Delete removes a profile. Returns 404 if not found.
func (h *ProfileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

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
		p.Target = "claude"
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

// --- helpers ---

func validateProfile(p *Profile) string {
	if p.Name == "" {
		return "name is required"
	}
	if p.APIKey == "" {
		return "apiKey is required"
	}
	if p.Target != "claude" && p.Target != "droid" && p.Target != "codex" {
		if p.Target == "" {
			p.Target = "claude"
		} else {
			return "target must be 'claude', 'droid', or 'codex'"
		}
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
