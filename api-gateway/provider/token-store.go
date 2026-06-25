package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/klxhunter/agent-rate-limit/api-gateway/store"
)

const tokenKeyPrefix = "arl:tokens:"

// Provider renames: old ID -> new ID. Migrated once on startup.
var providerRenames = map[string]string{
	"claude": "claude-oauth",
}

type ClaudeProfile struct {
	AccountUUID   string `json:"account_uuid,omitempty"`
	AccountName   string `json:"account_name,omitempty"`
	AccountEmail  string `json:"account_email,omitempty"`
	HasClaudeMax  bool   `json:"has_claude_max,omitempty"`
	HasClaudePro  bool   `json:"has_claude_pro,omitempty"`
	OrgUUID       string `json:"org_uuid,omitempty"`
	OrgName       string `json:"org_name,omitempty"`
	OrgType       string `json:"org_type,omitempty"`
	BillingType   string `json:"billing_type,omitempty"`
	RateLimitTier string `json:"rate_limit_tier,omitempty"`
	SeatTier      string `json:"seat_tier,omitempty"`
	SubStatus     string `json:"subscription_status,omitempty"`
	HasExtraUsage bool   `json:"has_extra_usage_enabled,omitempty"`
	AppUUID       string `json:"app_uuid,omitempty"`
	AppName       string `json:"app_name,omitempty"`
	OrgRole       string `json:"org_role,omitempty"`
	WorkspaceUUID string `json:"workspace_uuid,omitempty"`
	WorkspaceName string `json:"workspace_name,omitempty"`
	WorkspaceRole string `json:"workspace_role,omitempty"`
}

type TokenInfo struct {
	AccessToken   string         `json:"access_token"`
	RefreshToken  string         `json:"refresh_token,omitempty"`
	ExpiryDate    time.Time      `json:"expiry_date"`
	Email         string         `json:"email,omitempty"`
	AccountID     string         `json:"account_id"`
	Provider      string         `json:"provider"`
	ProjectID     string         `json:"project_id,omitempty"`
	Tier          string         `json:"tier,omitempty"`
	Paused        bool           `json:"paused"`
	IsDefault     bool           `json:"is_default"`
	CreatedAt     time.Time      `json:"created_at"`
	Scopes        string         `json:"scopes,omitempty"`
	UpstreamURL   string         `json:"upstream_url,omitempty"`
	ClaudeProfile *ClaudeProfile `json:"claude_profile,omitempty"`
}

func (t *TokenInfo) redisKey() string {
	return tokenKeyPrefix + t.Provider + ":" + t.AccountID
}

// IsExpired returns true if the token's access_token has passed its expiry_date.
// OAuth access tokens from Claude have ~1h TTL; after that the refresh_token must be used.
func (t *TokenInfo) IsExpired() bool {
	if t.ExpiryDate.IsZero() {
		return false
	}
	return time.Now().After(t.ExpiryDate)
}

func tokenKey(provider, accountID string) string {
	return tokenKeyPrefix + provider + ":" + accountID
}

type TokenStore struct {
	client *redis.Client
	pg     store.Store // nil when Postgres disabled
}

func NewTokenStore(redisAddr string, pg store.Store) *TokenStore {
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

	return &TokenStore{client: rdb, pg: pg}
}

type AcctRateLimit struct {
	Util5h float64 `json:"util_5h"`
	Status string  `json:"status"`
}

// GetRateLimits reads cached rate limit status for multiple accounts in one pipeline.
func (s *TokenStore) GetRateLimits(provider string, accountIDs []string) map[string]AcctRateLimit {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pipe := s.client.Pipeline()
	cmds := make(map[string]*redis.StringCmd, len(accountIDs))
	for _, id := range accountIDs {
		cmds[id] = pipe.Get(ctx, "arl:ratelimit:"+provider+":"+id)
	}
	pipe.Exec(ctx)

	result := make(map[string]AcctRateLimit, len(accountIDs))
	for id, cmd := range cmds {
		data, err := cmd.Bytes()
		if err != nil {
			continue
		}
		var rl AcctRateLimit
		if json.Unmarshal(data, &rl) == nil && rl.Util5h > 0 {
			result[id] = rl
		}
	}
	return result
}

