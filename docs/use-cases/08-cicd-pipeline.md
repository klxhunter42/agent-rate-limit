# Optimizer Gateway ใน CI/CD Pipeline - Use Cases สำหรับ DevOps Engineer

> ผู้เขียน: เรื่องเล่าของ "กิตติ" - Senior DevOps Engineer ที่กำลังติดตั้ง AI-assisted CI/CD ด้วย Optimizer Gateway

---

## บทนำ: ทำไมต้องใช้ Optimizer Gateway ใน CI/CD


1. **Token cost พุ่ง** - แต่ละ pipeline run ส่ง PR diff, logs, metrics เข้า AI หมด ค่าใช้จ่ายเพิ่ม 40%
2. **Latency สูง** - AI analysis ใช้เวลา 30-60 วินาที ต่อ step ทำให้ pipeline ช้าลง
3. **Secrets leak** - มีโอกาสที่ API keys, credentials ใน PR diff หรือ logs จะถูกส่งไปยัง AI provider

Optimizer Gateway แก้ปัญหาเหล่านี้ด้วย 13-stage pipeline:

```
GitHub Actions / PagerDuty / CronJob
         │
         ▼
┌─────────────────────────────────┐
│     Optimizer Gateway :9000     │
│  ┌───────────────────────────┐  │
│  │  Request Pipeline (pre)   │  │
│  │  ├─ Budget Level Calc     │  │
│  │  ├─ F7  Semantic Dedup    │  │
│  │  ├─ F1  Chunker           │  │
│  │  ├─ F8  Delta Encoding    │  │
│  │  ├─ F9  Sketch Duplicate  │  │
│  │  ├─ F13 Intent Filter     │  │
│  │  ├─ F17 TextComp          │  │
│  │  ├─ F16 Caveman           │  │
│  │  ├─ F18 ToolComp          │  │
│  │  ├─ F19 ToolFilter        │  │
│  │  └─ PasteGuard (PII mask) │  │
│  ├───────────────────────────┤  │
│  │  Provider API (Z.AI/...)  │  │
│  ├───────────────────────────┤  │
│  │  Feedback Pipeline (post) │  │
│  │  ├─ F4  Prefetcher        │  │
│  │  ├─ F10 Warm Start        │  │
│  │  ├─ F11 Waste Detection   │  │
│  │  ├─ F14 Cache Eviction    │  │
│  │  ├─ F5  Bandit            │  │
│  │  └─ F20 CompCache         │  │
│  └───────────────────────────┘  │
└─────────────────────────────────┘
         │
         ▼
   AI Response (optimized)
```

---

## Scenario 1: AI-Powered Code Review ใน GitHub Actions

### สถานการณ์


### Pipeline Flow

```
Developer pushes PR
        │
        ▼
GitHub Actions triggered
        │
        ▼
┌─────────────────────────────────────────┐
│ Step 1: Fetch PR diff (gh cli)          │
│ Step 2: Build AI review prompt          │
│ Step 3: POST /v1/messages → Gateway     │
│         │                               │
│         ├─ ToolFilter: เลือกเฉพาะ       │
│         │  Read,Edit,Bash tools         │
│         │  (ตัดออก 20+ tools ไม่จำเป็น) │
│         │  ประหยัด ~3000-6000 tokens    │
│         │                               │
│         ├─ Intent Filter (code intent): │
│         │  สกัดเฉพาะ code suggestions   │
│         │  ตัด explanation ออก          │
│         │                               │
│         ├─ PasteGuard: ตรวจ PR diff     │
│         │  mask EMAIL_ADDRESS,          │
│         │  PHONE_NUMBER อัตโนมัติ       │
│         │                               │
│         └─ TextComp: บีบ verbose prompt │
│            "Please carefully review..." │
│            → "Review code:"             │
└─────────────────────────────────────────┘
        │
        ▼
AI Response (code suggestions only)
        │
        ▼
Post review comment on PR via gh cli
```

### GitHub Actions Workflow YAML

