# Routing & Auth Resolution

How the API Gateway decides where to send requests and which credentials to use.

---

## 1. Two Separate Decisions

Every request involves two independent decisions:

```
┌─────────────────────────────────────────────────────────┐
│  REQUEST: model=claude-opus-4-7  auth=Bearer xyz...     │
└─────────────┬───────────────────────────────────────────┘
              │
              ├──► WHERE to send?  ── Resolver (model prefix → provider)
              │
              └──► WHAT key to use? ── Auth chain (profile → pool → fallback)
```

- **Routing** is determined by model name prefix only (e.g. `claude-` -> Anthropic)
- **Auth** is determined by profile/token/key pool availability
- The client's API key authenticates with the gateway. It does NOT determine routing.

---

## 2. Model Routing Table

The resolver matches the first prefix and tries providers in order.

```
┌──────────────────┬────────────────────────────────────┐
│  Model Prefix    │  Providers Tried (in order)        │
├──────────────────┼────────────────────────────────────┤
│  claude-         │  claude-oauth → anthropic           │
│  gpt-            │  openai                             │
│  o1- / o3- / o4- │  openai                             │
│  gemini-         │  gemini-oauth → gemini              │
│  glm-            │  zai                                │
│  qwen-           │  qwen                               │
│  or-             │  openrouter                         │
│  deepseek-       │  deepseek                           │
│  kimi-           │  kimi                               │
│  ollama          │  ollama                             │
│  agy-            │  agy                                │
├──────────────────┼────────────────────────────────────┤
│  unknown         │  Z.AI (GLM mode) / nil              │
└──────────────────┴────────────────────────────────────┘
```

Each provider tries to find a stored token in Redis. If found, returns a routing decision immediately. If not found, tries the next provider.

Source: `provider/resolver.go` line 155-177

---

## 3. Resolver Flow (GLM_MODE=true)

```
                        model = "claude-opus-4-7"
                                 │
                                 ▼
                   ┌─────────────────────────────┐
                   │  Match prefix "claude-"      │
                   │  providers: [claude-oauth,    │
                   │              anthropic]       │
                   └─────────────┬───────────────┘
                                 │
                    ┌────────────┴────────────┐
                    ▼                         ▼
          try claude-oauth          try anthropic
          (round-robin)             (default token)
                    │                         │
                    │  Redis lookup            │  Redis lookup
                    ▼                         ▼
              ┌──────────┐              ┌──────────┐
              │  token?   │              │  token?   │
              └────┬─────┘              └────┬─────┘
                   │                         │
            ┌──────┴──────┐           ┌──────┴──────┐
            ▼             ▼           ▼             ▼
          found          nil         found          nil
            │             │           │             │
            ▼             │           ▼             │
     route to             │    route to             │
   api.anthropic.com      │  api.anthropic.com      │
   use stored token       │  use stored token       │
            │             │           │             │
            ✅             │           ✅             │
                           │                         │
                           └───────────┬─────────────┘
                                       ▼
                              ┌────────────────────┐
                              │  GLM_MODE=true?     │
                              │  fallback to Z.AI   │
                              └────────┬───────────┘
                                       │
                                       ▼
                              route to Z.AI
                              key from pool
                                       ✅


                    model = "glm-5.1"
                         │
                         ▼
               ┌──────────────────────┐
               │  Match prefix "glm-" │
               │  providers: [zai]    │
               └──────────┬───────────┘
                          │
                          ▼
                    try zai
                    Redis lookup
                          │
                    ┌─────┴─────┐
                    ▼           ▼
                  found        nil
                    │           │
                    ▼           ▼
             route Z.AI   GLM_MODE=true?
             use token         │
                    │          ▼
                    ✅    fallback Z.AI
                          key from pool
                               ✅
```

### Key difference: GLM_MODE=false

```
No stored token for claude-oauth or anthropic
          │
          ▼
   GLM_MODE=false?
          │
          ▼
   return nil → 401 rejected
   (no fallback)
```

When GLM mode is off, there is no Z.AI safety net. Every request must have a valid stored token or the client must bring its own valid credential.

---

## 4. Auth Resolution Chain

After routing decides WHERE, this chain decides WHAT key to send upstream.

