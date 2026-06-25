package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Schema DDL executed on connect. Uses IF NOT EXISTS for idempotency.
const schema = `
CREATE TABLE IF NOT EXISTS provider_accounts (
  provider       TEXT NOT NULL,
  account_id    TEXT NOT NULL,
  email          TEXT,
  access_token   TEXT NOT NULL,
  refresh_token  TEXT,
  expiry_date    TIMESTAMPTZ,
  tier           TEXT,
  paused         BOOLEAN NOT NULL DEFAULT FALSE,
  is_default     BOOLEAN NOT NULL DEFAULT FALSE,
  scopes         TEXT,
  upstream_url   TEXT,
  claude_profile JSONB,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (provider, account_id)
);
CREATE INDEX IF NOT EXISTS idx_provider_accounts_provider ON provider_accounts (provider);

CREATE TABLE IF NOT EXISTS custom_providers (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  format     TEXT NOT NULL,
  upstream   TEXT NOT NULL,
  models     JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS provider_upstream (
  provider   TEXT PRIMARY KEY,
  upstream   TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS profiles (
  name                TEXT PRIMARY KEY,
  base_url            TEXT        NOT NULL DEFAULT '',
  api_key             TEXT        NOT NULL DEFAULT '',
  model               TEXT        NOT NULL DEFAULT '',
  opus_model          TEXT,
  sonnet_model        TEXT,
  haiku_model         TEXT,
  target              TEXT        NOT NULL DEFAULT '',
  provider            TEXT        NOT NULL DEFAULT '',
  account_ids         TEXT[],
  targets             JSONB,
  passthrough_auth    BOOLEAN     NOT NULL DEFAULT FALSE,
  optimizer_overrides JSONB,
  max_thinking_tokens INT,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS profile_api_keys (
  token        TEXT PRIMARY KEY,
  key_name     TEXT        NOT NULL,
  profile_name TEXT        NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_profile_api_keys_profile ON profile_api_keys (profile_name);

CREATE TABLE IF NOT EXISTS usage_metrics (
  granularity TEXT NOT NULL,
  period      TEXT NOT NULL,
  profile_name TEXT NOT NULL DEFAULT '',
  account_id  TEXT NOT NULL DEFAULT '',
  model       TEXT NOT NULL,
  input_tokens  BIGINT NOT NULL DEFAULT 0,
  output_tokens BIGINT NOT NULL DEFAULT 0,
  requests    BIGINT NOT NULL DEFAULT 0,
  errors      BIGINT NOT NULL DEFAULT 0,
  cost        DOUBLE PRECISION NOT NULL DEFAULT 0,
  expires_at  TIMESTAMPTZ,
  PRIMARY KEY (granularity, period, profile_name, account_id, model)
);

CREATE TABLE IF NOT EXISTS gateway_config (
  key        TEXT PRIMARY KEY,
  value      JSONB NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS bandit_state (
  arm        TEXT PRIMARY KEY,
  a_matrix   JSONB NOT NULL,
  b_vector   JSONB NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
`