```yaml
name: AI Code Review

on:
  pull_request:
    types: [opened, synchronize]

jobs:
  ai-review:
    runs-on: ubuntu-latest
    permissions:
      pull-requests: write
      contents: read

    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Get PR diff
        id: diff
        run: |
          # ดึงเฉพาะไฟล์ที่เปลี่ยนแปลง (added/modified)
          git diff origin/main...HEAD > /tmp/pr.diff
          echo "lines=$(wc -l < /tmp/pr.diff)" >> "$GITHUB_OUTPUT"

      - name: Skip if trivial
        if: steps.diff.outputs.lines < 20
        run: echo "Diff too small for review, skipping."

      - name: AI Review via Optimizer Gateway
        if: steps.diff.outputs.lines >= 20
        env:
          GATEWAY_URL: ${{ secrets.OPTIMIZER_GATEWAY_URL }}
          GATEWAY_API_KEY: ${{ secrets.OPTIMIZER_GATEWAY_KEY }}
        run: |
          DIFF_CONTENT=$(cat /tmp/pr.diff)

          # สร้าง request body - Gateway จะ optimize อัตโนมัติ
          # ToolFilter: เลือกเฉพาะ Read+Edit+Bash (จาก 27 tools)
          # Intent Filter: สกัดเฉพาะ code suggestions
          # PasteGuard: mask secrets ใน diff อัตโนมัติ
          # TextComp: บีบ verbose prompt
          RESPONSE=$(curl -s -X POST "${GATEWAY_URL}/v1/messages" \
            -H "Content-Type: application/json" \
            -H "x-api-key: ${GATEWAY_API_KEY}" \
            -d '{
              "model": "claude-sonnet-4-20250514",
              "max_tokens": 2048,
              "system": "You are a senior code reviewer for Go microservices. Focus on: security issues, race conditions, error handling, performance. Output only actionable code suggestions with file:line references.",
              "messages": [
                {
                  "role": "user",
                  "content": "Review this PR diff and provide specific code suggestions:\n\n'"${DIFF_CONTENT}"'"
                }
              ]
            }')

          # สกัด text จาก response
          REVIEW=$(echo "$RESPONSE" | jq -r '.content[0].text // empty')

          if [ -n "$REVIEW" ]; then
            # โพสต์ review comment ใน PR
            gh pr comment ${{ github.event.pull_request.number }} \
              --body "## AI Code Review (Optimizer Gateway)

          $REVIEW

          ---
          *Powered by Optimizer Gateway - Token savings tracked via Prometheus*"
          fi

      - name: Report metrics
        if: always()
        run: |
          # ดึง optimization metrics จาก gateway
          curl -s "${GATEWAY_URL}/metrics" | grep -E \
            'api_gateway_optimizer_chars_saved_total|api_gateway_filter_intents_total|api_gateway_caveman_compressions_total' \
            >> "$GITHUB_STEP_SUMMARY" || true
```

### สิ่งที่เกิดขึ้นใน Gateway

เมื่อ request เข้ามา Gateway ทำงานตาม pipeline:

```
Request (raw)
  │
  ├─ Budget Level: GREEN (context < 50%)
  │
  ├─ F7 Semantic Dedup
  │  Input:  "You are a senior code reviewer..."
  │           "You should review code carefully..."
  │           "Make sure to check for bugs..."
  │  Output: ตัด duplicate sentences → ประหยัด ~3-5% chars
  │
  ├─ F19 ToolFilter (ไม่ activate - request ไม่มี tools array)
  │
  ├─ F17 TextComp (balanced mode)
  │  Input:  "Please carefully review this PR diff and provide
  │           specific actionable code suggestions with file:line"
  │  Output: "Review PR diff. Code suggestions with file:line"
  │  → ประหยัด ~10% chars
  │
  ├─ F16 Caveman (lite tier - green budget)
  │  Inject: [OUTPUT STYLE - lite] directive
  │  → Model ตอบสั้นลง ~30%, ไม่มี filler phrases
  │
  ├─ PasteGuard (privacy masking)
  │  ตรวจ PR diff: mask ทุก EMAIL_ADDRESS, PHONE_NUMBER
  │  → ป้องกัน secrets leak ไปยัง AI provider
  │
  └─ POST to Provider → Response

Post-Response:
  ├─ F4 Prefetcher: บันทึก tool transitions สำหรับ session นี้
  ├─ F11 Waste Detection: ตรวจ wasted tokens
  └─ F5 Bandit: ปรับ optimizer weights ตามผลลัพธ์
```

### ผลลัพธ์ที่ได้

| Metric | Before Gateway | After Gateway | Savings |
|--------|---------------|---------------|---------|
| Input tokens/review | ~4,200 | ~1,800 | 57% |
| Output tokens/review | ~2,500 | ~1,200 | 52% |
| Cost/review | $0.025 | $0.010 | 60% |
| Latency | 45s | 18s | 60% |
| Secrets leaked | เคยเกิด 2 ครั้ง/เดือน | 0 | 100% |

---

## Scenario 2: Automated Incident Response

### สถานการณ์

เวลา 02:30 น. PagerDuty ส่ง alert P1 เข้ามา: "pod/payment-service CrashLoopBackOff - production" กิตติตั้งระบบไว้ให้ AI วิเคราะห์ incident อัตโนมัติผ่าน Gateway ก่อนจะปลุก on-call

### Incident Timeline with Optimizer Stages