```
┌──────────────────────────────────────────────────────────────┐
│  AUTH PRIORITY (first match wins)                            │
├──────┬───────────────────────────────────────────────────────┤
│  1.  │  Profile + accountIDs                                 │
│      │  → Pick from account token pool in Redis              │
│      │  (round-robin across linked accounts)                 │
├──────┼───────────────────────────────────────────────────────┤
│  2.  │  Profile + PassthroughAuth                            │
│      │  → Use client's own Bearer / x-api-key               │
│      │  (gateway doesn't touch the key)                      │
├──────┼───────────────────────────────────────────────────────┤
│  3.  │  Profile + default token                              │
│      │  → Lookup stored token for profile's provider         │
│      │  → Fallback to resolver decision key if none          │
├──────┼───────────────────────────────────────────────────────┤
│  4.  │  Transparent Claude OAuth                             │
│      │  → Client sent Bearer token + claude model            │
│      │  → Forward client's token to api.anthropic.com        │
├──────┼───────────────────────────────────────────────────────┤
│  5.  │  Resolver decision key                                │
│      │  → Use key from routing decision                      │
│      │  → If claude-oauth: prefer client's own token         │
├──────┼───────────────────────────────────────────────────────┤
│  6.  │  GLM key pool (fallback of last resort)               │
│      │  → Acquire from ZAI_API_KEYS rotation pool            │
│      │  → Only when GLM_MODE=true and no other source        │
├──────┼───────────────────────────────────────────────────────┤
│  7.  │  Client's raw x-api-key / Authorization header        │
│      │  → Last resort passthrough                            │
└──────┴───────────────────────────────────────────────────────┘
```

Source: `handler/handler.go` lines 620-700

---

## 5. Scenario Matrix (GLM_MODE=true)

### 5A. Client sends Z.AI token, requests claude-* model, no Anthropic token stored

```
Client                          Gateway                        Upstream
  │                               │                               │
  │  POST /v1/messages            │                               │
  │  Bearer: 1d63b5db...          │                               │
  │  model: claude-opus-4-7       │                               │
  │──────────────────────────────►│                               │
  │                               │                               │
  │                               │  Resolver: claude-            │
  │                               │  try claude-oauth → nil       │
  │                               │  try anthropic → nil          │
  │                               │  GLM fallback → zai           │
  │                               │                               │
  │                               │  Auth: key pool (ZAI key)     │
  │                               │──────────────────────────────►│
  │                               │                               │
  │                               │  POST /v1/messages            │
  │                               │  model: claude-opus-4-7       │
  │                               │  x-api-key: <zai-key>        │
  │                               │                  api.z.ai     │
  │                               │                               │
  │                               │  ✅ Z.AI responds             │
  │◄──────────────────────────────│◄──────────────────────────────│
```

### 5B. Client sends Z.AI token, requests glm-* model

```
Client                          Gateway                        Upstream
  │                               │                               │
  │  POST /v1/messages            │                               │
  │  Bearer: 1d63b5db...          │                               │
  │  model: glm-5.1               │                               │
  │──────────────────────────────►│                               │
  │                               │                               │
  │                               │  Resolver: glm- → zai        │
  │                               │                               │
  │                               │  Auth: key pool (ZAI key)     │
  │                               │──────────────────────────────►│
  │                               │                  api.z.ai     │
  │                               │  ✅ Z.AI responds             │
  │◄──────────────────────────────│◄──────────────────────────────│
```

### 5C. Client sends Z.AI token, requests claude-* model, Anthropic token IS stored

```
Client                          Gateway                        Upstream
  │                               │                               │
  │  POST /v1/messages            │                               │
  │  Bearer: 1d63b5db...          │                               │
  │  model: claude-opus-4-7       │                               │
  │──────────────────────────────►│                               │
  │                               │                               │
  │                               │  Resolver: claude-            │
  │                               │  try claude-oauth             │
  │                               │  → found stored token!        │
  │                               │                               │
  │                               │  Auth: stored Anthropic token │
  │                               │──────────────────────────────►│
  │                               │              api.anthropic.com│
  │                               │  ✅ Anthropic responds        │
  │◄──────────────────────────────│◄──────────────────────────────│
```

### 5D. Client with profile (PassthroughAuth), requests claude-* model

```
Client                          Gateway                        Upstream
  │                               │                               │
  │  POST /v1/messages            │                               │
  │  x-api-key: arl_profile_xxx   │                               │
  │  X-Profile: my-profile        │                               │
  │  model: claude-opus-4-7       │                               │
  │──────────────────────────────►│                               │
  │                               │                               │
  │                               │  Profile: my-profile          │
  │                               │  PassthroughAuth=true         │
  │                               │                               │
  │                               │  Auth: use client's own       │
  │                               │  Bearer token directly        │
  │                               │──────────────────────────────►│
  │                               │              api.anthropic.com│
  │                               │  (client token must be valid) │
  │                               │  ✅ or ❌ depends on token     │
  │◄──────────────────────────────│◄──────────────────────────────│
```

### 5E. Client with profile (account pool), requests claude-* model

