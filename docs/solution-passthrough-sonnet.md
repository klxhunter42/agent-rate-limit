# Passthrough Auth: Sonnet Model Access via Gateway

## 1. ปัญหา (Before)

ก่อนหน้านี้ gateway ทำงานแบบ **shared pool** -- client ส่ง `arl_` token เข้ามา gateway แทนที่ด้วย OAuth token จาก token store ของตัวเอง แล้วส่งต่อไป Anthropic

ปัญหาที่เกิดขึ้น:

- Client ใช้ `arl_` token -> gateway แทนที่ด้วย OAuth token จาก token store -> claude-oauth provider
- claude-oauth accounts ทั้งหมดใน pool โดน **429 Too Many Requests** (rate limit exhausted)
- **ไม่มี fallback** เพราะไม่มี `sk-ant-` API key ลงทะเบียนในระบบ
- `CLAUDE_CODE_SIMPLE=1` บังคับ bare mode ใน container ทำให้ **ไม่มี TUI** (interactive mode ไม่ทำงาน)

ผลลัพธ์: user ทุกคน share rate limit pool เดียวกัน เมื่อ pool หมด user ทุกคนใช้งานไม่ได้พร้อมกัน

---

## 2. สิ่งที่แก้ไข (Changes)

แก้ไข 4 จุดหลัก:

### A. PassthroughAuth - Profile struct

**File:** `api-gateway/handler/profile.go`

เพิ่ม field `PassthroughAuth bool` ใน Profile struct:

```go
type Profile struct {
    Name            string   `json:"name"`
    BaseURL         string   `json:"baseUrl"`
    APIKey          string   `json:"apiKey"`
    Model           string   `json:"model"`
    Target          string   `json:"target"`
    Provider        string   `json:"provider,omitempty"`
    AccountIDs      []string `json:"accountIds"`
    PassthroughAuth bool     `json:"passthroughAuth,omitempty"`  // <-- เพิ่มใหม่
    // ...
}
```

เมื่อ profile มี `passthroughAuth: true` gateway จะ **ข้าม** token store lookup แล้วใช้ Bearer token ของ client เองแทน

### B. Handler passthrough auth resolution

**File:** `api-gateway/handler/handler.go`

ใน `Messages()` handler เมื่อ profile มี `passthroughAuth: true`:

1. ดึง Bearer token จาก client's `Authorization` header
2. **ข้าม** token store lookup ทั้งหมด
3. Force `AuthMode: "bearer"` ใน ProxyOptions
4. ดึง `ExtraHeaders` (เช่น `anthropic-beta`) จาก provider route table
5. **ข้าม** key rotation callback (`OnRateLimitError`) -- client จัดการ token ตัวเอง
6. **ข้าม** OAuth refresh callback (`OnAuthError`) -- client จัดการ token lifecycle เอง

```go
// ส่วน auth resolution
if profileOverride.PassthroughAuth {
    // ดึง Bearer token จาก client
    if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
        apiKey = strings.TrimPrefix(ah, "Bearer ")
    }
    // ตัด arl_ prefix ออกถ้า client ส่ง key เดียวกัน
    if strings.HasPrefix(apiKey, "arl_") {
        apiKey = ""
    }
}

// ส่วน ProxyOptions
if profileOverride.PassthroughAuth {
    profileOpts.AuthMode = "bearer"
    // ดึง ExtraHeaders จาก provider route table
    if d, ok := h.resolver.ResolveByProvider(pid); ok && d != nil {
        profileOpts.ExtraHeaders = d.ExtraHeaders
        if profileOpts.UpstreamOverride == "" {
            profileOpts.UpstreamOverride = d.UpstreamURL
        }
    }
}

// ส่วน callbacks -- return false เพื่อข้าม
oauthRefreshFn := func(oldKey string) (string, bool) {
    if profileOverride != nil && profileOverride.PassthroughAuth {
        return "", false  // ข้าม refresh
    }
    // ...
}

rotateAccountFn := func(oldKey string) (string, bool) {
    if profileOverride != nil && profileOverride.PassthroughAuth {
        return "", false  // ข้าม rotation
    }
    // ...
}
```

### C. Provider cooldown fallback

**File:** `api-gateway/provider/resolver.go`

เพิ่มระบบ cooldown เพื่อให้ resolver skip provider ที่โดน rate limit:

```go
type Resolver struct {
    // ...
    cooldowns sync.Map // map[string]time.Time, providerID -> cooldown until
}

// MarkCooldown mark provider ว่ากำลัง rate limited
func (r *Resolver) MarkCooldown(providerID string, d time.Duration) {
    r.cooldowns.Store(providerID, time.Now().Add(d))
}

// isCoolingDown check ว่า provider ยังอยู่ในช่วง cooldown
func (r *Resolver) isCoolingDown(providerID string) bool {
    if v, ok := r.cooldowns.Load(providerID); ok {
        return time.Now().Before(v.(time.Time))
    }
    return false
}

// ResolveFallback สำหรับหา provider สำรองโดย skip providers ที่ cooldown
func (r *Resolver) ResolveFallback(model string, exclude []string) *RoutingDecision {
    // ...skip providers ใน exclude list...
}
```

ใน handler เมื่อได้ 429 จาก upstream:

```go
if statusCode == 429 || statusCode == 503 {
    h.resolver.MarkCooldown(decision.ProviderID, 2*time.Minute)
}
```

### D. Entrypoint changes

**File:** `docker/entrypoint-claude.sh`

การเปลี่ยนแปลง:

1. **เอา `CLAUDE_CODE_SIMPLE=1` ออก** -- ไม่บังคับ bare mode แล้ว เปิดเป็น conditional แทน (ถ้ามี TTY ก็ใช้ interactive mode)
2. อ่าน user OAuth token จาก `$CLAUDE_OAUTH_TOKEN` env หรือ `/root/.claude/credentials.json`
3. ถ้าเจอ token จริง (ขึ้นต้นด้วยอะไรก็ได้ที่ไม่ใช่ `arl_`) เพิ่ม `ANTHROPIC_AUTH_TOKEN` ใน settings.json
4. ตั้ง `CLAUDE_CODE_OAUTH_TOKEN` env ให้ Claude Code CLI

```bash
# อ่าน OAuth token จาก env หรือ credentials.json
if [ -n "$CLAUDE_OAUTH_TOKEN" ]; then
  USER_OAUTH="$CLAUDE_OAUTH_TOKEN"
elif [ -f /root/.claude/credentials.json ]; then
  USER_OAUTH=$(python3 -c "..." 2>/dev/null || true)
fi

# ถ้าเจอ token จริง (ไม่ใช่ arl_) เพิ่มใน settings
if [ -n "$USER_OAUTH" ] && [ "${USER_OAUTH:0:4}" != "arl_" ]; then
  AUTH_TOKEN_JSON=",\"ANTHROPIC_AUTH_TOKEN\":\"$USER_OAUTH\""
  export CLAUDE_CODE_OAUTH_TOKEN="$USER_OAUTH"
fi

# CLAUDE_CODE_SIMPLE เป็น conditional ไม่บังคับ
if [ "${CLAUDE_CODE_SIMPLE:-}" = "1" ]; then
  export CLAUDE_CODE_SIMPLE=1
fi
```

---

## 3. ผลลัพธ์ (After)

หลังจากแก้ไข:

- User ส่งทั้ง `x-api-key: arl_xxx` (สำหรับ profile lookup) + `Authorization: Bearer <user-oauth>` (สำหรับ upstream auth)
- Gateway ใช้ `arl_` หา profile แต่ใช้ **Bearer token ของ user** ส่งไป Anthropic
- Rate limit **เป็นของ user เอง** (แยกจาก gateway pool ที่ share กัน)
- **Interactive mode ทำงานได้** (มี TUI) เพราะไม่บังคับ bare mode แล้ว
- ถ้า user ไม่มี OAuth token ตัวเอง ระบบก็ยังใช้ gateway pool ได้ตามเดิม

---

## 4. Text Flow Diagram

### Before Flow (ปัญหาเดิม)

```
Client (Claude Code)
  |
  +-- x-api-key: arl_xxx
  |
  v
Gateway (handler.go)
  |
  +-- 1. Resolve arl_xxx -> Profile "meow"
  +-- 2. Profile -> Provider: claude-oauth
  +-- 3. Token Store -> Pick OAuth token (round-robin)
  +-- 4. REPLACE auth with stored OAuth token
  |
  v
Anthropic API
  |
  +-- Authorization: Bearer <STORED-OAUTH-TOKEN>
  |
  +-- X 429 Too Many Requests (shared pool exhausted)
  |
  v
Client -> Error: rate_limited
```

