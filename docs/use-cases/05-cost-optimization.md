# Cost Optimization Analysis: API Gateway สำหรับทีม Claude Code

**ผู้จัดทำ**: Platform Engineering Team
**วันที่**: 7 พฤษภาคม 2569
**กลุ่มเป้าหมาย**: ผู้จัดการฝ่าย IT
**ขอบเขต**: วิเคราะห์ต้นทุนและการประหยัดจาก 13-stage Token Optimization Pipeline

---

## สารบัญ

1. [ต้นทุนก่อนใช้ Gateway](#1-ต้นทุนก่อนใช้-gateway)
2. [การประหยัดแยกตาม Optimizer Stage](#2-การประหยัดแยกตาม-optimizer-stage)
3. [Multi-Provider Cost Comparison](#3-multi-provider-cost-comparison)
4. [ROI Calculation](#4-roi-calculation)
5. [Grafana Dashboard Metrics](#5-grafana-dashboard-metrics)

---

## 1. ต้นทุนก่อนใช้ Gateway

### 1.1 สมมติฐานการใช้งาน

| พารามิเตอร์ | ค่า |
|---|---|
| จำนวน Engineers | 10 คน |
| เวลาใช้งาน Claude Code | 8 ชม./วัน |
| Requests ต่อ Engineer | 50 requests/วัน |
| Total Requests/วัน | **500 requests/วัน** |
| เฉลี่ย Input Tokens | 3,000 tokens/request |
| เฉลี่ย Output Tokens | 800 tokens/request |
| วันทำงาน/เดือน | 22 วัน |
| Requests/เดือน | **11,000 requests/เดือน** |

### 1.2 ต้นทุนโดยตรง (Claude Sonnet Direct API)

| รายการ | อัตรา | ต่อวัน | ต่อเดือน |
|---|---|---|---|
| Input Tokens | $3.00/M tokens | 500 x 3,000 = 1,500,000 tokens = **$4.50** | **$99.00** |
| Output Tokens | $15.00/M tokens | 500 x 800 = 400,000 tokens = **$6.00** | **$132.00** |
| **ต้นทุนรวม** | | **$10.50/วัน** | **$231.00/เดือน** |

### 1.3 ต้นทุนแฝงที่ไม่คิดใน Bill

| รายการ | ผลกระทบ | ต้นทุนประมาณการ |
|---|---|---|
| Retry จาก 429 (Rate Limit) | 10-20% requests ต้อง retry | +10-20% token cost |
| Context Overflow | ส่ง context เกิน limit แล้ว fail | +5-10% waste |
| Verbose Output | Model ตอบยาวเกินความจำเป็น | +30-50% output tokens |
| ไม่มี Caching | System prompt ส่งซ้ำทุก request | +15-25% input tokens |
| Secrets/PII Leak | ความเสี่ยงด้าน compliance | มูลค่าไม่อาจประเมินได้ |

### 1.4 ต้นทุนจริงที่คาดว่าจะสูงกว่า

```
Base Cost:           $231.00/เดือน
+ Retry Overhead:    $34.65/เดือน  (+15%)
+ Context Waste:     $17.33/เดือน  (+7.5%)
+ Verbose Output:    $92.40/เดือน  (+40% output)
+ No Caching:        $46.20/เดือน  (+20% input)
-----------------------------
Estimated Real Cost: $421.58/เดือน
```

**สรุป**: ต้นทุนจริงประมาณ **$420-450/เดือน** สำหรับทีม 10 คน หรือประมาณ **$42-45/คน/เดือน**

---

## 2. การประหยัดแยกตาม Optimizer Stage

### 2.1 ตาราง Stage ทั้งหมด (17 Stages)

> ข้อมูลจาก Load Test จริง (7 requests, localhost, 2026-05-06) + Estimated Production Scale
> Production คูณด้วย scaling factor: 500 requests/วัน vs 7 requests load test = ~71x

| # | Stage | ประเภท | % Savings | Tokens Saved/วัน | Cost Saved/วัน | หมายเหตุ |
|---|---|---|---|---|---|---|
| F7 | Semantic Dedup | INPUT | 3-5% | ~5,250 | $0.016 | ลบประโยคซ้ำใน system prompt |
| F1 | Chunker | INPUT | 5-15% | ~7,875 | $0.024 | Reorder stable chunks, เพิ่ม cache hit |
| F8 | Delta Encoding | INPUT | 20-60% | ~52,500 | $0.158 | Diff-encode สำหรับ iterative edits |
| F9 | Sketch Dedup | INPUT | 5-30% | ~26,250 | $0.079 | ตรวจ near-duplicate prompts |
| F6 | Summarizer | INPUT | 50-70% | ~78,750* | $0.236* | Red budget เท่านั้น (>75% context) |
| F13 | Intent Filter | OUTPUT | 10-40% | ~20,000 | $0.300 | กรอง response ตาม intent |
| F17 | TextComp | INPUT | 5-15% | ~13,125 | $0.039 | ตัด filler/verbose text |
| F16 | **Caveman** | **OUTPUT** | **30-75%** | **~120,000** | **$1.800** | **ลด output ด้วย style injection** |
| F18 | **ToolComp** | **INPUT** | **40-80%** | **~42,000** | **$0.126** | **บีบ tool_result (shell, JSON)** |
| F19 | **ToolFilter** | **INPUT** | **60-80%** | **~4,500** | **$0.014** | **กรอง tool manifest (3000-6000 tok)** |
| F20 | CompCache | Redis | 60-80% | (indirect) | - | ลด Redis memory 60-80% |
| F15 | Disclosure | INPUT | 50-70% | ~15,000* | $0.045* | Yellow/Red budget เท่านั้น |
| - | Message WS+Dedup | INPUT | 3-8% | ~5,250 | $0.016 | Whitespace + sentence dedup |
| - | Message TextComp | INPUT | 5-15% | ~10,500 | $0.032 | TextComp บน message content |
| F4 | Prefetcher | Latency | 50-200ms | (indirect) | - | ลด latency, ไม่ลด tokens โดยตรง |
| F5 | Bandit | Meta | 5-15% | (indirect) | - | เรียนรู้ strategy ที่ดีที่สุด |
| F11 | Waste Detection | Diagnostic | 5-20% | (indirect) | - | ระบุ token waste patterns |
| F14 | Cache Eviction | Cache | ~10% hit rate | (indirect) | - | รักษา cache quality |
| F10 | Warm Start | Cold-start | 10-20% | (indirect) | - | ลด cold-start waste |

*ทำงานเฉพาะเมื่อ budget level เฉพาะเจาะจง (Red/Yellow)

### 2.2 Top 3 Savers (ผู้ประหยัดรายใหญ่)

#### 1st: Caveman (F16) - ประหยัด Output Tokens

```
Mechanism:  ฉีด [OUTPUT STYLE] directive เข้า system prompt
            สั่งให้ model ตอบสั้นลง (30-75% ตาม tier)

4 Tiers:
  Lite  (Green):  -30% output, bullet points
  Full  (Yellow): -50% output, code only
  Ultra (Red):    -75% output, raw only

จาก Load Test:  System prompt เฉลี่ย 467.6 chars ถูกแทนที่ด้วย 229-char directive
Overhead:       0.03ms/request (negligible)
```

**ผลประหยัดประมาณการ**: $1.80/วัน = **$39.60/เดือน** (ลด output cost ~40%)

#### 2nd: ToolComp (F18) - บีบ Tool Result

```
Mechanism:  ตรวจ format อัตโนมัติ (JSON/Shell/Table/Diff/Log/Prose)
            ใช้ compression เฉพาะ format
  JSON:     compact (ลบ whitespace)
  Shell ls: head + tail + summary
  Table:    strip separators
  Diff:     keep changes only
  Log:      dedup consecutive lines

ตัวอย่าง:  ls -la output 115 chars saved (load test, 6 tool_result blocks)
```

**ผลประหยัดประมาณการ**: $0.13/วัน = **$2.77/เดือน**

#### 3rd: ToolFilter (F19) - กรอง Tool Manifest

```
Mechanism:  เมื่อ tools > 15, จำแนก intent (code/search/analysis/action)
            Score แต่ละ tool, เก็บ top-K + essential tools

ตัวอย่าง:  27-tool manifest -> ลบ 960 chars (load test T3)
            ประหยัด 3,000-6,000 tokens/request สำหรับ MCP sessions
```

**ผลประหยัดประมาณการ**: $0.01/วัน = **$0.31/เดือน** (แต่สูงมากสำหรับ MCP-heavy workflows)

### 2.3 Cumulative Savings (Non-Additive)

Stages ทำงานต่อเนื่องกัน (pipeline) ไม่ได้บวกกันโดยตรง แต่ประเมินได้จาก:

```
ก่อน Gateway:  $421.58/เดือน (รวม hidden costs)

หลัง Gateway:
  Caveman ลด output ~40%:      -$132.00 x 0.40 = -$52.80
  Input stages ลด input ~25%:  -$148.50 x 0.25 = -$37.13
  Retry/context waste ลด ~80%:  -$98.18 x 0.80  = -$78.54
                                          รวม = -$168.47

ต้นทุนหลัง Gateway: $421.58 - $168.47 = $253.11/เดือน
```

### 2.4 สรุป Savings จาก Load Test จริง

จาก Load Test 7 requests (glm-5 pricing):

| ตัวชี้วัด | ค่า |
|---|---|
| Total Input Tokens ที่ส่งจริง | 2,793 |
| Total Output Tokens ที่ส่งจริง | 522 |
| Input Chars Saved (semantic_dedup + sketch + msg_textcomp + toolfilter) | 3,873 chars = ~968 tokens |
| System Prompt ที่ถูก Caveman replace | 3,273 chars = ~818 tokens |
| Cost จริงที่บันทึก | $0.0055 |
| Overhead รวมต่อ request | < 3ms |

---

## 3. Multi-Provider Cost Comparison

### 3.1 ราคา Provider หลัก (USD per 1M Tokens)

| Provider | Model | Input Price | Output Price | คู่แข่งที่ใกล้เคียง |
|---|---|---|---|---|
| **Anthropic** | claude-opus-4-7 | $15.00 | $75.00 | Premium tier |
| **Anthropic** | claude-sonnet-4-6 | $3.00 | $15.00 | Mid tier |
| **Anthropic** | claude-haiku-4-5 | $0.80 | $4.00 | Budget tier |
| **Z.AI** | glm-5 | $1.00 | $3.20 | กลาง-ประหยัด |
| **Z.AI** | glm-5-turbo | $1.20 | $4.00 | กลาง-ประหยัด |
| **Z.AI** | glm-5.1 | $1.40 | $4.40 | กลาง |
| **Z.AI** | glm-5-air | ต่ำกว่า | ต่ำกว่า | Budget tier |
| **Z.AI** | glm-5-flash | **ฟรี** | **ฟรี** | Free tier |
| **Z.AI** | glm-4.6v (Vision) | ~$0.60 | ~$2.20 | Vision |
| **Google** | gemini-2.5-pro | $1.25 | $10.00 | Mid tier |
| **Google** | gemini-2.0-flash | $0.10 | $0.40 | Budget tier |
| **Google** | gemini-2.0-flash (free) | **ฟรี** | **ฟรี** | Free tier |

### 3.2 Cost Breakdown ตามประเภทงาน

สมมติฐาน: 500 requests/วัน, แบ่งตามประเภทงาน

| ประเภทงาน | % ของ Requests | Avg Input Tok | Avg Output Tok | Requests/วัน |
|---|---|---|---|---|
| Code Generation | 35% | 4,000 | 1,200 | 175 |
| Config Edit | 25% | 2,500 | 400 | 125 |
| Analysis/Review | 20% | 5,000 | 1,500 | 100 |
| Chat/Q&A | 20% | 1,500 | 600 | 100 |

#### ต้นทุนต่อวัน ตาม Provider และประเภทงาน

| ประเภทงาน | Claude Sonnet | Z.AI glm-5 | Gemini Flash | ประหยัดสุด |
|---|---|---|---|---|
| Code Gen | $4.20 | $1.01 | $0.22 | Gemini Flash |
| Config Edit | $1.35 | $0.34 | $0.04 | Gemini Flash |
| Analysis | $5.25 | $1.30 | $0.28 | Gemini Flash |
| Chat/Q&A | $1.35 | $0.35 | $0.07 | Gemini Flash |
| **รวม/วัน** | **$12.15** | **$3.00** | **$0.61** | - |
| **รวม/เดือน** | **$267.30** | **$66.00** | **$13.42** | - |

### 3.3 Bandit-Optimized Routing Decisions

Gateway ใช้ LinUCB (Multi-Armed Bandit) เพื่อเรียนรู้ว่า provider ไหนเหมาะกับงานแบบไหน:

```
Bandit Decision Flow:
  1. รับ request -> สร้าง 10-dim context feature vector
  2. คำนวณ score สำหรับแต่ละ provider (arm)
     score = theta^T * phi + alpha * sqrt(phi^T * A^-1 * phi)
               ^^^^^^^^   ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
               mean        uncertainty bonus (exploration)
  3. เลือก provider ที่ score สูงสุด
  4. หลังได้ response -> คำนวณ reward (output/input ratio)
  5. อัปเดต A matrix + b vector ใน Redis (TTL 24h)
```

**กลยุทธ์ที่ Bandit เรียนรู้โดยทั่วไป:**

| ประเภทงาน | Provider ที่เลือก | เหตุผล |
|---|---|---|
| Code Generation | Z.AI glm-5.1 | ราคาประหยัด, code quality ดี |
| Config Edit | Z.AI glm-5-turbo | เร็ว, งานไม่ซับซ้อน |
| Analysis/Review | Z.AI glm-5 / Claude Sonnet | ต้องการ reasoning depth |
| Chat/Q&A | Z.AI glm-5-flash (ฟรี) | งานง่าย, ใช้ free tier ได้ |

### 3.4 Fallback Chain

```
glm -> openai -> anthropic -> gemini -> openrouter -> deepseek -> kimi -> ...
```

เมื่อ provider หลัก rate limit (429) หรือล่ม จะ fallback ไป provider ถัดไปอัตโนมัติ

---

## 4. ROI Calculation

### 4.1 Gateway Infrastructure Cost

#### ตัวเลือก A: Kubernetes (Production)

| Component | Replicas | CPU Req/Limit | Memory Req/Limit | ต้นทุนประมาณ/เดือน* |
|---|---|---|---|---|
| arl-gateway (Go) | 2 | 500m/2 | 256Mi/1Gi | $60 |
| arl-rate-limiter (Java) | 2 | 500m/2 | 512Mi/1.5Gi | $80 |
| arl-dragonfly (Redis) | 1 | 500m/2 | 1Gi/8Gi | $120 |
| arl-worker (Python) | 2 | 500m/2 | 512Mi/2Gi | $70 |
| arl-prometheus | 1 | 250m/2 | 256Mi/2Gi | $40 |
| arl-grafana | 1 | 250m/2 | 256Mi/1Gi | $25 |
| arl-otel | 1 | 250m/1 | 128Mi/512Mi | $15 |
| arl-proxy (Caddy) | 2 | 100m/500m | 32Mi/128Mi | $20 |
| arl-dashboard | 2 | 100m/500m | 64Mi/256Mi | $25 |
| arl-sidecar | 1 | 100m/500m | 128Mi/256Mi | $20 |
| **รวม** | **15 pods** | | | **$475/เดือน** |

*ประมาณการจาก Tencent Cloud TKE / AWS EKS pricing (managed K8s)

#### ตัวเลือก B: Docker Compose (Small Team)

| Component | Memory Limit | ต้นทุนประมาณ/เดือน* |
|---|---|---|
| 1x VM (8 vCPU, 16GB RAM) | ~12GB total | $120 |
| 1x VM (2 vCPU, 4GB RAM) - Observer | ~4GB total | $40 |
| **รวม** | | **$160/เดือน** |

*ประมาณการจาก Tencent Cloud CVM / AWS EC2

### 4.2 Net Savings Calculation

#### สถานการณ์ A: ใช้ Claude Sonnet Direct (ผ่าน Gateway)

| รายการ | จำนวน |
|---|---|
| ต้นทุน API ก่อน Gateway | $421.58/เดือน |
| ต้นทุน API หลัง Gateway (ลด ~40%) | $253.11/เดือน |
| **การประหยัด API** | **$168.47/เดือน** |
| ต้นทุน Infrastructure (K8s) | $475.00/เดือน |
| **Net (K8s)** | **-$306.53/เดือน (ขาดทุน)** |
| ต้นทุน Infrastructure (Docker) | $160.00/เดือน |
| **Net (Docker)** | **$8.47/เดือน (เพิ่มขึ้นเล็กน้อย)** |

> หมายเหตุ: สำหรับทีม 10 คนกับ Claude Sonnet อย่างเดียว Gateway ยังไม่คุ้มค่าทางการเงิน
> แต่ถ้ารวมผลประโยชน์ด้าน security (PasteGuard), observability, rate limiting, multi-provider routing
> คุณค่าที่ได้จะมากกว่าตัวเลขการเงิน

#### สถานการณ์ B: ใช้ Z.AI glm-5 ผ่าน Gateway (แนะนำ)

| รายการ | จำนวน |
|---|---|
| ต้นทุน Claude Sonnet Direct (ปัจจุบัน) | $421.58/เดือน |
| ต้นทุน Z.AI glm-5 ผ่าน Gateway (หลัง optimize) | $66.00 x 0.60 = $39.60/เดือน |
| **การประหยัดจาก Provider Switch** | **$381.98/เดือน** |
| ต้นทุน Infrastructure (Docker) | $160.00/เดือน |
| **Net Savings (Docker)** | **+$221.98/เดือน** |
| ต้นทุน Infrastructure (K8s) | $475.00/เดือน |
| **Net Savings (K8s)** | **-$93.02/เดือน** |

#### สถานการณ์ C: Mixed Provider + Gateway (Optimal)

ใช้ Z.AI เป็นหลัก + Claude สำหรับงานที่ต้องการ quality สูง + Gemini Flash สำหรับงานเบา

| รายการ | Provider | Cost/เดือน |
|---|---|---|
| Code Gen (175 req/วัน) | Z.AI glm-5.1 | $22.22 |
| Config Edit (125 req/วัน) | Z.AI glm-5-turbo | $7.48 |
| Analysis (100 req/วัน) | Claude Sonnet | $115.50 |
| Chat/Q&A (100 req/วัน) | Z.AI glm-5-flash (ฟรี) | $0.00 |
| **ต้นทุน API รวม** | | **$145.20/เดือน** |
| Gateway Optimization (-15%) | | -$21.78 |
| **ต้นทุน API หลัง Gateway** | | **$123.42/เดือน** |
| ต้นทุน Infrastructure (Docker) | | $160.00/เดือน |
| **Total** | | **$283.42/เดือน** |
| vs Claude Direct | | $421.58/เดือน |
| **Net Savings** | | **$138.16/เดือน (32.8%)** |

### 4.3 Payback Period

| สถานการณ์ | Infrastructure | Net Savings/เดือน | Payback Period* |
|---|---|---|---|
| Docker + Z.AI Only | $160/เดือน | $222/เดือน | **< 1 เดือน** |
| Docker + Mixed | $160/เดือน | $138/เดือน | **< 1 เดือน** |
| K8s + Mixed | $475/เดือน | -$93/เดือน | ไม่คุ้มจากตัวเลขเดียว** |
| K8s + Z.AI Only | $475/เดือน | -$93/เดือน | ไม่คุ้มจากตัวเลขเดียว** |

*Payback = เวลาที่ savings ครอบคลุม setup cost (ประมาณ 2-3 วันในการ deploy)

**K8s จะคุ้มค่าเมื่อทีมขยายเกิน 25+ คน หรือมี multi-team usage

### 4.4 3-Year TCO Comparison

| รายการ | Claude Direct | Z.AI via Gateway (Docker) | Mixed via Gateway (Docker) |
|---|---|---|---|
| API Cost/เดือน | $421.58 | $39.60 | $123.42 |
| Infra Cost/เดือน | $0 | $160.00 | $160.00 |
| Total/เดือน | $421.58 | $199.60 | $283.42 |
| **Total/ปี** | **$5,058.96** | **$2,395.20** | **$3,401.04** |
| **3-Year TCO** | **$15,176.88** | **$7,185.60** | **$10,203.12** |
| **3-Year Savings** | - | **$7,991.28** | **$4,973.76** |
| **% Savings** | - | **52.7%** | **32.8%** |

### 4.5 Scaling Analysis (คุ้มที่กี่คน?)

| จำนวน Engineers | Claude Direct | Z.AI Gateway (Docker) | Savings | คุ้มไหม? |
|---|---|---|---|---|
| 5 คน | $210.79 | $199.60 | $11.19 | คุ้มเล็กน้อย |
| 10 คน | $421.58 | $199.60 | $221.98 | **คุ้ม** |
| 25 คน | $1,053.95 | $199.60 | $854.35 | **คุ้มมาก** |
| 50 คน | $2,107.90 | $199.60 | $1,908.30 | **คุ้มมาก** |
| 100 คน | $4,215.80 | $199.60 | $4,016.20 | **คุ้มมาก** |

> Gateway infrastructure cost คงที่ ไม่ว่าจะมีกี่ users
> ดังนั้นยิ่งทีมใหญ่ ยิ่งคุ้มค่า

---

## 5. Grafana Dashboard Metrics

### 5.1 Key PromQL Queries สำหรับ Cost Monitoring

#### Cost per Request

```promql
# ต้นทุนเฉลี่ยต่อ request
sum(increase(api_gateway_cost_total[24h]))
/
sum(increase(api_gateway_request_latency_seconds_count[24h]))
```

#### Cost per User (Profile)

```promql
# ต้นทุนต่อ profile ต่อวัน
sum by (profile) (increase(api_gateway_profile_cost_total[24h]))
```

#### Cost per Team (Account)

```promql
# ต้นทุนต่อ account ต่อวัน
sum by (account_id) (
  increase(api_gateway_account_token_input_total[24h]) * blended_input_rate
  +
  increase(api_gateway_account_token_output_total[24h]) * blended_output_rate
)
```

#### Blended Rate (Dynamic)

```promql
# Blended input rate (USD per token) - คำนวณจากข้อมูลจริง
sum(increase(api_gateway_cost_total{type="input"}[24h]))
/
(sum(increase(api_gateway_token_input_total[24h])) + 1)
```

#### Cost Savings Rate

```promql
# อัตราการประหยัด ($/วินาที)
sum(rate(api_gateway_optimizer_chars_saved_total{direction="input"}[5m])) / 4
*
sum(rate(api_gateway_cost_total{type="input"}[5m]))
/
(sum(rate(api_gateway_token_input_total[5m])) + 1)
```

### 5.2 Cost Dashboard Panels

#### ต้นทุนรวม (Actual Billed Cost)

```promql
# Total cost (post-optimization) - ต้นทุนจริงที่ต้องจ่าย
sum(increase(api_gateway_cost_total[$__range]))
```

#### การประหยัดจาก Optimizer

```promql
# Estimated cost savings using dynamic blended rate
(
  sum(increase(api_gateway_optimizer_chars_saved_total{direction="input"}[$__range])) / 4
)
*
(
  sum(increase(api_gateway_cost_total{type="input"}[$__range]))
  /
  (sum(increase(api_gateway_token_input_total[$__range])) + 1)
)
```

#### ต้นทุนแยกตาม Model

```promql
# Cost distribution per model
sum by (model) (increase(api_gateway_cost_total[$__range]))
```

#### ต้นทุนแยกตาม Provider Path

```promql
# Cost by billing path (direct vs sidecar vs rejected)
sum by (path) (increase(api_gateway_billing_path_requests_total[$__range]))
```

#### Before vs After Optimization

```promql
# Without optimization (hypothetical)
sum(increase(api_gateway_token_input_total[$__range]))

# With optimization (actual billed)
sum(increase(api_gateway_token_input_total[$__range]))
-
sum(increase(api_gateway_optimizer_chars_saved_total{direction="input"}[$__range])) / 4
```

### 5.3 Budget Burn Rate Alerts

#### Alert: Daily Budget Exceeded

```yaml
# Prometheus Alert Rule
- alert: DailyBudgetBurnRate
  expr: |
    sum(increase(api_gateway_cost_total[1h])) * 24
    >
    57600 * 0.05  # QUOTA_DAILY_BUDGET * 5% threshold
  for: 30m
  labels:
    severity: warning
  annotations:
    summary: "Daily budget burn rate exceeds threshold"
    description: "Projected daily cost: {{ $value | printf \"%.2f\" }} USD"
```

#### Alert: Cost Anomaly Detection

```yaml
# ตรวจจับ cost spike (ใช้ Z-score)
- alert: CostAnomaly
  expr: |
    (
      sum(rate(api_gateway_cost_total[5m]))
      -
      avg_over_time(sum(rate(api_gateway_cost_total[5m]))[7d])
    )
    /
    stddev_over_time(sum(rate(api_gateway_cost_total[5m]))[7d])
    > 2.0
  for: 10m
  labels:
    severity: critical
  annotations:
    summary: "Unusual cost spike detected"
```

#### Alert: High Waste Detection

```yaml
# Waste tokens สูงผิดปกติ
- alert: HighWasteDetected
  expr: |
    sum(rate(api_gateway_waste_tokens_wasted_total[15m]))
    /
    sum(rate(api_gateway_token_input_total[15m]))
    > 0.15
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "More than 15% of tokens identified as waste"
```

#### Alert: Rate Limit Pressure

```yaml
# 429 rate สูง = คุณภาพการใช้งานต่ำ
- alert: HighRateLimitPressure
  expr: |
    sum(rate(api_gateway_upstream_429_total[5m]))
    /
    sum(rate(api_gateway_request_latency_seconds_count[5m]))
    > 0.10
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "More than 10% of requests hitting rate limits"
```

### 5.4 ตารางสรุป Dashboards ที่เกี่ยวข้องกับ Cost

| Dashboard | Panel หลัก | ใช้สำหรับ |
|---|---|---|
| Service Dashboard | Total Cost, Cost Savings, Optimization Rate | Executive overview |
| Token Optimization | Savings by Technique, Budget Level, Waste Detection | Optimizer performance |
| Cost Calculator | Cost by Model, API Usage, Billing Path | Cost breakdown |
| API Usage Monitor | Per-Profile, Per-Account usage | Chargeback/showback |
| Gateway Overview | Cost Rate by Model, 429 Rate | Technical health |
| Claude OAuth Billing | Billing Path Distribution, Profile Cost | OAuth-specific cost |

### 5.5 สูตรคำนวณ Cost สำหรับแต่ละ Model

```promql
# Z.AI GLM-5 (default pricing from MODEL_PRICING env)
# glm-5:1.0:3.2 = $1.00/M input, $3.20/M output

# Claude Sonnet (if using direct Anthropic)
# claude-sonnet-4-6:3:15 = $3.00/M input, $15.00/M output

# Claude Opus (premium)
# claude-opus-4-7:15:75 = $15.00/M input, $75.00/M output

# Cost per model:
sum by (model) (increase(api_gateway_cost_total[$__range]))
```

---

## สรุปสำหรับผู้จัดการฝ่าย IT

| ตัวชี้วัด | ค่า |
|---|---|
| ทีมปัจจุบัน | 10 Engineers |
| ต้นทุนปัจจุบัน (Claude Direct) | ~$420/เดือน ($42/คน) |
| ต้นทุนหลังใช้ Gateway + Z.AI | ~$200/เดือน ($20/คน) |
| **การประหยัด** | **$220/เดือน (52%)** |
| **3-Year Savings** | **$7,991** |
| Infrastructure Cost (Docker) | $160/เดือน (คงที่) |
| Payback Period | < 1 เดือน |
| จุดคุ้มทุน | 5+ Engineers |
| ผลประโยชน์เพิ่มเติม | Security (PasteGuard), Observability, Rate Limiting, Multi-Provider Fallback |

### คำแนะนำ

1. **เริ่มจาก Docker Compose** - ต้นทุนต่ำ, deploy เร็ว (1 วัน), เหมาะสำหรับทีม 5-25 คน
2. **ใช้ Z.AI glm-5 เป็นหลัก** - ราคา $1.00/M vs Claude $3.00/M (ประหยัด 67%)
3. **เก็บ Claude สำหรับงานที่ต้องการ quality สูง** - Analysis, Architecture decisions
4. **ใช้ Gemini Flash สำหรับงานเบา** - Chat/Q&A, Simple queries (ฟรี)
5. **Monitor ผ่าน Grafana** - ตั้ง budget alerts, ตรวจ waste detection ทุกสัปดาห์
6. **ขยายเป็น K8s เมื่อทีมเกิน 25 คน** - HPA auto-scale, multi-replica, high availability
