# Use Case: Kubernetes Production Incident Debugging via Optimizer Gateway

> บันทึกเหตุการณ์จริง - On-call Engineer Debugging Session
> Model: Claude Sonnet 4 via API Gateway | Session ID: `inc-20260507-0300`
> Context Window: 200K tokens | Session Duration: ~45 minutes

---

## ตัวละคร

- **วิชัย** - Senior DevOps Engineer on-call, 7 ปีประสบการณ์ K8s/AWS
- **Optimizer Gateway** - API Gateway ที่มี 20-stage token optimization pipeline
- **หมายเลข** - ชุด K8s cluster ที่ CP AXTRA รัน production workloads

---

## 03:00 - PagerDuty กระตุ้น

เสียง PagerDuty ดังขึ้นในความมืด วิชัยลืมตาขึ้นมองจอ iOS ในมือ

```
SEV-1 | Production | api-gateway-7d9f8b CrashLoopBackOff
Namespace: production | Cluster: axtra-prod-01
Alert: CrashLoopBackOff (5 restarts in 10min)
Runbook: https://wiki.internal/runbooks/api-gateway-crash
```

วิชัยเปิด laptop เข้า Claude Code ที่เชื่อมต่อ optimizer gateway เริ่ม session ใหม่

---

## Phase 1: Alert Received, Initial Investigation (Turns 1-5)

### Turn 1: Initial Check

```
วิชัย: pod api-gateway-7d9f8b CrashLoopBackOff namespace production
        ช่วยเช็คหน่อยว่าเกิดอะไรขึ้น
```

Gateway รับ request เข้า pipeline:

| Stage | Action | ผลลัพธ์ |
|-------|--------|---------|
| **F10 Warm Start** | Cosine similarity scan กับ session ก่อนหน้า | เจอ session `inc-20260505-network` คล้าย 0.73 (K8s debug) ดึง optimizer params มาใช้ |
| **F1 Chunker** | จัดเรียง stable chunks | Green budget < 60% |
| **F7 Semantic Dedup** | Dedup system prompt | -12 chars (เอา "You are a helpful assistant" ซ้ำออก) |
| **F16 Caveman** | Lite tier (green budget) | Replace system prompt 1,067 -> 229 chars |
| **F19 ToolFilter** | Intent=action, เลือก Bash, Read | จาก 27 tools -> เหลือ 4 (ประหยัด ~1,100 tokens manifest) |
| **F17 TextComp** | Compress message text | "ช่วยเช็คหน่อยว่าเกิดอะไรขึ้น" -> "เช็คสาเหตุ" (balanced mode) |

Claude เรียก Bash:

```bash
kubectl logs api-gateway-7d9f8b -n production --tail=100
kubectl describe pod api-gateway-7d9f8b -n production
kubectl get events -n production --sort-by='.lastTimestamp'
```

### Turn 2: Tool Output Processing

kubectl คืนผลมากมาย: 4,200 chars logs + 6,800 chars describe + 3,100 chars events

**F18 ToolComp** ทำงานหนัก:

| Output Type | Original | Compressed | Savings |
|-------------|----------|------------|---------|
| `kubectl logs` (Log format) | 4,200 chars | 1,260 chars | **70%** - dedup consecutive lines, keep error + context |
| `kubectl describe` (Table format) | 6,800 chars | 2,040 chars | **70%** - strip separators, keep Status/Conditions/Events |
| `kubectl get events` (Table format) | 3,100 chars | 930 chars | **69%** - head+tail+summary |

รวม: **14,100 -> 4,230 chars** (70% compression)

**F4 Prefetcher** เริ่มเรียนรู้ pattern:
```
Bash(kubectl logs) -> Bash(kubectl describe) [confidence: 0.9]
Bash(kubectl describe) -> Bash(kubectl get events) [confidence: 0.85]
```

Prefetcher เตรียม pre-warm Read tool สำหรับ config files ที่น่าจะอ่านต่อ

### Turn 3: Analyze Logs

```
วิชัย: จาก logs ดูเหมือน OOMKilled แต่ไม่แน่ใจ เช็ค memory limits และดู deployment yaml
```

