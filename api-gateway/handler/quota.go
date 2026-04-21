package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/klxhunter/agent-rate-limit/api-gateway/provider"
	"github.com/redis/go-redis/v9"
)


// ModelQuota represents quota usage for a single model.
type ModelQuota struct {
	Name        string  `json:"name"`
	DisplayName string  `json:"displayName"`
	Percentage  float64 `json:"percentage"`
	ResetTime   *string `json:"resetTime,omitempty"`
}

// QuotaResult holds quota data for a single account.
type QuotaResult struct {
	Success     bool         `json:"success"`
	Models      []ModelQuota `json:"models,omitempty"`
	LastUpdated string       `json:"lastUpdated"`
	Error       string       `json:"error,omitempty"`
	AccountID   string       `json:"accountId"`
	Provider    string       `json:"provider"`
}

// ProviderQuotaResult holds quota data across all accounts of a provider.
type ProviderQuotaResult struct {
	Provider    string        `json:"provider"`
	Accounts    []QuotaResult `json:"accounts"`
	LastUpdated string        `json:"lastUpdated"`
}

// QuotaHandler provides quota checking endpoints backed by Redis caching.
type QuotaHandler struct {
	redis      *redis.Client
	tokenStore *provider.TokenStore
	cfg        *config.Config
}

// NewQuotaHandler creates a new QuotaHandler connected to Redis.
func NewQuotaHandler(redisAddr string, ts *provider.TokenStore, cfg *config.Config) *QuotaHandler {
	opt, err := redis.ParseURL(redisAddr)
	if err != nil {
		opt = &redis.Options{Addr: redisAddr}
	}
	opt.PoolSize = cfg.QuotaRedisPoolSize
	opt.MinIdleConns = cfg.QuotaRedisMinIdle

	rdb := redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("quota handler: redis ping failed", "error", err)
	}

	return &QuotaHandler{redis: rdb, tokenStore: ts, cfg: cfg}
}

// Close releases the Redis connection.
func (q *QuotaHandler) Close() error {
	return q.redis.Close()
}

// Routes returns a chi router function that registers quota endpoints.
func (q *QuotaHandler) Routes() func(r chi.Router) {
	return func(r chi.Router) {
		r.Route("/v1/quota", func(r chi.Router) {
			r.Get("/{provider}/{accountId}", q.GetAccountQuota)
			r.Get("/{provider}", q.GetProviderQuota)
		})
	}
}

