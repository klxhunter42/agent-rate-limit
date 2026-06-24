# Storage Tiering: Postgres (durable) vs Dragonfly (ephemeral)

- Created: 2026-06-23
- Related: [Provider System](17-provider-system.md), [Distributed Rate Limiter](22-distributed-rate-limiter.md), [Cache & Privacy Filter](20-cache-privacy-filter.md)

---

## 1. Problem

Today every piece of state lives in **Dragonfly** (Redis-compatible). Dragonfly is an in-memory store with **no persistence by default** in this stack (the compose service has no AOF/RDB volume). If Dragonfly restarts or loses memory, **provider OAuth tokens and custom provider config vanish** — forcing re-auth of every account.

Goal: introduce an existing **Postgres** for data that must survive a crash, keep Dragonfly for hot/ephemeral data that can be recomputed.

---

## 2. Current state — every key is `arl:*` on Dragonfly

Inventory of Redis key prefixes (from `api-gateway/**/*.go`):

| Prefix | Stored value | Shape |
|---|---|---|
| `arl:tokens:<provider>:<account_id>` | OAuth access/refresh token, API key, account metadata | JSON `TokenInfo` + set index `arl:tokens:<provider>:_index` |
| `arl:providers:custom:<id>` | User-created custom provider (name, upstream, models) | JSON |
| `arl:provider_upstream:<provider>` | Per-provider upstream URL override | string |
| `arl:ratelimit:<provider>:<account_id>` | Cached 5h utilization + status | JSON `AcctRateLimit` |
| `arl:mcp:ratelimit:...` | MCP tool rate-limit counters | counters |
| `arl:mcp:cache:web_reader:web:<hash>` | MCP web_reader response cache | string |
| `arl:usage:...` | Usage / billing counters | counters / JSON |

Owner code:
- `provider/token-store.go` — `arl:tokens:*`
- `provider/handler.go` — `arl:providers:custom:*`, `arl:provider_upstream:*`
- `provider/token-store.go` (`GetRateLimits`) — `arl:ratelimit:*`
- `proxy/mcp.go` — `arl:mcp:*`
- `handler/usage.go`, `handler/quota.go` — `arl:usage:*`

---

## 3. Tiering recommendation

### Move to Postgres (durable — must not lose)

| Data | Why durable |
|---|---|
| **Provider tokens** (`arl:tokens:*`) | OAuth refresh tokens + API keys = account credentials. Loss = full re-OAuth every account. |
| **Custom providers** (`arl:providers:custom:*`) | User-created config (name, upstream, models). Loss = silent breakage of all profiles using them. |
| **Provider upstream overrides** (`arl:provider_upstream:*`) | Operator config. Loss = reverts to default upstream. |
| **Provider profiles** (default / paused flags, today embedded in token JSON) | Routing config tied to accounts. |

### Keep on Dragonfly (ephemeral — recompute / TTL OK)

| Data | Why ephemeral |
|---|---|
| **Rate-limit status** (`arl:ratelimit:*`, `arl:mcp:ratelimit:*`) | Rolling 5h window, refreshed by the rate-limiter; TTL/expiry is the whole point. |
| **In-flight / concurrency counters** | Per-request, self-healing on restart. |
| **MCP cache** (`arl:mcp:cache:*`) | Pure cache, misses just re-fetch. |
| **Pub/Sub channels** (ws events) | Real-time only, no persistence desired. |
| **compcache / warmstart / eviction** (`arl:cache:*`) | Optimization caches. |

### Needs a decision

| Data | Notes |
|---|---|
| **Usage / billing** (`arl:usage:*`) | If used for billing or long-term analytics -> Postgres. If only for live dashboards + re-derivable from logs -> Dragonfly. **Recommend Postgres** (billing accuracy). |

---

## 4. Proposed Postgres schema (sketch)

```sql
-- Provider OAuth/API-key credentials (was arl:tokens:*)
CREATE TABLE provider_accounts (
  provider       TEXT NOT NULL,
  account_id     TEXT NOT NULL,
  email          TEXT,
  access_token   TEXT NOT NULL,
  refresh_token  TEXT,
  expiry_date    TIMESTAMPTZ,
  tier           TEXT,
  paused         BOOLEAN NOT NULL DEFAULT FALSE,
  is_default     BOOLEAN NOT NULL DEFAULT FALSE,
  scopes         TEXT,
  upstream_url   TEXT,
  claude_profile JSONB,           -- ClaudeProfile block
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (provider, account_id)
);
CREATE INDEX ON provider_accounts (provider);

-- Custom providers (was arl:providers:custom:*)
CREATE TABLE custom_providers (
  id        TEXT PRIMARY KEY,
  name      TEXT NOT NULL,
  format    TEXT NOT NULL,
  upstream  TEXT NOT NULL,
  models    JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Per-provider upstream override (was arl:provider_upstream:*)
CREATE TABLE provider_upstream (
  provider TEXT PRIMARY KEY,
  upstream TEXT NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Usage (if promoted to durable)
CREATE TABLE usage_events (
  id          BIGSERIAL PRIMARY KEY,
  ts          TIMESTAMPTZ NOT NULL DEFAULT now(),
  provider    TEXT,
  account_id  TEXT,
  profile     TEXT,
  model       TEXT,
  input_tokens  INT,
  output_tokens INT,
  cost_usd    NUMERIC(10,4)
);
CREATE INDEX ON usage_events (ts DESC);
```

---

## 5. Migration plan

1. **Add Postgres service** to `docker-compose.yml` (`arl-postgres`, with a volume).
2. **New package** `api-gateway/store/` — a `Store` interface with a Postgres impl; `TokenStore` gains a Postgres backend behind the same methods (`Store`, `Get`, `ListByProvider`, `SetDefault`, `Pause`, ...).
3. **Dual-write + read-through** during migration: writes go to Postgres (source of truth) AND Dragonfly (hot cache); reads hit Dragonfly first, fall back to Postgres.
4. **Backfill**: one-shot job reads all `arl:tokens:*` / `arl:providers:custom:*` from Dragonfly and inserts into Postgres.
5. **Cutover**: once verified, stop dual-write to Dragonfly for durable keys; keep Dragonfly only for `arl:ratelimit:*`, `arl:mcp:*`, `arl:cache:*`.
6. **Encrypt at rest**: `access_token` / `refresh_token` columns should be encrypted (app-level or pgcrypto) since they're credentials.

---

## 6. What stays untouched

Rate-limiting path (`arl:ratelimit:*`, `arl:mcp:ratelimit:*`), all caches (`arl:mcp:cache:*`, compcache, warmstart), and pub/sub remain on Dragonfly — they are hot, TTL-driven, and recompute on miss. No durability requirement.

---

## 7. Decision log

| Date | Decision |
|---|---|
| 2026-06-23 | Classify `arl:tokens:*`, `arl:providers:custom:*`, `arl:provider_upstream:*` as **durable -> Postgres**. Rate-limit + cache stay on Dragonfly. Usage **(pending)** — recommend Postgres for billing accuracy. |
