# Profile-Based Routing

Profile is a set of configuration for connecting to a provider, stored in Redis. When sending `X-Profile` header or an `arl_*` API token with a request, the gateway loads the profile and uses its settings to route the request to the target provider.

## How It Works

```
Request with X-Profile: my-profile  OR  arl_* API token
|
v
Handler.Messages()
|-- Check X-Profile header
|-- If empty: check x-api-key/Authorization for arl_* prefix -> resolve profile token
|-- Load profile:{name} from Redis
|
+-- Profile found:
|   |-- If profile.model set: override request model
|   |-- Else if profile.target set and model doesn't match target:
|   |-- Token selection (priority order):
|   |   1. accountIds set -> pick from pool (round-robin)
|   |   2. passthroughAuth -> use client's Bearer token
|   |   3. provider default token from token store
|   |   4. fallback to resolver key pool
|   |-- If profile.baseURL set: override upstream URL
|   |-- Skip key pool + model fallback logic
|
+-- Profile not found:
    |-- Log warning
    |-- Return 401 ("profile not found")
```

### Token Selection Details

The token/apiKey used for the upstream request depends on profile configuration:

| Condition | Token Source |
|-----------|-------------|
| `accountIds` populated | `tokenStore.GetFromPool(provider, accountIds)` - round-robin among selected accounts |
| `passthroughAuth = true` | Client's own `Authorization: Bearer` or `x-api-key` header (strips `arl_` prefix) |
| No accountIds, no passthrough | `tokenStore.GetDefault(provider)` - default token for the provider |
| No stored token for provider | `decision.APIKey` from resolver (key pool / ZAI_API_KEYS) |

### Profile Requirement by Mode

| Mode | Profile Required | Auth Method |
|------|:----------------:|-------------|
| GLM Mode (`GLM_MODE=true`) | No | Uses `ZAI_API_KEYS` from env |
| Multi-provider mode (`GLM_MODE=false`) | Yes | `X-Profile` header or `arl_*` token |

---

## Creating a Profile

Profile create form requires only **name** + **target provider**:

```
Dashboard UI: /admin/profiles -> New
1. Name: profile name (required)
2. Target: select provider from dropdown (required)
3. Account Pool: select accounts to use (optional, empty = use default)
```

No need to enter base URL, model, or API key manually - gateway pulls from provider registry and token pool automatically.

---

## Profile Fields

| Field | Required | Description |
|-------|:--------:|-------------|
| `name` | Yes | Profile name (unique key) |
| `provider` | Auto | Set automatically from target |
| `accountIds` | No | Select specific accounts from pool (empty = use default) |
| `model` | No | Override model name (empty = use model from request) |
| `opusModel` | No | Override model when opus requested |
| `sonnetModel` | No | Override model when sonnet requested |
| `haikuModel` | No | Override model when haiku requested |
| `baseUrl` | No | Override upstream URL (empty = use provider default) |
| `apiKey` | No | Hardcoded API key (empty = use token store/pool) |
| `passthroughAuth` | No | Use client's own token instead of stored token |
| `targets` | No | Array of ProfileTarget for multi-target routing |

### ProfileTarget (multi-target)

```json
{
  "id": "optional-id",
  "target": "claude-oauth",
  "baseUrl": "https://custom-endpoint.example.com",
  "apiKey": "optional-key",
  "accountIds": ["account-1"],
  "passthroughAuth": false
}
```

---

## Model Mapping

When a profile has a `target` but the request model doesn't belong to that provider, the gateway maps to the provider's default model:

| Target Provider | Default Model |
|-----------------|---------------|
| `claude-oauth` | `claude-haiku-4-5-20251001` |
| `claude` | `claude-haiku-4-5-20251001` |
| `anthropic` | `claude-sonnet-4-20250514` |
| `gemini-oauth` | `gemini-2.5-flash` |
| `gemini` | `gemini-2.5-flash` |
| `openai` | `gpt-4o` |
| `zai` | `glm-4.5` |
| `deepseek` | `deepseek-chat` |
| `copilot` | `gpt-4o` |
| `openrouter` | `or-openai/gpt-4o` |
| `qwen` | `qwen-plus` |

Some providers also override the model name and clamp `max_tokens`:

| Provider | Model Override | Max Tokens Clamp |
|----------|---------------|-----------------|

---

## Provider Route Table

Each provider has a pre-configured route format, auth mode, and URL suffix:

