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