// AccountRow mirrors the provider_accounts table.
type AccountRow struct {
	Provider      string          `json:"provider"`
	AccountID     string          `json:"account_id"`
	Email         string          `json:"email,omitempty"`
	AccessToken   string          `json:"access_token"`
	RefreshToken  string          `json:"refresh_token,omitempty"`
	ExpiryDate    time.Time       `json:"expiry_date"`
	Tier          string          `json:"tier,omitempty"`
	Paused        bool            `json:"paused"`
	IsDefault     bool            `json:"is_default"`
	Scopes        string          `json:"scopes,omitempty"`
	UpstreamURL   string          `json:"upstream_url,omitempty"`
	ClaudeProfile json.RawMessage `json:"claude_profile,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// CustomProviderRow mirrors the custom_providers table.
type CustomProviderRow struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Format    string          `json:"format"`
	Upstream  string          `json:"upstream"`
	Models    json.RawMessage `json:"models,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// UpstreamRow mirrors the provider_upstream table.
type UpstreamRow struct {
	Provider  string    `json:"provider"`
	Upstream  string    `json:"upstream"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ProfileRow mirrors the profiles table.
type ProfileRow struct {
	Name               string          `json:"name"`
	BaseURL            string          `json:"base_url"`
	APIKey             string          `json:"api_key"`
	Model              string          `json:"model"`
	OpusModel          string          `json:"opus_model,omitempty"`
	SonnetModel        string          `json:"sonnet_model,omitempty"`
	HaikuModel         string          `json:"haiku_model,omitempty"`
	Target             string          `json:"target"`
	Provider           string          `json:"provider"`
	AccountIDs         []string        `json:"account_ids,omitempty"`
	Targets            json.RawMessage `json:"targets,omitempty"`
	PassthroughAuth    bool            `json:"passthrough_auth"`
	OptimizerOverrides json.RawMessage `json:"optimizer_overrides,omitempty"`
	MaxThinkingTokens  int             `json:"max_thinking_tokens,omitempty"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

// ProfileAPIKeyRow mirrors the profile_api_keys table.
type ProfileAPIKeyRow struct {
	Token       string    `json:"token"`
	KeyName     string    `json:"key_name"`
	ProfileName string    `json:"profile_name"`
	CreatedAt   time.Time `json:"created_at"`
}

// UsageMetricRow mirrors the usage_metrics table.
type UsageMetricRow struct {
	Granularity  string     `json:"granularity"`
	Period       string     `json:"period"`
	ProfileName  string     `json:"profile_name"`
	AccountID    string     `json:"account_id"`
	Model        string     `json:"model"`
	InputTokens  int64      `json:"input_tokens"`
	OutputTokens int64      `json:"output_tokens"`
	Requests     int64      `json:"requests"`
	Errors       int64      `json:"errors"`
	Cost         float64    `json:"cost"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

// GatewayConfigRow mirrors the gateway_config table.
type GatewayConfigRow struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// BanditStateRow mirrors the bandit_state table.
type BanditStateRow struct {
	Arm       string          `json:"arm"`
	AMatrix   json.RawMessage `json:"a_matrix"`
	BVector   json.RawMessage `json:"b_vector"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Store is the durable storage interface for Postgres-backed operations.
// Each method is a no-op when the underlying pool is nil (Postgres disabled).
type Store interface {
	// Token operations
	StoreAccount(ctx context.Context, r AccountRow) error
	GetAccount(ctx context.Context, provider, accountID string) (*AccountRow, error)
	DeleteAccount(ctx context.Context, provider, accountID string) error
	DeleteAccountsByProvider(ctx context.Context, provider string) error
	ListAccountsByProvider(ctx context.Context, provider string) ([]AccountRow, error)
	ListAllAccounts(ctx context.Context) ([]AccountRow, error)
	SetDefaultAccount(ctx context.Context, provider, accountID string) error
	PauseAccount(ctx context.Context, provider, accountID string) error
	ResumeAccount(ctx context.Context, provider, accountID string) error
	UpdateAccountEmail(ctx context.Context, provider, accountID, email string) error

	// Custom provider operations
	StoreCustomProvider(ctx context.Context, r CustomProviderRow) error
	GetCustomProvider(ctx context.Context, id string) (*CustomProviderRow, error)
	DeleteCustomProvider(ctx context.Context, id string) error
	ListCustomProviders(ctx context.Context) ([]CustomProviderRow, error)

	// Upstream override operations
	StoreUpstream(ctx context.Context, r UpstreamRow) error
	GetUpstream(ctx context.Context, provider string) (*UpstreamRow, error)
	DeleteUpstream(ctx context.Context, provider string) error
	ListUpstreams(ctx context.Context) ([]UpstreamRow, error)

	// Profile operations
	StoreProfile(ctx context.Context, r ProfileRow) error
	GetProfileByName(ctx context.Context, name string) (*ProfileRow, error)
	ListProfiles(ctx context.Context) ([]ProfileRow, error)
	DeleteProfile(ctx context.Context, name string) error

	// Profile API key operations
	StoreProfileAPIKey(ctx context.Context, r ProfileAPIKeyRow) error
	ListProfileAPIKeysByProfile(ctx context.Context, profile string) ([]ProfileAPIKeyRow, error)
	ListAllProfileAPIKeys(ctx context.Context) ([]ProfileAPIKeyRow, error)
	DeleteProfileAPIKey(ctx context.Context, token string) error
	DeleteProfileAPIKeysByProfile(ctx context.Context, profile string) error

	// Usage metrics operations
	UpsertUsageMetric(ctx context.Context, r UsageMetricRow) error
	ListUsageMetrics(ctx context.Context, granularity, profileName, accountID string) ([]UsageMetricRow, error)

	// Gateway config operations (key -> JSONB)
	SetGatewayConfig(ctx context.Context, key string, value json.RawMessage) error
	GetGatewayConfig(ctx context.Context, key string) (json.RawMessage, error)
	ListGatewayConfig(ctx context.Context) ([]GatewayConfigRow, error)
	DeleteGatewayConfig(ctx context.Context, key string) error

	// Bandit state operations (arm -> A/B matrices)
	SetBanditState(ctx context.Context, r BanditStateRow) error
	GetBanditState(ctx context.Context, arm string) (*BanditStateRow, error)
	ListBanditStates(ctx context.Context) ([]BanditStateRow, error)

	// Lifecycle
	Close()
}

// PostgresStore implements Store using a pgx connection pool.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// New connects to Postgres, runs schema DDL, and returns a ready Store.
// Returns nil Store (and nil error) when dsn is empty.
func New(ctx context.Context, dsn string) (Store, error) {
	if dsn == "" {
		return nil, nil
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MinConns = 2

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres connect: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}

	if _, err := pool.Exec(ctx, schema); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres schema migrate: %w", err)
	}

	slog.Info("postgres store connected and schema ready")
	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// --- Token operations ---

func (s *PostgresStore) StoreAccount(ctx context.Context, r AccountRow) error {
	if s.pool == nil {
		return nil
	}
	q := `INSERT INTO provider_accounts
	     (provider, account_id, email, access_token, refresh_token, expiry_date,
	      tier, paused, is_default, scopes, upstream_url, claude_profile, created_at, updated_at)
	     VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
	     ON CONFLICT (provider, account_id) DO UPDATE SET
	       email=EXCLUDED.email, access_token=EXCLUDED.access_token,
	       refresh_token=EXCLUDED.refresh_token, expiry_date=EXCLUDED.expiry_date,
	       tier=EXCLUDED.tier, paused=EXCLUDED.paused, is_default=EXCLUDED.is_default,
	       scopes=EXCLUDED.scopes, upstream_url=EXCLUDED.upstream_url,
	       claude_profile=EXCLUDED.claude_profile, updated_at=now()`
	_, err := s.pool.Exec(ctx, q,
		r.Provider, r.AccountID, r.Email, r.AccessToken, r.RefreshToken, r.ExpiryDate,
		r.Tier, r.Paused, r.IsDefault, r.Scopes, r.UpstreamURL, r.ClaudeProfile,
		r.CreatedAt, r.UpdatedAt)
	return err
}

func (s *PostgresStore) GetAccount(ctx context.Context, provider, accountID string) (*AccountRow, error) {
	if s.pool == nil {
		return nil, nil
	}
	r := &AccountRow{}
	q := `SELECT provider,account_id,email,access_token,refresh_token,expiry_date,
	          tier,paused,is_default,scopes,upstream_url,claude_profile,created_at,updated_at
	          FROM provider_accounts WHERE provider=$1 AND account_id=$2`
	err := s.pool.QueryRow(ctx, q, provider, accountID).Scan(
		&r.Provider, &r.AccountID, &r.Email, &r.AccessToken, &r.RefreshToken,
		&r.ExpiryDate, &r.Tier, &r.Paused, &r.IsDefault, &r.Scopes, &r.UpstreamURL,
		&r.ClaudeProfile, &r.CreatedAt, &r.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *PostgresStore) DeleteAccount(ctx context.Context, provider, accountID string) error {
	if s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM provider_accounts WHERE provider=$1 AND account_id=$2`, provider, accountID)
	return err
}

func (s *PostgresStore) DeleteAccountsByProvider(ctx context.Context, provider string) error {
	if s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM provider_accounts WHERE provider=$1`, provider)
	return err
}

func (s *PostgresStore) ListAccountsByProvider(ctx context.Context, provider string) ([]AccountRow, error) {
	if s.pool == nil {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT provider,account_id,email,access_token,refresh_token,expiry_date,
		        tier,paused,is_default,scopes,upstream_url,claude_profile,created_at,updated_at
		        FROM provider_accounts WHERE provider=$1`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAccountRows(rows)
}

func (s *PostgresStore) ListAllAccounts(ctx context.Context) ([]AccountRow, error) {
	if s.pool == nil {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT provider,account_id,email,access_token,refresh_token,expiry_date,
		        tier,paused,is_default,scopes,upstream_url,claude_profile,created_at,updated_at
		        FROM provider_accounts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAccountRows(rows)
}

func scanAccountRows(rows pgx.Rows) ([]AccountRow, error) {
	var out []AccountRow
	for rows.Next() {
		var r AccountRow
		if err := rows.Scan(
			&r.Provider, &r.AccountID, &r.Email, &r.AccessToken, &r.RefreshToken,
			&r.ExpiryDate, &r.Tier, &r.Paused, &r.IsDefault, &r.Scopes, &r.UpstreamURL,
			&r.ClaudeProfile, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *PostgresStore) SetDefaultAccount(ctx context.Context, provider, accountID string) error {
	if s.pool == nil {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Check if target is already default.
	var already bool
	err = tx.QueryRow(ctx,
		`SELECT is_default FROM provider_accounts WHERE provider=$1 AND account_id=$2`,
		provider, accountID).Scan(&already)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	if already {
		// Clear all defaults for this provider.
		_, err = tx.Exec(ctx, `UPDATE provider_accounts SET is_default=false,updated_at=now() WHERE provider=$1 AND is_default=true`, provider)
	} else {
		_, err = tx.Exec(ctx, `UPDATE provider_accounts SET is_default=false,updated_at=now() WHERE provider=$1 AND is_default=true`, provider)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE provider_accounts SET is_default=true,updated_at=now() WHERE provider=$1 AND account_id=$2`, provider, accountID)
	}
	return err
}

func (s *PostgresStore) PauseAccount(ctx context.Context, provider, accountID string) error {
	if s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `UPDATE provider_accounts SET paused=true,updated_at=now() WHERE provider=$1 AND account_id=$2`, provider, accountID)
	return err
}

func (s *PostgresStore) ResumeAccount(ctx context.Context, provider, accountID string) error {
	if s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `UPDATE provider_accounts SET paused=false,updated_at=now() WHERE provider=$1 AND account_id=$2`, provider, accountID)
	return err
}