```
Client                          Gateway                        Upstream
  │                               │                               │
  │  POST /v1/messages            │                               │
  │  x-api-key: arl_profile_xxx   │                               │
  │  X-Profile: team-profile      │                               │
  │  model: claude-opus-4-7       │                               │
  │──────────────────────────────►│                               │
  │                               │                               │
  │                               │  Profile: team-profile        │
  │                               │  accountIDs: [acc1, acc2]     │
  │                               │                               │
  │                               │  Auth: pick from account pool │
  │                               │  (round-robin, low utilization)│
  │                               │──────────────────────────────►│
  │                               │              api.anthropic.com│
  │                               │  ✅ Uses account's token      │
  │◄──────────────────────────────│◄──────────────────────────────│
```

---

## 6. Profile with Mixed Provider Accounts

When a profile has account pools from multiple providers (e.g. zai + kimi),
routing and account selection are two separate lookups that can mismatch.

### Profile Setup

```
Profile: shared-team
  targets:
    - target: zai,   accountIDs: [zai-acc-1, zai-acc-2]
    - target: kimi,  accountIDs: [kimi-acc-1, kimi-acc-2]

Redis token store:
  zai/zai-acc-1   -> token_abc
  zai/zai-acc-2   -> token_def
  kimi/kimi-acc-1 -> token_ghi
  kimi/kimi-acc-2 -> token_jkl
```

### How effectiveProviderID is determined

`effectiveProviderID` comes from the **profile config**, not from the resolver.
It determines which account pool is used for token selection.

```
effectiveProviderID = profile.Provider
if empty → profile.Target        (primary target)

accountIDs = targets[effectiveProviderID].AccountIDs
if empty → profile.AccountIDs    (top-level fallback)
```

Source: `handler/handler.go` lines 576-592

### 6A. Model matches profile target (happy path)

```
REQUEST: model=glm-5.1  profile=shared-team (target=zai)
         │
         ▼
  Resolver: "glm-" → zai
  decision.providerID = "zai"
         │
         ▼
  Profile: effectiveProviderID = "zai" (from profile.Target)
           targets[zai].accountIDs = [zai-acc-1, zai-acc-2]
         │
         ▼
  GetFromPool("zai", [zai-acc-1, zai-acc-2])
    → picks zai-acc-1 (round-robin, low utilization)
    → apiKey = token_abc
         │
         ▼
  Upstream: Z.AI  +  Auth: zai token     ← MATCH ✅
```

### 6B. Different model, different provider

```
REQUEST: model=kimi-latest  profile=shared-team (target=zai)
         │
         ▼
  Resolver: "kimi-" → kimi
  decision.providerID = "kimi"
         │
         ▼
  Profile: effectiveProviderID = "zai" (from profile.Target)
           targets[zai].accountIDs = [zai-acc-1, zai-acc-2]
         │
         ▼
  GetFromPool("zai", [zai-acc-1, zai-acc-2])
    → picks zai-acc-1
    → apiKey = token_abc
         │
         ▼
  Upstream: Kimi  +  Auth: zai token     ← MISMATCH ❌
  (sends zai token to Kimi API - will fail)
```

### 6C. Claude model with no Anthropic account (GLM_MODE=true)

```
REQUEST: model=claude-opus-4-7  profile=shared-team (target=zai)
         │
         ▼
  Resolver: "claude-"
    claude-oauth → no stored token → nil
    anthropic   → no stored token → nil
    GLM fallback → zai
  decision.providerID = "zai"
         │
         ▼
  Profile: effectiveProviderID = "zai" (from profile.Target)
           targets[zai].accountIDs = [zai-acc-1, zai-acc-2]
         │
         ▼
  GetFromPool("zai", [zai-acc-1, zai-acc-2])
    → picks zai-acc-1
    → apiKey = token_abc
         │
         ▼
  Upstream: Z.AI  +  Auth: zai token  ← route matches
  but model=claude-opus-4-7 → Z.AI may reject if unsupported
```

### 6D. No account matches at all

```
REQUEST: model=claude-opus-4-7  profile=shared-team (target=kimi)
  (no claude-oauth, no anthropic tokens stored)
         │
         ▼
  Resolver: GLM fallback → zai
         │
         ▼
  Profile: effectiveProviderID = "kimi" (from profile.Target)
           targets[kimi].accountIDs = [kimi-acc-1, kimi-acc-2]
         │
         ▼
  GetFromPool("kimi", [kimi-acc-1, kimi-acc-2])
    → picks kimi-acc-1
    → apiKey = token_ghi
         │
         ▼
  Upstream: Z.AI  +  Auth: kimi token   ← MISMATCH ❌
  (sends kimi token to Z.AI - Z.AI rejects unknown key)
```

