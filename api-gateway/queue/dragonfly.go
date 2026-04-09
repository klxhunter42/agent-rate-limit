package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/klxhunter/agent-rate-limit/api-gateway/config"
	"github.com/redis/go-redis/v9"
)

// Job represents a single AI inference request enqueued for async processing.
type Job struct {
	RequestID   string            `json:"request_id"`
	AgentID     string            `json:"agent_id"`
	Model       string            `json:"model"`
	Messages    []map[string]any  `json:"messages"`
	MaxTokens   int               `json:"max_tokens"`
	Temperature float64           `json:"temperature"`
	Provider    string            `json:"provider"`
	RetryCount  int               `json:"retry_count"`
	Metadata    map[string]string `json:"metadata"`
}

const (
	pushTimeout  = 3 * time.Second
	getTimeout   = 2 * time.Second
	setTimeout   = 2 * time.Second
	defaultTTL   = 10 * time.Minute
	resultPrefix = "result:"
)

// DragonflyClient wraps a go-redis client connected to the Dragonfly instance
// and provides queue + cache primitives for the gateway.
type DragonflyClient struct {
	client    *redis.Client
	queueName string
}

// NewDragonflyClient creates a new client with optimized connection pool,
// ping-tests the connection, and returns an error if Dragonfly is unreachable.
func NewDragonflyClient(cfg *config.Config) (*DragonflyClient, error) {
	opt, err := redis.ParseURL(fmt.Sprintf("redis://%s", cfg.RedisURL()))
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}

	// Production connection pool tuning
	opt.PoolSize = 50             // connections per CPU for high throughput
	opt.MinIdleConns = 10         // keep warm connections
	opt.ConnMaxIdleTime = 5 * time.Minute
	opt.ConnMaxLifetime = 30 * time.Minute
	opt.DialTimeout = 3 * time.Second
	opt.ReadTimeout = 3 * time.Second
	opt.WriteTimeout = 3 * time.Second
	opt.PoolTimeout = 4 * time.Second

	rdb := redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping dragonfly: %w", err)
	}

	slog.Info("connected to dragonfly", "addr", cfg.RedisAddr)

	return &DragonflyClient{
		client:    rdb,
		queueName: cfg.QueueName,
	}, nil
}

// PushJob serialises the job to JSON and LPUSHes it onto the queue.
// This method is safe to call from a goroutine.
func (dc *DragonflyClient) PushJob(ctx context.Context, job *Job) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal job: %w", err)
	}

	pushCtx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()

	res, err := dc.client.LPush(pushCtx, dc.queueName, data).Result()
	if err != nil {
		return fmt.Errorf("lpush job: %w", err)
	}

	slog.Debug("job pushed to queue",
		"request_id", job.RequestID,
		"agent_id", job.AgentID,
		"queue", dc.queueName,
		"queue_len", res,
	)
	return nil
}

// GetResult retrieves a cached result for the given requestID.
// Returns (nil, nil) when the key does not exist.
func (dc *DragonflyClient) GetResult(ctx context.Context, requestID string) (string, error) {
	key := resultPrefix + requestID

	getCtx, cancel := context.WithTimeout(ctx, getTimeout)
	defer cancel()

	val, err := dc.client.Get(getCtx, key).Result()
	if err == redis.Nil {
		return "", nil // no result yet
	}
	if err != nil {
		return "", fmt.Errorf("get result: %w", err)
	}
	return val, nil
}

// SetResult stores a result in the cache with the given TTL.
func (dc *DragonflyClient) SetResult(ctx context.Context, requestID string, result string, ttl time.Duration) error {
	key := resultPrefix + requestID

	setCtx, cancel := context.WithTimeout(ctx, setTimeout)
	defer cancel()

	if err := dc.client.Set(setCtx, key, result, ttl).Err(); err != nil {
		return fmt.Errorf("set result: %w", err)
	}

	slog.Debug("result cached", "request_id", requestID, "ttl", ttl)
	return nil
}

// SetResultWithDefaultTTL stores a result using the default TTL.
func (dc *DragonflyClient) SetResultWithDefaultTTL(ctx context.Context, requestID string, result string) error {
	return dc.SetResult(ctx, requestID, result, defaultTTL)
}

// Close gracefully closes the underlying Redis connection.
func (dc *DragonflyClient) Close() error {
	return dc.client.Close()
}

// QueueDepth returns the current length of the job queue for metrics.
func (dc *DragonflyClient) QueueDepth(ctx context.Context) (int64, error) {
	depthCtx, cancel := context.WithTimeout(ctx, getTimeout)
	defer cancel()

	n, err := dc.client.LLen(depthCtx, dc.queueName).Result()
	if err != nil {
		return 0, fmt.Errorf("llen queue: %w", err)
	}
	return n, nil
}