// GetAccountQuota returns quota usage for a specific provider account.
func (q *QuotaHandler) GetAccountQuota(w http.ResponseWriter, r *http.Request) {
	prov := chi.URLParam(r, "provider")
	accountID := chi.URLParam(r, "accountId")
	if prov == "" || accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider and accountId are required"})
		return
	}

	result, err := q.fetchQuota(r.Context(), prov, accountID)
	if err != nil {
		slog.Error("quota fetch failed", "provider", prov, "accountId", accountID, "error", err)
		writeJSON(w, http.StatusInternalServerError, QuotaResult{
			Success:     false,
			Error:       fmt.Sprintf("failed to fetch quota: %s", err),
			AccountID:   accountID,
			Provider:    prov,
			LastUpdated: time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// GetProviderQuota returns quota usage for all accounts of a provider.
func (q *QuotaHandler) GetProviderQuota(w http.ResponseWriter, r *http.Request) {
	prov := chi.URLParam(r, "provider")
	if prov == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider is required"})
		return
	}

	var accounts []QuotaResult

	if q.tokenStore != nil {
		tokens, err := q.tokenStore.ListByProvider(prov)
		if err != nil {
			slog.Error("failed to list provider accounts", "provider", prov, "error", err)
		}
		for _, t := range tokens {
			result, err := q.fetchQuota(r.Context(), prov, t.AccountID)
			if err != nil {
				accounts = append(accounts, QuotaResult{
					Success:     false,
					Error:       fmt.Sprintf("failed to fetch quota: %s", err),
					AccountID:   t.AccountID,
					Provider:    prov,
					LastUpdated: time.Now().UTC().Format(time.RFC3339),
				})
				continue
			}
			accounts = append(accounts, *result)
		}
	}

	if len(accounts) == 0 {
		now := time.Now().UTC().Format(time.RFC3339)
		models := q.computeUsageQuota(r.Context(), prov, now)
		accounts = append(accounts, QuotaResult{
			Success:     true,
			Models:      models,
			AccountID:   "default",
			Provider:    prov,
			LastUpdated: now,
		})
	}

	writeJSON(w, http.StatusOK, ProviderQuotaResult{
		Provider:    prov,
		Accounts:    accounts,
		LastUpdated: time.Now().UTC().Format(time.RFC3339),
	})
}

// fetchQuota retrieves quota data from Redis cache or falls through to the provider fetcher.
func (q *QuotaHandler) fetchQuota(ctx context.Context, prov, accountID string) (*QuotaResult, error) {
	cacheKey := fmt.Sprintf("quota:%s:%s", prov, accountID)

	cached, err := q.redis.Get(ctx, cacheKey).Result()
	if err == nil && cached != "" {
		var result QuotaResult
		if json.Unmarshal([]byte(cached), &result) == nil {
			return &result, nil
		}
	}

	result := q.fetchFromProvider(ctx, prov, accountID)

	data, err := json.Marshal(result)
	if err == nil {
		q.redis.Set(ctx, cacheKey, data, q.cfg.QuotaCacheTTL)
	}

	return result, nil
}

// fetchFromProvider calls the provider-specific quota fetcher.
func (q *QuotaHandler) fetchFromProvider(ctx context.Context, prov, accountID string) *QuotaResult {
	now := time.Now().UTC().Format(time.RFC3339)

	// Try to compute quota from real usage data in Redis.
	if models := q.computeUsageQuota(ctx, prov, now); len(models) > 0 {
		return &QuotaResult{
			Success:     true,
			Models:      models,
			AccountID:   accountID,
			Provider:    prov,
			LastUpdated: now,
		}
	}

	switch prov {
	case "claude-oauth", "anthropic":
		return &QuotaResult{
			Success:     true,
			Models:      []ModelQuota{},
			AccountID:   accountID,
			Provider:    "claude-oauth",
			LastUpdated: now,
		}
	case "gemini", "gemini-oauth":
		return &QuotaResult{
			Success:     true,
			Models:      []ModelQuota{},
			AccountID:   accountID,
			Provider:    "gemini",
			LastUpdated: now,
		}
	default:
		return &QuotaResult{
			Success:     true,
			Models:      []ModelQuota{},
			AccountID:   accountID,
			Provider:    prov,
			LastUpdated: now,
		}
	}
}

// computeUsageQuota reads today's usage from Redis and computes quota percentages.
func (q *QuotaHandler) computeUsageQuota(ctx context.Context, prov, _ string) []ModelQuota {
	today := time.Now().UTC().Format("2006-01-02")
	dailyKey := "usage:daily:" + today

	vals, err := q.redis.HGetAll(ctx, dailyKey).Result()
	if err != nil || len(vals) == 0 {
		return nil
	}

	type usage struct {
		requests int64
		input    int64
		output   int64
	}

	models := map[string]*usage{}
	for field, val := range vals {
		parts := strings.SplitN(field, ":", 2)
		if len(parts) != 2 {
			continue
		}
		model, metric := parts[0], parts[1]
		if strings.HasPrefix(metric, "error") || metric == "cost" {
			continue
		}
		if models[model] == nil {
			models[model] = &usage{}
		}
		switch metric {
		case "requests":
			models[model].requests = atoi64(val)
		case "input":
			models[model].input = atoi64(val)
		case "output":
			models[model].output = atoi64(val)
		}
	}

	if len(models) == 0 {
		return nil
	}

	// Daily budget from config.
	dailyBudget := q.cfg.QuotaDailyBudget

	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Truncate(24 * time.Hour)
	resetTime := tomorrow.Format(time.RFC3339)

	providerModelPrefix := config.ParseProviderModelPrefixes(q.cfg.ProviderModelPrefixes)

	modelBelongsTo := func(model, provider string) bool {
		prefixes, ok := providerModelPrefix[provider]
		if !ok {
			return false
		}
		for _, pfx := range prefixes {
			if strings.HasPrefix(model, pfx) {
				return true
			}
		}
		return false
	}

	var result []ModelQuota
	for model, u := range models {
		if !modelBelongsTo(model, prov) {
			continue
		}
		pct := float64(u.requests) / float64(dailyBudget) * 100
		if pct > 100 {
			pct = 100
		}
		pct = math.Round(pct*100) / 100

		result = append(result, ModelQuota{
			Name:        model,
			DisplayName: model,
			Percentage:  pct,
			ResetTime:   &resetTime,
		})
	}

	return result
}

// CheckQuota verifies if a model is within quota limits for the given provider/account.
// Returns (allowed, maxPercentage, error). Fails open on errors.
func (q *QuotaHandler) CheckQuota(provider, accountID, model string) (allowed bool, pct float64, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, ferr := q.fetchQuota(ctx, provider, accountID)
	if ferr != nil {
		return true, 0, ferr
	}

	for _, mq := range result.Models {
		if mq.Name == model && mq.Percentage >= q.cfg.QuotaBlockPct {
			return false, mq.Percentage, nil
		}
	}
	return true, 0, nil
}