func (s *PostgresStore) UpdateAccountEmail(ctx context.Context, provider, accountID, email string) error {
	if s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `UPDATE provider_accounts SET email=$3,updated_at=now() WHERE provider=$1 AND account_id=$2`, provider, accountID, email)
	return err
}

// --- Custom provider operations ---

func (s *PostgresStore) StoreCustomProvider(ctx context.Context, r CustomProviderRow) error {
	if s.pool == nil {
		return nil
	}
	q := `INSERT INTO custom_providers (id,name,format,upstream,models,created_at)
	     VALUES ($1,$2,$3,$4,$5,$6)
	     ON CONFLICT (id) DO UPDATE SET
	       name=EXCLUDED.name, format=EXCLUDED.format, upstream=EXCLUDED.upstream, models=EXCLUDED.models`
	_, err := s.pool.Exec(ctx, q, r.ID, r.Name, r.Format, r.Upstream, r.Models, r.CreatedAt)
	return err
}

func (s *PostgresStore) GetCustomProvider(ctx context.Context, id string) (*CustomProviderRow, error) {
	if s.pool == nil {
		return nil, nil
	}
	r := &CustomProviderRow{}
	err := s.pool.QueryRow(ctx,
		`SELECT id,name,format,upstream,models,created_at FROM custom_providers WHERE id=$1`, id).
		Scan(&r.ID, &r.Name, &r.Format, &r.Upstream, &r.Models, &r.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *PostgresStore) DeleteCustomProvider(ctx context.Context, id string) error {
	if s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM custom_providers WHERE id=$1`, id)
	return err
}

func (s *PostgresStore) ListCustomProviders(ctx context.Context) ([]CustomProviderRow, error) {
	if s.pool == nil {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id,name,format,upstream,models,created_at FROM custom_providers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CustomProviderRow
	for rows.Next() {
		var r CustomProviderRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Format, &r.Upstream, &r.Models, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// --- Upstream override operations ---

func (s *PostgresStore) StoreUpstream(ctx context.Context, r UpstreamRow) error {
	if s.pool == nil {
		return nil
	}
	q := `INSERT INTO provider_upstream (provider,upstream,updated_at)
	     VALUES ($1,$2,$3)
	     ON CONFLICT (provider) DO UPDATE SET upstream=EXCLUDED.upstream, updated_at=now()`
	_, err := s.pool.Exec(ctx, q, r.Provider, r.Upstream, r.UpdatedAt)
	return err
}

func (s *PostgresStore) GetUpstream(ctx context.Context, provider string) (*UpstreamRow, error) {
	if s.pool == nil {
		return nil, nil
	}
	r := &UpstreamRow{}
	err := s.pool.QueryRow(ctx,
		`SELECT provider,upstream,updated_at FROM provider_upstream WHERE provider=$1`, provider).
		Scan(&r.Provider, &r.Upstream, &r.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *PostgresStore) DeleteUpstream(ctx context.Context, provider string) error {
	if s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM provider_upstream WHERE provider=$1`, provider)
	return err
}