// MigrateProviderRenames copies tokens from old provider IDs to new ones.
func (s *TokenStore) MigrateProviderRenames() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for oldID, newID := range providerRenames {
		oldIdx := tokenKeyPrefix + oldID + ":_index"
		ids, err := s.client.SMembers(ctx, oldIdx).Result()
		if err != nil || len(ids) == 0 {
			continue
		}

		newIdx := tokenKeyPrefix + newID + ":_index"
		for _, id := range ids {
			if id == "_index" {
				continue
			}
			oldKey := tokenKeyPrefix + oldID + ":" + id
			newKey := tokenKeyPrefix + newID + ":" + id

			data, err := s.client.Get(ctx, oldKey).Bytes()
			if err != nil {
				continue
			}

			var t TokenInfo
			if json.Unmarshal(data, &t) != nil {
				continue
			}
			t.Provider = newID
			migrated, _ := json.Marshal(t)

			s.client.Set(ctx, newKey, migrated, 0)
			s.client.SAdd(ctx, newIdx, id)
			s.client.Del(ctx, oldKey)
		}
		s.client.Del(ctx, oldIdx)
		slog.Info("migrated provider tokens", "from", oldID, "to", newID, "count", len(ids))
	}
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

	// Dual-write: Postgres (source of truth) then Dragonfly (hot cache).
	if s.pg != nil {
		profileJSON, _ := json.Marshal(token.ClaudeProfile)
		if err := s.pg.StoreAccount(ctx, store.AccountRow{
			Provider:      token.Provider,
			AccountID:     token.AccountID,
			Email:         token.Email,
			AccessToken:   token.AccessToken,
			RefreshToken:  token.RefreshToken,
			ExpiryDate:    token.ExpiryDate,
			Tier:          token.Tier,
			Paused:        token.Paused,
			IsDefault:     token.IsDefault,
			Scopes:        token.Scopes,
			UpstreamURL:   token.UpstreamURL,
			ClaudeProfile: profileJSON,
			CreatedAt:     token.CreatedAt,
			UpdatedAt:     time.Now(),
		}); err != nil {
			slog.Error("pg store account", "error", err)
		}
	}

	key := token.redisKey()
	if err := s.client.Set(ctx, key, data, 0).Err(); err != nil {
		return fmt.Errorf("redis set token: %w", err)
	}

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
		// Read-through: fall back to Postgres.
		if s.pg != nil {
			return s.getFromPostgres(ctx, provider, accountID)
		}
		return nil, nil
	}
	if err != nil {
		// Dragonfly error, try Postgres.
		if s.pg != nil {
			return s.getFromPostgres(ctx, provider, accountID)
		}
		return nil, fmt.Errorf("redis get token: %w", err)
	}

	var token TokenInfo
	if err := json.Unmarshal([]byte(data), &token); err != nil {
		return nil, fmt.Errorf("unmarshal token: %w", err)
	}
	return &token, nil
}

// getFromPostgres reads a token from Postgres and populates the Dragonfly cache.
func (s *TokenStore) getFromPostgres(ctx context.Context, provider, accountID string) (*TokenInfo, error) {
	row, err := s.pg.GetAccount(ctx, provider, accountID)
	if err != nil {
		return nil, fmt.Errorf("pg get account: %w", err)
	}
	if row == nil {
		return nil, nil
	}
	token := accountRowToTokenInfo(row)

	// Rehydrate Dragonfly cache.
	data, _ := json.Marshal(token)
	s.client.Set(ctx, token.redisKey(), data, 0)
	s.client.SAdd(ctx, tokenKeyPrefix+provider+":_index", accountID)

	return &token, nil
}

// loadFromPostgres is the read-through fallback when the Dragonfly cache is cold
// or has evicted keys (--cache_mode LRU). Postgres is the source of truth;
// Dragonfly is a disposable hot cache, so a miss here must never cause missing
// data, only a brief slowdown. The returned tokens are also written back into
// Dragonfly so subsequent reads stay fast.
func (s *TokenStore) loadFromPostgres(ctx context.Context, all bool, provider string) ([]TokenInfo, error) {
	if s.pg == nil {
		return nil, nil
	}
	var rows []store.AccountRow
	var err error
	if all {
		rows, err = s.pg.ListAllAccounts(ctx)
	} else {
		rows, err = s.pg.ListAccountsByProvider(ctx, provider)
	}
	if err != nil {
		return nil, fmt.Errorf("pg list accounts: %w", err)
	}

	tokens := make([]TokenInfo, 0, len(rows))
	pipe := s.client.Pipeline()
	for i := range rows {
		t := accountRowToTokenInfo(&rows[i])
		data, _ := json.Marshal(t)
		pipe.Set(ctx, t.redisKey(), data, 0)
		pipe.SAdd(ctx, tokenKeyPrefix+t.Provider+":_index", t.AccountID)
		tokens = append(tokens, t)
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		// Best-effort: serve from Postgres even if cache rehydration partially failed.
		slog.Warn("token store: cache rehydrate failed, serving from postgres", "error", err)
	}
	if len(tokens) > 0 {
		slog.Info("token store: cold cache, rehydrated from postgres", "count", len(tokens), "all", all)
	}
	return tokens, nil
}

