# API Reference

## API Gateway (`:8080`)

### Core Proxy Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/messages` | Anthropic-compatible sync proxy (main endpoint for Claude Code) |
| `POST` | `/v1/chat/completions` | Async queue mode (batch agents) |
| `GET` | `/v1/results/{requestID}` | Get async job result |
| `GET` | `/health` | Health check (queue depth, uptime) |
| `GET` | `/metrics` | Prometheus metrics |
| `GET` | `/api/metrics` | Prometheus metrics (alternate path) |

### Claude Code CLI Passthrough

| Method | Path | Description |
|--------|------|-------------|
| `ANY` | `/api/claude_code/policy_limits` | Proxy to Anthropic policy_limits endpoint |
| `ANY` | `/api/claude_code/settings` | Proxy to Anthropic settings endpoint |
| `ANY` | `/v1/mcp_servers` | Proxy to Anthropic MCP servers endpoint |

### Models

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/models` | List available models (auto-detects Claude CLI User-Agent) |
| `POST` | `/v1/messages/count_tokens` | Count tokens via upstream Z.AI API |

### Adaptive Limiter

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/limiter-status` | Adaptive limiter state (models, key pool, GLM mode) |
| `POST` | `/v1/limiter-override` | Set/clear model concurrency limit |

### Routing

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/routing/strategy` | Get current routing strategy (round-robin / fill-first) |
| `PUT` | `/v1/routing/strategy` | Set routing strategy |

### Error Logs

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/logs/errors` | Get recent error log entries (last 100) |
| `GET` | `/v1/logs/errors/count` | Get error log total and buffered count |

### Rate Limits (per-account)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/auth/accounts/ratelimits` | Get cached Anthropic rate limit utilization for all accounts |

### ZAI Web Chat (if enabled)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/zaiweb/status` | Get ZAI web chat token status |
| `POST` | `/v1/zaiweb/token` | Update ZAI web chat JWT token |
| `POST` | `/v1/images/generations` | Proxy image generation to image.z.ai |
| `POST` | `/v1/audio/tts` | Proxy TTS to audio.z.ai |

### Token Optimization

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/waste/findings` | Get waste detection findings |

### Mock Data (Grafana testing)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/mock/seed` | Seed mock metrics data |
| `POST` | `/v1/mock/loop/start` | Start mock metrics loop |
| `POST` | `/v1/mock/loop/stop` | Stop mock metrics loop |
| `GET` | `/v1/mock/status` | Get mock loop status |

### OAuth Auth Callback

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/callback` | Claude OAuth loopback callback (redirect_uri) |
| `POST` | `/callback` | Claude OAuth callback (POST variant) |

### Provider Auth

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/auth/{provider}/start` | Start OAuth flow (device code / auth code) |
| `POST` | `/v1/auth/{provider}/start-url` | Get OAuth start URL (for browser redirect) |
| `POST` | `/v1/auth/{provider}/register` | Register API key / session cookie |
| `GET` | `/v1/auth/{provider}/callback` | OAuth callback endpoint |
| `POST` | `/v1/auth/{provider}/callback` | OAuth callback (POST variant) |
| `GET` | `/v1/auth/{provider}/status` | Check OAuth status (polling) |
| `POST` | `/v1/auth/{provider}/cancel` | Cancel in-progress OAuth flow |
| `GET` | `/v1/auth/accounts` | List all accounts across providers |
| `GET` | `/v1/auth/accounts/{provider}` | List accounts by provider |
| `DELETE` | `/v1/auth/accounts/{provider}/{accountId}` | Delete account |
| `POST` | `/v1/auth/accounts/{provider}/{accountId}/pause` | Pause account |
| `POST` | `/v1/auth/accounts/{provider}/{accountId}/resume` | Resume account |
| `POST` | `/v1/auth/accounts/{provider}/{accountId}/default` | Set as default account |
| `POST` | `/v1/auth/accounts/{provider}/{accountId}/email` | Update account email |
| `POST` | `/v1/auth/accounts/{provider}/{accountId}/refresh` | Force token refresh |
| `POST` | `/v1/auth/login` | Dashboard login |
| `POST` | `/v1/auth/logout` | Dashboard logout |
| `GET` | `/v1/auth/check` | Check dashboard auth status |

### Providers

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/providers` | List all providers |
| `PUT` | `/v1/providers/{provider}/upstream` | Update provider upstream URL |
| `POST` | `/v1/providers/custom` | Create custom provider |
| `DELETE` | `/v1/providers/custom/{provider}` | Delete custom provider |

### Profiles

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/profiles` | List all profiles |
| `POST` | `/v1/profiles` | Create profile |
| `POST` | `/v1/profiles/delete` | Delete profile by name (body: `{name}`) |
| `POST` | `/v1/profiles/import` | Import profile |
| `GET` | `/v1/profiles/recommended-models` | Get recommended models for a target provider |
| `GET` | `/v1/profiles/{name}` | Get profile by name |
| `PUT` | `/v1/profiles/{name}` | Update profile |
| `DELETE` | `/v1/profiles/{name}` | Delete profile |
| `POST` | `/v1/profiles/{name}/copy` | Copy profile |
| `GET` | `/v1/profiles/{name}/export` | Export profile (API key redacted) |
| `POST` | `/v1/profiles/{name}/export` | Export profile (POST variant) |
| `GET` | `/v1/profiles/{name}/tokens` | List profile API tokens |
| `POST` | `/v1/profiles/{name}/tokens` | Generate profile API token |
| `DELETE` | `/v1/profiles/{name}/tokens/{keyName}` | Revoke profile API token |

