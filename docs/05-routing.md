# Profile-Based Routing

Profile is a set of configuration for connecting to a provider, stored in Redis. When sending `X-Profile` header with a request, the gateway loads the profile and uses its settings to route the request to the target provider.

## How It Works

```
Request with X-Profile: my-profile
|
v
Handler.Messages()
|-- Read X-Profile header
|-- Load profile:{name} from Redis
|
+-- Profile found:
|   |-- Use target provider of the profile
|   |-- Get token from provider token pool
|   |   |-- Has accountIds: select from pool (specific accounts only)
|   |   |-- No accountIds: use default token of provider
|   |-- Proxy directly to provider upstream
|   |-- Skip key pool + model fallback logic
|
+-- Profile not found:
    |-- Log warning
    |-- Use normal routing (key pool + adaptive limiter)
```

## Creating a Profile

Profile create form is simple — requires only **name** + **target provider**:

```
Dashboard UI: /admin/profiles -> New
1. Name: profile name (required)
2. Target: select provider from dropdown (required)
3. Account Pool: select accounts to use (optional, empty = use all)
```

No need to enter base URL, model, or API key manually — gateway pulls from provider registry and token pool automatically.

## Usage Examples

```bash
# Create profile
curl -X POST http://localhost:8080/v1/profiles \
  -H "Content-Type: application/json" \
  -d '{"name": "my-claude", "target": "claude-oauth"}'

# Use profile in request
curl -X POST http://localhost:8080/v1/messages \
  -H "X-Profile: my-claude" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet-4-20250514","max_tokens":100,"messages":[...]}'

# Use with Claude Code (apiKeyHelper)
# ~/.claude/settings.json
{
  "env": { "ANTHROPIC_BASE_URL": "http://localhost:8080" },
  "apiKeyHelper": "~/.claude/get-token.sh"
}
# ~/.claude/get-token.sh
#!/bin/bash
echo "proxy-no-key"
```

## Profile Fields

| Field | Required | Description |
|-------|:--------:|-------------|
| `name` | Yes | Profile name |
| `target` | Yes | Provider ID (e.g., `claude-oauth`, `gemini-oauth`, `anthropic`) |
| `accountIds` | No | Select specific accounts from pool (empty = use default) |
| `model` | No | Override model (empty = use model from request) |
| `provider` | Auto | Automatically set from target |

## Profile API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/profiles` | List all profiles |
| `POST` | `/v1/profiles` | Create profile (name + target only) |
| `GET` | `/v1/profiles/{name}` | Get profile by name |
| `PUT` | `/v1/profiles/{name}` | Update profile |
| `DELETE` | `/v1/profiles/{name}` | Delete profile |
| `POST` | `/v1/profiles/{name}/copy` | Copy profile |
| `POST` | `/v1/profiles/{name}/export` | Export profile (API key redacted) |
| `POST` | `/v1/profiles/import` | Import profile |

---

*Back to [Manual](../MANUAL.md)*
