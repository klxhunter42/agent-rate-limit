# Changelog

> สรุปการเปลี่ยนแปลงทั้งหมดของระบบ

## [2026-05-11] Fix: Tab-to-Space Conversion Breaking Edit Tool

### แก้ไข: `OptimizeWhitespace` แปลง tab เป็น space ทำให้ Claude Code Edit tool match string ไม่เจอ (High)

Claude Code ส่ง message content ที่มี tab-indented Go code ผ่าน gateway `OptimizeWhitespace`
แปลง tab ทั้งหมดเป็น space ก่อนส่งต่อไป upstream model เห็น space-indented code
จึง generate Edit response ด้วย space เวลา Edit tool ส่ง space string ไป match กับไฟล์จริง
ที่เป็น tab --> match ไม่เจอ --> Edit failed

**Before/After Diagram:**

```
=== BEFORE (tab ถูกแปลงเป็น space) ===

  Claude Code sends:     Gateway OptimizeWhitespace:    Model sees:
  ┌──────────────┐       ┌──────────────────────┐       ┌──────────────┐
  │ \tfunc main(){│──────>│ optimizeProseWS()    │──────>│ func main(){ │
  │ \t\tlog.Println│      │ tab → space (BUG)    │      │   log.Println│
  │ \t}           │       │ TrimRight(line," \t")│       │ }            │
  └──────────────┘       │ TrimSpace() strips   │       └──────────────┘
                         │ leading tabs         │               │
                         └──────────────────────┘               │
                                                                v
                                                         Model generates:
                                                         ┌──────────────┐
                                                         │ Edit: spaces │
                                                         │ "  log.Print"│
                                                         └──────┬───────┘
                                                                │
                                                                v
                                                         File has tabs:
                                                         ┌──────────────┐
                                                         │ \tlog.Print  │
                                                         └──────┬───────┘
                                                                │
                                                         String mismatch!
                                                         >>> Edit failed <<<


=== AFTER (tab ถูก preserved) ===

  Claude Code sends:     Gateway OptimizeWhitespace:    Model sees:
  ┌──────────────┐       ┌──────────────────────┐       ┌──────────────┐
  │ \tfunc main(){│──────>│ optimizeProseWS()    │──────>│\tfunc main(){│
  │ \t\tlog.Println│      │ tab preserved (FIX)  │      │\t\tlog.Println│
  │ \t}           │       │ TrimRight(line," ")  │      │\t}           │
  └──────────────┘       │ only trim trailing   │       └──────────────┘
                         │ spaces, not tabs     │               │
                         └──────────────────────┘               │
                                                                v
                                                         Model generates:
                                                         ┌──────────────┐
                                                         │ Edit: tabs   │
                                                         │ "\tlog.Print"│
                                                         └──────┬───────┘
                                                                │
                                                                v
                                                         File has tabs:
                                                         ┌──────────────┐
                                                         │ \tlog.Print  │
                                                         └──────┬───────┘
                                                                │
                                                         String match!
                                                         >>> Edit succeeds <<<
```

**Root cause (3 จุดใน `optimizeProseWhitespace`):**

| # | จุด | Before | After |
|---|---|---|---|
| 1 | Tab handling loop | `r == ' ' || r == '\t'` -> write space | Tab มี branch ต่างหาก, write `\t` |
| 2 | Line trim | `TrimRight(line, " \t")` | `TrimRight(line, " ")` - trim เฉพาะ trailing space |
| 3 | Final trim | `strings.TrimSpace(out.String())` | `strings.Trim(result, "\n")` - ไม่ strip leading tabs |

**ไฟล์:** `api-gateway/tokenizer/optimizer.go`, `api-gateway/tokenizer/optimizer_test.go`

---

## [2026-05-11] Claude OAuth Account-Level Fallback Fix

### แก้ไข: Profile ที่มี Claude OAuth ไม่ fallback ไป account อื่นเมื่อโดน 429 (Critical)

เมื่อ account หนึ่งใน pool โดน 429 (usage เต็ม), ระบบ cooldown ทั้ง provider 2 นาที ทำให้ account อื่นที่ยังใช้งานได้ถูก skip ด้วย request ถูก reject ทั้งหมดจนกว่า cooldown จะหมด

**Root causes (5 จุด):**

1. **Feedback callback ใช้ provider-level cooldown** - `MarkCooldown(providerID, 2min)` block ทุก account ใน provider ไม่ใช่แค่ account ที่โดน 429
2. **Initial account selection ไม่กรอง cooldown** - `GetFromPool` เลือก account จาก pool โดยไม่เช็คว่า account นั้นอยู่ใน cooldown อยู่หรือไม่
3. **`rotateAccountFn` ไม่มี account-level cooldown check** - ตอน rotate ไม่ skip account ที่กำลัง cooldown
4. **`rotateAccountFn` ไม่ track tried accounts** - ถ้า token ใน pool มีหลายตัว อาจ rotate กลับไปใช้ account เดิมซ้ำ
5. **`tryResolveRoundRobin` ไม่ skip cooling account** - round-robin เลือก account โดยไม่กรอง cooldown

**แก้ไข:**