```
02:30:00  PagerDuty alert triggered
          │
02:30:01  ┌─────────────────────────────────────────┐
          │ Webhook handler receives alert          │
          │ POST → Optimizer Gateway /v1/messages   │
          │                                         │
          │ F10 Warm Start:                         │
          │ ค้นหา session ที่คล้ายใน Redis (7 วัน)  │
          │ → เจอ incident "payment-service         │
          │    CrashLoop 3 ครั้งที่แล้ว"            │
          │ → โหลด patterns มาใช้ทันที              │
          │ ประหยัด cold-start waste ~15%           │
          │                                         │
          │ F4 Prefetcher:                          │
          │ ทำนายคำสั่งถัดไปจาก Markov chain        │
          │   kubectl logs → kubectl describe →     │
          │   kubectl get events                    │
          │ → prefetch ข้อมูลเหล่านี้ล่วงหน้า       │
          └─────────────────────────────────────────┘
          │
02:30:03  ┌─────────────────────────────────────────┐
          │ Step 1: Fetch diagnostic data           │
          │ - kubectl logs payment-svc --tail=100   │
          │ - kubectl describe pod payment-service  │
          │ - kubectl get events --field-selector...│
          │                                         │
          │ F18 ToolComp: บีบ log output            │
          │ Input: 150 บรรทัด kubectl logs (4,500ch)│
          │ Output: 35 บรรทัด (head+tail+dedup)     │
          │ → ประหยัด ~75% tool_result tokens       │
          │                                         │
          │ Log format detection: "Log" type        │
          │ บีบ: dedup consecutive identical lines  │
          │ "[ERROR] connection refused" x 50       │
          │ → "[ERROR] connection refused (x50)"    │
          └─────────────────────────────────────────┘
          │
02:30:08  ┌─────────────────────────────────────────┐
          │ Step 2: AI Analysis                     │
          │                                         │
          │ F11 Waste Detection (runs every 60s):   │
          │ ตรวจพบ "retry_churn" pattern            │
          │ → AI สั่ง kubectl logs ซ้ำ 3 ครั้ง      │
          │ → Flag severity=medium                  │
          │                                         │
          │ F9 Sketch: ตรวจ near-duplicate prompt   │
          │ "Analyze this pod error..." ≈ 0.92      │
          │ similarity กับ incident ก่อนหน้า        │
          │ → Flag ว่าเป็นปัญหาเดิม                 │
          └─────────────────────────────────────────┘
          │
02:30:12  ┌─────────────────────────────────────────┐
          │ Step 3: Generate runbook                │
          │                                         │
          │ F8 Delta Encoding:                      │
          │ เปรียบเทียบ runbook กับ cached baseline │
          │ "sys:glm-5" key ใน Redis                │
          │ ส่งเฉพาะ +/=/- operations               │
          │ → ประหยัด ~40% input tokens             │
          │                                         │
          │ F16 Caveman (full tier - yellow budget):│
          │ Inject: [OUTPUT STYLE - full]           │
          │ → Model ตอบแบบ action items เท่านั้น    │
          │ → ไม่มี "I see that..." filler          │
          └─────────────────────────────────────────┘
          │
02:30:15  AI analysis complete → Post to Slack + PagerDuty
```

### Incident Response Script

```bash
#!/bin/bash
# incident-responder.sh - วิเคราะห์ incident อัตโนมัติผ่าน Optimizer Gateway
# ติดตั้งเป็น PagerDuty webhook หรือ run จาก Lambda/CloudRun

set -euo pipefail

GATEWAY_URL="${OPTIMIZER_GATEWAY_URL:-http://gateway.internal:9000}"
GATEWAY_KEY="${OPTIMIZER_GATEWAY_KEY}"
ALERT_PAYLOAD="$1"  # PagerDuty webhook JSON

# สกัดข้อมูลจาก alert
CLUSTER=$(echo "$ALERT_PAYLOAD" | jq -r '.cluster // "production"')
NAMESPACE=$(echo "$ALERT_PAYLOAD" | jq -r '.namespace // "default"')
SERVICE=$(echo "$ALERT_PAYLOAD" | jq -r '.service // .pod_name // "unknown"')
SEVERITY=$(echo "$ALERT_PAYLOAD" | jq -r '.severity // "P2"')

echo "=== Incident: ${SEVERITY} - ${SERVICE} in ${CLUSTER}/${NAMESPACE} ==="

# ดึง diagnostic data (จะถูก ToolComp บีบอัตโนมัติ)
LOGS=$(kubectl logs "deploy/${SERVICE}" -n "$NAMESPACE" --tail=200 2>&1 || echo "LOGS_UNAVAILABLE")
DESCRIBE=$(kubectl describe "deploy/${SERVICE}" -n "$NAMESPACE" 2>&1 | head -80 || echo "DESCRIBE_UNAVAILABLE")
EVENTS=$(kubectl get events -n "$NAMESPACE" --sort-by='.lastTimestamp' 2>&1 | tail -30 || echo "EVENTS_UNAVAILABLE")

# ส่งเข้า Gateway - Warm Start จะโหลด incident patterns ที่คล้ายกัน
# Prefetcher จะทำนาย diagnostic steps ถัดไป
# ToolComp จะบีบ LOGS + DESCRIBE + EVENTS (~75% compression)
RESPONSE=$(curl -s --max-time 30 -X POST "${GATEWAY_URL}/v1/messages" \
  -H "Content-Type: application/json" \
  -H "x-api-key: ${GATEWAY_KEY}" \
  -d "$(jq -n \
    --arg severity "$SEVERITY" \
    --arg service "$SERVICE" \
    --arg cluster "$CLUSTER" \
    --arg ns "$NAMESPACE" \
    --arg logs "$LOGS" \
    --arg describe "$DESCRIBE" \
    --arg events "$EVENTS" \
    '{
      model: "glm-5",
      max_tokens: 1024,
      system: "You are an incident response AI. Analyze Kubernetes incidents. Output: ROOT CAUSE (1 line), EVIDENCE (bullet points), FIX (kubectl commands), SEVERITY ASSESSMENT. No filler text.",
      messages: [{
        role: "user",
        content: "SEVERITY: \($severity)\nSERVICE: \($service)\nCLUSTER: \($cluster)/\($ns)\n\nLOGS (last 200 lines):\n\($logs)\n\nPOD DESCRIBE:\n\($describe)\n\nRECENT EVENTS:\n\($events)"
      }]
    }')")

# สกัด analysis
ANALYSIS=$(echo "$RESPONSE" | jq -r '.content[0].text // "Analysis unavailable"')
TOKENS_USED=$(echo "$RESPONSE" | jq -r '.usage.input_tokens // 0')

echo "=== AI Analysis ==="
echo "$ANALYSIS"
echo ""
echo "Tokens used: ${TOKENS_USED}"

# ส่งผลไป Slack
if [ -n "${SLACK_WEBHOOK_URL:-}" ]; then
  jq -n \
    --arg text "*[${SEVERITY}] ${SERVICE} - ${CLUSTER}/${NAMESPACE}*\n\`\`\`\n${ANALYSIS}\n\`\`\`\n_Tokens: ${TOKENS_USED} | via Optimizer Gateway_" \
    '{text: $text}' \
    | curl -s -X POST "$SLACK_WEBHOOK_URL" -H "Content-Type: application/json" -d @-
fi
```

