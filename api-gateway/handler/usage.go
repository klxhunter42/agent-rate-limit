package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

// UsageRecord holds aggregated usage for a single model in a time bucket.
type UsageRecord struct {
	Model        string  `json:"model"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	Cost         float64 `json:"cost"`
	Requests     int64   `json:"requests"`
	Errors       int64   `json:"errors"`
	Period       string  `json:"period"`
}

// UsageSummary holds total usage across all models.
type UsageSummary struct {
	TotalRequests  int64   `json:"total_requests"`
	TotalErrors    int64   `json:"total_errors"`
	TotalTokensIn  int64   `json:"total_tokens_in"`
	TotalTokensOut int64   `json:"total_tokens_out"`
	TotalCost      float64 `json:"total_cost"`
	Models         int     `json:"models"`
	Period         string  `json:"period"`
}

// UsageHandler provides usage analytics endpoints backed by Redis.
type UsageHandler struct {
	rdb *redis.Client
}

// NewUsageHandler connects to Redis and returns a ready handler.
func NewUsageHandler(redisAddr string) *UsageHandler {
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	return &UsageHandler{rdb: rdb}
}

// Close releases the Redis connection.
func (u *UsageHandler) Close() error {
	return u.rdb.Close()
}

// Routes returns a chi router function that registers all usage endpoints.
func (u *UsageHandler) Routes() func(r chi.Router) {
	return func(r chi.Router) {
		r.Route("/v1/usage", func(r chi.Router) {
			r.Get("/summary", u.Summary)
			r.Get("/hourly", u.Hourly)
			r.Get("/daily", u.Daily)
			r.Get("/models", u.Models)
			r.Get("/sessions", u.Sessions)
			r.Get("/monthly", u.Monthly)
		})
	}
}

// RecordUsage increments usage counters for the given model across all time buckets.
// Call this from the metrics middleware after each proxied request.
func (u *UsageHandler) RecordUsage(model string, inputTokens, outputTokens int, cost float64) {
	ctx := context.Background()
	now := time.Now().UTC()

	hourlyKey := "usage:hourly:" + now.Format("2006-01-02T15")
	dailyKey := "usage:daily:" + now.Format("2006-01-02")
	monthlyKey := "usage:monthly:" + now.Format("2006-01")

	pipe := u.rdb.Pipeline()

	field := model
	for _, key := range []string{hourlyKey, dailyKey, monthlyKey} {
		pipe.HIncrByFloat(ctx, key, field+":cost", cost)
		pipe.HIncrBy(ctx, key, field+":input", int64(inputTokens))
		pipe.HIncrBy(ctx, key, field+":output", int64(outputTokens))
		pipe.HIncrBy(ctx, key, field+":requests", 1)
	}

	// TTL: hourly 48h, daily 35d, monthly 400d.
	pipe.Expire(ctx, hourlyKey, 48*time.Hour)
	pipe.Expire(ctx, dailyKey, 35*24*time.Hour)
	pipe.Expire(ctx, monthlyKey, 400*24*time.Hour)

	// Track unique models in a set per bucket for enumeration.
	for _, key := range []string{hourlyKey, dailyKey, monthlyKey} {
		pipe.SAdd(ctx, key+":models", model)
	}

	// Session tracking: keyed by calendar day.
	sessionKey := "usage:sessions:" + now.Format("2006-01-02")
	pipe.HIncrBy(ctx, sessionKey, model+":requests", 1)
	pipe.HIncrBy(ctx, sessionKey, model+":input", int64(inputTokens))
	pipe.HIncrBy(ctx, sessionKey, model+":output", int64(outputTokens))
	pipe.HIncrByFloat(ctx, sessionKey, model+":cost", cost)
	pipe.Expire(ctx, sessionKey, 35*24*time.Hour)

	pipe.Exec(ctx)
}

// RecordError increments the error counter for a model in the current time bucket.
func (u *UsageHandler) RecordError(model string) {
	ctx := context.Background()
	now := time.Now().UTC()

	hourlyKey := "usage:hourly:" + now.Format("2006-01-02T15")
	dailyKey := "usage:daily:" + now.Format("2006-01-02")
	monthlyKey := "usage:monthly:" + now.Format("2006-01")

	pipe := u.rdb.Pipeline()
	for _, key := range []string{hourlyKey, dailyKey, monthlyKey} {
		pipe.HIncrBy(ctx, key, model+":errors", 1)
	}
	pipe.Exec(ctx)
}

// scanKeys replaces KEYS with SCAN for production-safe key enumeration.
func scanKeys(ctx context.Context, rdb *redis.Client, pattern string) ([]string, error) {
	var keys []string
	iter := rdb.Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	return keys, iter.Err()
}

// Summary returns aggregated totals for a given period.
// Query params: period=24h|7d|30d|all (default 24h).
func (u *UsageHandler) Summary(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "24h"
	}

	bucketPrefix, _ := periodToBucketPrefix(period)
	if bucketPrefix == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid period, use 24h/7d/30d/all"})
		return
	}

	ctx := r.Context()
	keys, err := scanKeys(ctx, u.rdb, bucketPrefix)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "redis error"})
		return
	}

	// Filter out :models suffix keys.
	var dataKeys []string
	for _, k := range keys {
		if !strings.HasSuffix(k, ":models") {
			dataKeys = append(dataKeys, k)
		}
	}

	summary := UsageSummary{Period: period}
	seenModels := map[string]bool{}

	for _, key := range dataKeys {
		vals, err := u.rdb.HGetAll(ctx, key).Result()
		if err != nil {
			continue
		}
		for field, val := range vals {
			parts := strings.SplitN(field, ":", 2)
			if len(parts) != 2 {
				continue
			}
			model, metric := parts[0], parts[1]
			seenModels[model] = true
			switch metric {
			case "requests":
				summary.TotalRequests += atoi64(val)
			case "errors":
				summary.TotalErrors += atoi64(val)
			case "input":
				summary.TotalTokensIn += atoi64(val)
			case "output":
				summary.TotalTokensOut += atoi64(val)
			case "cost":
				summary.TotalCost += atof64(val)
			}
		}
	}
	summary.Models = len(seenModels)
	writeJSON(w, http.StatusOK, summary)
}

// Hourly returns hourly usage for the last 24-48 hours.
// Query param: hours=24 (default) or 48.
func (u *UsageHandler) Hourly(w http.ResponseWriter, r *http.Request) {
	hours := 24
	if h := r.URL.Query().Get("hours"); h != "" {
		if v, err := strconv.Atoi(h); err == nil && (v == 48 || v == 24) {
			hours = v
		}
	}

	now := time.Now().UTC()
	var periods []string
	for i := 0; i < hours; i++ {
		periods = append(periods, now.Add(-time.Duration(i)*time.Hour).Format("2006-01-02T15"))
	}

	records := u.collectRecords(r.Context(), "usage:hourly:", periods)
	writeJSON(w, http.StatusOK, records)
}

// Daily returns daily usage for the last 30 days.
func (u *UsageHandler) Daily(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	var periods []string
	for i := 0; i < 30; i++ {
		periods = append(periods, now.AddDate(0, 0, -i).Format("2006-01-02"))
	}

	records := u.collectRecords(r.Context(), "usage:daily:", periods)
	writeJSON(w, http.StatusOK, records)
}

// Monthly returns monthly usage for the last 12 months.
func (u *UsageHandler) Monthly(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	var periods []string
	for i := 0; i < 12; i++ {
		periods = append(periods, now.AddDate(0, -i, 0).Format("2006-01"))
	}

	records := u.collectRecords(r.Context(), "usage:monthly:", periods)
	writeJSON(w, http.StatusOK, records)
}

// Models returns a per-model breakdown for a given period.
// Query param: period=24h|7d|30d (default 24h).
func (u *UsageHandler) Models(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "24h"
	}

	bucketPrefix, _ := periodToBucketPrefix(period)
	if bucketPrefix == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid period"})
		return
	}

	ctx := r.Context()
	keys, err := scanKeys(ctx, u.rdb, bucketPrefix)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "redis error"})
		return
	}

	// Filter out :models suffix keys.
	var dataKeys []string
	for _, k := range keys {
		if !strings.HasSuffix(k, ":models") {
			dataKeys = append(dataKeys, k)
		}
	}

	// Aggregate per model across all matched buckets.
	type modelAgg struct {
		Model        string  `json:"model"`
		InputTokens  int64   `json:"input_tokens"`
		OutputTokens int64   `json:"output_tokens"`
		Cost         float64 `json:"cost"`
		Requests     int64   `json:"requests"`
		Errors       int64   `json:"errors"`
	}

	agg := map[string]*modelAgg{}

	for _, key := range dataKeys {
		vals, err := u.rdb.HGetAll(ctx, key).Result()
		if err != nil {
			continue
		}
		for field, val := range vals {
			parts := strings.SplitN(field, ":", 2)
			if len(parts) != 2 {
				continue
			}
			model, metric := parts[0], parts[1]
			if agg[model] == nil {
				agg[model] = &modelAgg{Model: model}
			}
			switch metric {
			case "input":
				agg[model].InputTokens += atoi64(val)
			case "output":
				agg[model].OutputTokens += atoi64(val)
			case "cost":
				agg[model].Cost += atof64(val)
			case "requests":
				agg[model].Requests += atoi64(val)
			case "errors":
				agg[model].Errors += atoi64(val)
			}
		}
	}

	result := make([]*modelAgg, 0, len(agg))
	for _, a := range agg {
		result = append(result, a)
	}
	writeJSON(w, http.StatusOK, map[string]any{"period": period, "models": result})
}

// Sessions returns session-level (daily) usage for the last 7 days.
func (u *UsageHandler) Sessions(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	days := 7
	if d := r.URL.Query().Get("days"); d != "" {
		if v, err := strconv.Atoi(d); err == nil && v > 0 && v <= 30 {
			days = v
		}
	}

	ctx := r.Context()
	var periods []string
	for i := 0; i < days; i++ {
		periods = append(periods, now.AddDate(0, 0, -i).Format("2006-01-02"))
	}

	records := u.collectRecords(ctx, "usage:sessions:", periods)
	writeJSON(w, http.StatusOK, records)
}

// collectRecords fetches and groups usage records from Redis hash maps.
func (u *UsageHandler) collectRecords(ctx context.Context, keyPrefix string, periods []string) []UsageRecord {
	pipe := u.rdb.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(periods))
	for i, p := range periods {
		cmds[i] = pipe.HGetAll(ctx, keyPrefix+p)
	}
	pipe.Exec(ctx)

	type bucket struct {
		model  string
		input  int64
		output int64
		cost   float64
		reqs   int64
		errors int64
	}

	var results []UsageRecord

	for i, cmd := range cmds {
		vals, err := cmd.Result()
		if err != nil || len(vals) == 0 {
			continue
		}

		models := map[string]*bucket{}
		for field, val := range vals {
			parts := strings.SplitN(field, ":", 2)
			if len(parts) != 2 {
				continue
			}
			model, metric := parts[0], parts[1]
			if models[model] == nil {
				models[model] = &bucket{model: model}
			}
			switch metric {
			case "input":
				models[model].input = atoi64(val)
			case "output":
				models[model].output = atoi64(val)
			case "cost":
				models[model].cost = atof64(val)
			case "requests":
				models[model].reqs = atoi64(val)
			case "errors":
				models[model].errors = atoi64(val)
			}
		}

		for _, b := range models {
			results = append(results, UsageRecord{
				Model:        b.model,
				InputTokens:  b.input,
				OutputTokens: b.output,
				Cost:         b.cost,
				Requests:     b.reqs,
				Errors:       b.errors,
				Period:       periods[i],
			})
		}
	}

	return results
}

// periodToBucketPrefix maps a period label to a Redis key glob pattern.
func periodToBucketPrefix(period string) (string, int) {
	switch period {
	case "24h":
		return "usage:hourly:*", 48
	case "7d":
		return "usage:daily:*", 35
	case "30d":
		return "usage:daily:*", 35
	case "all":
		return "usage:*", 0
	default:
		return "", 0
	}
}

func atoi64(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

func atof64(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