| จุด | แก้ | ไฟล์ |
|---|---|---|
| Feedback cooldown | เปลี่ยนเป็น `MarkAccountCooldown(providerID, accountID, 5min)` เฉพาะ account ที่โดน 429, fallback ไป provider-level สำหรับ provider ที่ไม่มี account info | handler.go |
| Initial selection | เพิ่ม cooldown filter ก่อน `GetFromPool` - ถ้าทุก account cooldown ให้ใช้ pool เดิม (fail-open) | handler.go |
| Rotate function | Rewrite `rotateAccountFn` - track tried accounts ด้วย `rotateTriedKeys map`, skip cooling accounts, ลอง profile Targets[] ก่อน fallback ไป model rules | handler.go |
| Round-robin resolver | เพิ่ม `IsAccountCoolingDown` check ใน `tryResolveRoundRobin` ตอน build active token list | resolver.go |

**กลไกใหม่ใน resolver.go:**

```go
func (r *Resolver) MarkAccountCooldown(providerID, accountID string, d time.Duration)
func (r *Resolver) IsAccountCoolingDown(providerID, accountID string) bool
```

Cooldown key: `providerID:account:accountID` (per-account, ไม่ใช่ per-provider)

**ไฟล์:** `api-gateway/provider/resolver.go`, `api-gateway/handler/handler.go`

---

## [2026-05-11] Enable caveman + pordee optimizer for GLM=false (Claude OAuth transparent mode)

### Root Cause

`!transparent` guard ใน `OptimizeSystemPrompt` ครอบทั้ง caveman และ pordee ไว้ใน block เดียวกัน ทำให้ GLM=false (Claude OAuth) ไม่มี output optimization เลย:

- `caveman_output` (direction="output") ถูก skip
- `pordee` (direction="output") ถูก skip
- `response_trim` ทำงานแค่ non-stream path ซึ่งแทบไม่มี traffic

ผล: panel "Output Tokens Optimized" และ "Pordee Injections" เป็น 0 สำหรับทุก profile ที่ใช้ GLM=false

### แก้ไข

1. ย้าย pordee ออกจาก `!transparent` block -- ทำงานทุก mode เพราะเป็น Thai output injection ไม่ขึ้นกับ proxy path
2. เอา `!transparent` guard ออกจาก caveman -- input compression + output style injection ทำงานทุก mode

**File:** `api-gateway/handler/optimizers.go`

### ผลกระทบ

- GLM=false จะได้ `caveman_output` + `pordee` + `response_trim` เหมือน GLM=true
- System prompt จะถูก modify ด้วย regex compression (pleasantries/filler removal) + output style injection
- Code block / URL / filepath ยังปลอดภัย -- `maskProtected` ป้องกันอยู่
- MinSize=500 chars -- prompt สั้นกว่านี้จะไม่ถูกแตะ

## [2026-05-10] Comprehensive SanitizeGarbledOutput Coverage (undefined Recurrence Fix)

### Root Cause

`SanitizeGarbledOutput` was added incrementally to specific paths when the "undefined" bug was first discovered. The fix had two gaps:

1. **Regex too narrow**: `(?:undefined[\s]*){2,}` only matched 2+ consecutive "undefined". GLM changed behavior to emit single "undefined" which passed through.

2. **Incomplete path coverage**: Only `relayStreamWithTracking` and `ProxySidecar` (unmasker=nil branch) had sanitization. All other proxy paths had zero coverage.

### แก้ไข: Regex จับ single undefined

```
Before: (?:undefined[\s]*){2,}   -- จับ 2+ ตัวเท่านั้น
After:  (?:undefined[\s]*)+      -- จับ 1+ ตัว (single + repeated)
```

**File:** `api-gateway/privacy/masking/stream.go`

---

### แก้ไข: SanitizeGarbledOutput ทุก streaming + non-stream path

**Before (4 paths covered):**

| Path | Status |
|---|---|
| relayStreamWithTracking text deltas | OK |
| ProxySidecar (unmasker=nil) | OK |
| handleNonStreamResponse | OK |
| ProxySidecar non-stream | OK |
| openai.go streaming | BUG - no sanitization |
| openai.go non-stream | BUG - no sanitization |
| convertOpenAIStreamResponse | BUG - no sanitization |
| convertOpenAIResponse non-stream | BUG - no sanitization |
| gemini-apikey.go streaming | BUG - no sanitization |
| gemini-apikey.go non-stream | BUG - no sanitization |
| gemini-codeassist.go streaming | BUG - no sanitization |
| gemini-codeassist.go non-stream | BUG - no sanitization |
| claude-session.go streaming | BUG - no sanitization |
| All flush paths (unmasker.Flush, stripper.Flush) | BUG - no sanitization |

**After (all paths covered):**