Claude เรียก:
```bash
kubectl get deployment api-gateway -n production -o yaml
```

อ่าน config:
```
Read: /k8s/base/api-gateway/deployment.yaml
```

**F4 Prefetcher** ทำนายถูก! Bash -> Read transition hit, latency ลด 120ms

**F9 Sketch** check: prompt ไม่ซ้ำ, ผ่าน

| Stage | Action | ผลลัพธ์ |
|-------|--------|---------|
| **F18 ToolComp** | YAML output compression | 5,400 -> 1,890 chars (65%) |
| **F19 ToolFilter** | Code intent detected | เลือก Bash, Read, Edit (3 tools) |
| **F8 Delta** | Diff system prompt กับ baseline | ไม่ activate (system prompt ยังไม่เปลี่ยน) |

### Turn 4: Memory Analysis

```
วิชัย: memory limits ตั้ง 512Mi แต่ request 200Mi
        RSS ใน logs แสดง 490MB ก่อน crash
        เช็คว่ามี memory leak ไหม
```

Claude เรียกหลายคำสั่งต่อกัน:
```bash
kubectl top pods -n production --containers
kubectl logs api-gateway-7d9f8b -n production --previous | grep -i "memory\|oom\|heap"
```

**F9 Sketch** เริ่มจับ pattern:
- Turn 3 prompt vs Turn 4 prompt: similarity 0.82 (ใกล้ threshold 0.85)
- ยังไม่ flag เป็น duplicate

**F11 Waste Detection** (post-proxy feedback):
```
Waste scan (request count: 4):
  - empty_response: CLEAR
  - retry_churn: CLEAR
  - loop_detection: CLEAR
  - oversized_context: CLEAR (32% of context)
```

### Turn 5: Confirm Root Cause

```
วิชัย: ได้แล้ว memory leak จาก connection pool
        ตัวเลข goroutine ขึ้นจาก 200 เป็น 15,000 ใน 4 ชม.
        ช่วยเขียน patch ให้ deployment เพิ่ม memory limit
```

| Stage | Action | ผลลัพธ์ |
|-------|--------|---------|
| **F8 Delta** | System prompt เปลี่ยนเล็กน้อย | Activate! Diff encode: `===` (unchanged) + `+เพิ่ม memory limit` = **45% savings** |
| **F13 Intent Filter** | Code intent detected | Extract เฉพาะ YAML patch จาก verbose response |
| **F16 Caveman** | Lite tier | 229 chars directive |

### Phase 1 Metrics Summary

| Metric | Value |
|--------|-------|
| **Total Turns** | 5 |
| **Budget Level** | Green (22% context used) |
| **Input Tokens Without Optimization** | ~18,400 |
| **Input Tokens With Optimization** | ~11,200 |
| **Input Token Savings** | **39.1%** |
| **ToolComp Savings** | 9,870 chars (70% on tool output) |
| **ToolFilter Savings** | ~1,100 tokens (manifest reduction) |
| **Prefetcher Hits** | 2/3 predictions correct |
| **Optimization Overhead** | < 2ms per request |

---

## Phase 2: Deep Investigation (Turns 6-12)

### Turn 6: Paste Error Logs with Secrets

วิชัย copy-paste error log ทั้งก้อนจาก terminal เข้ามาใน chat

```
วิชัย: [paste 3,200 chars of error output]

ERROR [2026-05-07 03:12:44] connection pool exhausted
  dsn: postgres://appuser:s3cureP@ss!@db.internal.axtra.local:5432/production
  DB_PASSWORD=s3cureP@ss!
  API_KEY=sk-live-axtra-4f8b2c1d9e3a7f0b
  upstream: http://10.0.1.45:8080/health -> 503
  internal_ip: 10.0.1.45, 10.0.1.46, 10.0.1.47
```

**PasteGuard** (Privacy Masking) ทำงานทันที:

| Field Detected | Masked To |
|----------------|-----------|
| `s3cureP@ss!` (DB password) | `__PII_SECRET_db_password__` |
| `sk-live-axtra-4f8b2c1d9e3a7f0b` (API key) | `__PII_SECRET_api_key__` |
| `postgres://appuser:***@db.internal.axtra.local:5432/production` | Connection string masked |
| `10.0.1.45` | `__PII_IP_internal_1__` |
| `10.0.1.46` | `__PII_IP_internal_2__` |
| `10.0.1.47` | `__PII_IP_internal_3__` |