### Prometheus Metrics ที่วัดผลได้

```promql
# ToolComp compression สำหรับ incident logs
sum by (technique) (rate(api_gateway_optimizer_chars_saved_total{technique="toolcomp"}[5m]))

# Waste detection - ตรวจจับ retry churn ใน incident analysis
sum by (detector, severity) (rate(api_gateway_waste_findings_total{detector="retry_churn"}[1h]))

# Warm Start hit rate สำหรับ incident sessions
sum(rate(api_gateway_warmstart_sessions_warmed_total{result="hit"}[1h]))
/
sum(rate(api_gateway_warmstart_sessions_warmed_total[1h]))

# Prefetcher accuracy
sum(rate(api_gateway_prefetcher_predictions_total{correct="true"}[1h]))
/
sum(rate(api_gateway_prefetcher_predictions_total[1h]))
```

---

## Scenario 3: Infrastructure Drift Detection

### สถานการณ์

กิตติตั้ง cron job ให้รันทุกคืน เปรียบเทียบ Terraform plan กับ live state ของ Kubernetes cluster ถ้ามี drift จะให้ AI วิเคราะห์ว่าอะไรเปลี่ยน และอันตรายแค่ไหน

### Pipeline Flow

```
02:00  CronJob triggered (daily)
         │
         ▼
┌─────────────────────────────────────────┐
│ 1. terraform plan -out=tfplan           │
│ 2. terraform show -json tfplan > plan   │
│ 3. kubectl get all -o json > live       │
│ 4. Diff: plan vs live state             │
│                                         │
│ Gateway Optimization:                   │
│                                         │
│ F8 Delta Encoding:                      │
│ เปรียบเทียบกับ baseline ของวันก่อน      │
│ key: "sys:glm-5" in Redis               │
│ → ส่งเฉพาะ +/=/- operations             │
│ → "aws_instance.web: count 3→5"         │
│ → "k8s_deployment.api: image tag diff"  │
│ ประหยัด ~40-60% เพราะส่วนใหญ่ไม่เปลี่ยน │
│                                         │
│ F20 CompCache:                          │
│ บีบ cached Terraform state comparisons  │
│ ใน Redis ด้วย zstd level 3              │
│ → ประหยัด 60-80% Redis memory           │
│                                         │
│ F14 Cache Eviction:                     │
│ รันทุก 5 นาที ลบ cached comparisons     │
│ ที่มี ROI ต่ำ (bottom 10%)              │
│ → ทิ้ง state ของ env ที่ไม่ได้ใช้แล้ว   │
│ → เก็บ state production ไว้ (high ROI)  │
│                                         │
│ F9 Sketch:                              │
│ ตรวจว่าวันนี้ diff เหมือนเมื่อวานไหม    │
│ → similarity > 0.85 → flag duplicate    │
│ → ข้าม analysis ประหยัดทั้ง request     │
└─────────────────────────────────────────┘
         │
         ▼
Slack notification (if drift detected)
```

### Automation Script

