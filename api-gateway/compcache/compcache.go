package compcache

import (
	"bytes"
	"context"
	"os"
	"strings"
	"strconv"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/redis/go-redis/v9"
)

type Config struct {
	Enabled bool
	MinSize int // minimum value size to compress (bytes)
	Level   int // zstd compression level (1-22)
}

func envBoolOr(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func LoadConfig() Config {
	return Config{
		Enabled: envBoolOr("COMPCACHE_ENABLED", true),
		MinSize: envIntOr("COMPCACHE_MIN_SIZE", 512),
		Level:   envIntOr("COMPCACHE_LEVEL", 3),
	}
}

const prefix = "zstd:"

// CompCache wraps redis.Client with transparent compression for large values.
type CompCache struct {
	rdb *redis.Client
	cfg Config
	enc *zstd.Encoder
	dec *zstd.Decoder
}

// New creates a compressed cache wrapper. Falls back to raw operations if zstd init fails.
func New(rdb *redis.Client, cfg Config) *CompCache {
	enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(cfg.Level)))
	if err != nil {
		return &CompCache{rdb: rdb, cfg: cfg}
	}
	dec, err := zstd.NewReader(nil)
	if err != nil {
		enc.Close()
		return &CompCache{rdb: rdb, cfg: cfg}
	}
	return &CompCache{rdb: rdb, cfg: cfg, enc: enc, dec: dec}
}

// CompressedSet stores value with zstd compression if it exceeds MinSize.
func (c *CompCache) CompressedSet(ctx context.Context, key string, value string, ttl time.Duration) error {
	if c.rdb == nil {
		return nil
	}
	if !c.cfg.Enabled || len(value) < c.cfg.MinSize || c.enc == nil {
		return c.rdb.Set(ctx, key, value, ttl).Err()
	}

	compressed := c.compress([]byte(value))
	if len(compressed) >= len(value) {
		// Compression didn't help, store raw
		return c.rdb.Set(ctx, key, value, ttl).Err()
	}

	return c.rdb.Set(ctx, key, prefix+string(compressed), ttl).Err()
}

// CompressedGet retrieves value, decompressing if needed. Backward compatible with raw values.
func (c *CompCache) CompressedGet(ctx context.Context, key string) (string, error) {
	if c.rdb == nil {
		return "", redis.Nil
	}

	val, err := c.rdb.Get(ctx, key).Result()
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(val, prefix) || c.dec == nil {
		return val, nil
	}

	decompressed, err := c.decompress([]byte(val[len(prefix):]))
	if err != nil {
		// Return raw value if decompression fails
		return val, nil
	}

	return string(decompressed), nil
}

// CompressionRatio returns the ratio of compressed/original size. 0 means no compression.
func (c *CompCache) CompressionRatio(original []byte) float64 {
	if c.enc == nil || len(original) < c.cfg.MinSize {
		return 0
	}
	compressed := c.compress(original)
	if len(compressed) >= len(original) {
		return 0
	}
	return float64(len(compressed)) / float64(len(original))
}

func (c *CompCache) compress(data []byte) []byte {
	c.enc.Reset(nil)
	c.enc.Write(data)
	return c.enc.EncodeAll(data, nil)
}

func (c *CompCache) decompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	err := c.dec.Reset(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	buf.ReadFrom(c.dec)
	return buf.Bytes(), nil
}
