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

func redisAddr() string {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	return addr
}

func TestRecordProfileAccountUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: requires Redis")
	}
	uh := NewUsageHandler(redisAddr())
	defer uh.Close()

	profile := "test-profile-unit"
	accountID := "test-acc-001"
	model := "glm-5.1"

	// Clean up any leftover data from previous runs
	ctx := t.Context()
	for _, key := range []string{
		"usage:profile:" + profile + ":accounts",
		"usage:profile:" + profile + ":account:test-acc-001:summary",
		"usage:profile:" + profile + ":account:test-acc-002:summary",
	} {
		uh.rdb.Del(ctx, key)
	}

	// Record usage twice to verify accumulation
	uh.RecordProfileAccountUsage(profile, accountID, model, 1000, 500, 0.05)
	uh.RecordProfileAccountUsage(profile, accountID, model, 2000, 800, 0.12)

	// Also record for a second account
	uh.RecordProfileAccountUsage(profile, "test-acc-002", "glm-4.6", 500, 200, 0.02)

	// Verify data was written to Redis
	summaryKey := "usage:profile:" + profile + ":account:" + accountID + ":summary"
	vals, err := uh.rdb.HGetAll(ctx, summaryKey).Result()
	require.NoError(t, err)
	t.Logf("Redis summary key %s: %+v", summaryKey, vals)
	require.NotEmpty(t, vals, "summary key should have data")

	// Set up router and request
	r := chi.NewRouter()
	r.Get("/v1/usage/profiles/{name}/keys", uh.ProfileKeysUsage)

	req := httptest.NewRequest(http.MethodGet, "/v1/usage/profiles/"+profile+"/keys", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, profile, resp["profile"])

	keys, ok := resp["keys"].([]any)
	require.True(t, ok, "keys should be an array")
	assert.Len(t, keys, 2, "should have 2 accounts")

	// Find acc-001 entry
	var acc1 map[string]any
	for _, k := range keys {
		entry := k.(map[string]any)
		if entry["accountId"] == accountID {
			acc1 = entry
		}
	}
	require.NotNil(t, acc1, "account test-acc-001 should exist")
	assert.Equal(t, float64(2), acc1["total_requests"])
	assert.Equal(t, float64(3000), acc1["total_tokens_in"])
	assert.Equal(t, float64(1300), acc1["total_tokens_out"])

	t.Logf("response: %s", w.Body.String())
}

func TestRecordProfileAccountUsageIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: requires Redis")
	}
	uh := NewUsageHandler(redisAddr())
	defer uh.Close()

	// Clean up any leftover data from previous runs
	ctx := t.Context()
	for _, key := range []string{
		"usage:profile:profile-a-unit:accounts",
		"usage:profile:profile-a-unit:account:acc-x:summary",
		"usage:profile:profile-b-unit:accounts",
		"usage:profile:profile-b-unit:account:acc-x:summary",
	} {
		uh.rdb.Del(ctx, key)
	}

	// Profile A uses account X
	uh.RecordProfileAccountUsage("profile-a-unit", "acc-x", "glm-5.1", 100, 50, 0.01)
	// Profile B uses same account X
	uh.RecordProfileAccountUsage("profile-b-unit", "acc-x", "glm-5.1", 200, 100, 0.02)

	// Profile A should only see its own usage for acc-x
	r := chi.NewRouter()
	r.Get("/v1/usage/profiles/{name}/keys", uh.ProfileKeysUsage)

	req := httptest.NewRequest(http.MethodGet, "/v1/usage/profiles/profile-a-unit/keys", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	keys := resp["keys"].([]any)
	require.Len(t, keys, 1)
	acc := keys[0].(map[string]any)
	assert.Equal(t, "acc-x", acc["accountId"])
	assert.Equal(t, float64(100), acc["total_tokens_in"], "profile-a should only see its own 100 input tokens")

	t.Logf("response: %s", w.Body.String())
}