```bash
#!/bin/bash
# drift-detector.sh - ตรวจจับ infrastructure drift ทุกคืน
# Deploy เป็น Kubernetes CronJob

set -euo pipefail

GATEWAY_URL="${OPTIMIZER_GATEWAY_URL:-http://gateway.internal:9000}"
GATEWAY_KEY="${OPTIMIZER_GATEWAY_KEY}"
TF_DIR="/workspace/terraform"
REPORT_FILE="/tmp/drift-report-$(date +%Y%m%d).json"

# === Step 1: สร้าง Terraform plan ===
cd "$TF_DIR"
terraform plan -out=tfplan -detailed-exitcode 2>&1 | tee /tmp/tf-plan-output.txt
PLAN_EXIT=$?

# Exit code 0 = no changes, 1 = error, 2 = changes detected
if [ "$PLAN_EXIT" -eq 0 ]; then
  echo "No infrastructure drift detected. Exiting."
  exit 0
fi

if [ "$PLAN_EXIT" -eq 1 ]; then
  echo "Terraform plan failed. Exiting."
  exit 1
fi

# === Step 2: ดึง plan JSON ===
terraform show -json tfplan > /tmp/tfplan.json 2>/dev/null

# สกัดเฉพาะ changed resources (Delta Encoding จะจัดการใน Gateway)
# Gateway จะเปรียบเทียบกับ cached baseline อัตโนมัติ
CHANGED_RESOURCES=$(jq -r '
  .planned_values.root_module.resources // [] |
  .[] |
  "\(.type).\(.name): \(.values // {})"
' /tmp/tfplan.json 2>/dev/null || echo "Parse failed")

# ดึง live state สำหรับเปรียบเทียบ
LIVE_STATE=$(kubectl get deployments,statefulsets,configmaps,secrets \
  -A -o json 2>/dev/null | jq -r '
  .items |
  map({
    kind: .kind,
    name: .metadata.name,
    namespace: .metadata.namespace,
    resource_version: .metadata.resourceVersion,
    generation: .metadata.generation
  })
' 2>/dev/null || echo "Live state unavailable")

# === Step 3: ส่งเข้า Gateway ===
# Delta Encoding: เปรียบเทียบกับ baseline ของเมื่อวาน (cached ใน Redis)
# CompCache: บีบ cached state comparisons ด้วย zstd
# Sketch: ถ้า diff เหมือนเมื่อวาน → ข้าม
RESPONSE=$(curl -s --max-time 60 -X POST "${GATEWAY_URL}/v1/messages" \
  -H "Content-Type: application/json" \
  -H "x-api-key: ${GATEWAY_KEY}" \
  -d "$(jq -n \
    --arg changes "$CHANGED_RESOURCES" \
    --arg live "$LIVE_STATE" \
    --arg plan_output "$(tail -50 /tmp/tf-plan-output.txt)" \
    '{
      model: "glm-5",
      max_tokens: 1500,
      system: "You are an infrastructure drift analyst. Compare Terraform planned state vs live Kubernetes state. Classify each drift: SAFE (expected), RISKY (needs review), DANGEROUS (act now). Output JSON array only.",
      messages: [{
        role: "user",
        content: "Terraform plan changes:\n\($changes)\n\nPlan output:\n\($plan_output)\n\nLive Kubernetes state:\n\($live)"
      }]
    }')")

ANALYSIS=$(echo "$RESPONSE" | jq -r '.content[0].text // "No analysis"')
INPUT_TOKENS=$(echo "$RESPONSE" | jq -r '.usage.input_tokens // 0')

echo "=== Drift Analysis ==="
echo "$ANALYSIS"

# บันทึก report
jq -n \
  --arg analysis "$ANALYSIS" \
  --arg tokens "$INPUT_TOKENS" \
  --arg date "$(date -I)" \
  '{date: $date, analysis: $analysis, tokens: $tokens}' \
  > "$REPORT_FILE"

# ส่งแจ้งเตือนถ้ามี drift อันตราย
if echo "$ANALYSIS" | grep -qi "DANGEROUS"; then
  curl -s -X POST "${SLACK_WEBHOOK_URL}" \
    -H "Content-Type: application/json" \
    -d "{\"text\": \":warning: *DANGEROUS DRIFT DETECTED*\n\`\`\`\n${ANALYSIS}\n\`\`\`\"}"
fi
```

### CronJob Manifest

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: drift-detector
  namespace: platform-tools
spec:
  schedule: "0 2 * * *"  # รัน 02:00 ทุกคืน
  concurrencyPolicy: Forbid
  successfulJobsHistoryLimit: 7
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: drift-detector
          containers:
            - name: drift-detector
              env:
                - name: OPTIMIZER_GATEWAY_URL
                  value: "http://arl-gateway.platform-tools.svc:8080"
                - name: OPTIMIZER_GATEWAY_KEY
                  valueFrom:
                    secretKeyRef:
                      name: gateway-credentials
                      key: api-key
              volumeMounts:
                - name: terraform
                  mountPath: /workspace/terraform
          volumes:
            - name: terraform
              configMap:
                name: terraform-configs
          restartPolicy: OnFailure
