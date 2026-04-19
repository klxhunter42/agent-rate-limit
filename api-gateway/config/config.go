package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all configuration for the API gateway, loaded from environment
// variables with sensible defaults for containerised deployment.
type Config struct {
	ServerPort        string
	RedisAddr         string
	RateLimiterAddr   string
	QueueName         string
	GlobalRateLimit   int
	AgentRateLimit    int
	WorkerPoolSize    int
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	OTLPEndpoint      string
	RedisPoolSize     int
	RedisMinIdleConns int
	UpstreamURL       string
	StreamTimeout     time.Duration

	// Per-model upstream concurrency limits.
	// Map of model name → max concurrent requests to upstream.
	// If a model is not in this map, DefaultLimit is used.
	ModelLimits  map[string]int
	DefaultLimit int
	GlobalLimit  int // total concurrent upstream requests across all models

	// Upstream retry on 429 rate-limit errors.
	UpstreamMaxRetries       int
	UpstreamRetryBaseBackoff time.Duration

	// Token efficiency features.
	EnablePromptInjection bool
	EnableResponseTrim    bool
	EnableSmartMaxTokens  bool
	PromptInjectionText   string

	// Multi-key rotation pool.
	UpstreamAPIKeys  []string
	UpstreamRPMLimit int // per-key requests-per-minute budget

	// Adaptive probe: how many times the initial limit to probe upward.
	// e.g. initial=1, multiplier=5 → maxLimit=5 (discovers real ceiling).
	ProbeMultiplier int

	// Per-model pricing: model -> {input, output} price per 1M tokens (USD).
	ModelPricing map[string]ModelPrice

	// Native Zhipu endpoint for vision requests (base64 image support).
	NativeVisionURL string

	// IP filtering: comma-separated IPs/CIDRs for whitelist or blacklist.
	IPWhitelist string
	IPBlacklist string

	// Quota settings.
	QuotaCacheTTL      time.Duration
	QuotaDailyBudget   int64
	QuotaBlockPct      float64
	QuotaRedisPoolSize int
	QuotaRedisMinIdle  int

	// Provider model prefix mapping: "provider:prefix1,prefix2;provider2:prefix3"
	ProviderModelPrefixes string

	// Request limits.
	MaxRequestBody int64

	// Default values for chat requests.
	DefaultModel       string
	DefaultProvider    string
	DefaultTemperature float64
	DefaultMaxTokens   int

	// Gemini endpoints.
	GeminiCodeAssistEndpoint string
	GeminiAPIEndpoint        string
	GeminiDefaultModel       string

	// Anthropic API version header.
	AnthropicVersion string

	// Adaptive limiter tuning.
	ModelPriority      string // "model:priority,model:priority"
	AnomalyCooldownSec int
	AnomalyZThreshold  float64
}

// ModelPrice holds per-token pricing for cost calculation.
type ModelPrice struct {
	InputPerMillion  float64 // USD per 1M input tokens
	OutputPerMillion float64 // USD per 1M output tokens
}

