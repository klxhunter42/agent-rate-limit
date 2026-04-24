# Claude Sonnet via OAuth: จากใช้ไม่ได้ สู่ใช้ได้

> สรุป timeline ของการแก้ปัญหาให้ Claude Code ใช้ Sonnet ผ่าน OAuth token ได้
> Session: `7d6420f4-8187-4c97-921e-74dedbcbe4b3` (2026-04-22 -- 2026-04-23)

---

## Timeline

### 2026-04-20 -- ค้นพบปัญหา

**อาการ:** Claude OAuth flow ใช้ไม่ได้หลายจุด

1. **OIDC scopes ผิด** -- ใช้ scopes ที่ Anthropic endpoint ไม่รองรับ (`org:create_api_key`, `user:profile`)
2. **Token refresh ส่ง form-urlencoded** -- Claude token endpoint ต้องการ JSON body
3. **Gateway ส่ง header ไม่ครบ** -- ขาด beta flags, x-app, session ID

**สาเหตุ root:** ก๊อปมาจาก Gemini OAuth flow ที่ใช้ form-urlencoded + different scopes

### 2026-04-21 -- Fix 1: Revert unsupported scopes

Commit `174b3d5` -- revert OIDC scopes ที่ Anthropic ไม่รองรับ

### 2026-04-22 -- Fix 2: PKCE flow แก้ไข

Commit `9d1a0b9` -- 3 ปัญหาหลัก:

#### 2a. OAuth 403: `code=true` parameter

Authorization URL ขาด `code=true` parameter -- ไม่มี = ไม่ได้ inference scopes

```go
// provider/oauth_authcode.go:110
if pc.ID == "claude-oauth" {
    params.Set("code", "true")
}
```

**ผล:** หลังจากนี้ auth flow ผ่าน, ได้ access token ที่มี `user:inference` scope

#### 2b. Scope จาก token response

ใช้ scope ที่ server ตอบกลับมา แทนที่จะใช้ client-configured scopes

```go
// ก่อน:
Scopes: strings.Join(pc.Scopes, " "),

// หลัง:
Scopes: firstNonEmpty(tokResp.Scope, strings.Join(pc.Scopes, " ")),
```

#### 2c. Token format ยืนยัน

- Access token: `sk-ant-oat01-...` (108 chars)
- Refresh token: `sk-ant-ort01-...` (108 chars)
- Stored: `~/.claude/.credentials.json` under `claudeAiOauth`

### 2026-04-23 02:56 -- Fix 3: JSON body สำหรับ token refresh

Commit `5f20635` -- token refresh ส่ง form-urlencoded ไปหา Claude endpoint แล้ว fail

**ปัญหา:** Claude token endpoint ต้องการ `Content-Type: application/json`, ไม่ใช่ `application/x-www-form-urlencoded` (non-standard, deviate จาก RFC 6749)

```go
// provider/token-refresh.go
if t.Provider == "claude-oauth" {
    payload := map[string]string{
        "grant_type":    "refresh_token",
        "refresh_token": t.RefreshToken,
        "client_id":     pc.ClientID,
    }
    body, _ := json.Marshal(payload)
    req.Header.Set("Content-Type", "application/json")
} else {
    // gemini-oauth, others: form-urlencoded (existing behavior)
    req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
}
```

**เพิ่มเติม:** `refreshAll()` เรียกทันทีตอน startup (ก่อน ticker แรก) -- ป้องกัน expired token หลัง restart

### 2026-04-23 14:37 -- Fix 4: Full Claude Code compatibility

Commit `4c94fc2` -- ปัญหาหลัก: upstream ปฏิเสธ request จาก gateway เพราะไม่เหมือน Claude Code CLI จริง

#### 4a. Resolver: headers เหมือน Claude Code CLI จริง

```go
// provider/resolver.go -- เดิมมีแค่:
"anthropic-beta": "oauth-2025-04-20,claude-code-20250219",
"x-app":          "cli",
"User-Agent":     "claude-code/1.0.39",

// เปลี่ยนเป็น full header set:
"anthropic-beta": "oauth-2025-04-20,interleaved-thinking-2025-05-14,...",
"x-app":          "cli",
"User-Agent":     "claude-cli/2.1.94 (external, sdk-cli)",
"X-Stainless-Lang":            "js",
"X-Stainless-Package-Version": "0.81.0",
"X-Stainless-OS":              "MacOS",
"X-Stainless-Arch":            "arm64",
"X-Stainless-Runtime":         "node",
"X-Stainless-Runtime-Version": "v25.5.0",
// + Accept, accept-language, sec-fetch-mode, etc.
```