สิ่งที่ Claude เห็น:
```
ERROR connection pool exhausted
  dsn: __PII_SECRET_db_connection__
  upstream: http://__PII_IP_internal_1__:8080/health -> 503
```

Claude วิเคราะห์ root cause ได้โดยไม่เห็น credential จริง

### Turn 7-8: Repeated Investigation Queries

```
วิชัย [Turn 7]: เช็ค connection pool metrics อีกที
วิชัย [Turn 8]: ดู connection pool stats อีกรอบ
วิชัย [Turn 9]: ช่วยตรวจสอบ connection pool ของ pod นี้ที
```

**F9 Sketch** จับได้!

| Turn | Similarity vs Previous | Action |
|------|----------------------|--------|
| 7 vs 6 | 0.78 | ผ่าน (ต่ำกว่า 0.85) |
| 8 vs 7 | 0.91 | **FLAG: near-duplicate** - บันทึก 980 chars diagnostic |
| 9 vs 8 | 0.88 | **FLAG: near-duplicate** - บันทึก 1,020 chars diagnostic |

Sketch ไม่ได้ลบ prompt แต่ flag ให้ waste detector ใช้

### Turn 10: Waste Detection Triggers

**F11 Waste Detection** (post-proxy, 10 requests ครบ threshold):

```
Waste scan (request count: 10):
  - loop_detection: ⚠️ WARNING
    → "connection pool" queries repeated 4 times (turns 6,7,8,9)
    → Estimated waste: 2,400 tokens
    → Severity: medium
  - retry_churn: CLEAR
  - empty_response: CLEAR
  - redundant_tool_call: ⚠️ INFO
    → kubectl top pods called twice (turn 4, turn 7)
```

Gateway แนบ waste report เป็น system hint:
```
[OPTIMIZER HINT] loop_detection: Similar queries detected (4x).
Consider consolidating your investigation.
```

### Turn 11-12: Deeper Dive with New Angle

```
วิชัย [Turn 11]: โอเค เปลี่ยนมุม เช็คว่า GOMAXPROCS ตั้งถูกไหม
                   และ GOMEMLIMIT ตรงกับ cgroup limits ไหม
```

```
วิชัย [Turn 12]: เจอแล้ว GOMEMLIMIT ไม่ถูกต้อง ช่วยเขียน patch ทั้ง deployment yaml
                   เพิ่ม memory limit เป็น 1Gi และ set GOMEMLIMIT=900MiB
```

**F8 Delta Encoding** activate อย่างเต็มที่:
- System prompt ใน Turn 12 เทียบกับ baseline: เปลี่ยนแค่ env var values
- Delta encode: `=== [main block unchanged]` + `- GOMEMLIMIT=512MiB` + `+ GOMEMLIMIT=900MiB` + `- memory: 512Mi` + `+ memory: 1Gi`
- **Savings: 58%** (จาก 2,400 chars เหลือ 1,008 chars)

**F13 Intent Filter**: Code intent -> extract เฉพาะ YAML บรรทัดที่เปลี่ยน

### Phase 2 Metrics Summary

| Metric | Value |
|--------|-------|
| **Total Turns** | 7 (Turns 6-12) |
| **Budget Level** | Green -> Yellow transition (52%) |
| **PasteGuard Masks** | 6 secrets + 3 internal IPs |
| **Sketch Duplicate Flags** | 2 (turns 8, 9) |
| **Waste Findings** | loop_detection (WARNING), redundant_tool_call (INFO) |
| **Delta Savings** | 58% on system prompt |
| **Input Tokens Without Optimization** | ~24,600 |
| **Input Tokens With Optimization** | ~14,100 |
| **Input Token Savings** | **42.7%** |
| **Tokens Wasted (diagnostic)** | 2,400 (flagged, not saved) |

---

## Phase 3: Fix and Verify (Turns 13-18)

### Turn 13: Apply the Fix

```
วิชัย: ใช้ patch นี้เลย apply เข้า cluster
```