| Path | File | Coverage |
|---|---|---|
| relayStreamWithTracking text/thinking deltas | anthropic.go | ProcessChunk + SanitizeGarbledOutput |
| relayStreamWithTracking flush paths | anthropic.go | Flush + SanitizeGarbledOutput |
| ProxySidecar (unmasker active) | anthropic.go | ProcessChunk + SanitizeGarbledOutput |
| ProxySidecar (unmasker nil) | anthropic.go | SanitizeGarbledOutput |
| ProxySidecar flush paths | anthropic.go | Flush + SanitizeGarbledOutput |
| handleNonStreamResponse | anthropic.go | SanitizeGarbledOutput |
| convertOpenAIStreamResponse | anthropic.go | ProcessChunk + SanitizeGarbledOutput |
| convertOpenAIResponse non-stream | anthropic.go | SanitizeGarbledOutput |
| openai.go streaming text path | openai.go | ProcessChunk + SanitizeGarbledOutput |
| openai.go flush paths | openai.go | Flush + SanitizeGarbledOutput |
| openai.go non-stream | openai.go | SanitizeGarbledOutput |
| gemini-apikey.go streaming | gemini-apikey.go | ProcessChunk + SanitizeGarbledOutput |
| gemini-apikey.go flush paths | gemini-apikey.go | Flush + SanitizeGarbledOutput |
| gemini-apikey.go non-stream (x2) | gemini-apikey.go | SanitizeGarbledOutput |
| gemini-codeassist.go streaming | gemini-codeassist.go | ProcessChunk + SanitizeGarbledOutput |
| gemini-codeassist.go flush paths | gemini-codeassist.go | Flush + SanitizeGarbledOutput |
| gemini-codeassist.go non-stream | gemini-codeassist.go | SanitizeGarbledOutput |
| claude-session.go streaming | claude-session.go | ProcessChunk + SanitizeGarbledOutput |
| claude-session.go flush paths | claude-session.go | Flush + SanitizeGarbledOutput |

**Files:** `api-gateway/privacy/masking/stream.go`, `api-gateway/proxy/anthropic.go`, `api-gateway/proxy/openai.go`, `api-gateway/proxy/gemini-apikey.go`, `api-gateway/proxy/gemini-codeassist.go`, `api-gateway/proxy/claude-session.go`

---

### เพิ่ม: Grafana Dashboard Panels (41+ panels)

- token-optimization.json: 6 pordee + 4 optimizer + 23 sub-metric panels
- api-gateway-overview.json: 18 panels (bandit, cache eviction, vision, prefetcher, warmstart, summarizer)
- Fixed datasource on 83 panels across 7 dashboards
- All metrics tests pass (TestNoMissingMetrics, TestRegisteredMetricsComplete, TestDashboardPromQLValidation)

---

## [2026-05-09] Masking-Independent "undefined" Guard + GLM Mode Routing Fix

### เพิ่ม: SanitizeGarbledOutput -- final undefined guard (independent of masking)

GLM models output garbled repeated `undefinedundefined...` in responses. Existing guards only run when privacy masking is active (`HasSecrets || HasPII`). When no masking occurs, garbled output leaks to the client unchanged.

**Fix:**

Added `SanitizeGarbledOutput(text string) string` in `privacy/masking/stream.go`:
- Regex `(?:undefined[\s]*){2,}` matches 2+ consecutive "undefined" (with optional whitespace)
- Single "undefined" (e.g. `typeof x === "undefined"`) is preserved
- Runs at 4 response write points, regardless of masking state

**Response paths covered:**

| Path | Location |
|---|---|
| ProxyTransparent stream | `relayStreamWithTracking` -- text/thinking deltas always sanitized |
| ProxyTransparent non-stream | `handleNonStreamResponse` -- body sanitized before JSON validation |
| ProxySidecar stream | Scanner loop -- content_block_delta sanitized when unmasker is nil |
| ProxySidecar non-stream | `w.Write(respBody)` -- body sanitized before write |

**Files:** `api-gateway/privacy/masking/stream.go`, `api-gateway/proxy/anthropic.go`

---

### แก้ไข: GLM_MODE=true Z.AI fallback for all models

Resolver previously only fell back to Z.AI when the model's provider list included "zai". Now in GLM mode, ANY model without stored credentials falls back to Z.AI, not just `glm-*`.

**Before:**
```
claude-sonnet request -> no Anthropic token -> nil -> 401 rejected
```

**After (GLM_MODE=true):**
```
claude-sonnet request -> no Anthropic token -> Z.AI fallback -> 200 OK
```

**File:** `api-gateway/provider/resolver.go` -- `Resolve()` GLM fallback block

---

### เพิ่ม: GLM Mode documentation

Added GLM Mode section to README.md with:
- Diagrams for GLM_MODE=true (Z.AI fallback) and GLM_MODE=false (strict routing)
- Quick comparison table (profile required, unknown model behavior, fallback behavior)
- Link to detailed routing-and-auth.md

**Files:** `README.md`, `docs/routing-and-auth.md`

---

## [2026-05-08] GLM "undefined" Streaming Fix + server_tool_use Filtering Removed

### แก้ไข: GLM "undefined" leaking in streaming responses (Critical)

Z.AI/GLM models output literal `undefined` instead of preserving `[[TYPE_N]]` placeholders. When `undefined` split across SSE chunks (e.g. `undef` + `ined`), existing per-chunk fallback missed it, causing garbled output like `undefinedundefinedundefined172.18.0.9` leaking to client.

**แก้:**
- Added cross-chunk `undefinedBuffer` with `bufferPartialUndefined` / `stripPartialUndefined`
- 3-phase budget fallback: replace with originals -> dedup adjacent -> strip remaining
- `HasContexts()` guard preserves legitimate `undefined` in code when no masking active
- Non-streaming: `replaceUndefinedNonStream` in `privacy/pipeline.go`

**ไฟล์:** `api-gateway/privacy/masking/stream.go`, `api-gateway/privacy/pipeline.go`

**Test coverage:** 2700+ tests across edge cases, fuzz, random splits

**ไฟล์ทดสอบ:** `stream_undefined_edge_test.go`, `stream_undefined_weird_test.go`, `stream_fuzz_test.go`, `stream_unique_fuzz_test.go`, `undefined_fix_test.go`

---

## [2026-05-08] server_tool_use Filtering Removed

