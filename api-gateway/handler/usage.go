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
			r.Get("/profiles", u.ProfileUsage)
			r.Get("/profiles/{name}", u.ProfileUsageByName)
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

// RecordProfileUsage increments usage counters for a profile+model.
func (u *UsageHandler) RecordProfileUsage(profile, model string, inputTokens, outputTokens int, cost float64) {
	ctx := context.Background()
	now := time.Now().UTC()

	dailyKey := "usage:profile:" + profile + ":daily:" + now.Format("2006-01-02")
	summaryKey := "usage:profile:" + profile + ":summary"

	pipe := u.rdb.Pipeline()

	field := model
	for _, key := range []string{dailyKey, summaryKey} {
		pipe.HIncrByFloat(ctx, key, field+":cost", cost)
		pipe.HIncrBy(ctx, key, field+":input", int64(inputTokens))
		pipe.HIncrBy(ctx, key, field+":output", int64(outputTokens))
		pipe.HIncrBy(ctx, key, field+":requests", 1)
		pipe.SAdd(ctx, key+":models", model)
	}

	pipe.Expire(ctx, dailyKey, 35*24*time.Hour)
	pipe.Expire(ctx, summaryKey, 0) // no expiry for summary

	pipe.Exec(ctx)
}

// ProfileUsage returns per-profile aggregated usage across all profiles.
func (u *UsageHandler) ProfileUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	keys, err := scanKeys(ctx, u.rdb, "usage:profile:*:summary")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "redis error"})
		return
	}

	type modelEntry struct {
		Model    string  `json:"model"`
		Requests int64   `json:"requests"`
		Input    int64   `json:"input_tokens"`
		Output   int64   `json:"output_tokens"`
		Cost     float64 `json:"cost"`
	}

	type profileEntry struct {
		Name      string       `json:"name"`
		TotalReqs int64        `json:"total_requests"`
		TotalIn   int64        `json:"total_tokens_in"`
		TotalOut  int64        `json:"total_tokens_out"`
		TotalCost float64      `json:"total_cost"`
		Models    []modelEntry `json:"models"`
	}

	result := make([]profileEntry, 0)

	for _, key := range keys {
		if strings.HasSuffix(key, ":models") {
			continue
		}
		// Extract profile name: usage:profile:{name}:summary
		parts := strings.SplitN(key, ":", 5)
		if len(parts) < 4 {
			continue
		}
		profileName := parts[2]

		vals, err := u.rdb.HGetAll(ctx, key).Result()
		if err != nil || len(vals) == 0 {
			continue
		}

		entry := profileEntry{Name: profileName}
		models := map[string]*modelEntry{}

		for field, val := range vals {
			fp := strings.SplitN(field, ":", 2)
			if len(fp) != 2 {
				continue
			}
			m, metric := fp[0], fp[1]
			if models[m] == nil {
				models[m] = &modelEntry{Model: m}
			}
			switch metric {
			case "requests":
				models[m].Requests = atoi64(val)
				entry.TotalReqs += atoi64(val)
			case "input":
				models[m].Input = atoi64(val)
				entry.TotalIn += atoi64(val)
			case "output":
				models[m].Output = atoi64(val)
				entry.TotalOut += atoi64(val)
			case "cost":
				models[m].Cost = atof64(val)
				entry.TotalCost += atof64(val)
			}
		}

		for _, me := range models {
			entry.Models = append(entry.Models, *me)
		}
		result = append(result, entry)
	}

	writeJSON(w, http.StatusOK, map[string]any{"profiles": result})
}

// ProfileUsageByName returns usage for a single profile.
func (u *UsageHandler) ProfileUsageByName(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	ctx := r.Context()
	key := "usage:profile:" + name + ":summary"
	vals, err := u.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "redis error"})
		return
	}

	type modelEntry struct {
		Model    string  `json:"model"`
		Requests int64   `json:"requests"`
		Input    int64   `json:"input_tokens"`
		Output   int64   `json:"output_tokens"`
		Cost     float64 `json:"cost"`
	}

	totalReqs := int64(0)
	totalIn := int64(0)
	totalOut := int64(0)
	totalCost := float64(0)
	models := map[string]*modelEntry{}

	for field, val := range vals {
		fp := strings.SplitN(field, ":", 2)
		if len(fp) != 2 {
			continue
		}
		m, metric := fp[0], fp[1]
		if models[m] == nil {
			models[m] = &modelEntry{Model: m}
		}
		switch metric {
		case "requests":
			models[m].Requests = atoi64(val)
			totalReqs += atoi64(val)
		case "input":
			models[m].Input = atoi64(val)
			totalIn += atoi64(val)
		case "output":
			models[m].Output = atoi64(val)
			totalOut += atoi64(val)
		case "cost":
			models[m].Cost = atof64(val)
			totalCost += atof64(val)
		}
	}

	ml := make([]modelEntry, 0)
	for _, me := range models {
		ml = append(ml, *me)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"name":             name,
		"total_requests":   totalReqs,
		"total_tokens_in":  totalIn,
		"total_tokens_out": totalOut,
		"total_cost":       totalCost,
		"models":           ml,
	})
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
// Query params: period=5m|15m|1h|6h|24h|7d|30d|all (default 24h).
func (u *UsageHandler) Summary(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "24h"
	}

	keys, ok := u.keysForPeriod(period)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid period, use 5m/15m/1h/6h/24h/7d/30d/all"})
		return
	}

	ctx := r.Context()
	summary := UsageSummary{Period: period}
	seenModels := map[string]bool{}

	for _, key := range keys {
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
// Query param: period=5m|15m|1h|6h|24h|7d|30d (default 24h).
func (u *UsageHandler) Models(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "24h"
	}

	keys, ok := u.keysForPeriod(period)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid period"})
		return
	}

	ctx := r.Context()

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

	for _, key := range keys {
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

// keysForPeriod returns Redis keys for a given period label.
func (u *UsageHandler) keysForPeriod(period string) ([]string, bool) {
	now := time.Now().UTC()
	switch period {
	case "5m", "15m", "1h":
		return []string{"usage:hourly:" + now.Format("2006-01-02T15")}, true
	case "6h":
		keys := make([]string, 6)
		for i := range keys {
			keys[i] = "usage:hourly:" + now.Add(-time.Duration(i)*time.Hour).Format("2006-01-02T15")
		}
		return keys, true
	case "24h":
		return u.scanAndFilter("usage:hourly:*")
	case "7d", "30d":
		return u.scanAndFilter("usage:daily:*")
	case "all":
		return u.scanAndFilter("usage:*")
	default:
		return nil, false
	}
}

func (u *UsageHandler) scanAndFilter(pattern string) ([]string, bool) {
	ctx := context.Background()
	keys, err := scanKeys(ctx, u.rdb, pattern)
	if err != nil {
		return nil, false
	}
	var filtered []string
	for _, k := range keys {
		if !strings.HasSuffix(k, ":models") {
			filtered = append(filtered, k)
		}
	}
	return filtered, true
}

func atoi64(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

func atof64(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