### After Flow (Passthrough Auth)

```
Client (Claude Code)
  |
  +-- x-api-key: arl_xxx              <-- profile lookup
  +-- Authorization: Bearer <USER-OAUTH> <-- user's own token
  |
  v
Gateway (handler.go)
  |
  +-- 1. Resolve arl_xxx -> Profile "meow"
  +-- 2. Profile.passthroughAuth = true
  +-- 3. SKIP token store lookup
  +-- 4. Extract Bearer <USER-OAUTH> from request
  +-- 5. Force AuthMode: "bearer"
  +-- 6. Attach ExtraHeaders (anthropic-beta, x-app, etc.)
  +-- 7. SKIP key rotation callback
  +-- 8. SKIP OAuth refresh callback
  |
  v
Anthropic API
  |
  +-- Authorization: Bearer <USER-OAUTH>
  +-- anthropic-beta: oauth-2025-04-20,...
  |
  +-- OK 200 (user's own rate limit, isolated from pool)
  |
  v
Client -> Response with sonnet model
```

### Fallback Flow (Provider Cooldown)

```
Client -> Gateway
  |
  +-- Profile: meow (passthroughAuth: false, uses gateway pool)
  +-- Provider: claude-oauth -> 429!
  |   +-- MarkCooldown("claude-oauth", 2min)
  +-- Resolver fallback -> anthropic provider
  |   +-- Uses sk-ant- key (if registered)
  |
  v
Anthropic API -> OK 200


Note: สำหรับ passthrough auth, fallback ไม่จำเป็นเพราะ
      rate limit เป็นของ user เอง, ไม่ใช่ shared pool
```

### Docker Entrypoint Flow

```
Container starts
  |
  +-- Wait for gateway health check
  +-- Provision arl_ token for profile
  |
  +-- Read OAuth token:
  |   +-- $CLAUDE_OAUTH_TOKEN env?
  |   +-- OR /root/.claude/credentials.json?
  |   +-- Valid token (not arl_)?
  |       +-- YES -> Add ANTHROPIC_AUTH_TOKEN to settings.json
  |       +--              Set CLAUDE_CODE_OAUTH_TOKEN env
  |       +-- NO  -> Use gateway pool only (default behavior)
  |
  +-- Interactive mode:
  |   +-- TTY attached? -> Full TUI mode
  |   +-- CLAUDE_CODE_SIMPLE=1? -> Bare mode (no TUI)
  |
  v
exec claude "$@"
```

---

## 5. วิธีใช้งาน

### เปิด passthrough บน profile

```bash
curl -X PUT http://localhost:9000/v1/profiles/meow \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "meow",
    "target": "claude-oauth",
    "passthroughAuth": true
  }'
```

### Docker (mount credentials)

แบบที่ 1: ผ่าน environment variable

```yaml
services:
  claude:
    image: agent-rate-limit/claude
    environment:
      - CLAUDE_OAUTH_TOKEN=<user-oauth-token>
```

แบบที่ 2: mount credentials file

```yaml
services:
  claude:
    image: agent-rate-limit/claude
    volumes:
      - ~/.claude:/root/.claude
```

### Local client settings.json

สำหรับ client ที่รัน local แล้วชี้ไป gateway:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://proxy:9000",
    "ANTHROPIC_API_KEY": "arl_xxx",
    "ANTHROPIC_AUTH_TOKEN": "<user-oauth-token>"
  }
}
```

- `ANTHROPIC_API_KEY` (`arl_xxx`) -- gateway ใช้หา profile
- `ANTHROPIC_AUTH_TOKEN` -- Claude Code CLI ใช้เป็น `Authorization: Bearer` header
- Gateway เห็นทั้งสอง header, ใช้ `arl_` หา profile, ใช้ Bearer token ส่ง Anthropic

### ตรวจสอบว่า passthrough ทำงาน

ดู log ใน gateway:

```
level=INFO msg="profile passthrough auth" profile=meow
```

ถ้าเห็น log นี้แสดงว่า gateway ใช้ token ของ user แทน token store

### ปิด passthrough (กลับไปใช้ gateway pool)

```bash
curl -X PUT http://localhost:9000/v1/profiles/meow \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "meow",
    "target": "claude-oauth",
    "passthroughAuth": false
  }'
```