### Usage

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/usage/summary` | Usage summary |
| `GET` | `/v1/usage/hourly` | Hourly usage |
| `GET` | `/v1/usage/daily` | Daily usage |
| `GET` | `/v1/usage/models` | Per-model usage |
| `GET` | `/v1/usage/sessions` | Session usage |
| `GET` | `/v1/usage/monthly` | Monthly usage |
| `GET` | `/v1/usage/profiles` | Per-profile usage |
| `GET` | `/v1/usage/profiles/{name}` | Per-profile usage by name |
| `GET` | `/v1/usage/accounts` | Per-account usage |
| `GET` | `/v1/usage/accounts/{accountId}` | Per-account usage by ID |

### Quota

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/quota/{provider}` | Get provider quota |
| `GET` | `/v1/quota/{provider}/{accountId}` | Get account quota |

### Overview & Health

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/overview` | System overview (tokens, services, health checks) |
| `GET` | `/v1/health/detailed` | Detailed health check (Dragonfly, Rate Limiter, providers) |
| `POST` | `/v1/health/fix/{checkId}` | Fix a health check issue |

### Config

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/config` | Get gateway config |
| `GET` | `/v1/config/raw` | Get raw config dump |
| `PUT` | `/v1/config` | Update config values |
| `GET` | `/v1/thinking` | Get thinking/budget token settings |
| `PUT` | `/v1/thinking` | Update thinking settings |
| `GET` | `/v1/global-env` | Get global environment config |
| `PUT` | `/v1/global-env` | Update global environment config |
| `GET` | `/v1/max-tokens` | Get per-model max_tokens defaults |
| `PUT` | `/v1/max-tokens` | Update per-model max_tokens overrides |

### Dashboard (SPA)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/` | Management dashboard SPA (requires DASHBOARD_PASSWORD) |
| `GET` | `/*` | SPA sub-routes (fallback to index.html) |
| `GET` | `/assets/*` | Static assets (JS, CSS) |

### WebSocket

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/ws` | WebSocket for real-time events (config changes, anomalies, quota warnings) |

## Rate Limiter (`:8080` internal)

### Rate Limit Check

```bash
# POST /api/ratelimit/check
docker exec arl-rate-limiter curl -s -X POST http://localhost:8080/api/ratelimit/check \
  -H "Content-Type: application/json" \
  -d '{"key": "test-user"}'
```

### Rate Limit Config

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/ratelimit/config` | View current config |
| `POST` | `/api/ratelimit/config/keys/{key}` | Set per-key rate limit |
| `POST` | `/api/ratelimit/config/patterns/{pattern}` | Set pattern-based rate limit |
| `POST` | `/api/ratelimit/config/default` | Set default rate limit |
| `DELETE` | `/api/ratelimit/config/keys/{key}` | Delete per-key config |
| `POST` | `/api/ratelimit/config/reload` | Reload config from properties |
| `GET` | `/api/ratelimit/config/stats` | View rate limit statistics |

### Admin

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/admin/limits/{key}` | View token bucket state for key |
| `PUT` | `/admin/limits/{key}` | Modify token bucket for key |
| `DELETE` | `/admin/limits/{key}` | Delete token bucket |
| `GET` | `/admin/keys` | List all keys in system |

### Adaptive Rate Limiting

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/ratelimit/adaptive/{key}/status` | View adaptive status |
| `POST` | `/api/ratelimit/adaptive/{key}/override` | Override rate limit |
| `DELETE` | `/api/ratelimit/adaptive/{key}/override` | Delete override |
| `GET` | `/api/ratelimit/adaptive/config` | View adaptive config |

### Scheduled Rate Limits

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/ratelimit/schedule` | Create schedule |
| `GET` | `/api/ratelimit/schedule` | List all schedules |
| `PUT` | `/api/ratelimit/schedule/{name}` | Update schedule |
| `DELETE` | `/api/ratelimit/schedule/{name}` | Delete schedule |
| `POST` | `/api/ratelimit/schedule/{name}/activate` | Activate schedule |
| `POST` | `/api/ratelimit/schedule/emergency` | Create emergency rate limit |

### Examples

```bash
# List all keys
docker exec arl-rate-limiter curl -s http://localhost:8080/admin/keys

# View token bucket state
docker exec arl-rate-limiter curl -s http://localhost:8080/admin/limits/my-api-key

# Set per-key rate limit
docker exec arl-rate-limiter curl -s -X POST \
  http://localhost:8080/api/ratelimit/config/keys/my-key \
  -H "Content-Type: application/json" \
  -d '{"capacity": 50, "refillRate": 10}'

# Set default rate limit
docker exec arl-rate-limiter curl -s -X POST \
  http://localhost:8080/api/ratelimit/config/default \
  -H "Content-Type: application/json" \
  -d '{"capacity": 2000, "refillRate": 200}'

# Create schedule (reduce rate limit during peak)
docker exec arl-rate-limiter curl -s -X POST \
  http://localhost:8080/api/ratelimit/schedule \
  -H "Content-Type: application/json" \
  -d '{"name": "peak-hours", "cronExpression": "0 9 * * 1-5", "capacity": 500, "refillRate": 50, "active": true}'

# Health check
docker exec arl-rate-limiter curl -s http://localhost:8080/actuator/health
```

---

*Back to [Manual](../MANUAL.md)*