```

---

## Scenario 4: Deployment Safety ด้วย AI Gate

### สถานการณ์

ก่อน deploy service ใหม่ขึ้น production กิตติตั้ง "AI Gate" ไว้ - AI จะวิเคราะห์ canary metrics แล้วตัดสินใจ go/no-go โดยใช้ budget-aware optimization เพื่อให้ decision รวดเร็วและประหยัด tokens

### Deployment Pipeline Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                    Deployment Pipeline                       │
│                                                              │
│  Stage 1: Build & Test                                       │
│     ↓ (auto-pass)                                            │
│                                                              │
│  Stage 2: Canary Deploy (10% traffic)                        │
│     ↓                                                        │
│  Stage 3: Canary Metrics Collection (5 min observation)      │
│     ↓                                                        │
│  Stage 4: ★ AI GATE ★                                        │
│     │                                                        │
│     ├─ GREEN BUDGET (< 50% context):                         │
│     │  Caveman lite: ตอบสั้น "APPROVE" or "ROLLBACK: reason" │
│     │  Intent filter: สกัดเฉพาะ decision keywords            │
│     │                                                        │
│     ├─ YELLOW BUDGET (50-75% context):                       │
│     │  Caveman full: บีบ verbose metric explanations         │
│     │  Budget-aware disclosure: truncate ให้เหลือ key metrics│
│     │  → เก็บเฉพาะ p99 latency, error rate, CPU/memory       │
│     │                                                        │
│     └─ RED BUDGET (> 75% context - multi-service deploy):    │
│        Caveman ultra: raw decision เท่านั้น                  │
│        Summarizer: บีบ 5 นาที metrics เป็น summary           │
│        → Output: "REJECT" หรือ "APPROVE" + 1 บรรทัด          │
│                                                              │
│  Stage 5: Full Rollout (if APPROVED)                         │
│     or                                                       │
│  Stage 5: Auto-Rollback (if REJECTED)                        │
└──────────────────────────────────────────────────────────────┘
```

### ArgoCD / GitHub Actions Integration

```yaml
name: Deploy with AI Gate

on:
  workflow_dispatch:
    inputs:
      service:
        description: 'Service to deploy'
        required: true
      environment:
        description: 'Target environment'
        type: choice
        options: [staging, production]

jobs:
  deploy:
    runs-on: ubuntu-latest
    environment: ${{ inputs.environment }}
    steps:
      - uses: actions/checkout@v4

      - name: Deploy Canary
        run: |
          # Deploy canary version (10% traffic)
          kubectl apply -f k8s/${{ inputs.service }}-canary.yaml
          kubectl set image deployment/${{ inputs.service }}-canary \
            app=${{ inputs.service }}:${{ github.sha }} \
            --namespace production

      - name: Observe Canary Metrics (5 min)
        run: |
          echo "Waiting 5 minutes for canary metrics..."
          sleep 300

      - name: AI Gate Decision
        id: ai-gate
        env:
          GATEWAY_URL: ${{ secrets.OPTIMIZER_GATEWAY_URL }}
          GATEWAY_KEY: ${{ secrets.OPTIMIZER_GATEWAY_KEY }}
          SERVICE: ${{ inputs.service }}
        run: |
          # ดึง canary metrics
          METRICS=$(curl -s "http://prometheus.internal:9090/api/v1/query_range" \
            --data-urlencode "query={
              service=\"${SERVICE}-canary\",
              job=\"kubernetes-pods\"
            }" \
            --data-urlencode "start=$(date -d '5 minutes ago' -Iseconds)" \
            --data-urlencode "end=$(date -Iseconds)" \
            --data-urlencode "step=30s" | jq -r '.data.result')

          # ดึง baseline metrics (main deployment)
          BASELINE=$(curl -s "http://prometheus.internal:9090/api/v1/query_range" \
            --data-urlencode "query={
              service=\"${SERVICE}\",
              job=\"kubernetes-pods\"
            }" \
            --data-urlencode "start=$(date -d '5 minutes ago' -Iseconds)" \
            --data-urlencode "end=$(date -Iseconds)" \
            --data-urlencode "step=30s" | jq -r '.data.result')

          # AI Gate: วิเคราะห์ canary health
          # Caveman mode จะทำให้ AI ตอบแค่ APPROVE/ROLLBACK + reason
          # Intent filter (action intent): สกัดเฉพาะ decision
          RESPONSE=$(curl -s --max-time 30 -X POST "${GATEWAY_URL}/v1/messages" \
            -H "Content-Type: application/json" \
            -H "x-api-key: ${GATEWAY_KEY}" \
            -d "$(jq -n \
              --arg metrics "$METRICS" \
              --arg baseline "$BASELINE" \
              --arg service "$SERVICE" \
              '{
                model: "glm-5",
                max_tokens: 256,
                system: "You are a deployment gate AI. Compare canary vs baseline metrics. Decide: APPROVE or ROLLBACK. Criteria: error_rate < 1%, p99_latency within 20% of baseline, no OOM kills. Output ONLY: DECISION: APPROVE/ROLLBACK | REASON: one line",
                messages: [{
                  role: "user",
                  content: "Service: \($service)\nCanary metrics (5min):\n\($metrics)\nBaseline metrics (5min):\n\($baseline)"
                }]
              }')")

          DECISION=$(echo "$RESPONSE" | jq -r '.content[0].text' | head -5)
          echo "decision=${DECISION}" >> "$GITHUB_OUTPUT"
          echo "### AI Gate Decision" >> "$GITHUB_STEP_SUMMARY"
          echo "\`\`\`" >> "$GITHUB_STEP_SUMMARY"
          echo "$DECISION" >> "$GITHUB_STEP_SUMMARY"
          echo "\`\`\`" >> "$GITHUB_STEP_SUMMARY"

          # ตรวจ decision
          if echo "$DECISION" | grep -qi "ROLLBACK"; then
            echo "::error::AI Gate rejected deployment"
            exit 1
          fi

      - name: Full Rollout
        if: success()
        run: |
          echo "AI Gate APPROVED. Rolling out..."
          kubectl set image deployment/${{ inputs.service }} \
            app=${{ inputs.service }}:${{ github.sha }} \
            --namespace production
          kubectl rollout status deployment/${{ inputs.service }} \
            --namespace production --timeout=300s

      - name: Auto Rollback
        if: failure()
        run: |
          echo "Rolling back canary..."
          kubectl rollout undo deployment/${{ inputs.service }}-canary \
            --namespace production
          kubectl delete deployment/${{ inputs.service }}-canary \
            --namespace production --ignore-not-found
```