Claude generates `kubectl patch` command:

```bash
kubectl patch deployment api-gateway -n production --type='json' \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/resources/limits/memory","value":"1Gi"},
        {"op":"replace","path":"/spec/template/spec/containers/0/env/2/value","value":"900MiB"}]'
```

**F13 Intent Filter**: Code intent -> ดึงเฉพาะ kubectl command จาก verbose explanation
- Original response: 890 chars
- Filtered: 340 chars
- **Savings: 62% output tokens**

### Turn 14: Wait for Rollout

```
วิชัย: rollout status เช็คหน่อย
```

```bash
kubectl rollout status deployment/api-gateway -n production --timeout=120s
```

**F19 ToolFilter**: Intent=action, รักษา Bash + Edit เท่านั้น

**F18 ToolComp**: Rollout output compression:
```
# Original (2,400 chars of "Waiting for rollout" repeated)
# ToolComp (Log format): dedup consecutive
Waiting for rollout to finish: 1 of 3 pods updated...
Waiting for rollout to finish: 2 of 3 pods updated...
deployment "api-gateway" successfully rolled out
# Result: 180 chars (92.5% compression!)
```

### Turn 15: Verify Fix

```
วิชัย: pod ขึ้นมาใหม่แล้ว เช็ค memory usage และ goroutine count
```

```bash
kubectl top pods -n production --containers
kubectl exec api-gateway-7d9f8c -n production -- curl -s localhost:8080/debug/vars
```

**F4 Prefetcher** prediction: `Bash -> Bash` (correct! confidence 0.92)

Tool output:
```
# Memory: 380MB / 1024MB limit (37% utilization) -- HEALTHY
# Goroutines: 245 (stable over 5 min)
# Connection pool: 48 active / 100 max
```

### Turn 16: Stress Test

```
วิชัย: รัน load test สั้นๆ เพื่อ confirm ว่า memory ไม่ leak แล้ว
```

**F5 Bandit** เริ่มเรียนรู้ reward signal:

| Arm | Context Features | Reward | Selections |
|-----|-----------------|--------|------------|
| toolcomp + sketch | K8s debug session | 0.87 | 8 |
| toolcomp + delta | K8s debug session | 0.71 | 3 |
| chunker + delta | K8s debug session | 0.45 | 2 |

Bandit สรุป: สำหรับ incident sessions, **toolcomp + sketch** combo ให้ reward สูงสุด

### Turn 17: Check Error Rates

```
วิชัย: เช็ค error rate บน Grafana 5xx rate ลดลงหรือยัง
```

Claude เรียก:
```bash
curl -s 'http://grafana.internal:3000/api/datasources/proxy/1/api/v1/query_range?query=sum(rate(http_requests_total{status=~"5..",namespace="production"}[5m]))' | jq
```

**F18 ToolComp**: JSON format compression
- Original JSON response: 3,800 chars
- Compressed (compact JSON + summary): 760 chars
- **80% compression**

### Turn 18: Confirm Resolution

```
วิชัย: ดีมาก 5xx rate กลับเป็น 0 แล้ว
        pod stable 15 นาทีแล้ว เพิ่ม annotation ให้ deployment
        แล้วปิด incident ticket
```

**F8 Delta**: เปลี่ยนแค่ annotation value
- `=== [all previous context unchanged]`
- `+ incident.inc-20260507-0300: resolved`
- **Savings: 52%**

### Phase 3 Metrics Summary

| Metric | Value |
|--------|-------|
| **Total Turns** | 6 (Turns 13-18) |
| **Budget Level** | Yellow (58%) |
| **Intent Filter Activations** | 3 (code intent) |
| **Delta Savings (avg)** | 52% |
| **ToolComp Peak** | 92.5% (rollout output) |
| **Bandit Best Arm** | toolcomp + sketch (reward: 0.87) |
| **Prefetcher Accuracy** | 85% (6/7 predictions correct) |
| **Input Tokens Without Optimization** | ~16,800 |
| **Input Tokens With Optimization** | ~9,200 |
| **Input Token Savings** | **45.2%** |

---

## Phase 4: Post-Mortem (Turns 19-25)