### เปลี่ยน: Gateway no longer filters server_tool_use / server_tool_result blocks

Gateway previously filtered `server_tool_use` and `server_tool_result` content blocks from requests and responses when routing to Z.AI. These blocks are now passed through to upstream and client unchanged.

**การเปลี่ยนแปลง:**
- `filterUnsupportedContent()` is now a no-op for `server_tool_use` blocks
- `server_tool_use` and `server_tool_result` blocks pass through as-is in both directions
- Streaming response handling no longer skips/filters `server_tool_use` content blocks
- Vision routing (`AnthropicToOpenAI`) still filters these for Zhipu native vision endpoint (that endpoint does not support them)

**เหตุผล:** Z.AI Anthropic-compatible endpoint now handles `server_tool_use` blocks correctly. Filtering was needed previously but is no longer required.

**ไฟล์ที่เกี่ยวข้อง:** `api-gateway/handler/handler.go`, `api-gateway/proxy/anthropic.go`

---

---

## [2026-05-07] Kimi Provider Migration: Moonshot to Anthropic-Compatible API

### เปลี่ยน: Kimi provider ย้ายจาก Moonshot platform มาใช้ Kimi's own Anthropic-compatible API

Kimi provider ย้ายจาก OpenAI-compatible endpoint (`api.moonshot.cn`) ไปยัง Anthropic-compatible endpoint ของ Kimi เอง (`api.kimi.com/coding`)

**การเปลี่ยนแปลง:**
- Upstream URL: `https://api.moonshot.cn/v1` -> `https://api.kimi.com/coding`
- API format: OpenAI (`/v1/chat/completions`) -> Anthropic (`/v1/messages`)
- Default model: `moonshot-v1-8k` -> `kimi-for-coding`
- Auth: API key (ไม่เปลี่ยนแปลง)
- Kimi ย้ายออกจาก OpenAI-format provider group ไปยัง Anthropic-format provider group

**ไฟล์ที่เกี่ยวข้อง:** `api-gateway/provider/registry.go`, docs

---

## [2026-04-21] Bug Fixes: CodeAssist Error Handling, Favorite Toggle, Profile Edit

### แก้ไข: CodeAssist empty 200 on upstream errors (Critical)

เมื่อ Google CodeAssist upstream return non-200, proxy return empty 200 (Content-Length: 0) ให้ client แทน error response เพราะ `ProxyCodeAssist` return error โดยไม่ได้ write response body ไปที่ `http.ResponseWriter`

**แก้:** write JSON error body + upstream status code ไป client ก่อน return error

**ไฟล์:** `api-gateway/proxy/gemini-codeassist.go`

---

### แก้ไข: CodeAssist 401 auto-refresh

เพิ่ม `onAuthError` callback ใน `ProxyCodeAssist` - เมื่อ upstream return 401 จะ refresh token ผ่าน `oauthRefreshFn` แล้ว retry request 1 ครั้ง (pattern เดียวกับ `anthropic.go`)

**ไฟล์:** `api-gateway/proxy/gemini-codeassist.go`, `api-gateway/handler/handler.go`

---

### แก้ไข: Favorite/Unfavorite toggle ไม่ทำงาน

`SetDefault` backend ทำได้แค่ "set default" - กด star อีกทีกับ account เดิมก็ยังเป็น default อยู่ (no toggle behavior)

**แก้:** `SetDefault` เปลี่ยนเป็น toggle - ถ้า account ที่กดเป็น default อยู่แล้วจะ unset (clear default ทั้งหมด)

**ไฟล์:** `api-gateway/provider/token-store.go` `SetDefault()` method

---

### แก้ไข: Profile edit ไม่ทำงาน + provider badge แสดง "undefined"

`CreateProfileForm` ส่ง `{ name, target, accountIds }` โดยไม่มี `provider` field -> profile ที่สร้างใหม่ไม่มี `provider` -> edit form โหลด accounts ไม่ได้ (ใช้ `profile.provider`) + badge แสดง "undefined"

**แก้:**
1. ส่ง `provider: target` เมื่อสร้าง profile (provider = target ในทุกกรณี)
2. `ProfileCard` ใช้ `resolvedProvider = profile.provider || profile.target || ''` เป็น fallback

**ไฟล์:** `ui/src/pages/profiles/index.tsx`

---

## [2026-04-20] Z.AI Vision Conversion Fix

### แก้ไข: Vision API error 1210

`AnthropicToOpenAI()` ใน `api-gateway/proxy/anthropic.go` เขียนใหม่เพื่อแก้ error 1210 ("API 调用参数有误") จาก Z.AI vision API:

1. **System role handling**: System prompt (`role: "system"`) ไม่ส่งไป Z.AI vision API แล้ว -- แทนที่ด้วยการนำ system prompt text ไปไว้ด้านหน้าของ user message แรก
2. **Content type filtering**: ส่งเฉพาะ `text`, `image`, `image_url` content types -- strip `server_tool_use`, `tool_use`, `tool_result` และ Anthropic-specific content blocks อื่นๆ
3. **Role filtering**: ส่งเฉพาะ `user` และ `assistant` roles -- `system` และ `tool` roles ถูก drop

**Root cause**: Z.AI vision API (open.bigmodel.cn) รองรับเฉพาะ `user`/`assistant` roles และ `text`/`image`/`image_url` content types เท่านั้น การส่ง `system` role หรือ `server_tool_use` content blocks ทำให้เกิด error 1210