### Budget-Aware Behavior

AI Gate ปรับพฤติกรรมตาม budget level อัตโนมัติ:

```
┌─────────────────────────────────────────────────────────────┐
│ Budget Level: GREEN (< 50% context used)                     │
│                                                              │
│ Deploying single service, first deployment of the day         │
│                                                              │
│ Activated stages:                                            │
│  ├─ Caveman lite: "DECISION: APPROVE | latency p99 within 5%"│
│  ├─ Intent filter (action): สกัดเฉพาะ DECISION line          │
│  └─ TextComp: บีบ verbose metric descriptions                │
│                                                              │
│ Output: 1-2 บรรทัด                                          │
│ Tokens: ~150 input, ~80 output                               │
├─────────────────────────────────────────────────────────────┤
│ Budget Level: YELLOW (50-75% context used)                   │
│                                                              │
│ Deploying 3rd service, context มี canary metrics 2 รอบแล้ว    │
│                                                              │
│ Activated stages:                                            │
│  ├─ Caveman full: บีบ 50% output                             │
│  ├─ Budget-aware disclosure: truncate metrics > 2000 chars    │
│  │   → เก็บเฉพาะ error_rate, p99, CPU, memory               │
│  │   → ตัด network I/O, disk I/O, custom metrics            │
│  └─ Delta Encoding: เปรียบเทียบกับ baseline cache             │
│                                                              │
│ Output: "APPROVE | p99 245ms (baseline 230ms), err 0.3%"     │
│ Tokens: ~400 input, ~120 output                              │
├─────────────────────────────────────────────────────────────┤
│ Budget Level: RED (> 75% context used)                       │
│                                                              │
│ Emergency multi-service deploy, context เต็ม                  │
│                                                              │
│ Activated stages:                                            │
│  ├─ Summarizer: บีบ 5 นาที metrics เป็น 3 บรรทัด summary      │
│  ├─ Caveman ultra: raw output เท่านั้น                        │
│  └─ Intent filter: สกัด decision keyword เท่านั้น              │
│                                                              │
│ Output: "APPROVE" หรือ "ROLLBACK: err 5.2%"                  │
│ Tokens: ~200 input (after summarizer), ~20 output            │
└─────────────────────────────────────────────────────────────┘
```

---

## สรุป: Token Savings รวมทั้ง 4 Scenarios

| Scenario | Optimizer Stages ที่ใช้ | Input Savings | Output Savings | ฟีเจอร์หลัก |
|----------|----------------------|---------------|----------------|-------------|
| Code Review | ToolFilter, Intent Filter, PasteGuard, TextComp, Caveman | 57% | 52% | ป้องกัน secrets leak, บีบ tool manifest |
| Incident Response | Warm Start, Prefetcher, ToolComp, Waste Detection, Sketch, Delta, Caveman | 65% | 50% | โหลด incident patterns อัตโนมัติ, บีบ logs |
| Drift Detection | Delta Encoding, CompCache, Cache Eviction, Sketch | 40-60% | 30% | ส่งเฉพาะส่วนที่เปลี่ยน, บีบ Redis cache |
| Deploy AI Gate | Caveman (tier-aware), Summarizer, Intent Filter, Disclosure, Delta | 50% | 70% | Budget-aware decision, auto-adjust verbosity |

### Prometheus Dashboard Queries สำหรับติดตาม

```promql
# 1. Token savings รวมต่อ technique
sum by (technique) (rate(api_gateway_optimizer_chars_saved_total[1h]))

# 2. Waste detection - ตรวจจับ wasted tokens ใน CI/CD requests
sum by (detector) (rate(api_gateway_waste_tokens_wasted_total[24h]))

# 3. Cache hit rate (CompCache + Delta + Sketch effectiveness)
sum(rate(api_gateway_delta_encodes_total{result="encoded"}[24h]))
/
sum(rate(api_gateway_delta_encodes_total[24h]))

# 4. Budget level distribution ใน deployment pipeline
sum by (le) (rate(api_gateway_budget_level[1h]))

# 5. Warm Start hit rate สำหรับ incident sessions
sum(rate(api_gateway_warmstart_sessions_warmed_total{result="hit"}[24h]))

# 6. Caveman compression ratio ตาม tier
histogram_quantile(0.5, rate(api_gateway_caveman_compression_ratio_bucket[1h]))
```

### การตั้งค่า Environment Variables สำหรับ CI/CD Workload