### Turn 19: Budget Shifts to Yellow

Session ยาวเกิน 50% context window

**F15 Disclosure** เริ่มทำงาน:

| Budget Level | Tool Output Handling |
|-------------|---------------------|
| Green (< 60%) | Pass through (no truncation) |
| **Yellow (>=60%)** | **Truncate to L2Tokens * 8 chars for content > 2000** |
| Red (> 75%) | Truncate to L1Tokens * 4 chars |

วิชัย paste post-mortem template ยาว 4,500 chars

**F15 Disclosure** truncate เป็น 4,000 chars (L2Tokens * 8 = 60 * 8 = 480, แต่ 4,500 < 2,000 threshold ไม่ถูกตัดในกรณีนี้เพราะเป็น user input ไม่ใช่ tool_result)

### Turn 20-22: Write Post-Mortem Document

```
วิชัย [Turn 20]: ช่วยเขียน post-mortem จากข้อมูลทั้งหมดที่เราเก็บมา
                   ใส่ timeline, root cause, action items
```

**F16 Caveman** เปลี่ยนเป็น **Full tier** (yellow budget):
- Lite (green): 30% output reduction
- **Full (yellow): 50% output reduction**
- Ultra (red): 75% output reduction

Caveman inject `[OUTPUT STYLE - full]` directive:
- Response สั้นลง 50% โดยยังคง timeline + root cause + action items ครบ
- ไม่มี "Based on the analysis..." filler
- ไม่มี "It is important to note..." hedging

```
วิชัย [Turn 21]: เพิ่ม section monitoring improvements
วิชัย [Turn 22]: เพิ่ม section เกี่ยวกับ runbook update
```

**F9 Sketch**: Turn 20-22 prompts คล้ายกัน (similarity 0.89) -> flagged near-duplicate
- เป็น expected behavior: post-mortem writing เป็น iterative refinement

### Turn 23: Budget Nears Red

Context usage: 72% -> **Yellow upper boundary**

**F6 Summarizer** เริ่มเตรียมตัว (ยังไม่ activate เพราะ < 75%)

**F15 Disclosure** บีบ tool output ที่ใหญ่ขึ้น:
- Grafana query result 4,200 chars -> truncate เป็น 2,100 chars (50%)

### Turn 24: Final Review

```
วิชัย: สรุป final action items ให้หน่อย และ assign owners
```

**F16 Caveman**: Full tier ยังคงทำงาน

**F13 Intent Filter**: Action intent -> extract เฉพาะ bullet list + owner names
- Verbose response: 1,200 chars
- Filtered: 420 chars
- **65% output savings**

### Turn 25: Session Cleanup

```
วิชัย: เสร็จแล้ว ขอบคุณมาก ปิด session ได้
```

**F14 Cache Eviction** ทำงาน (post-proxy):

| Cache Key | ROI Score | Action |
|-----------|-----------|--------|
| `sys:claude-sonnet-4` (baseline) | 8.7 | KEEP (high ROI) |
| `cache:stats:inc-20260507-0300:k8s_logs` | 1.2 | EVICT (low ROI, session-specific) |
| `cache:stats:inc-20260507-0300:memory_analysis` | 0.8 | EVICT |
| `cache:stats:inc-20260507-0300:connection_pool` | 0.3 | EVICT |

Cache eviction ลบ incident-specific entries ที่จะไม่ใช้แล้ว ประหยัด Redis memory

**F20 CompCache**: Zstd compression บน cache entries ที่เก็บไว้
- Baseline system prompt cache: 4,200 bytes -> 1,050 bytes (75% compression)
- Bandit arm data: 1,800 bytes -> 540 bytes (70% compression)

### Phase 4 Metrics Summary

| Metric | Value |
|--------|-------|
| **Total Turns** | 7 (Turns 19-25) |
| **Budget Level** | Yellow (58% -> 71%) |
| **Caveman Tier** | Full (50% output reduction) |
| **Disclosure Truncations** | 2 (tool outputs > 2000 chars) |
| **Sketch Flags** | 3 (iterative post-mortem refinement) |
| **Cache Evictions** | 3 keys (low ROI incident data) |
| **CompCache Savings** | 75% on retained cache entries |
| **Input Tokens Without Optimization** | ~28,400 |
| **Input Tokens With Optimization** | ~17,200 |
| **Input Token Savings** | **39.4%** |