// Load reads configuration from environment variables, falling back to defaults
// suitable for the docker-compose / Kubernetes deployment.
func Load() *Config {
	return &Config{
		ServerPort:               envOr("SERVER_PORT", ":8080"),
		RedisAddr:                envOr("REDIS_ADDR", "dragonfly:6379"),
		RateLimiterAddr:          envOr("RATE_LIMITER_ADDR", "http://rate-limiter:8080"),
		QueueName:                envOr("QUEUE_NAME", "ai_jobs"),
		GlobalRateLimit:          envIntOr("GLOBAL_RATE_LIMIT", 100),
		AgentRateLimit:           envIntOr("AGENT_RATE_LIMIT", 5),
		WorkerPoolSize:           envIntOr("WORKER_POOL_SIZE", 100),
		ReadTimeout:              envDurationOr("READ_TIMEOUT", 5*time.Second),
		WriteTimeout:             envDurationOr("WRITE_TIMEOUT", 10*time.Second),
		OTLPEndpoint:             envOr("OTLP_ENDPOINT", "otel-collector:4317"),
		RedisPoolSize:            envIntOr("REDIS_POOL_SIZE", 50),
		RedisMinIdleConns:        envIntOr("REDIS_MIN_IDLE_CONNS", 10),
		UpstreamURL:              envOr("UPSTREAM_URL", "https://api.z.ai/api/anthropic"),
		StreamTimeout:            envDurationOr("STREAM_TIMEOUT", 300*time.Second),
		ModelLimits:              parseModelLimits(envOr("UPSTREAM_MODEL_LIMITS", "glm-5.1:1,glm-5-turbo:1,glm-5:2,glm-4.7:2,glm-4.6:3,glm-4.6v:10,glm-4.5v:10,glm-4.6v-flashx:3,glm-4.6v-flash:1")),
		DefaultLimit:             envIntOr("UPSTREAM_DEFAULT_LIMIT", 1),
		GlobalLimit:              envIntOr("UPSTREAM_GLOBAL_LIMIT", 9),
		UpstreamMaxRetries:       envIntOr("UPSTREAM_MAX_RETRIES", 3),
		UpstreamRetryBaseBackoff: envDurationOr("UPSTREAM_RETRY_BACKOFF", 500*time.Millisecond),
		EnablePromptInjection:    envBoolOr("ENABLE_PROMPT_INJECTION", true),
		EnableResponseTrim:       envBoolOr("ENABLE_RESPONSE_TRIM", true),
		EnableSmartMaxTokens:     envBoolOr("ENABLE_SMART_MAX_TOKENS", true),
		PromptInjectionText:      envOr("PROMPT_INJECTION_TEXT", defaultPromptInjection),
		UpstreamAPIKeys:          parseAPIKeys(envOr("UPSTREAM_API_KEYS", "")),
		UpstreamRPMLimit:         envIntOr("UPSTREAM_RPM_LIMIT", 40),
		ProbeMultiplier:          envIntOr("UPSTREAM_PROBE_MULTIPLIER", 5),
		ModelPricing:             parseModelPricing(envOr("MODEL_PRICING", defaultModelPricing)),
		NativeVisionURL:          envOr("NATIVE_VISION_URL", "https://open.bigmodel.cn/api/paas/v4/chat/completions"),
		IPWhitelist:              envOr("IP_WHITELIST", ""),
		IPBlacklist:              envOr("IP_BLACKLIST", ""),

		// Quota settings.
		QuotaCacheTTL:         envDurationOr("QUOTA_CACHE_TTL", 30*time.Second),
		QuotaDailyBudget:      envInt64Or("QUOTA_DAILY_BUDGET", 57600),
		QuotaBlockPct:         envFloatOr("QUOTA_BLOCK_PCT", 95),
		QuotaRedisPoolSize:    envIntOr("QUOTA_REDIS_POOL_SIZE", 5),
		QuotaRedisMinIdle:     envIntOr("QUOTA_REDIS_MIN_IDLE", 2),
		ProviderModelPrefixes: envOr("PROVIDER_MODEL_PREFIXES", "zai:glm-;anthropic:claude-;claude:claude-;openai:gpt-,o3,o4-;gemini:gemini-;gemini-oauth:gemini-;openrouter:or-;qwen:qwen-"),

		// Request limits.
		MaxRequestBody: envInt64Or("MAX_REQUEST_BODY", 10*1024*1024),

		// Default chat request values.
		DefaultModel:       envOr("DEFAULT_MODEL", "glm-5"),
		DefaultProvider:    envOr("DEFAULT_PROVIDER", "glm"),
		DefaultTemperature: envFloatOr("DEFAULT_TEMPERATURE", 0.7),
		DefaultMaxTokens:   envIntOr("DEFAULT_MAX_TOKENS", 1024),

		// Gemini endpoints.
		GeminiCodeAssistEndpoint: envOr("GEMINI_CODEASSIST_ENDPOINT", "https://cloudcode-pa.googleapis.com/v1internal"),
		GeminiAPIEndpoint:        envOr("GEMINI_API_ENDPOINT", "https://generativelanguage.googleapis.com"),
		GeminiDefaultModel:       envOr("GEMINI_DEFAULT_MODEL", "models/gemini-2.5-flash-preview-05-20"),

		// Anthropic API version.
		AnthropicVersion: envOr("ANTHROPIC_API_VERSION", "2023-06-01"),

		// Adaptive limiter tuning.
		ModelPriority:      envOr("MODEL_PRIORITY", "glm-5.1:100,glm-5-turbo:90,glm-5:80,glm-4.7:70,glm-4.6:60,glm-4.5:50"),
		AnomalyCooldownSec: envIntOr("ANOMALY_COOLDOWN_SEC", 5),
		AnomalyZThreshold:  envFloatOr("ANOMALY_Z_THRESHOLD", 2.0),
	}
}

