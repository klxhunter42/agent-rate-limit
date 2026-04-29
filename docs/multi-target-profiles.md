# Multi-Target Profiles

One profile can have multiple targets with priority-based failover. When target #1 fails (rate limit, error, timeout), the gateway automatically falls back to target #2, then #3, etc.

## How It Works

Each target in a profile specifies:
- **target**: provider type (e.g. `claude-oauth`, `gemini-oauth`, `anthropic`)
- **accountIds**: accounts to use for this target (optional, defaults to all)
- **baseUrl**, **apiKey**, **passthroughAuth**: custom routing overrides (optional)

Priority is determined by array order. Target at index 0 is tried first.

## Creating a Multi-Target Profile

### Via API

```bash
curl -X POST http://localhost:9000/v1/profiles \
  -H "Content-Type: application/json" \
  -d '{
    "name": "hybrid",
    "targets": [
      {
        "id": "t1",
        "target": "claude-oauth",
        "accountIds": ["user@example.com"]
      },
      {
        "id": "t2",
        "target": "gemini-oauth",
        "accountIds": ["user@gmail.com"]
      }
    ]
  }'
```

### Via Dashboard UI

1. Click **New** on the Profiles page
2. Fill in the profile name
3. The first target is created automatically - select provider and accounts
4. Click **Add Target** to add more targets
5. Use arrow buttons (up/down) to reorder priority
6. Click **Create**

## Backend: ProfileTarget Struct

```go
type ProfileTarget struct {
    ID              string   `json:"id"`
    Target          string   `json:"target"`
    BaseURL         string   `json:"baseUrl,omitempty"`
    APIKey          string   `json:"apiKey,omitempty"`
    AccountIDs      []string `json:"accountIds,omitempty"`
    PassthroughAuth bool     `json:"passthroughAuth,omitempty"`
}
```

## Failover Logic

In `handler.go`, `resolveProfileTarget()` iterates targets in order:
1. If profile has `targets` array and length > 0, use targets
2. Otherwise, fall back to legacy single `target` + `accountIds` fields
3. For each target, select an account from the pool (round-robin, skip cooldown)
4. If the selected target's account pool is exhausted or all accounts in cooldown, try the next target

## Usage Tracking

### Per-Profile Usage
- Endpoint: `GET /v1/usage/profiles/{name}`
- Tracks total requests, tokens in/out, cost, per-model breakdown

### Per-Account Usage
- Endpoint: `GET /v1/usage/accounts`
- Tracks per-account: requests, tokens in/out, cost, per-model breakdown
- Redis keys:
  - `usage:account:{accountId}:daily:{date}` (35d TTL)
  - `usage:account:{accountId}:summary` (no expiry)
- Hash fields: `{model}:cost`, `{model}:input`, `{model}:output`, `{model}:requests`

### Dashboard Display
Profile cards show:
- **Usage section**: total requests, tokens, cost, per-model breakdown
- **Account Usage section**: per-account rows with email, requests, tokens, cost, usage % bar, per-model breakdown
- Usage % = (account cost / profile total cost) * 100

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/v1/profiles` | Create profile (supports `targets` array) |
| PUT | `/v1/profiles/{name}` | Update profile |
| GET | `/v1/profiles` | List all profiles |
| GET | `/v1/usage/profiles/{name}` | Get profile usage |
| GET | `/v1/usage/accounts` | Get all account usage |
| GET | `/v1/usage/accounts/{accountId}` | Get single account usage |