**ทำไมต้อง:** Anthropic backend ตรวจสอบ headers เหล่านี้เพื่อจำแนกว่าเป็น Claude Code CLI request หรือไม่ -- มีผลต่อ rate limit pool และ feature availability

#### 4b. Proxy: merge incoming beta กับ route beta

```go
// proxy/anthropic.go
if k == "anthropic-beta" {
    if incoming := r.Header.Get("anthropic-beta"); incoming != "" {
        httpReq.Header.Set(k, mergeBetas(incoming, v))
        continue
    }
}
```

**ทำไม:** Claude Code CLI ส่ง beta flags มาด้วย -- ต้อง merge เข้ากับ gateway's beta flags ไม่ใช่ overwrite

#### 4c. Proxy: pass-through session ID และ request ID

```go
// proxy/anthropic.go
reqID := r.Header.Get("x-client-request-id")
sessionID := r.Header.Get("X-Claude-Code-Session-Id")
httpReq.Header.Set("x-client-request-id", reqID)
httpReq.Header.Set("X-Claude-Code-Session-Id", sessionID)
```

#### 4d. Strip unsupported betas ตาม model

```go
func stripUnsupportedBetas(h *http.Header, model string) {
    // haiku, 3.5-sonnet: ไม่รองรับ effort, interleaved-thinking
    if strings.Contains(model, "haiku") || strings.Contains(model, "3-5-sonnet") {
        // strip effort-*, interleaved-thinking-*
    }
}
```

#### 4e. Keep context_management สำหรับ native Anthropic

```go
// handler/handler.go
isNativeAnthropic := decision != nil && decision.AuthMode == "bearer"
stripUnsupportedFields(payload, isNativeAnthropic, selectedModel)
```

**ทำไม:** `context_management` (clear thinking edits) ทำงานกับ native Anthropic แต่ Z.AI/Zhipu ไม่รองรับ -- ต้องแยก logic

#### 4f. Rate limit header capture + smart account selection

- Capture `anthropic-ratelimit-unified-*` headers จาก upstream response
- Normalize utilization (percentage vs fraction)
- Resolver เลือก account ที่มี 5h utilization ต่ำสุด (ไม่ random round-robin)

#### 4g. Profile routing context propagation

```go
if profileName != "" {
    *r = *r.WithContext(context.WithValue(r.Context(), profileCtxKey{}, profileName))
}
```

### 2026-04-23 -- ทดสอบ

Commit `58c5c24` -- switch meow profile ไปใช้ sonnet:

```json
// docker/settings-meow.json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://arl-gateway:8080",
    "ANTHROPIC_API_KEY": "arl_...",
    "CLAUDE_CODE_USE_BEDROCK": "0",
    "CLAUDE_CODE_USE_VERTEX": "0"
  },
  "model": "claude-sonnet-4-6"
}
```

**ผลลัพธ์:** Sonnet ทำงานผ่าน gateway แล้ว -- auth pass, tool loop สมบูรณ์, streaming ปกติ

Commit `b357563` -- resolver tests ยืนยัน model routing ถูกต้อง

Commit `a320545` -- disable adaptive thinking ใน container (ลด complexity, haiku container ไม่ต้องการ)

---

## สรุปสิ่งที่แก้ (4 fixes)

| # | ปัญหา | สาเหตุ | แก้ | Commit |
|---|-------|--------|-----|--------|
| 1 | OAuth 403 | ขาด `code=true` param | เพิ่ม param ใน auth URL | `9d1a0b9` |
| 2 | Token refresh fail | ส่ง form-urlencoded แทน JSON | เลือก content type ตาม provider | `5f20635` |
| 3 | Headers ไม่ครบ | ส่งแค่ beta + UA 3 ตัว | Full header set เหมือน CLI จริง | `4c94fc2` |
| 4 | Beta flags ขัดแย้ง | Overwrite แทน merge | mergeBetas + stripUnsupported | `4c94fc2` |

## สิ่งที่เรียนรู้

1. **Claude OAuth ไม่ follow RFC 6749 อย่างเคร่งครัด** -- JSON body แทน form-urlencoded, `code=true` param, `state` ใน token exchange
2. **Anthropic backend ตรวจ headers ค่อนข้างละเอียด** -- X-Stainless-*, x-app, User-Agent มีผลต่อ behavior
3. **Beta flags ต้อง merge ไม่ใช่ overwrite** -- Claude Code CLI ส่ง betas มาด้วย ต้องรวมกับ gateway's betas
4. **Rate limit ผูกกับ account ไม่ใช่ token** -- multi-account ต้อง smart selection ตาม utilization
5. **429 จาก Sonnet บน Pro เป็นปกติ** -- per-model rate limit แยกกัน (haiku > sonnet > opus)