func (s *PostgresStore) ListUpstreams(ctx context.Context) ([]UpstreamRow, error) {
	if s.pool == nil {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT provider,upstream,updated_at FROM provider_upstream`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UpstreamRow
	for rows.Next() {
		var r UpstreamRow
		if err := rows.Scan(&r.Provider, &r.Upstream, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// --- Profile operations ---

func (s *PostgresStore) StoreProfile(ctx context.Context, r ProfileRow) error {
	if s.pool == nil {
		return nil
	}
	q := `INSERT INTO profiles
	    (name, base_url, api_key, model, opus_model, sonnet_model, haiku_model,
	     target, provider, account_ids, targets, passthrough_auth,
	     optimizer_overrides, max_thinking_tokens, created_at, updated_at)
	    VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
	    ON CONFLICT (name) DO UPDATE SET
	      base_url=EXCLUDED.base_url, api_key=EXCLUDED.api_key, model=EXCLUDED.model,
	      opus_model=EXCLUDED.opus_model, sonnet_model=EXCLUDED.sonnet_model, haiku_model=EXCLUDED.haiku_model,
	      target=EXCLUDED.target, provider=EXCLUDED.provider, account_ids=EXCLUDED.account_ids,
	      targets=EXCLUDED.targets, passthrough_auth=EXCLUDED.passthrough_auth,
	      optimizer_overrides=EXCLUDED.optimizer_overrides, max_thinking_tokens=EXCLUDED.max_thinking_tokens,
	      updated_at=now()`
	_, err := s.pool.Exec(ctx, q,
		r.Name, r.BaseURL, r.APIKey, r.Model, r.OpusModel, r.SonnetModel, r.HaikuModel,
		r.Target, r.Provider, r.AccountIDs, r.Targets, r.PassthroughAuth,
		r.OptimizerOverrides, r.MaxThinkingTokens, r.CreatedAt, r.UpdatedAt)
	return err
}

const profileCols = `name,base_url,api_key,model,opus_model,sonnet_model,haiku_model,
       target,provider,account_ids,targets,passthrough_auth,optimizer_overrides,
       max_thinking_tokens,created_at,updated_at`

func scanProfile(r *ProfileRow, rows pgx.Rows) error {
	return rows.Scan(
		&r.Name, &r.BaseURL, &r.APIKey, &r.Model, &r.OpusModel, &r.SonnetModel, &r.HaikuModel,
		&r.Target, &r.Provider, &r.AccountIDs, &r.Targets, &r.PassthroughAuth,
		&r.OptimizerOverrides, &r.MaxThinkingTokens, &r.CreatedAt, &r.UpdatedAt)
}

func (s *PostgresStore) GetProfileByName(ctx context.Context, name string) (*ProfileRow, error) {
	if s.pool == nil {
		return nil, nil
	}
	r := &ProfileRow{}
	err := s.pool.QueryRow(ctx, `SELECT `+profileCols+` FROM profiles WHERE name=$1`, name).Scan(
		&r.Name, &r.BaseURL, &r.APIKey, &r.Model, &r.OpusModel, &r.SonnetModel, &r.HaikuModel,
		&r.Target, &r.Provider, &r.AccountIDs, &r.Targets, &r.PassthroughAuth,
		&r.OptimizerOverrides, &r.MaxThinkingTokens, &r.CreatedAt, &r.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *PostgresStore) ListProfiles(ctx context.Context) ([]ProfileRow, error) {
	if s.pool == nil {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT `+profileCols+` FROM profiles`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProfileRow
	for rows.Next() {
		var r ProfileRow
		if err := scanProfile(&r, rows); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *PostgresStore) DeleteProfile(ctx context.Context, name string) error {
	if s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM profiles WHERE name=$1`, name)
	return err
}

// --- Profile API key operations ---

func (s *PostgresStore) StoreProfileAPIKey(ctx context.Context, r ProfileAPIKeyRow) error {
	if s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO profile_api_keys (token, key_name, profile_name, created_at)
		 VALUES ($1,$2,$3,$4)
		 ON CONFLICT (token) DO UPDATE SET key_name=EXCLUDED.key_name, profile_name=EXCLUDED.profile_name`,
		r.Token, r.KeyName, r.ProfileName, r.CreatedAt)
	return err
}

func scanProfileAPIKeyRows(rows pgx.Rows) ([]ProfileAPIKeyRow, error) {
	var out []ProfileAPIKeyRow
	for rows.Next() {
		var r ProfileAPIKeyRow
		if err := rows.Scan(&r.Token, &r.KeyName, &r.ProfileName, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *PostgresStore) ListProfileAPIKeysByProfile(ctx context.Context, profile string) ([]ProfileAPIKeyRow, error) {
	if s.pool == nil {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT token,key_name,profile_name,created_at FROM profile_api_keys WHERE profile_name=$1`, profile)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProfileAPIKeyRows(rows)
}

func (s *PostgresStore) ListAllProfileAPIKeys(ctx context.Context) ([]ProfileAPIKeyRow, error) {
	if s.pool == nil {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT token,key_name,profile_name,created_at FROM profile_api_keys`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProfileAPIKeyRows(rows)
}

func (s *PostgresStore) DeleteProfileAPIKey(ctx context.Context, token string) error {
	if s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM profile_api_keys WHERE token=$1`, token)
	return err
}

func (s *PostgresStore) DeleteProfileAPIKeysByProfile(ctx context.Context, profile string) error {
	if s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM profile_api_keys WHERE profile_name=$1`, profile)
	return err
}

// --- Usage metrics operations (additive upsert: callers pass deltas) ---

func (s *PostgresStore) UpsertUsageMetric(ctx context.Context, r UsageMetricRow) error {
	if s.pool == nil {
		return nil
	}
	q := `INSERT INTO usage_metrics
	    (granularity, period, profile_name, account_id, model,
	     input_tokens, output_tokens, requests, errors, cost, expires_at)
	    VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	    ON CONFLICT (granularity, period, profile_name, account_id, model) DO UPDATE SET
	      input_tokens = usage_metrics.input_tokens + EXCLUDED.input_tokens,
	      output_tokens = usage_metrics.output_tokens + EXCLUDED.output_tokens,
	      requests = usage_metrics.requests + EXCLUDED.requests,
	      errors = usage_metrics.errors + EXCLUDED.errors,
	      cost = usage_metrics.cost + EXCLUDED.cost,
	      expires_at = EXCLUDED.expires_at`
	_, err := s.pool.Exec(ctx, q,
		r.Granularity, r.Period, r.ProfileName, r.AccountID, r.Model,
		r.InputTokens, r.OutputTokens, r.Requests, r.Errors, r.Cost, r.ExpiresAt)
	return err
}

func (s *PostgresStore) ListUsageMetrics(ctx context.Context, granularity, profileName, accountID string) ([]UsageMetricRow, error) {
	if s.pool == nil {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT granularity,period,profile_name,account_id,model,
		        input_tokens,output_tokens,requests,errors,cost,expires_at
		 FROM usage_metrics
		 WHERE granularity=$1 AND COALESCE(profile_name,'')=$2 AND COALESCE(account_id,'')=$3`,
		granularity, profileName, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageMetricRow
	for rows.Next() {
		var r UsageMetricRow
		if err := rows.Scan(&r.Granularity, &r.Period, &r.ProfileName, &r.AccountID, &r.Model,
			&r.InputTokens, &r.OutputTokens, &r.Requests, &r.Errors, &r.Cost, &r.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// --- Gateway config operations (key -> JSONB) ---

func (s *PostgresStore) SetGatewayConfig(ctx context.Context, key string, value json.RawMessage) error {
	if s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO gateway_config (key, value, updated_at) VALUES ($1,$2,now())
		 ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, updated_at=now()`,
		key, value)
	return err
}

func (s *PostgresStore) GetGatewayConfig(ctx context.Context, key string) (json.RawMessage, error) {
	if s.pool == nil {
		return nil, nil
	}
	var v json.RawMessage
	err := s.pool.QueryRow(ctx, `SELECT value FROM gateway_config WHERE key=$1`, key).Scan(&v)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (s *PostgresStore) ListGatewayConfig(ctx context.Context) ([]GatewayConfigRow, error) {
	if s.pool == nil {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT key,value,updated_at FROM gateway_config`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GatewayConfigRow
	for rows.Next() {
		var r GatewayConfigRow
		if err := rows.Scan(&r.Key, &r.Value, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *PostgresStore) DeleteGatewayConfig(ctx context.Context, key string) error {
	if s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM gateway_config WHERE key=$1`, key)
	return err
}

// --- Bandit state operations (arm -> A/B matrices) ---

func (s *PostgresStore) SetBanditState(ctx context.Context, r BanditStateRow) error {
	if s.pool == nil {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO bandit_state (arm, a_matrix, b_vector, updated_at) VALUES ($1,$2,$3,now())
		 ON CONFLICT (arm) DO UPDATE SET a_matrix=EXCLUDED.a_matrix, b_vector=EXCLUDED.b_vector, updated_at=now()`,
		r.Arm, r.AMatrix, r.BVector)
	return err
}

func (s *PostgresStore) GetBanditState(ctx context.Context, arm string) (*BanditStateRow, error) {
	if s.pool == nil {
		return nil, nil
	}
	r := &BanditStateRow{}
	err := s.pool.QueryRow(ctx, `SELECT arm,a_matrix,b_vector,updated_at FROM bandit_state WHERE arm=$1`, arm).
		Scan(&r.Arm, &r.AMatrix, &r.BVector, &r.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

func (s *PostgresStore) ListBanditStates(ctx context.Context) ([]BanditStateRow, error) {
	if s.pool == nil {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT arm,a_matrix,b_vector,updated_at FROM bandit_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BanditStateRow
	for rows.Next() {
		var r BanditStateRow
		if err := rows.Scan(&r.Arm, &r.AMatrix, &r.BVector, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