// RedisURL returns the full Redis connection string used by go-redis.
func (c *Config) RedisURL() string {
	return c.RedisAddr
}

// RateLimiterCheckURL returns the full URL for the rate-limit check endpoint.
func (c *Config) RateLimiterCheckURL() string {
	return fmt.Sprintf("%s/api/ratelimit/check", c.RateLimiterAddr)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envDurationOr(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func envInt64Or(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return fallback
}

func envFloatOr(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func envBoolOr(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

const defaultPromptInjection = `[GATEWAY RULES — strict]
Be extremely concise. Use short, direct answers. Skip filler, preamble, and summaries.
Prefer code over prose. Omit disclaimers and caveats. If the answer fits in one line, use one line.
Never repeat or paraphrase the question back.

[VISION — strict]
When an image is provided: examine every pixel region carefully before answering.
Identify dominant colors, shapes, text, objects, and spatial layout.
Answer based only on what is visibly present in the image — never assume or guess.
If the image is unclear or too small, state that explicitly.`

const defaultModelPricing = "glm-5.1:0.5:1.5,glm-5-turbo:0.5:1.5,glm-5:0.5:1.5,glm-4.7:0.3:1.0,glm-4.6:0.3:1.0,glm-4.5:0.15:0.75,glm-4.6v:0.3:1.0,glm-4.5v:0.15:0.75,glm-4.6v-flashx:0.1:0.5,glm-4.6v-flash:0.1:0.5"

// parseModelPricing parses "model1:input:output,model2:input:output" into a pricing map.
// Prices are USD per 1M tokens.
func parseModelPricing(s string) map[string]ModelPrice {
	m := make(map[string]ModelPrice)
	for _, pair := range splitComma(s) {
		parts := splitColon(pair)
		if len(parts) == 3 {
			inp, err1 := strconv.ParseFloat(parts[1], 64)
			out, err2 := strconv.ParseFloat(parts[2], 64)
			if err1 == nil && err2 == nil {
				m[parts[0]] = ModelPrice{InputPerMillion: inp, OutputPerMillion: out}
			}
		}
	}
	return m
}

// parseModelLimits parses "model1:limit1,model2:limit2" into a map.
func parseModelLimits(s string) map[string]int {
	m := make(map[string]int)
	for _, pair := range splitComma(s) {
		parts := splitColon(pair)
		if len(parts) == 2 {
			if n, err := strconv.Atoi(parts[1]); err == nil && n > 0 {
				m[parts[0]] = n
			}
		}
	}
	return m
}

func splitComma(s string) []string {
	return strings.Split(s, ",")
}

// parseAPIKeys splits a comma-separated list of API keys, trimming whitespace.
func parseAPIKeys(s string) []string {
	if s == "" {
		return nil
	}
	var keys []string
	for _, k := range strings.Split(s, ",") {
		k = strings.TrimSpace(k)
		if k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

func splitColon(s string) []string {
	return strings.Split(s, ":")
}

// ParseProviderModelPrefixes parses "provider:prefix1,prefix2;provider2:prefix3" into a map.
func ParseProviderModelPrefixes(s string) map[string][]string {
	m := make(map[string][]string)
	for _, entry := range strings.Split(s, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			continue
		}
		provider := strings.TrimSpace(parts[0])
		prefixes := strings.Split(parts[1], ",")
		clean := make([]string, 0, len(prefixes))
		for _, p := range prefixes {
			p = strings.TrimSpace(p)
			if p != "" {
				clean = append(clean, p)
			}
		}
		m[provider] = clean
	}
	return m
}

// ParseModelPriority parses "model:priority,model:priority" into a map.
func ParseModelPriority(s string) map[string]int {
	return parseModelLimits(s)
}