**ไฟล์:** `api-gateway/proxy/anthropic.go`

### แผนภาพการแปลง Vision

```
Client (Claude Code)                    arl-gateway                     Z.AI Vision API
====================                    ===========                     ================
                                         │                                │
POST /v1/messages                       │                                 │
  system: "Examine every pixel..."      │                                 │
  messages: [                           │                                 │
    {role: user,                        │                                 │
     content: [                         │                                 │
       {type: image, source: {...}},   │                                  │
       {type: text, text: "describe"}, │                                  │
       {type: server_tool_use, ...}    │   AnthropicToOpenAI()            │
     ]}                                │     ┌───────────────────────┐    │
  ]                                      │   │ 1. Extract system     │    │
                                         │   │ 2. Filter roles:      │    │
                                         │   │    keep user/assist   │    │
                                         │   │    drop system/tool   │    │
                                         │   │ 3. Filter content:    │    │
                                         │   │    keep text/image/   │    │
                                         │   │          image_url    │    │
                                         │   │    drop server_tool_  │    │
                                         │   │         use/tool_use  │    │
                                         │   │ 4. Prepend system     │    │
                                         │   │    text to first user │    │
                                         │   └───────────────────────┘    │
                                         │                                │
                                         POST /chat/completions ────────► │
                                         model: glm-4.6v                  │
                                         messages: [                      │
                                           {role: user,                   │
                                            content: [                    │
                                              {type: text,
                                               text: "Examine...\n\ndescribe"},
                                              {type: image_url,           │
                                               image_url: {url: ...}}     │
                                            ]}                            │
                                         ]                                │
```

---

## [2026-04-19] Integration: Profile Routing + Quota Enforcement + Usage Recording + WS Events

### เพิ่ม: Profile-Based Routing (wired into request flow)

`X-Profile` header loads profile from Redis, overrides model, apiKey, baseUrl:
- Profile found: skip key pool + model fallback, proxy directly with profile config
- Profile not found: fall through to normal routing
- Handler struct expanded with `profileRedis` (redis.Client) field

**ไฟล์:** `api-gateway/handler/handler.go` lines ~260-275

---

### เพิ่ม: Usage Recording (wired via callback)

`metrics.RecordTokens()` now auto-calls `usageHandler.RecordUsage()` via hook:
- `metrics.SetUsageRecorder()` called in main.go wires the callback
- Every request populates Redis hourly/daily/monthly/session buckets automatically
- No separate call needed in handler code

**ไฟล์:** `api-gateway/metrics/metrics.go` lines ~221-237, `api-gateway/main.go` lines ~107-111

---

### เพิ่ม: Quota Enforcement (wired into Messages handler)

Checks quota before acquiring model slot in `Messages()`:
- >= 95% quota: returns 429 (Anthropic rate_limit_error format)
- >= 80% quota: broadcasts `quota-warning` via WebSocket, continues processing
- Fail-open on errors (quota check failure does not block requests)
- `CheckQuota(provider, accountID, model)` method added to QuotaHandler

**ไฟล์:** `api-gateway/handler/handler.go` lines ~314-330, `api-gateway/handler/quota.go`

---

### เพลี่ยนแปลง: WebSocket Events (expanded from 1 to 6 event types)

Previously only `config-changed` was wired. Now 5 additional event types broadcast:
- `request-completed`: {model, statusCode, rtt_ms} on successful upstream response
- `request-error`: {model, statusCode, rtt_ms} on failed upstream response
- `anomaly-detected`: {type, severity, model, rtt_ms} on high-severity anomaly
- `request-queued`: {requestId, model, provider} from ChatCompletions enqueue
- `quota-warning`: {provider, accountId, model, percentage} when approaching limits

Handler struct holds `wsBroadcast` (func) for event broadcasting.

**ไฟล์:** `api-gateway/handler/handler.go` lines ~408-432

---

### เปลี่ยนแปลง: Handler struct expansion

Handler now holds: `usageHandler`, `quotaHandler`, `profileRedis` (redis.Client), `wsBroadcast` (func).
Constructor updated with 4 new parameters.

---

### เปลี่ยนแปลง: .env cleanup

- `GLM_API_KEYS` and `GLM_ENDPOINT` removed from sync proxy path
- Replaced by `ZAI_API_KEYS` + `UPSTREAM_URL` (already pointed to Z.AI)
- Worker async path still uses `GLM_API_KEYS` + `GLM_ENDPOINT` independently

---

### เพลี่ยนแปลง: Z.AI pricing update

19 Z.AI models now have accurate pricing from https://docs.z.ai/guides/overview/pricing:
- Added 9 new models including flash (free tier), air, turbo variants
- `api_gateway_cost_total` metric now reflects real Z.AI pricing

**ไฟล์:** `api-gateway/handler/handler.go` lines ~794-812

---

## [2026-04-19] Dashboard SPA + Profile Management + Usage Analytics + New Providers

### เพิ่ม: Profile Management API

CRUD endpoints สำหรับจัดการ provider connection profiles:

- `GET /v1/profiles` - List all profiles
- `POST /v1/profiles` - Create profile (409 if exists)
- `GET /v1/profiles/{name}` - Get profile by name
- `PUT /v1/profiles/{name}` - Update profile
- `DELETE /v1/profiles/{name}` - Delete profile
- `POST /v1/profiles/{name}/copy` - Copy to new name
- `POST /v1/profiles/{name}/export` - Export bundle (API key redacted by default)
- `POST /v1/profiles/import` - Import from bundle

