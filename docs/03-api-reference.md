# API Reference

## API Gateway (`:8080`)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/messages` | Anthropic-compatible sync proxy (Claude Code) |
| `POST` | `/v1/chat/completions` | Async queue mode (batch agents) |
| `GET` | `/v1/results/{id}` | Get async job result |
| `GET` | `/health` | Health check |
| `GET` | `/metrics` | Prometheus metrics |
| `GET` | `/admin` | Management dashboard (SPA, API key auth) |
| `GET` | `/admin/*` | SPA sub-routes (fallback to index.html) |
| `GET` | `/v1/limiter-status` | Adaptive limiter state (requires x-api-key) |
| `POST` | `/v1/limiter-override` | Set/clear model concurrency limit (requires x-api-key) |
| `GET` | `/v1/profiles` | List all profiles |
| `POST` | `/v1/profiles` | Create profile (name + target only) |
| `GET` | `/v1/profiles/{name}` | Get profile by name |
| `PUT` | `/v1/profiles/{name}` | Update profile |
| `DELETE` | `/v1/profiles/{name}` | Delete profile |
| `POST` | `/v1/profiles/{name}/copy` | Copy profile |
| `POST` | `/v1/profiles/{name}/export` | Export profile (API key redacted) |
| `POST` | `/v1/profiles/import` | Import profile |
| `POST` | `/v1/auth/{provider}/start` | Start OAuth flow (device code / auth code) |
| `GET` | `/v1/auth/{provider}/callback` | OAuth callback endpoint |
| `GET` | `/v1/auth/{provider}/status` | Check OAuth status |
| `POST` | `/v1/auth/{provider}/register` | Register API key / session cookie |
| `GET` | `/v1/auth/accounts` | List all accounts |
| `GET` | `/v1/auth/accounts/{provider}` | List accounts by provider |
| `DELETE` | `/v1/auth/accounts/{provider}/{accountId}` | Delete account |
| `POST` | `/v1/auth/accounts/{provider}/{accountId}/pause` | Pause account |
| `POST` | `/v1/auth/accounts/{provider}/{accountId}/resume` | Resume account |
| `POST` | `/v1/auth/accounts/{provider}/{accountId}/default` | Set as default account |
| `POST` | `/v1/auth/accounts/{provider}/{accountId}/email` | Update account email |
| `GET` | `/v1/providers` | List all providers |

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