```bash
# docker-compose override สำหรับ CI/CD workload
# เน้น: latency ต่ำ + cost savings สูง

# Core optimization
CHUNKER_ENABLED=true
DELTA_ENABLED=true
DELTA_MIN_SAVINGS_PCT=5.0          # ลดจาก default 10 เพื่อ activate ง่ายขึ้น
SKETCH_ENABLED=true
SKETCH_THRESHOLD=0.80              # ลดจาก 0.85 เพื่อจับ duplicates มากขึ้น
TEXTCOMP_ENABLED=true
TEXTCOMP_MODE=aggressive           # ใช้ aggressive สำหรับ CI/CD (ไม่ต้องการ prose)

# Output control
CAVEMAN_ENABLED=true
CAVEMAN_AUTO_DETECT=true
FILTER_ENABLED=true

# Tool optimization
TOOLCOMP_ENABLED=true
TOOLCOMP_MAX_LINES=30              # ลดจาก 50 เพราะ CI/CD logs ยาว
TOOLFILTER_ENABLED=true
TOOLFILTER_MAX_TOOLS=10            # ลดจาก 15 เพื่อกรองเยอะขึ้น

# Cache & memory
COMPCACHE_ENABLED=true
COMPCACHE_LEVEL=5                  # เพิ่มจาก 3 เพื่อบีบมากขึ้น (CPU ใช้เพิ่มนิดหน่อย)
CACHE_EVICTION_ENABLED=true
CACHE_EVICTION_PCT=15.0            # เพิ่มจาก 10 เพื่อ clean cache เร็วขึ้น

# Post-processing
WASTE_ENABLED=true
WASTE_MIN_REQUESTS=5               # ลดจาก 10 เพื่อ detect เร็วขึ้น
PREFETCHER_ENABLED=true
WARMSTART_ENABLED=true
BANDIT_ENABLED=true
```

---

## สถาปัตยกรรมรวม: Optimizer Gateway ใน CI/CD Ecosystem

```
┌─────────────────────────────────────────────────────────────────┐
│                        CI/CD Platform                            │
│                                                                  │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐       │
│  │ GitHub   │  │ ArgoCD   │  │ PagerDuty│  │ CronJob  │       │
│  │ Actions  │  │ Pipeline │  │ Webhook  │  │ (daily)  │       │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘       │
│       │              │              │              │              │
│       └──────────────┴──────┬───────┴──────────────┘             │
│                             │                                    │
│                             ▼                                    │
│  ┌──────────────────────────────────────────────────────┐       │
│  │              Optimizer Gateway (:9000)                │       │
│  │                                                      │       │
│  │  Request Pipeline:                                   │       │
│  │   ├─ F7  Semantic Dedup ─────────── 3-5% savings     │       │
│  │   ├─ F1  Chunker ────────────────── 5-15% cache hit  │       │
│  │   ├─ F8  Delta Encoding ────────── 20-60% on diffs   │       │
│  │   ├─ F9  Sketch ────────────────── 5-30% dup detect  │       │
│  │   ├─ F17 TextComp ──────────────── 5-15% filler      │       │
│  │   ├─ F16 Caveman ───────────────── 30-75% output     │       │
│  │   ├─ F18 ToolComp ──────────────── 40-80% logs        │       │
│  │   ├─ F19 ToolFilter ────────────── 60-80% manifest   │       │
│  │   └─ PasteGuard ────────────────── secrets masked     │       │
│  │                                                      │       │
│  │  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─   │       │
│  │                                                      │       │
│  │  Feedback Pipeline:                                  │       │
│  │   ├─ F10 Warm Start ────────────── 10-20% cold-start │       │
│  │   ├─ F4  Prefetcher ────────────── 50-200ms latency  │       │
│  │   ├─ F11 Waste Detection ────────── 5-20% waste ID   │       │
│  │   ├─ F14 Cache Eviction ─────────── ~10% cache hit+   │       │
│  │   ├─ F5  Bandit ────────────────── 5-15% meta-opt    │       │
│  │   └─ F20 CompCache ─────────────── 60-80% Redis mem  │       │
│  │                                                      │       │
│  │  ┌─────────┐ ┌──────────┐ ┌─────────────────┐        │       │
│  │  │ Dragonfly│ │Prometheus│ │ Grafana Dashboard│       │      │
│  │  │ (Redis)  │ │ Metrics  │ │ (visualization) │       │       │
│  │  └─────────┘ └──────────┘ └─────────────────┘        │       │
│  └──────────────────────────────────────────────────────┘       │
│                             │                                    │
│                             ▼                                    │
│                    ┌─────────────────┐                           │
│                    │  AI Provider     │                          │
│                    │  (Z.AI / Claude) │                          │
│                    └─────────────────┘                           │
└─────────────────────────────────────────────────────────────────┘
```

กิตติสรุปประโยชน์หลัง deploy ไป 2 สัปดาห์:

- **Token cost ลด 55%** จากเดิม ~$500/เดือน เหลือ ~$225/เดือน
- **Pipeline latency ลด 40%** เฉลี่ยจาก 45s/step เหลือ 27s/step
- **Secrets leak = 0** จากเดิมเคยเกิด 2-3 ครั้ง/เดือน
- **Incident response เร็วขึ้น** เพราะ Warm Start โหลด patterns จาก incidents ก่อนหน้า
- **Drift detection ประหยัด Redis memory 65%** ด้วย CompCache + Cache Eviction