Profile struct: name, baseUrl, apiKey, model, opusModel, sonnetModel, haikuModel, target (claude/droid/codex), provider, timestamps.

**ไฟล์:** `api-gateway/handler/profile.go`

---

### เพิ่ม: Usage Analytics API

Endpoints สำหรับ usage analytics แบ่งตาม time bucket:

- `GET /v1/usage/summary?period=24h|7d|30d|all` - Aggregated totals
- `GET /v1/usage/hourly?hours=24|48` - Hourly breakdown
- `GET /v1/usage/daily` - Last 30 days
- `GET /v1/usage/monthly` - Last 12 months
- `GET /v1/usage/models?period=24h|7d|30d` - Per-model breakdown
- `GET /v1/usage/sessions?days=1-30` - Session-level (daily) usage

Data stored in Redis hashes with TTLs: hourly 48h, daily 35d, monthly 400d.

**ไฟล์:** `api-gateway/handler/usage.go`

---

### เพิ่ม: Quota Tracking API

Per-provider/account quota monitoring:

- `GET /quota/{provider}/{accountId}` - Per-account quota (30s Redis cache)
- `GET /quota/{provider}` - All accounts for a provider

รองรับ Claude, Gemini, และ fallback stub สำหรับ providers อื่น.

**ไฟล์:** `api-gateway/handler/quota.go`

---

### เพิ่ม: Dashboard Overview & Health Checks

- `GET /v1/overview` - Dashboard summary (profiles, accounts, providers, keys, queue depth, health status, uptime, request/error counts)
- `GET /v1/health/detailed` - 6 automated health checks:
  1. **Dragonfly** - Redis connectivity (QueueDepth ping)
  2. **Rate Limiter** - HTTP health check
  3. **Prometheus** - Metrics endpoint active
  4. **Key Pool** - Active API keys count
  5. **Upstream** - Upstream URL reachability
  6. **Memory** - Go heap usage (<75% pass, 75-90% warn, >90% fail)
- `POST /v1/health/fix/{checkId}` - Auto-fix hints

Overall status: healthy / degraded (warn) / unhealthy (fail).

**ไฟล์:** `api-gateway/handler/overview.go`

---

### เพิ่ม: Server Config API

Runtime configuration management:

- `GET /v1/config` - Current config (secrets redacted)
- `GET /v1/config/raw` - Config as plain text
- `PUT /v1/config` - Merge config overrides (preserve `[redacted]` values)
- `GET /v1/thinking` - Thinking budget config (defaultBudget, per-model budgets, enabled toggle)
- `PUT /v1/thinking` - Update thinking config
- `GET /v1/global-env` - Global env vars (sensitive keys auto-redacted)
- `PUT /v1/global-env` - Update global env vars

Sensitive key detection: keys containing "key", "secret", "token", "password".

**ไฟล์:** `api-gateway/handler/config.go`

---

### เพิ่ม: WebSocket Real-Time Updates

- `GET /ws` - WebSocket endpoint for live dashboard updates
- Hub-based broadcast to all connected clients
- Ping/pong keepalive (54s period, 60s pong deadline)
- Event types: `config-changed` (from .env file watcher)
- UI integration: `use-websocket.ts` hook with exponential backoff reconnect

**ไฟล์:** `api-gateway/handler/websocket.go`

---

### เพิ่ม: Login Rate Limiter Middleware

Per-IP rate limiter สำหรับ login/auth endpoints:

- 5 attempts per 15-minute window per IP
- 429 response with `Retry-After: 900` when exceeded
- Background cleanup every 5 minutes

**ไฟล์:** `api-gateway/middleware/login_limiter.go`

---

### เพิ่ม: Session Secret Persistence

- Session cookie signing secret persisted to `config/session_secret` file
- Auto-generates 64-byte hex secret on first run
- File watcher (fsnotify) for hot-reload without restart
- File permissions: directory 0700, file 0600

**ไฟล์:** `api-gateway/middleware/session_secret.go`

---

### เพิ่ม: Config File Watcher

- Watches `.env` file for changes using fsnotify
- Debounced (500ms) to avoid duplicate events
- Changed keys broadcast via WebSocket as `config-changed` events
- Runs in background goroutine

**ไฟล์:** `api-gateway/middleware/config_watcher.go`

---

### เพิ่ม: Provider Registry (17 Providers)

Gateway provider registry ขยายจาก 5 เป็น 17 providers:

**ใหม่ (API key auth):**
- DeepSeek (`api.deepseek.com`)
- Kimi / Moonshot (`api.moonshot.cn/v1`) *(migrated 2026-05-07 to `api.kimi.com/coding`, Anthropic-compatible)*
- Hugging Face (`api-inference.huggingface.co/models`)
- Ollama (`localhost:11434`, configurable)
- AGY / Antigravity (`antigravity.com`)
- Cursor (`api2.cursor.sh`)
- CodeBuddy (`api.codebuddy.io`)
- Kilo (`api.kilo.ai`)

**ใหม่ (OAuth):**
- Claude OAuth (PKCE, `platform.claude.com`)
- Gemini OAuth via Code Assist (`cloudcode-pa.googleapis.com`)

**ใหม่ (Device code):**
- GitHub Copilot (`api.github.com/copilot`)
- Qwen / Aliyun (`dashscope.aliyuncs.com`)

