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
		ModelLimits:              parseModelLimits(envOr("UPSTREAM_MODEL_LIMITS", "glm-5.1:1,glm-5-turbo:1,glm-5:2,glm-4.7:2,glm-4.6:3")),
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
Never repeat or paraphrase the question back.`

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