func accountRowToTokenInfo(r *store.AccountRow) TokenInfo {
	t := TokenInfo{
		AccessToken:  r.AccessToken,
		RefreshToken: r.RefreshToken,
		ExpiryDate:   r.ExpiryDate,
		Email:        r.Email,
		AccountID:    r.AccountID,
		Provider:     r.Provider,
		Tier:         r.Tier,
		Paused:       r.Paused,
		IsDefault:    r.IsDefault,
		CreatedAt:    r.CreatedAt,
		Scopes:       r.Scopes,
		UpstreamURL:  r.UpstreamURL,
	}
	if len(r.ClaudeProfile) > 0 && string(r.ClaudeProfile) != "null" {
		json.Unmarshal(r.ClaudeProfile, &t.ClaudeProfile)
	}
	return t
}

// DeleteByProvider removes all tokens for a provider (used when deleting a custom provider).
func (s *TokenStore) DeleteByProvider(provider string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tokens, err := s.ListByProvider(provider)
	if err != nil {
		return fmt.Errorf("list tokens: %w", err)
	}

	for _, t := range tokens {
		s.client.Del(ctx, tokenKey(provider, t.AccountID))
	}
	s.client.Del(ctx, tokenKeyPrefix+provider+":_index")

	if s.pg != nil {
		if err := s.pg.DeleteAccountsByProvider(ctx, provider); err != nil {
			slog.Error("pg delete accounts by provider", "provider", provider, "error", err)
		}
	}

	if len(tokens) > 0 {
		slog.Info("deleted all tokens for provider", "provider", provider, "count", len(tokens))
	}
	return nil
}

func (s *TokenStore) Delete(provider, accountID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := s.client.Del(ctx, tokenKey(provider, accountID)).Err(); err != nil {
		return fmt.Errorf("redis del token: %w", err)
	}

	idxKey := tokenKeyPrefix + provider + ":_index"
	s.client.SRem(ctx, idxKey, accountID)

	if s.pg != nil {
		if err := s.pg.DeleteAccount(ctx, provider, accountID); err != nil {
			slog.Error("pg delete account", "error", err)
		}
	}

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
		// Cold cache: read through from Postgres and rehydrate.
		return s.loadFromPostgres(ctx, false, provider)
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
	// Index existed but every key missed (LRU eviction under --cache_mode): fall back.
	if len(tokens) == 0 {
		return s.loadFromPostgres(ctx, false, provider)
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
	// Cold cache: read through from Postgres and rehydrate.
	if len(tokens) == 0 {
		return s.loadFromPostgres(ctx, true, "")
	}
	return tokens, nil
}

func (s *TokenStore) SetDefault(provider, accountID string) error {
	tokens, err := s.ListByProvider(provider)
	if err != nil {
		return err
	}

	alreadyDefault := false
	for _, t := range tokens {
		if t.AccountID == accountID && t.IsDefault {
			alreadyDefault = true
			break
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, t := range tokens {
		if alreadyDefault {
			t.IsDefault = false
		} else {
			t.IsDefault = t.AccountID == accountID
		}
		data, _ := json.Marshal(t)
		s.client.Set(ctx, tokenKey(t.Provider, t.AccountID), data, 0)
	}

	if s.pg != nil {
		if err := s.pg.SetDefaultAccount(ctx, provider, accountID); err != nil {
			slog.Error("pg set default", "error", err)
		}
	}

	slog.Info("default account updated", "provider", provider, "account_id", accountID, "cleared", alreadyDefault)
	return nil
}

func (s *TokenStore) Pause(provider, accountID string) error {
	if err := s.updateField(provider, accountID, func(t *TokenInfo) { t.Paused = true }); err != nil {
		return err
	}
	if s.pg != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.pg.PauseAccount(ctx, provider, accountID); err != nil {
			slog.Error("pg pause account", "error", err)
		}
	}
	return nil
}

func (s *TokenStore) Resume(provider, accountID string) error {
	if err := s.updateField(provider, accountID, func(t *TokenInfo) { t.Paused = false }); err != nil {
		return err
	}
	if s.pg != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.pg.ResumeAccount(ctx, provider, accountID); err != nil {
			slog.Error("pg resume account", "error", err)
		}
	}
	return nil
}

func (s *TokenStore) GetDefault(provider string) (*TokenInfo, error) {
	tokens, err := s.ListByProvider(provider)
	if err != nil {
		return nil, err
	}
	for _, t := range tokens {
		if t.IsDefault && !t.Paused && !t.IsExpired() {
			tCopy := t
			return &tCopy, nil
		}
	}
	// Fallback: return first non-paused, non-expired token.
	for _, t := range tokens {
		if !t.Paused && !t.IsExpired() {
			tCopy := t
			return &tCopy, nil
		}
	}
	return nil, nil
}

func (s *TokenStore) UpdateEmail(provider, accountID, email string) error {
	if err := s.updateField(provider, accountID, func(t *TokenInfo) {
		t.Email = email
	}); err != nil {
		return err
	}
	if s.pg != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.pg.UpdateAccountEmail(ctx, provider, accountID, email); err != nil {
			slog.Error("pg update email", "error", err)
		}
	}
	return nil
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
// Prefers accounts with lowest 5h utilization when multiple are available.
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
		if t.IsExpired() {
			slog.Warn("skipping expired OAuth token", "provider", provider, "account_id", t.AccountID, "expiry", t.ExpiryDate)
			continue
		}
		if !t.Paused {
			candidates = append(candidates, t)
		}
	}

	if len(candidates) == 0 {
		// Cold cache: read through from Postgres, rehydrate, and retry selection.
		if s.pg != nil {
			want := make(map[string]struct{}, len(accountIDs))
			for _, id := range accountIDs {
				want[id] = struct{}{}
			}
			if loaded, err := s.loadFromPostgres(ctx, false, provider); err == nil {
				for _, t := range loaded {
					if _, ok := want[t.AccountID]; ok && !t.Paused && !t.IsExpired() {
						candidates = append(candidates, t)
					}
				}
			}
		}
		if len(candidates) == 0 {
			return nil, nil
		}
	}

	if len(candidates) == 1 {
		tCopy := candidates[0]
		return &tCopy, nil
	}

	// Multiple accounts: pick the one with lowest 5h utilization.
	// Among accounts with same utilization (or all unknown), pick randomly.
	rls := s.GetRateLimits(provider, accountIDs)
	best := candidates[0]
	bestUtil := 200.0
	ties := []TokenInfo{candidates[0]}
	for _, t := range candidates[1:] {
		util := 200.0
		if rl, ok := rls[t.AccountID]; ok && rl.Util5h > 0 {
			util = rl.Util5h
		}
		if util < bestUtil {
			bestUtil = util
			best = t
			ties = []TokenInfo{t}
		} else if util == bestUtil {
			ties = append(ties, t)
		}
	}
	if len(ties) > 1 {
		best = ties[rand.Intn(len(ties))]
	}
	tCopy := best
	return &tCopy, nil
}