**เดิม (updated):**
- Anthropic, Gemini (API key), OpenAI, Z.AI, OpenRouter

**ไฟล์:** `api-gateway/provider/registry.go`

---

### เพิ่ม: Provider OAuth & Token Store

- `provider.TokenStore` - Dragonfly-backed OAuth token persistence
- `provider.AuthHandler` - OAuth device code + auth code + API key registration endpoints
- `provider.Resolver` - Maps requests to correct upstream based on provider/token
- `provider.RefreshWorker` - Background OAuth token refresh

**ไฟล์:** `api-gateway/provider/`

---

### เพิ่ม: Dashboard SPA (UI)

Embedded Vite-built React SPA served from `/`:

- `/` - Dashboard SPA entry point (optional auth via `DASHBOARD_PASSWORD`)
- `/profiles` - Profile CRUD management
- `/quota` - Quota tracking per provider/account
- `/settings` - Server Config, Thinking Config, Global Env sections

**UI Components/Hooks:**
- `usage-api-section.tsx` - Usage analytics from backend API
- `openrouter-model-picker.tsx` - OpenRouter model picker with localStorage cache
- `use-websocket.ts` - WebSocket hook with exponential backoff reconnect
- `use-ws-refresh.ts` - Hook to trigger data refetch on WS events
- `ws-events.ts` - Event bus for WS event broadcasting
- WSBridge component in layout.tsx

**ไฟล์:** `ui/`

---

## [2026-04-19] Vision Model Auto-Select + SSE Streaming

### เพิ่ม: Vision model auto-select

Gateway วิเคราะห์ image payload (total base64 bytes + count) แล้วเลือก vision model อัตโนมัติ:

- Scoring: `score = totalBase64KB + (imageCount * 300)`
- score <= 2000 && count < 3 -> `glm-4.6v` (10 slots, best quality)
- score > 2000 || count >= 3 -> `glm-4.6v` (heavy payload fallback)

**ไฟล์:** `api-gateway/handler/handler.go`

---

### เพิ่ม: SSE streaming สำหรับ vision

Vision requests ที่ `stream: true` ขณะนี้รองรับ SSE streaming แบบ real-time:

- Zhipu SSE chunks (OpenAI format) ถูก convert เป็น Anthropic SSE events
- รองรับ `delta.content` และ `delta.reasoning_content`
- Event sequence: message_start -> content_block_start -> content_block_delta -> content_block_stop -> message_delta -> message_stop

**ไฟล์:** `api-gateway/proxy/anthropic.go`

---

### เพิ่ม: Vision model concurrency slots

Vision models เพิ่มเข้า `UPSTREAM_MODEL_LIMITS`:

- glm-4.6v: 10 slots
- glm-4.5v: 10 slots

**ไฟล์:** `api-gateway/config/config.go`

---

### ลบ: glm-5v-turbo references

ลบการอ้างอิง glm-5v-turbo ออกจาก documentation ทั้งหมด (model นี้ไม่มีในระบบจริง)

**ไฟล์:** `MANUAL.md`, `docs/architecture.md`, `docs/providers.md`, `docs/known-issues.md`

---

## [2026-04-18] Vision Auto-Routing + Content Filtering

### เพิ่ม: Vision auto-routing

Gateway ตรวจจับ image content ใน request อัตโนมัติ แล้ว route ไป native Zhipu vision endpoint แทน z.ai Anthropic endpoint เพราะ z.ai ไม่สามารถ decode base64 image ผ่าน Anthropic-compatible format ได้

**Features:**
- Auto-detect image content in messages
- Format conversion: Anthropic Messages API <-> Zhipu OpenAI API (both directions)
- Content filtering: strip server_tool_use blocks, convert image format
- Supported models: glm-4.6v, glm-4.5v

**ไฟล์:** `api-gateway/handler/handler.go`, `api-gateway/proxy/anthropic.go`, `api-gateway/config/config.go`

---

### เพิ่ม: Content filtering

Strip unsupported content block types (server_tool_use) ก่อนส่งไป upstream:
- Anthropic image format -> GLM image_url format
- server_tool_use blocks removed

**ไฟล์:** `api-gateway/handler/handler.go`

---

### เพิ่ม: NATIVE_VISION_URL config

Env var ใหม่สำหรับตั้ง native Zhipu vision endpoint:
- Default: `https://open.bigmodel.cn/api/paas/v4/chat/completions`
- Configurable via `NATIVE_VISION_URL`

**ไฟล์:** `api-gateway/config/config.go`

---

### เพิ่ม: VISION system prompt

เพิ่ม vision-specific instructions ใน default system prompt:
- Examine every pixel region before answering
- Identify colors, shapes, text, objects, spatial layout
- Answer based only on what is visibly present

**ไฟล์:** `api-gateway/config/config.go`

---

## [2026-04-14] Bug fixes + Hardening

### ปัญหา: Global slot starvation (Critical)

เวลา request เข้ามาเยอะ ทุก request จะจับ "global slot" ไว้แล้วรอ model slot นาน 30 วินาที ทำให้ slot หมดทั้งระบบ คนอื่นเข้าไม่ได้เลย

**แก้:** ปล่อย global slot ก่อนรอ พอได้ model slot แล้วค่อยขอ global slot ใหม่

**ไฟล์:** `api-gateway/middleware/adaptive_limiter.go`

---

### ปัญหา: Key pool RPM leak (High)