| Provider ID | Format | Auth Mode | URL Suffix | Notes |
|-------------|--------|-----------|------------|-------|
| `anthropic` | anthropic | api_key | `/v1/messages` | Direct API key |
| `claude-oauth` | anthropic | api_key | `/v1/messages?beta=true` | OAuth token, extra headers |
| `claude` | anthropic | api_key | `/v1/messages?beta=true` | Alias for claude-oauth |
| `zai` | anthropic | api_key | `/v1/messages` | Z.AI API |
| `openai` | openai | bearer | `/v1/chat/completions` | OpenAI API |
| `copilot` | openai | bearer | `/v1/chat/completions` | GitHub Copilot |
| `openrouter` | openai | bearer | `/v1/chat/completions` | HTTP-Referer header |
| `qwen` | openai | bearer | `/compatible-mode/v1/chat/completions` | Qwen API |
| `gemini` | gemini | api_key | (model+key in query) | Gemini API key |
| `gemini-oauth` | gemini | bearer | (model+key in query) | Gemini OAuth |
| `deepseek` | openai | bearer | `/v1/chat/completions` | DeepSeek API |
| `kimi` | openai | bearer | `/v1/chat/completions` | Kimi API |
| `huggingface` | openai | bearer | `/v1/chat/completions` | HuggingFace |
| `ollama` | openai | bearer | `/v1/chat/completions` | Ollama local |
| `agy` | anthropic | api_key | `/v1/messages` | AGY provider |
| `cursor` | openai | bearer | `/v1/chat/completions` | Cursor |
| `codebuddy` | openai | bearer | `/v1/chat/completions` | CodeBuddy |
| `kilo` | openai | bearer | `/v1/chat/completions` | Kilo |

---

## Model Prefix Routing Rules

Model-to-provider resolution uses prefix matching in priority order:

| Model Prefix | Providers (priority order) |
|-------------|---------------------------|
| `claude-` | `claude-oauth`, `anthropic` |
| `gpt-` | `openai` |
| `o1-` | `openai` |
| `o3-` | `openai` |
| `o4-` | `openai` |
| `gemini-` | `gemini-oauth`, `gemini` |
| `glm-` | `zai` |
| `qwen-` | `qwen` |
| `or-` | `openrouter` |
| `anthropic/` | `anthropic`, `openrouter` |
| `openai/` | `openrouter` |
| `google/` | `openrouter` |
| `meta/` | `openrouter` |
| `deepseek/` | `openrouter` |
| `qwen/` | `openrouter` |
| `deepseek-` | `deepseek` |
| `kimi-` | `kimi` |
| `huggingface/` | `huggingface` |
| `ollama` | `ollama` |
| `agy-` | `agy` |

For `claude-oauth` and `gemini-oauth`, the resolver uses round-robin across active accounts, preferring accounts with lower 5h utilization (<80%).

---

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

# Use with profile API token (auto-resolves to profile)
curl -X POST http://localhost:8080/v1/messages \
  -H "x-api-key: arl_xxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet-4-20250514","max_tokens":100,"messages":[...]}'

# Use with Claude Code (apiKeyHelper)
# ~/.claude/settings.json
{
  "apiKeyHelper": "echo $ANTHROPIC_API_KEY",
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:9000",
    "ANTHROPIC_API_KEY": "arl_your-profile-token"
  }
}
```

---

## Profile API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/profiles` | List all profiles |
| `POST` | `/v1/profiles` | Create profile (name + target only) |
| `GET` | `/v1/profiles/recommended-models` | Get recommended models for a target (query: `?target=claude-oauth`) |
| `GET` | `/v1/profiles/{name}` | Get profile by name |
| `PUT` | `/v1/profiles/{name}` | Update profile |
| `DELETE` | `/v1/profiles/{name}` | Delete profile |
| `POST` | `/v1/profiles/delete` | Delete profile by name (body: `{"name": "..."}`) |
| `POST` | `/v1/profiles/{name}/copy` | Copy profile |
| `GET`/`POST` | `/v1/profiles/{name}/export` | Export profile (API key redacted) |
| `POST` | `/v1/profiles/import` | Import profile |
| `GET` | `/v1/profiles/{name}/tokens` | List profile API tokens |
| `POST` | `/v1/profiles/{name}/tokens` | Generate profile API token |
| `DELETE` | `/v1/profiles/{name}/tokens/{keyName}` | Revoke profile API token |

---

## Transparent Passthrough

When a request carries a Claude OAuth token (`Authorization: Bearer` or `x-api-key` with `sk-ant-oat01-` prefix), the gateway enables transparent passthrough:

1. Skips optimizer and privacy masking pipeline
2. Skips system prompt injection and smart max_tokens
3. Preserves exact client payload
4. Routes through billing injection path: Go direct -> sidecar fallback -> direct proxy

---

*Back to [Manual](../MANUAL.md)*