---

## Full Session Metrics

### Per-Phase Token Comparison

| Phase | Turns | Budget | Tokens (Raw) | Tokens (Optimized) | Savings % | Key Optimizers |
|-------|-------|--------|--------------|---------------------|-----------|----------------|
| **1. Initial Investigation** | 1-5 | Green | 18,400 | 11,200 | **39.1%** | ToolComp, ToolFilter, Prefetcher, Caveman Lite |
| **2. Deep Investigation** | 6-12 | Green->Yellow | 24,600 | 14,100 | **42.7%** | PasteGuard, Sketch, Waste, Delta |
| **3. Fix and Verify** | 13-18 | Yellow | 16,800 | 9,200 | **45.2%** | Delta, IntentFilter, Bandit, ToolComp |
| **4. Post-Mortem** | 19-25 | Yellow (high) | 28,400 | 17,200 | **39.4%** | Caveman Full, Disclosure, CacheEviction, CompCache |
| **TOTAL** | **25** | - | **88,200** | **51,700** | **41.4%** | - |

### Cost Comparison (Claude Sonnet 4 pricing: $3/M input, $15/M output)

| | Without Optimization | With Optimization | Savings |
|---|---|---|---|
| **Input Tokens** | 88,200 | 51,700 | 36,500 tokens |
| **Output Tokens** | ~14,000 | ~7,200* | 6,800 tokens |
| **Input Cost** | $0.2646 | $0.1551 | **$0.1095** |
| **Output Cost** | $0.2100 | $0.1080 | **$0.1020** |
| **Total Cost** | **$0.4746** | **$0.2631** | **$0.2115 (44.6%)** |
| **Overhead** | - | $0.0012 (Redis + compute) | - |
| **Net Savings** | - | - | **$0.2103 (44.3%)** |

*Output reduction จาก Caveman (lite/full tier) + Intent Filter

### Optimizer Activation Heatmap

```
Stage              | P1  | P2  | P3  | P4  | Total Activations
-------------------|-----|-----|-----|-----|-------------------
F1 Chunker         |  5  |  7  |  6  |  7  | 25
F4 Prefetcher      |  4  |  5  |  6  |  4  | 19
F5 Bandit          |  5  |  7  |  6  |  7  | 25
F6 Summarizer      |  -  |  -  |  -  |  -  | 0 (never hit red)
F7 Semantic Dedup  |  5  |  7  |  6  |  7  | 25
F8 Delta           |  1  |  3  |  3  |  5  | 12
F9 Sketch          |  3  |  5  |  4  |  5  | 17
F10 Warm Start     |  1  |  -  |  -  |  -  | 1
F11 Waste          |  2  |  4  |  3  |  3  | 12
F13 Intent Filter  |  1  |  2  |  3  |  2  | 8
F14 Cache Eviction |  1  |  1  |  1  |  1  | 4
F15 Disclosure     |  -  |  -  |  1  |  2  | 3
F16 Caveman        |  5  |  7  |  6  |  7  | 25
F17 TextComp       |  5  |  7  |  6  |  7  | 25
F18 ToolComp       |  4  |  6  |  5  |  3  | 18
F19 ToolFilter     |  5  |  5  |  4  |  3  | 17
F20 CompCache      |  5  |  7  |  6  |  7  | 25
```

### Per-Technique Token Savings Breakdown