### Termination: when no upstream can handle the model

```
Profile has no claude/anthropic accounts
No stored tokens for claude-oauth or anthropic
GLM fallback sends to Z.AI
Z.AI does not support claude-* models
         │
         ▼
  ┌──────────────────────────────┐
  │  Z.AI                        │
  │  "claude-opus-4-7?"          │
  │  ❌ 400/404 model not found  │
  └──────────────┬───────────────┘
                 │
                 ▼
  Gateway returns upstream error to client
  No retry / no further fallback
  END
```

Three possible termination states:

```
┌──────────────────────────────────┬──────────────────────────────────────┐
│  Condition                       │  Result                              │
├──────────────────────────────────┼──────────────────────────────────────┤
│  Profile has no accounts at all  │  Gateway returns 403                 │
│  for resolved provider           │  "profile has no accounts selected"  │
├──────────────────────────────────┼──────────────────────────────────────┤
│  Upstream does not support model │  Upstream error (400/404) returned   │
│  (e.g. claude-* on Z.AI)        │  to client as-is                     │
├──────────────────────────────────┼──────────────────────────────────────┤
│  Auth key mismatch (wrong pool)  │  Upstream auth error (401/403)       │
│  (e.g. kimi token sent to Z.AI) │  returned to client as-is            │
└──────────────────────────────────┴──────────────────────────────────────┘
```

---

## 7. GLM_MODE=true vs false Comparison

```
┌───────────────────────┬──────────────────────┬──────────────────────┐
│  Aspect               │  GLM_MODE=true        │  GLM_MODE=false      │
├───────────────────────┼──────────────────────┼──────────────────────┤
│  Profile required?    │  No (key pool fallback)│  Yes (401 if none)  │
│  Default model        │  glm-5                │  (none)              │
│  Unknown model route  │  Z.AI fallback        │  nil (401)           │
│  No stored token for  │  Falls back to Z.AI   │  Returns nil (401)   │
│  claude-/gemini- etc  │                       │                      │
│  ZAI_API_KEYS pool    │  Active               │  Not used            │
│  Profile CRUD         │  Available            │  Available           │
│  MCP proxy            │  Available            │  Not available       │
│  GLM models in list   │  Shown                │  Hidden              │
└───────────────────────┴──────────────────────┴──────────────────────┘
```

---

## 7. Profile Resolution (how the gateway identifies you)

```
Client request arrives
         │
         ▼
┌─────────────────────────┐
│  Check auth key format   │
└────────┬────────────────┘
         │
    ┌────┴────────────────────┐
    ▼                         ▼
  arl_* prefix          Any other key
    │                         │
    ▼                         ▼
Redis lookup           Check X-Profile header
arl_token → profile     │
    │                    ▼
    │              HMAC validate
    │              authKey == profile.APIKey
    │                    │
    ▼                    ▼
 profileName       profileName
    │                    │
    └────────┬───────────┘
             ▼
    ┌────────────────┐
    │  Profile found? │
    └────┬───────┬───┘
         │       │
       Yes      No
         │       │
         ▼       ▼
  Use profile   GLM_MODE=true? → allow (key pool)
  settings      GLM_MODE=false? → 401 rejected
```

---

## 8. Request Lifecycle (Full Picture)

```
CLIENT
  │
  │  POST /v1/messages
  │  Authorization: Bearer <key>
  │  { "model": "claude-opus-4-7", ... }
  │
  ▼
┌─────────────────────────────────────────────────────────┐
│  GATEWAY                                                │
  │                                                         │
  │  1. MIDDLEWARE (rate limit, PII guard, logging)         │
  │           │                                             │
  │           ▼                                             │
  │  2. PARSE REQUEST (extract model, payload)              │
  │           │                                             │
  │           ▼                                             │
  │  3. RESOLVE PROFILE (arl_ / X-Profile / none)           │
  │           │                                             │
  │           ▼                                             │
  │  4. RESOLVE ROUTE (model prefix → provider → upstream)  │
  │           │                                             │
  │           ▼                                             │
  │  5. SELECT AUTH (profile → transparent → pool)          │
  │           │                                             │
  │           ▼                                             │
  │  6. OPTIMIZER (delta, chunk, sketch, budget)            │
  │           │                                             │
  │           ▼                                             │
  │  7. PROXY TO UPSTREAM (with selected key + route)       │
  │           │                                             │
  │           ▼                                             │
  │  8. STREAM RESPONSE back to client                      │
  │                                                         │
└─────────────────────────────────────────────────────────┘
  │
  │  Response
  │
  ▼
CLIENT
```
