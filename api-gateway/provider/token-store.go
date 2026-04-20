package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const tokenKeyPrefix = "arl:tokens:"

type TokenInfo struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiryDate   time.Time `json:"expiry_date"`
	Email        string    `json:"email,omitempty"`
	AccountID    string    `json:"account_id"`
	Provider     string    `json:"provider"`
	Tier         string    `json:"tier,omitempty"`
	Paused       bool      `json:"paused"`
	IsDefault    bool      `json:"is_default"`
	CreatedAt    time.Time `json:"created_at"`
	Scopes       string    `json:"scopes,omitempty"`
}

func (t *TokenInfo) redisKey() string {
	return tokenKeyPrefix + t.Provider + ":" + t.AccountID
}

func tokenKey(provider, accountID string) string {
	return tokenKeyPrefix + provider + ":" + accountID
}

type TokenStore struct {
	client *redis.Client
}

func NewTokenStore(redisAddr string) *TokenStore {
	opt, err := redis.ParseURL(redisAddr)
	if err != nil {
		opt = &redis.Options{Addr: redisAddr}
	}
	opt.PoolSize = 20
	opt.MinIdleConns = 5

	rdb := redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("token store: redis ping failed", "error", err)
	}

	return &TokenStore{client: rdb}
}

func (s *TokenStore) Client() *redis.Client {
	return s.client
}

func (s *TokenStore) Store(token TokenInfo) error {
	if token.CreatedAt.IsZero() {
		token.CreatedAt = time.Now()
	}
	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	key := token.redisKey()
	if err := s.client.Set(ctx, key, data, 0).Err(); err != nil {
		return fmt.Errorf("redis set token: %w", err)
	}

	// Add to provider index.
	idxKey := tokenKeyPrefix + token.Provider + ":_index"
	if err := s.client.SAdd(ctx, idxKey, token.AccountID).Err(); err != nil {
		return fmt.Errorf("redis sadd index: %w", err)
	}

	slog.Info("token stored", "provider", token.Provider, "account_id", token.AccountID)
	return nil
}

func (s *TokenStore) Get(provider, accountID string) (*TokenInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	data, err := s.client.Get(ctx, tokenKey(provider, accountID)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("redis get token: %w", err)
	}

	var token TokenInfo
	if err := json.Unmarshal([]byte(data), &token); err != nil {
		return nil, fmt.Errorf("unmarshal token: %w", err)
	}
	return &token, nil
}

func (s *TokenStore) Delete(provider, accountID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := s.client.Del(ctx, tokenKey(provider, accountID)).Err(); err != nil {
		return fmt.Errorf("redis del token: %w", err)
	}

	idxKey := tokenKeyPrefix + provider + ":_index"
	s.client.SRem(ctx, idxKey, accountID)

	slog.Info("token deleted", "provider", provider, "account_id", accountID)
	return nil
}

func (s *TokenStore) ListByProvider(provider string) ([]TokenInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	idxKey := tokenKeyPrefix + provider + ":_index"
	ids, err := s.client.SMembers(ctx, idxKey).Result()
	if err != nil {
		return nil, fmt.Errorf("redis smembers: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	// Pipeline all GETs to avoid N+1 round trips.
	pipe := s.client.Pipeline()
	cmds := make([]*redis.StringCmd, len(ids))
	for i, id := range ids {
		cmds[i] = pipe.Get(ctx, tokenKey(provider, id))
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		// Ignore individual key misses; only fail on true connection errors.
		if len(cmds) == 0 {
			return nil, fmt.Errorf("redis pipeline: %w", err)
		}
	}

	tokens := make([]TokenInfo, 0, len(ids))
	for _, cmd := range cmds {
		data, err := cmd.Bytes()
		if err != nil {
			continue
		}
		var t TokenInfo
		if err := json.Unmarshal(data, &t); err != nil {
			continue
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}

func (s *TokenStore) ListAll() ([]TokenInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var tokens []TokenInfo
	iter := s.client.Scan(ctx, 0, tokenKeyPrefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		// Skip index keys.
		if len(key) > 7 && key[len(key)-7:] == ":_index" {
			continue
		}

		data, err := s.client.Get(ctx, key).Result()
		if err != nil {
			continue
		}
		var t TokenInfo
		if err := json.Unmarshal([]byte(data), &t); err != nil {
			continue
		}
		tokens = append(tokens, t)
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("redis scan: %w", err)
	}
	return tokens, nil
}

func (s *TokenStore) SetDefault(provider, accountID string) error {
	tokens, err := s.ListByProvider(provider)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, t := range tokens {
		t.IsDefault = t.AccountID == accountID
		data, _ := json.Marshal(t)
		s.client.Set(ctx, tokenKey(t.Provider, t.AccountID), data, 0)
	}

	slog.Info("default account set", "provider", provider, "account_id", accountID)
	return nil
}

func (s *TokenStore) Pause(provider, accountID string) error {
	return s.updateField(provider, accountID, func(t *TokenInfo) { t.Paused = true })
}

func (s *TokenStore) Resume(provider, accountID string) error {
	return s.updateField(provider, accountID, func(t *TokenInfo) { t.Paused = false })
}

func (s *TokenStore) GetDefault(provider string) (*TokenInfo, error) {
	tokens, err := s.ListByProvider(provider)
	if err != nil {
		return nil, err
	}
	for _, t := range tokens {
		if t.IsDefault && !t.Paused {
			tCopy := t
			return &tCopy, nil
		}
	}
	// Fallback: return first non-paused token.
	for _, t := range tokens {
		if !t.Paused {
			tCopy := t
			return &tCopy, nil
		}
	}
	return nil, nil
}

func (s *TokenStore) updateField(provider, accountID string, fn func(*TokenInfo)) error {
	token, err := s.Get(provider, accountID)
	if err != nil {
		return err
	}
	if token == nil {
		return fmt.Errorf("token not found: %s/%s", provider, accountID)
	}
	fn(token)
	return s.Store(*token)
}

// GetFromPool returns a non-paused token from the given account IDs for a provider.
// Falls back to GetDefault if accountIDs is empty.
func (s *TokenStore) GetFromPool(provider string, accountIDs []string) (*TokenInfo, error) {
	if len(accountIDs) == 0 {
		return s.GetDefault(provider)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pipe := s.client.Pipeline()
	cmds := make([]*redis.StringCmd, len(accountIDs))
	for i, id := range accountIDs {
		cmds[i] = pipe.Get(ctx, tokenKey(provider, id))
	}
	if _, err := pipe.Exec(ctx); err != nil && len(cmds) == 0 {
		return nil, fmt.Errorf("redis pipeline: %w", err)
	}

	var candidates []TokenInfo
	for _, cmd := range cmds {
		data, err := cmd.Bytes()
		if err != nil {
			continue
		}
		var t TokenInfo
		if err := json.Unmarshal(data, &t); err != nil {
			continue
		}
		if !t.Paused {
			candidates = append(candidates, t)
		}
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// Prefer default account if it's in the pool.
	for _, t := range candidates {
		if t.IsDefault {
			tCopy := t
			return &tCopy, nil
		}
	}

	// Round-robin: return first available.
	tCopy := candidates[0]
	return &tCopy, nil
}
