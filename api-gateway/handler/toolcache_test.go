package handler

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/klxhunter/agent-rate-limit/api-gateway/toolfilter"
	"github.com/stretchr/testify/assert"
)

func TestToolCacheStoreAndLookup(t *testing.T) {
	tc := NewToolCache()
	tools := []any{
		map[string]any{"name": "Bash", "description": "Run a command"},
		map[string]any{"name": "Read", "description": "Read a file"},
	}
	tc.Store("profile:test", tools)

	result := tc.Lookup("profile:test")
	assert.NotNil(t, result)
	assert.Len(t, result, 2)
}

func TestToolCacheLookupMiss(t *testing.T) {
	tc := NewToolCache()
	assert.Nil(t, tc.Lookup("nonexistent"))
}

func TestToolCacheExpiredEntry(t *testing.T) {
	tc := NewToolCache()
	tools := []any{map[string]any{"name": "Bash"}}
	tc.Store("profile:old", tools)

	// Manually expire
	tc.mu.Lock()
	tc.store["profile:old"].cachedAt = time.Now().Add(-2 * time.Hour)
	tc.mu.Unlock()

	assert.Nil(t, tc.Lookup("profile:old"))
}

func TestToolCacheEvictExpired(t *testing.T) {
	tc := NewToolCache()
	tc.Store("profile:a", []any{map[string]any{"name": "A"}})
	tc.Store("profile:b", []any{map[string]any{"name": "B"}})

	tc.mu.Lock()
	tc.store["profile:a"].cachedAt = time.Now().Add(-2 * time.Hour)
	tc.mu.Unlock()

	tc.EvictExpired()
	assert.Nil(t, tc.Lookup("profile:a"))
	assert.NotNil(t, tc.Lookup("profile:b"))
}

func TestToolCacheEmptyKey(t *testing.T) {
	tc := NewToolCache()
	tc.Store("", []any{map[string]any{"name": "Bash"}})
	assert.Nil(t, tc.Lookup(""))
}

func TestToolCacheEmptyTools(t *testing.T) {
	tc := NewToolCache()
	tc.Store("profile:empty", []any{})
	assert.Nil(t, tc.Lookup("profile:empty"))
}

func TestToolCacheConcurrentAccess(t *testing.T) {
	tc := NewToolCache()
	tools := []any{map[string]any{"name": "Bash"}}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			tc.Store("profile:concurrent", tools)
		}()
		go func() {
			defer wg.Done()
			tc.Lookup("profile:concurrent")
		}()
	}
	wg.Wait()
	assert.NotNil(t, tc.Lookup("profile:concurrent"))
}

func TestToolCacheKeyProfile(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	assert.Equal(t, "profile:myprofile", toolCacheKey("myprofile", r))
}

func TestToolCacheKeyOAuth(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("Authorization", "Bearer sk-ant-oat01-test123")
	key := toolCacheKey("", r)
	assert.Equal(t, "oauth:", key[:6])
	assert.NotEmpty(t, key[6:])
}

func TestToolCacheKeyAPIKey(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("x-api-key", "sk-test-key")
	key := toolCacheKey("", r)
	assert.Equal(t, "apikey:", key[:7])
}

func TestToolCacheKeyEmpty(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	assert.Equal(t, "", toolCacheKey("", r))
}

func TestExtractRecentText(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "user", "content": "hello world"},
		map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "text", "text": "hi there"},
		}},
	}
	text := extractRecentText(msgs)
	assert.Contains(t, text, "hi there")
	assert.Contains(t, text, "hello world")
}

func TestToolCacheLoadDefaultTools(t *testing.T) {
	tools := []any{
		map[string]any{"name": "Bash", "description": "Run commands"},
		map[string]any{"name": "Read", "description": "Read files"},
	}
	data, _ := json.Marshal(tools)
	tmp := filepath.Join(t.TempDir(), "tools.json")
	os.WriteFile(tmp, data, 0644)

	tc := NewToolCache()
	err := tc.loadDefaultTools(tmp)
	assert.NoError(t, err)
	assert.Len(t, tc.DefaultTools(), 2)
}

func TestToolCacheLoadDefaultToolsInvalidPath(t *testing.T) {
	tc := NewToolCache()
	err := tc.loadDefaultTools("/nonexistent/tools.json")
	assert.Error(t, err)
}

func TestContextNeedsToolsThai(t *testing.T) {
	tests := []struct {
		input      string
		needsTools bool
	}{
		{"ช่วยสร้างโปรเจคให้หน่อย", true},
		{"เขียนโค้ดส่วนนี้ให้ได้ไหม", true},
		{"แก้บักนี้ให้หน่อย", true},
		{"ติดตั้ง dependencies ให้หนู", true},
		{"หาไฟล์ config อยู่ที่ไหน", true},
		{"รันเทสให้หน่อย", true},
		{"อธิบายโค้ดส่วนนี้ให้หน่อย", false},
		{"วิเคราะห์สถาปัตยกรรมของระบบนี้", false},
	}
	for _, tt := range tests {
		intent := classifyIntentExported(tt.input)
		needsTools := intent != "analysis"
		assert.Equal(t, tt.needsTools, needsTools, "input=%q intent=%s", tt.input, intent)
	}
}

// classifyIntentExported wraps toolfilter.ClassifyIntent for test convenience.
func classifyIntentExported(text string) string {
	return toolfilter.ClassifyIntent(text)
}
