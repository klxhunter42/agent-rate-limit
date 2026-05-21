package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/klxhunter/agent-rate-limit/api-gateway/toolfilter"
)

const toolCacheTTL = 1 * time.Hour

type cachedTools struct {
	tools    []any
	cachedAt time.Time
}

// ToolCache stores tool definitions per identity key (profile or API key hash).
type ToolCache struct {
	mu           sync.RWMutex
	store        map[string]*cachedTools
	defaultTools []any // loaded from DEFAULT_TOOLS_FILE
}

func NewToolCache() *ToolCache {
	tc := &ToolCache{store: make(map[string]*cachedTools)}
	if path := os.Getenv("DEFAULT_TOOLS_FILE"); path != "" {
		if err := tc.loadDefaultTools(path); err != nil {
			slog.Error("default_tools_load_failed", "path", path, "error", err)
		}
	}
	return tc
}

func (tc *ToolCache) loadDefaultTools(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var tools []any
	if err := json.Unmarshal(data, &tools); err != nil {
		return err
	}
	tc.defaultTools = tools
	slog.Info("default_tools_loaded", "path", path, "tool_count", len(tools))
	return nil
}

// DefaultTools returns the loaded default tools catalog (for testing).
func (tc *ToolCache) DefaultTools() []any {
	return tc.defaultTools
}

// Store saves tools for the given cache key.
func (tc *ToolCache) Store(key string, tools []any) {
	if key == "" || len(tools) == 0 {
		return
	}
	copied := make([]any, len(tools))
	copy(copied, tools)
	tc.mu.Lock()
	tc.store[key] = &cachedTools{tools: copied, cachedAt: time.Now()}
	tc.mu.Unlock()
	slog.Info("tool_cache_store", "key", key, "tool_count", len(tools))
}

// Lookup returns cached tools for the key, or nil if none/expired.
func (tc *ToolCache) Lookup(key string) []any {
	if key == "" {
		return nil
	}
	tc.mu.RLock()
	entry, ok := tc.store[key]
	tc.mu.RUnlock()
	if !ok || entry == nil {
		return nil
	}
	if time.Since(entry.cachedAt) > toolCacheTTL {
		tc.mu.Lock()
		if e, ok := tc.store[key]; ok && e == entry {
			delete(tc.store, key)
		}
		tc.mu.Unlock()
		slog.Info("tool_cache_expired", "key", key)
		return nil
	}
	result := make([]any, len(entry.tools))
	copy(result, entry.tools)
	return result
}

// EvictExpired removes all entries older than toolCacheTTL.
func (tc *ToolCache) EvictExpired() {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	now := time.Now()
	for k, v := range tc.store {
		if now.Sub(v.cachedAt) > toolCacheTTL {
			delete(tc.store, k)
		}
	}
}

// toolCacheKey returns a cache key: profile > oauth hash > apikey hash.
func toolCacheKey(profileName string, r *http.Request) string {
	if profileName != "" {
		return "profile:" + profileName
	}
	if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
		tok := strings.TrimPrefix(ah, "Bearer ")
		if strings.HasPrefix(tok, "sk-ant-oat") {
			h := sha256.Sum256([]byte(tok))
			return "oauth:" + hex.EncodeToString(h[:])
		}
		h := sha256.Sum256([]byte(tok))
		return "bearer:" + hex.EncodeToString(h[:])
	}
	if ak := r.Header.Get("x-api-key"); ak != "" {
		if strings.HasPrefix(ak, "sk-ant-oat") {
			h := sha256.Sum256([]byte(ak))
			return "oauth:" + hex.EncodeToString(h[:])
		}
		h := sha256.Sum256([]byte(ak))
		return "apikey:" + hex.EncodeToString(h[:])
	}
	return ""
}

// extractRecentText pulls the last ~500 chars of message content for intent scoring.
func extractRecentText(messages []any) string {
	recentText := ""
	for i := len(messages) - 1; i >= 0 && len(recentText) < 500; i-- {
		mm, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		switch c := mm["content"].(type) {
		case string:
			recentText = c + " " + recentText
		case []any:
			for _, part := range c {
				if pm, ok := part.(map[string]any); ok {
					if t, ok := pm["text"].(string); ok {
						recentText = t + " " + recentText
					}
				}
			}
		}
	}
	return recentText
}

// injectCachedTools caches tools from requests that have them,
// and injects cached tools into requests that lack them.
// Returns true if tools were injected (caller must re-marshal body).
func (h *Handler) injectCachedTools(payload map[string]any, profileName string, r *http.Request) (injected bool) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("tool_cache_panic", "error", rec)
			injected = false
		}
	}()

	cacheKey := toolCacheKey(profileName, r)
	if cacheKey == "" {
		return false
	}

	toolsRaw := payload["tools"]
	if toolSlice, ok := toolsRaw.([]any); ok && len(toolSlice) > 0 {
		h.toolCache.Store(cacheKey, toolSlice)
		return false
	}

	// No tools in request: try to inject from cache, then default catalog.
	recentText := ""
	if msgs, ok := payload["messages"].([]any); ok {
		recentText = extractRecentText(msgs)
	}

	source := h.toolCache.Lookup(cacheKey)
	if len(source) == 0 {
		// Cache miss: check context to decide if tools are needed.
		if len(h.toolCache.defaultTools) == 0 {
			return false
		}
		intent := toolfilter.ClassifyIntent(recentText)
		if intent == "analysis" {
			return false
		}
		source = h.toolCache.defaultTools
		slog.Info("tool_cache_default", "key", cacheKey, "intent", intent, "tool_count", len(source))
	}

	// Use toolfilter to select relevant subset based on context.
	toInject := source
	if h.optimizers != nil && h.optimizers.ToolFilter != nil {
		parsedTools := make([]toolfilter.Tool, 0, len(source))
		for _, t := range source {
			tm, ok := t.(map[string]any)
			if !ok {
				continue
			}
			name, _ := tm["name"].(string)
			desc, _ := tm["description"].(string)
			parsedTools = append(parsedTools, toolfilter.Tool{Name: name, Description: desc})
		}

		filtered := h.optimizers.ToolFilter.FilterTools(parsedTools, recentText)
		if len(filtered) < len(parsedTools) {
			filteredMap := make(map[string]bool, len(filtered))
			for _, ft := range filtered {
				filteredMap[ft.Name] = true
			}
			newTools := make([]any, 0, len(filtered))
			for _, t := range source {
				if tm, ok := t.(map[string]any); ok {
					if name, ok := tm["name"].(string); ok {
						if filteredMap[name] {
							newTools = append(newTools, t)
						}
					}
				}
			}
			toInject = newTools
		}
	}

	payload["tools"] = toInject
	slog.Info("tool_cache_inject", "key", cacheKey, "injected_count", len(toInject))
	return true
}