| Technique | Chars Saved | Est. Tokens | % of Total Savings | Category |
|-----------|-------------|-------------|---------------------|----------|
| **ToolComp** | 28,400 | 7,100 | 19.4% | Input |
| **Caveman** | 19,200* | 4,800 | 13.2% | Output influence |
| **Delta** | 12,800 | 3,200 | 8.8% | Input |
| **ToolFilter** | 6,600 | 1,650 | 4.5% | Input |
| **Intent Filter** | 5,200 | 1,300 | 3.6% | Output |
| **Sketch** | 3,800 (flagged) | 950 | 2.6% | Input diagnostic |
| **TextComp** | 3,200 | 800 | 2.2% | Input |
| **Semantic Dedup** | 1,400 | 350 | 1.0% | Input |
| **Message Dedup** | 800 | 200 | 0.5% | Input |
| **Disclosure** | 4,200 | 1,050 | 2.9% | Input |
| **CompCache** | 8,400 (Redis) | N/A | Indirect | Redis memory |
| **Warm Start** | N/A | ~800 | 2.2% | Cold-start prevention |
| **Prefetcher** | N/A | ~1,200 latency | Indirect | Latency reduction |
| **Bandit** | N/A | ~540 | 1.5% | Meta-optimization |

*Caveman chars = system prompt replaced, not direct output savings

### Session Timeline

```
03:00 ┃ PagerDuty alert received
03:02 ┃ Session started, Warm Start loads K8s debug profile
03:03 ┃ Turn 1: kubectl logs/describe/events -> ToolComp 70%
03:05 ┃ Turn 3: Prefetcher predicts Bash->Read correctly
03:08 ┃ Turn 5: Delta first activation (45% savings)
03:10 ┃ Turn 6: PasteGuard masks 6 secrets + 3 IPs
03:14 ┃ Turn 8-9: Sketch flags duplicate queries
03:16 ┃ Turn 10: Waste detection -> loop_detection WARNING
03:20 ┃ Turn 12: Delta peak (58%) for env var change
03:22 ┃ Turn 13: Intent Filter extracts kubectl patch command
03:24 ┃ Turn 14: ToolComp 92.5% on rollout output
03:28 ┃ Turn 16: Bandit identifies toolcomp+sketch as best arm
03:32 ┃ Turn 18: Incident resolved, annotation added
03:35 ┃ Turn 19: Budget shifts to Yellow, Caveman -> Full tier
03:40 ┃ Turn 20-22: Post-mortem writing with Full tier brevity
03:44 ┃ Turn 23: Disclosure truncates Grafana output 50%
03:46 ┃ Turn 25: Cache Eviction cleans incident entries
03:47 ┃ Session closed
```

---

## Key Takeaways

**1. ToolComp ประหยัดที่สุดสำหรับ incident workflows**
kubectl output มี structure ที่ compress ได้ดีมาก (log format, table format) เฉลี่ย 70-80% compression

**2. Delta Encoding โดดเด่นเมื่อ fix ซ้ำ**
เปลี่ยนแค่ env var หรือ annotation -> 58% savings เพราะส่วนใหญ่ของ system prompt เหมือนเดิม

**3. PasteGuard ป้องกัน credential leak โดยอัตโนมัติ**
วิชัย paste log ที่มี DB password, API key, internal IPs โดยไม่ตั้งใจ -> masked ทั้งหมดก่อนส่งให้ Claude

**4. Bandit เรียนรู้ pattern ของ session type**
หลัง 16 turns, Bandit รู้แล้วว่า incident session ควรใช้ toolcomp + sketch combination

**5. Waste Detection ช่วย break loop**
วิชัยถามซ้ำ 4 ครั้งเกี่ยวกับ connection pool -> loop_detection flag -> วิชัยเปลี่ยนมุมมองการสืบสวน

**6. Budget-aware degradation ทำงานโดยไม่รู้สึก**
Green -> Yellow transition ระหว่าง session: Caveman เปลี่ยน Lite -> Full, Disclosure เริ่ม truncate
วิชัยไม่รู้สึกเลยว่า output เปลี่ยน เพราะข้อมูลสำคัญยังคงอยู่ครบ

**7. 44.3% ประหยัดต้นทุน สำหรับ SEV-1 incident**
Session 25 turns ใช้เงิน $0.26 แทน $0.47 โดยไม่สูญเสียคุณภาพการวิเคราะห์เลย
สำหรับทีมที่มี 50+ incidents/เดือน ประหยัด ~$10/เดือนเฉพาะ incident sessions (ไม่รวม regular usage)

---

> Generated: 2026-05-07 | Session: inc-20260507-0300
> Optimizer Pipeline: 20 stages | Model: Claude Sonnet 4
> All metrics are illustrative estimates based on optimizer stage specifications
