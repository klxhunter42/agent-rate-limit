package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestUsageHandler(t *testing.T) *UsageHandler {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	uh := NewUsageHandler(addr)
	return uh
}

func TestRecordProfileAccountUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: requires Redis")
	}
	uh := newTestUsageHandler(t)
	defer uh.Close()

	profile := "test-profile"
	accountID := "test-acc-001"
	model := "glm-5.1"

	// Record usage twice to verify accumulation
	uh.RecordProfileAccountUsage(profile, accountID, model, 1000, 500, 0.05)
	uh.RecordProfileAccountUsage(profile, accountID, model, 2000, 800, 0.12)

	// Also record for a second account
	uh.RecordProfileAccountUsage(profile, "test-acc-002", "glm-4.6", 500, 200, 0.02)

	// Set up router and request
	r := chi.NewRouter()
	r.Get("/v1/usage/profiles/{name}/keys", uh.ProfileKeysUsage)

	req := httptest.NewRequest(http.MethodGet, "/v1/usage/profiles/test-profile/keys", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, "test-profile", resp["profile"])

	keys, ok := resp["keys"].([]any)
	require.True(t, ok, "keys should be an array")
	assert.Len(t, keys, 2, "should have 2 accounts")

	// Find acc-001 entry
	var acc1 map[string]any
	for _, k := range keys {
		entry := k.(map[string]any)
		if entry["accountId"] == "test-acc-001" {
			acc1 = entry
		}
	}
	require.NotNil(t, acc1, "account test-acc-001 should exist")
	assert.Equal(t, float64(2), acc1["total_requests"], "should have 2 requests")
	assert.Equal(t, float64(3000), acc1["total_tokens_in"], "input should accumulate to 3000")
	assert.Equal(t, float64(1300), acc1["total_tokens_out"], "output should accumulate to 1300")

	t.Logf("response: %s", w.Body.String())
}

func TestRecordProfileAccountUsageIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: requires Redis")
	}
	uh := newTestUsageHandler(t)
	defer uh.Close()

	// Profile A uses account X
	uh.RecordProfileAccountUsage("profile-a", "acc-x", "glm-5.1", 100, 50, 0.01)
	// Profile B uses same account X
	uh.RecordProfileAccountUsage("profile-b", "acc-x", "glm-5.1", 200, 100, 0.02)

	// Profile A should only see its own usage for acc-x
	r := chi.NewRouter()
	r.Get("/v1/usage/profiles/{name}/keys", uh.ProfileKeysUsage)

	req := httptest.NewRequest(http.MethodGet, "/v1/usage/profiles/profile-a/keys", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	keys := resp["keys"].([]any)
	require.Len(t, keys, 1)
	acc := keys[0].(map[string]any)
	assert.Equal(t, "acc-x", acc["accountId"])
	assert.Equal(t, float64(100), acc["total_tokens_in"], "profile-a should only see its own 100 input tokens, not profile-b's 200")

	t.Logf("response: %s", w.Body.String())
}