// MigrateClaudeOAuthEmails fetches profiles for existing claude-oauth tokens
// and updates their email/account_id from the profile data.
// Call this once after deploying the email-from-profile fix.
func (s *TokenStore) MigrateClaudeOAuthEmails() int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tokens, err := s.ListByProvider("claude-oauth")
	if err != nil || len(tokens) == 0 {
		return 0
	}

	updated := 0
	for _, token := range tokens {
		// Skip if already has proper email format
		if token.Email != "" && !strings.HasPrefix(token.Email, "claude-oauth_") {
			continue
		}

		// Fetch profile using access token
		if token.ClaudeProfile != nil && token.ClaudeProfile.AccountEmail != "" {
			// Profile already cached, use it
			newEmail := token.ClaudeProfile.AccountEmail
			if newEmail != token.Email {
				s.updateEmailAndAccountID(ctx, &token, newEmail)
				updated++
			}
			continue
		}

		// No cached profile, fetch from API
		profile, err := FetchClaudeProfile(ctx, token.AccessToken)
		if err != nil {
			slog.Warn("failed to fetch profile for migration", "account_id", token.AccountID, "error", err)
			continue
		}

		if profile.AccountEmail != "" {
			token.ClaudeProfile = profile
			s.updateEmailAndAccountID(ctx, &token, profile.AccountEmail)
			updated++
		}
	}

	if updated > 0 {
		slog.Info("migrated claude-oauth emails", "count", updated)
	}
	return updated
}

func (s *TokenStore) updateEmailAndAccountID(ctx context.Context, token *TokenInfo, newEmail string) {
	oldKey := token.redisKey()
	oldAccountID := token.AccountID

	token.Email = newEmail
	token.AccountID = newEmail
	newKey := token.redisKey()

	data, _ := json.Marshal(token)

	// Write new key
	s.client.Set(ctx, newKey, data, 0)

	// Update index
	idxKey := tokenKeyPrefix + token.Provider + ":_index"
	s.client.SRem(ctx, idxKey, oldAccountID)
	s.client.SAdd(ctx, idxKey, newEmail)

	// Delete old key if different
	if oldKey != newKey {
		s.client.Del(ctx, oldKey)
	}

	slog.Info("migrated token email", "old_account_id", oldAccountID, "new_email", newEmail)
}
