# Pordee: Thai Output Optimization

> **พอดี** (pordee) -- Thai for "just right"

Server-side Thai language output compression for the API Gateway. Cuts ~60-75% of output tokens when model responds in Thai, while preserving full technical accuracy.

Inspired by [pordee](https://github.com/kerlos/pordee) Claude Code plugin and [caveman](https://github.com/JuliusBrussee/caveman) English compression.

---

## Table of Contents

1. [Why Thai Needs Separate Optimization](#1-why-thai-needs-separate-optimization)
2. [How It Works: Theory](#2-how-it-works-theory)
3. [Architecture](#3-architecture)
4. [Before/After Scenarios](#4-beforeafter-scenarios)
5. [Auto-Clarity: Safety Mechanism](#5-auto-clarity-safety-mechanism)
6. [Boundaries: What Never Gets Compressed](#6-boundaries-what-never-gets-compressed)
7. [Cost and Performance Impact](#7-cost-and-performance-impact)
8. [Configuration](#8-configuration)
9. [Metrics](#9-metrics)

---

## 1. Why Thai Needs Separate Optimization

### The Problem

Thai language has structural verbosity that inflates tokens without adding meaning:

| Category | Examples | Token Cost |
|---|---|---|
| Polite particles | ครับ, ค่ะ, นะคะ, นะครับ, จ้ะ | ~1-2 each, used 2-5x per response |
| Hedging | อาจจะ, น่าจะ, ค่อนข้างจะ, จริงๆแล้ว | ~2-3 each, used 1-3x |
| Filler words | ก็, ก็คือ, นั่นคือ, แบบว่า | ~1 each, used 3-8x |
| Pleasantries | ยินดีครับ, ได้เลยครับ, แน่นอนครับ | ~3-4 each, opens every response |
| Verbose synonyms | ทำการแก้ไข, ดำเนินการ, ตรวจสอบ | 2-3x longer than terse form |

### Why Caveman Alone Is Not Enough

The existing caveman pipeline uses English-centric compression rules:

```
Caveman (English)                    Pordee (Thai)
─────────────────                    ────────────
"Be extremely terse."                "ตัด ครับ/ค่ะ/นะคะ"
"No explanations."                   "ตัด อาจจะ/น่าจะ/จริงๆแล้ว"
"Code only."                         "เพราะ ไม่ใช่ เนื่องจาก"
                                     "แก้ ไม่ใช่ ทำการแก้ไข"

Model response:                      Model response:
"re-render. useMemo."                "Object ref ใหม่ทุก render.
                                      ห่อด้วย useMemo."

Thai text NOT compressed             Thai-specific semantic compression
```

Caveman tells the model to skip English pleasantries. Thai politeness and filler patterns are fundamentally different -- they need Thai-specific rules.

---

## 2. How It Works: Theory

### Mechanism: System Prompt Injection

Pordee does NOT post-process output. It injects compression instructions into the system prompt, and the model follows them when generating.

This is the same proven mechanism as caveman, already running in production:

```
                    REQUEST FLOW
                    ============

  Client Request
       |
       v
  +----------+    +----------+    +-----------+    +----------+
  | Privacy  |--->| Caveman  |--->| PORDEE    |--->| Upstream |
  | Mask     |    | (English |    | INJECT    |    | (Claude/ |
  |          |    |  tier)   |    | (Thai)    |    |  GLM)    |
  +----------+    +----------+    +-----------+    +----------+
       |              |               |                |
       |              |   Thai text   |                |
       |              |   detected?   |                |
       |              |               |                |
       |              |   YES -----> Inject pordee     |
       |              |               rules to system  |
       |              |               prompt            |
       |              |               |                 |
       |              |   NO ------> Skip (caveman      |
       |              |               handles it)       |
       |              |                                |
       |<---------- Thai output already compressed <---+
       |             at source (fewer tokens)
       |
  Client receives terse Thai response
```

### Why This Works

1. LLMs follow system prompt instructions reliably
2. Thai politeness/filler patterns are well-defined and consistent
3. The model generates fewer tokens = faster response + lower cost
4. No information loss -- technical accuracy preserved by boundary rules

### Compression Levels

| Level | Trigger | Behavior |
|---|---|---|
| **off** | default / `X-Pordee-Level: off` | No Thai compression. Model responds naturally. |
| **lite** | `X-Pordee-Level: lite` | Drop polite particles, hedging, pleasantries. Grammar intact. Professional Thai. |
| **full** | `X-Pordee-Level: full` | All lite rules + drop redundant particles, verbose synonyms. Fragments OK. Short pattern. |

---

## 3. Architecture

### Injection Hierarchy

The system prompt is assembled in this order:

```
  Position    Content
  ────────    ───────
  system[0]   Billing header (InjectBillingHeader)
  system[1]   Identity string ("You are Claude Code...")
  system[2]   Config prompt injection (PromptInjectionText)
  ...         User's original system prompt
  system[N]   Privacy prompt injection (if masking active)
  system[N+1] Caveman tier injection (if English text)
  system[N+2] PORDEE injection (if Thai text)     <-- NEW
```

### Detection Logic

```
  ┌─────────────────────────────────────────────┐
  │                                             │
  │  Input: request messages content            │
  │                                             │
  │  thaiRe.MatchString(content)?               │
  │  (U+0E00 - U+0E7F = Thai Unicode range)     │
  │                                             │
  │     YES                NO                   │
  │      │                  │                   │
  │      v                  v                   │
  │  Skip caveman        Use caveman            │
  │  Inject pordee       (existing logic)       │
  │  rules                                      │
  │      │                                      │
  │      v                                      │
  │  Level from:                                │
  │  1. X-Pordee-Level header                   │
  │  2. PORDEE_LEVEL env var                    │
  │  3. Default: full                           │
  │                                             │
  └─────────────────────────────────────────────┘
```

### Integration Point

Pordee integrates as stage F17 in the optimizer pipeline (after caveman at F16):

```
  Optimizer Stages (system prompt):
  ─────────────────────────────────
  F1  semantic_dedup
  F2  chunker
  F3  delta (metrics)
  F4  sketch_dedup
  F5  summarizer (red budget only)
  F6  textcomp
  F16 caveman (English compression)
  F17 pordee (Thai compression)     <-- NEW
```

---

## 4. Before/After Scenarios

### Scenario 1: Kubernetes Pod Crash

```
  USER: "ทำไม pod ของผมมันเข้า CrashLoopBackOff ตลอดเลยครับ"

  ┌──────────────────────────┬──────────────────────────────────────┐
  │  BEFORE (no pordee)      │  AFTER (pordee full)                 │
  │  ~120 tokens             │  ~35 tokens (-71%)                   │
  ├──────────────────────────┼──────────────────────────────────────┤
  │                          │                                      │
  │  "แน่นอนครับ ผมยินดีช่ว │  "CrashLoopBackOff = container        │
  │   ยแก้ปัญหาให้นะครับ    │   exit != 0 แล้ว restart loop.        │
  │   ปัญหาที่คุณเจออาจจะ   │                                       │
  │   เกิดจากหลายสาเหตุ     │   Debug:"                             │
  │   ครับ จริงๆ แล้วอาจจะ  │   1. kubectl logs <pod> --previous    │
  │   เริ่มจากการตรวจสอบ    │   2. kubectl describe pod <pod>       │
  │   container logs ก่อน    │   3. ดู reason ใน Last State         │
  │   ครับ โดยการรันคำสั่ง  │                                       │
  │   kubectl logs <pod>     │   สาเหตุที่พบบ่อย:"                  │
  │   --previous ครับ ซึ่ง   │   - OOMKilled -> เพิ่ม limits        │
  │   จะแสดง logs ของ       │   - Error -> ดู app log               │
  │   container ที่ crash    │   - CrashLoop -> probe/start fail    │
  │   ไปแล้วนะครับ แล้ว     │                                       │
  │   ก็ควรจะตรวจสอบ        │                                       │
  │   kubectl describe pod   │                                      │
  │   <pod> ด้วยครับเพื่อ    │                                      │
  │   ดู events และสาเหตุ   │                                       │
  │   ที่แน่นอนครับ"         │                                      │
  │                          │                                      │
  │  ████████████████████    │  ██████░░░░░░░░░░░░░░░░░░░░░░░       │
  │  120 tokens              │  35 tokens                           │
  └──────────────────────────┴──────────────────────────────────────┘
```

### Scenario 2: Terraform State Lock

```
  USER: "terraform apply แล้วติด Error acquiring the state lock ทำไงดี"

  ┌──────────────────────────┬──────────────────────────────────────┐
  │  BEFORE (no pordee)      │  AFTER (pordee full)                 │
  │  ~100 tokens             │  ~25 tokens (-75%)                   │
  ├──────────────────────────┼──────────────────────────────────────┤
  │                          │                                      │
  │  "ครับ ปัญหานี้น่าจะเกิด│  "State lock ค้าง. สาเหตุ: lock       │
  │   จากมี process อื่นที่  │   จาก process อื่น.                  │
  │   กำลังใช้ state lock   │                                       │
  │   อยู่ครับ หรืออาจจะมี   │   Fix:"                              │
  │   process ก่อนหน้าที่    │   1. ดูว่ามี terraform apply อื่น    │
  │   crash ไปโดยไม่ได้     │      รันอยู่ไหม                       │
  │   release lock ครับ      │   2. ถ้าไม่มี:                       │
  │   ดังนั้นคุณอาจจะต้อง   │      terraform force-unlock <id>      │
  │   ตรวจสอบก่อนว่ามี      │   3. DynamoDB: ลบ item ใน             │
  │   process อื่นรันอยู่    │      tf-state-lock table             │
  │   หรือเปล่านะครับ แล้ว  │                                       │
  │   ถ้าไม่มีจริงๆ ก็คง    │                                       │
  │   ต้องใช้คำสั่ง         │                                       │
  │   terraform force-unlock│                                       │
  │   ครับ"                  │                                      │
  │                          │                                      │
  │  ████████████████████    │  █████░░░░░░░░░░░░░░░░░░░░░░░░       │
  │  100 tokens              │  25 tokens                           │
  └──────────────────────────┴──────────────────────────────────────┘
```

### Scenario 3: ArgoCD Sync Failure

```
  USER: "ArgoCD application sync ติด OutOfSync แก้ยังไง"

  ┌──────────────────────────┬──────────────────────────────────────┐
  │  BEFORE (no pordee)      │  AFTER (pordee full)                 │
  │  ~90 tokens              │  ~20 tokens (-78%)                   │
  ├──────────────────────────┼──────────────────────────────────────┤
  │                          │                                      │
  │  "ครับ ผมขออธิบายให้นะ  │  "OutOfSync = Git manifest !=         │
  │   ครับ จริงๆ แล้ว OutOf- │   cluster state.                     │
  │   Sync นั้นหมายความว่า   │                                      │
  │   manifest ใน Git กับ    │   แก้:"                              │
  │   state บน cluster ไม่   │   - Manual: argocd app sync <app>    │
  │   ตรงกันครับ ซึ่งคุณอาจ │   - Auto: syncPolicy.automated:       │
  │   จะต้องทำการ sync ใหม่ │     selfHeal: true                    │
  │   ครับ โดยสามารถกดปุ่ม  │   - Diff: argocd app diff <app>       │
  │   Sync ใน UI ได้เลย     │                                       │
  │   ครับ หรือจะใช้คำสั่ง  │                                       │
  │   argocd app sync ก็ได้  │                                      │
  │   ครับ"                  │                                      │
  │                          │                                      │
  │  ████████████████████    │  ████░░░░░░░░░░░░░░░░░░░░░░░░        │
  │  90 tokens               │  20 tokens                           │
  └──────────────────────────┴──────────────────────────────────────┘
```

### Scenario 4: Helm Release Stuck

```
  USER: "helm upgrade ติด another operation is in progress ทำยังไง"

  ┌──────────────────────────┬──────────────────────────────────────┐
  │  BEFORE (no pordee)      │  AFTER (pordee full)                 │
  │  ~80 tokens              │  ~15 tokens (-81%)                   │
  ├──────────────────────────┼──────────────────────────────────────┤
  │                          │                                      │
  │  "ครับ ปัญหานี้เกิดจาก  │  "Helm lock ค้าง. Fix:"               │
  │   มี operation ก่อนหน้า  │   kubectl get secret -l              │
  │   ที่ยังไม่เสร็จครับ     │     owner=helm,status=pending-       │
  │   คุณน่าจะต้องลบ config │     install                           │
  │   map ที่เก็บสถานะ      │   kubectl delete secret <rel>.v1      │
  │   pending operation ออก │   helm upgrade --install <rel>        │
  │   ก่อนครับ โดยใช้คำสั่ง │     <chart>                           │
  │   kubectl get secret    │                                       │
  │   -l owner=helm แล้ว   │                                        │
  │   ลบตัวที่มี status=     │                                      │
  │   pending-install ครับ" │                                       │
  │                          │                                      │
  │  ████████████████████    │  ███░░░░░░░░░░░░░░░░░░░░░░░░░        │
  │  80 tokens               │  15 tokens                           │
  └──────────────────────────┴──────────────────────────────────────┘
```

### Scenario 5: Vault Seal

```
  USER: "HashiCorp Vault ติด seal หลัง restart ทำไง"

  ┌──────────────────────────┬──────────────────────────────────────┐
  │  BEFORE (no pordee)      │  AFTER (pordee full)                 │
  │  ~110 tokens             │  ~28 tokens (-75%)                   │
  ├──────────────────────────┼──────────────────────────────────────┤
  │                          │                                      │
  │  "ครับ หลังจากที่ Vault │  "Vault auto-seal หลัง restart        │
  │   ถูก restart ไปแล้ว    │   เพราะ unseal key ไม่ได้เก็บใน       │
  │   นั้น มันจะเข้าสถานะ   │   memory.                             │
  │   sealed โดยอัตโนมัติ   │                                       │
  │   ครับ เพราะว่า unseal  │   Unseal:"                            │
  │   key จะหายไปจาก       │   - Manual: vault operator unseal      │
  │   memory ครับ ดังนั้น   │     (3 key holders)                   │
  │   คุณจะต้องทำการ       │   - Auto:ใช้ transit seal หรือ         │
  │   unseal ใหม่ครับ โดย  │     AWS KMS                            │
  │   คุณอาจจะใช้วิธี manual│   - ตรวจ: vault status                │
  │   คือ vault operator   │                                        │
  │   unseal 3 ครั้ง หรือ   │                                       │
  │   จะใช้ auto-unseal    │                                        │
  │   ผ่าน AWS KMS ก็ได้    │                                       │
  │   ครับ"                  │                                      │
  │                          │                                      │
  │  ████████████████████    │  ██████░░░░░░░░░░░░░░░░░░░░░░░       │
  │  110 tokens              │  28 tokens                           │
  └──────────────────────────┴──────────────────────────────────────┘
```

### Scenario 6: React Component Re-render (General Dev)

```
  USER: "ทำไม React component ถึง re-render"

  ┌──────────────────────────┬──────────────────────────────────────┐
  │  BEFORE (no pordee)      │  AFTER (pordee full)                 │
  │  ~80 tokens              │  ~22 tokens (-73%)                   │
  ├──────────────────────────┼──────────────────────────────────────┤
  │                          │                                      │
  │  "แน่นอนครับ ผมยินดีจะ  │  "Object ref ใหม่ทุก render.          │
  │   อธิบายให้นะครับ จริงๆ │   Inline object prop = ref ใหม่       │
  │   แล้วเหตุผลที่ React   │   = re-render.                        │
  │   component ของคุณ       │   ห่อด้วย useMemo."                  │
  │   re-render นั้น น่าจะ   │                                      │
  │   เกิดจากการที่คุณส่ง    │                                      │
  │   object reference ใหม่  │                                      │
  │   เป็น prop ในทุกครั้ง  │                                       │
  │   ที่ component ถูก      │                                      │
  │   render ซึ่งทำให้      │                                       │
  │   React มองว่า prop     │                                       │
  │   เปลี่ยน และทำการ     │                                        │
  │   re-render component   │                                       │
  │   ลูก ดังนั้นคุณอาจจะ   │                                       │
  │   ลองใช้ useMemo ดูครับ"│                                       │
  │                          │                                      │
  │  ████████████████████    │  ████░░░░░░░░░░░░░░░░░░░░░░░░        │
  │  80 tokens               │  22 tokens                           │
  └──────────────────────────┴──────────────────────────────────────┘
```

### Scenario 7: Pordee Lite (Lighter Compression)

```
  USER: "SSL certificate หมดอายุ ต่ออายุยังไง"

  ┌──────────────────────────┬──────────────────────────────────────┐
  │  BEFORE (no pordee)      │  AFTER (pordee lite)                 │
  │  ~95 tokens              │  ~50 tokens (-47%)                   │
  ├──────────────────────────┼──────────────────────────────────────┤
  │                          │                                      │
  │  "ครับ การต่ออายุ SSL  │  "SSL หมดอายุ. ต่ออายุ:                │
  │   certificate นั้นค่อน  │                                       │
  │   ข้างจะง่ายครับ จริงๆ │   Let's Encrypt:                       │
  │   แล้วก็ขึ้นอยู่กับว่า  │   certbot renew --force-renewal       │
  │   คุณใช้ provider ไหน   │                                       │
  │   นะครับ ถ้าใช้ Let's   │   ตรวจ:                               │
  │   Encrypt ก็จะง่าย     │   certbot certificates                 │
  │   หน่อยครับ เพราะว่า   │   echo | openssl s_client -connect     │
  │   มันตั้งค่า auto-     │     :443 2>/dev/null | openssl x509    │
  │   renewal ได้เลย"       │     -noout -dates                     │
  │                          │                                      │
  │  ████████████████████    │  ██████████░░░░░░░░░░░░░░░░░░░       │
  │  95 tokens               │  50 tokens                           │
  └──────────────────────────┴──────────────────────────────────────┘
```

---

## 5. Auto-Clarity: Safety Mechanism

Pordee temporarily disables itself when precision matters more than brevity:

```
  ┌─────────────────────────────────────────────────────────────────┐
  │                                                                 │
  │  USER: "ช่วยลบ database ชื่อ production_logs ทิ้งหน่อย"         │
  │                                                                 │
  │  Pordee detects: "ลบ" + "production" = irreversible action      │
  │  --> DROP pordee temporarily, respond with full clarity         │
  │                                                                 │
  │  Response (pordee OFF for this section):                        │
  │                                                                 │
  │  "Warning: คุณกำลังขอลบ database "production_logs"              │
  │   ซึ่งเป็นการกระทำที่ย้อนกลับไม่ได้ (irreversible)              │
  │                                                                 │
  │   ก่อนดำเนินการ กรุณายืนยัน:                                    │
  │   1. คุณได้สำรองข้อมูล (backup) แล้วหรือยัง?                    │
  │   2. มี application ใดใช้ database นี้อยู่หรือไม่?              │
  │   3. คุณแน่ใจจริงๆ หรือว่าต้องการลบ, ไม่ใช่แค่ truncate?        │
  │                                                                 │
  │   ถ้ายืนยัน: DROP DATABASE production_logs;"                    │
  │                                                                 │
  │  After this section: pordee resumes                             │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘
```

### Auto-Clarity Triggers

| Trigger | Examples |
|---|---|
| Security warnings | `Warning:`, `⚠️`, `SECURITY` |
| Irreversible commands | `DROP TABLE`, `rm -rf`, `git push --force`, `git reset --hard`, `git branch -D` |
| Multi-step sequences | Where order matters and fragments could be misread |
| User confusion signals | "อะไรนะ", "พูดอีกที", "อธิบายชัดๆ", "ไม่เข้าใจ", "งง", "ขยายความ" |
| Production keywords | "production", "prod", "live" + destructive verb |

---

## 6. Boundaries: What Never Gets Compressed

```
  ┌─────────────────────────────────────────────────────────────────┐
  │                                                                 │
  │  NEVER compress these (byte-for-byte exact):                    │
  │                                                                 │
  │  ┌─────────────────────────────────────────────────────────┐   │
  │  │ Code blocks                                            │   │
  │  │   ```hcl                                               │   │
  │  │   resource "aws_eks_cluster" "main" {                  │   │
  │  │     name     = "production-eks"                        │   │
  │  │     role_arn = aws_iam_role.cluster.arn                │   │
  │  │   }                                                    │   │
  │  │   ```                                                  │   │
  │  └─────────────────────────────────────────────────────────┘   │
  │                                                                 │
  │  Commit messages, PR descriptions                               │
  │  Error messages (exact quote)                                   │
  │  File paths, URLs, identifiers                                  │
  │  Stack traces                                                   │
  │  Technical English terms: token, function, async, middleware,   │
  │    hook, plugin, build, deploy, error, bug, fix, kubectl,      │
  │    terraform, helm, argocd, vault, k8s, pod, deploy, svc      │
  │                                                                 │
  └─────────────────────────────────────────────────────────────────┘
```

---

## 7. Cost and Performance Impact

### Per-Response Savings

```
                    Tokens per Response

  ┌─────────────────────────────────────────────────────────────┐
  │                                                             │
  │  Normal Thai      ████████████████████████████████  500     │
  │                                                             │
  │  Pordee Lite      █████████████████████████░░░░░░░  280     │
  │                    ├── 44% saved ──┤                        │
  │                                                             │
  │  Pordee Full      ██████████████████░░░░░░░░░░░░░  135      │
  │                    ├──── 73% saved ──────┤                  │
  │                                                             │
  └─────────────────────────────────────────────────────────────┘
```

### Monthly Cost Projection

```
  Scenario: 50 developers, 200 Thai requests/day

  ┌──────────────────────────────────────────────────────────────────┐
  │                                                                  │
  │  Model: claude-sonnet-4-6 ($15/1M output tokens)                 │
  │                                                                  │
  │  BEFORE (no pordee)                                       $2,250 │
  │  ████████████████████████████████████████████████████████        │
  │                                                                  │
  │  AFTER (pordee full)                                       $608  │
  │  █████████████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░           │
  │                                                                  │
  │  SAVED                                                   $1,642  │
  │  ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░             │
  │                                                                  │
  ├──────────────────────────────────────────────────────────────────┤
  │                                                                  │
  │  Model: glm-5.1 ($4.4/1M output tokens)                          │
  │                                                                  │
  │  BEFORE (no pordee)                                        $660  │
  │  ████████████████████████████████████████████████████            │
  │                                                                  │
  │  AFTER (pordee full)                                       $178  │
  │  ██████████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░              │
  │                                                                  │
  │  SAVED                                                     $482  │
  │  ░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░                │
  │                                                                  │
  └──────────────────────────────────────────────────────────────────┘
```

### 12-Month Cumulative (scaling +20%/quarter)

```
  $30K ┤                                                    ╭───
       │                                                ╭───╯
  $25K ┤                                            ╭───╯
       │  BEFORE ██████████████████████████████████████
  $20K ┤                                        ╭───╯
       │                                    ╭───╯
  $15K ┤                                ╭───╯
       │                            ╭───╯  AFTER
  $10K ┤                        ╭───╯  ████████████████████
       │                    ╭───╯
   $5K ┤                ╭───╯
       │  SAVED ░░░░░░░╯░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░
       └──┬─────────┬─────────┬─────────┬─────────┬────────
         M1        M3        M6        M9        M12

  12-month: BEFORE $28,800 | AFTER $7,776 | SAVED $21,024 (73%)
```

### Latency Improvement

```
  Single Thai response (claude-sonnet-4-6, streaming)

  BEFORE:  |████████████████████████████| 500 tokens
           TTFB ~1.2s     Stream ~4.8s     Total ~6.0s

  AFTER:   |██████████████| 135 tokens
           TTFB ~1.1s     Stream ~1.3s     Total ~2.4s

  Delta:   -60% total response time
           User perceives: "snappy" response
```

---

## 8. Configuration

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `PORDEE_ENABLED` | `false` | Enable/disable Thai compression |
| `PORDEE_LEVEL` | `full` | Default level: `lite`, `full`, or `off` |

### Request Header Override

| Header | Values | Description |
|---|---|---|
| `X-Pordee-Level` | `off`, `lite`, `full` | Per-request level override |

### Priority Order

```
  1. X-Pordee-Level header (per-request)
  2. PORDEE_LEVEL env var (default)
  3. "full" (fallback)
```

---

## 9. Metrics

### Prometheus Metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `api_gateway_pordee_injections_total` | Counter | `level` | Number of pordee injections by level |
| `api_gateway_pordee_tokens_saved_total` | Counter | `level`, `model` | Estimated output tokens saved |
| `api_gateway_pordee_cost_saved_total` | Counter | `model` | Estimated USD saved |
| `api_gateway_pordee_duration_seconds` | Histogram | `level` | Time to detect Thai + inject rules |

### Grafana Dashboard Panel

```
  ┌─────────────────────────────────────────────────────────────┐
  │  Pordee Thai Compression                                    │
  ├──────────────────────┬──────────────────────────────────────┤
  │                      │                                      │
  │  Injection Rate      │  Estimated Token Savings             │
  │  [graph over time]   │  [counter: 2.1M tokens saved]        │
  │                      │                                      │
  ├──────────────────────┼──────────────────────────────────────┤
  │                      │                                      │
  │  Level Distribution  │  Cost Saved                          │
  │  full  ████████ 85%  │  $1,642 this month                   │
  │  lite  ████     15%  │                                      │
  │                      │                                      │
  └──────────────────────┴──────────────────────────────────────┘
```

---

## References

- Pordee plugin: `improvements/pordee/` (original Claude Code plugin)
- Caveman pipeline: `api-gateway/caveman/caveman.go` (English compression, same mechanism)
- Thai detection: `api-gateway/proxy/anthropic.go` line 385 (`thaiRe` regex)
- Optimizer pipeline: `api-gateway/handler/optimizers.go` (integration point at stage F17)
- Model pricing: `api-gateway/config/config.go` (`defaultModelPricing`)