request ที่ body พังหรือ JSON ไม่ถูกต้อง ยังไปกิน RPM quota ของ API key ทิ้ง

**แก้:** ย้าย `keyPool.Acquire()` ไปหลัง validate body + parse JSON เรียบร้อย

**ไฟล์:** `api-gateway/handler/handler.go`

---

### ปัญหา: Retry backoff ไม่รับรู้ context cancel (Medium)

เวลา client disconnect ระหว่างรอ retry backoff ระบบยังนอนรอต่อเปล่าๆ

**แก้:** ใช้ `select` + `ctx.Done()` แทน `time.Sleep` ถ้า client ยกเลิกก็หยุดทันที

**ไฟล์:** `api-gateway/proxy/anthropic.go`

---

### ปัญหา: Feedback ยิงซ้ำเกิน

เวลาโดน 429 แล้ว retry แต่ละครั้ง feedback ไปลด limit ตลอด ทำให้ limit ตกลงไปเร็วเกิน

**แก้:** ยิง feedback ไป adaptive limiter แค่ retry ครั้งสุดท้าย

**ไฟล์:** `api-gateway/proxy/anthropic.go`

---

### ปัญหา: Docker-compose defaults ไม่ตรงกัน

gateway ตั้ง default limit ไว้ 2/15 แต่ worker ตั้ง 1/9 ถ้าไม่มี .env สองฝั่งทำงานไม่เหมือนกัน

**แก้:** ให้ตรงกันหมดเป็น `DEFAULT_LIMIT=1`, `GLOBAL_LIMIT=9`

**ไฟล์:** `docker-compose.yml`

---

### ปัญหา: Limiter status endpoint ไม่มี auth

ใครก็เข้าดูสถานะ limiter ได้

**แก้:** เพิ่ม `IsValidKey()` เช็ค API key ก่อนอนุญาต

**ไฟล์:** `api-gateway/proxy/key_pool.go`, `api-gateway/handler/handler.go`

---

### ปัญหา: Password ใน .env.example

เผลอใส่รหัสผ่านจริงในไฟล์ตัวอย่าง

**แก้:** เปลี่ยนเป็น `changeme`

**ไฟล์:** `.env.example`

---

## [2026-04-13] Adaptive Limiter + Probe Multiplier

### เพิ่ม: Adaptive concurrency limiter

ระบบจำกัดจำนวน concurrent request แบบ adaptive - ปรับ limit อัตโนมัติตาม feedback จาก upstream

**Algorithm (inspired by Envoy gradient controller):**
- โดน 429: limit ลด 50% (`limit * 0.5`)
- สำเร็จ: ใช้ gradient formula `(minRTT + buffer) / sampleRTT` เพิ่ม limit
- Cooldown 5 วินาทีหลัง 429 ก่อนเพิ่ม limit ใหม่
- จำค่าที่โดน 429 (`peakBefore429`) ไม่ให้ขยายเกิน แต่ลืมหลัง 5 นาที

**ไฟล์:** `api-gateway/middleware/adaptive_limiter.go`

---

### เพิ่ม: Probe multiplier

ให้ limiter ลองขยาย limit สูงสุดได้ `N` เท่าของ initial limit เพื่อค้นหา upstream limit จริง

- Default: 5x (`UPSTREAM_PROBE_MULTIPLIER=5`)
- ถ้า initial limit = 2, probe max = 10
- โดน 429 ก็ลดลงเองตาม adaptive algorithm

**ไฟล์:** `api-gateway/config/config.go`, `api-gateway/main.go`

---

### เพิ่ม: Model fallback with priority

เวลา model ที่ขอเต็ม ระบบลองรอ 2 วินาทีก่อน แล้ว fallback ตาม priority:

- glm-5.1 (100) > glm-5-turbo (90) > glm-5 (80) > glm-4.7 (70) > glm-4.6 (60) > glm-4.5 (50)
- ข้าม model ที่ห่างกันเกิน 2 tier (gap >= 50)

**ไฟล์:** `api-gateway/middleware/adaptive_limiter.go`

---

### เพิ่ม: Token metrics

Prometheus counters สำหรับติดตาม token usage:

- `api_gateway_token_input_total{model}` (gateway)
- `api_gateway_token_output_total{model}` (gateway)
- `ai_worker_token_input_total{provider,model}` (worker)
- `ai_worker_token_output_total{provider,model}` (worker)

**ไฟล์:** `api-gateway/metrics/metrics.go`, `api-gateway/proxy/anthropic.go`, `ai-worker/prom_metrics.py`

---

## Documentation

| ไฟล์                        | สถานะ                                                                   |
|-----------------------------|-------------------------------------------------------------------------|
| `docs/providers.md`         | v1.6 - ZAI_API_KEYS split, Z.AI pricing table, usage recording          |
| `docs/architecture.md`      | v3.1 - profile routing, quota enforcement, usage recording, 6 WS events |
| `docs/known-issues.md`      | v1.3 - quota enforcement wired (placeholder data remains)               |
| `docs/claude-code-proxy.md` | v2.7 - profile routing, quota enforcement, WS events, usage integration |
| `docs/changelog.md`         | v1.4 - Z.AI vision conversion fix (error 1210), text diagram            |
| `docs/known-issues.md`      | v1.4 - vision conversion fix with text diagram (TH)                     |
| `docs/architecture.md`      | v3.2 - updated format conversion diagram (TH)                           |
| `docs/providers.md`         | v1.7 - vision format conversion notes (TH)                              |
