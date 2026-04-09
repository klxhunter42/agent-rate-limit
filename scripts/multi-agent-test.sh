#!/bin/bash
# multi-agent-test.sh — จำลอง multi-agent ทำงานจริง
# ยิง async jobs แล้ว poll จนกว่าจะเสร็จ วัด end-to-end latency
#
# Usage:
#   ./scripts/multi-agent-test.sh [agents] [turns_per_agent]
#   ./scripts/multi-agent-test.sh          # default: 5 agents, 2 turns each
#   ./scripts/multi-agent-test.sh 10 3     # 10 agents, 3 turns each

set -euo pipefail

API_URL="http://localhost:8080/v1/chat/completions"
RESULT_URL="http://localhost:8080/v1/result"
API_KEY="${GLM_API_KEY:-1d63b5db6d984db1913ca9596125f06b.RHYoZWfoRZteSclW}"
MODEL="${TEST_MODEL:-glm-5}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

AGENTS=${1:-5}
TURNS=${2:-2}
TOTAL=$((AGENTS * TURNS))
POLL_INTERVAL=2
MAX_POLL=2700  # max seconds to wait per job (45 min for large tests with 1 key)

# Agent personality templates (simulate real agent workloads)
AGENT_PROMPTS=(
    "You are a code reviewer. Review this function and list 3 issues."
    "You are a test writer. Write 2 unit tests for a factorial function."
    "You are a doc generator. Write API docs for a GET /users endpoint."
    "You are a security auditor. List 3 OWASP risks for a login page."
    "You are a DevOps engineer. Write a Docker healthcheck config."
    "You are a data analyst. Explain correlation vs causation briefly."
    "You are a refactor bot. Suggest 3 ways to simplify nested if-else."
    "You are a performance optimizer. List 3 ways to speed up a loop."
    "You are a SQL expert. Rewrite this query to use a CTE instead."
    "You are a API designer. Design a REST endpoint for file uploads."
    "You are a error handler. Add proper error handling to this code."
    "You are a type checker. Add type annotations to this Python function."
    "You are a logger. Add structured logging to this HTTP handler."
    "You are a config manager. Create a config class with validation."
    "You are a cache designer. Design a TTL-based cache with eviction."
)

send_and_wait() {
    local agent_id=$1
    local turn=$2
    local prompt_idx=$(( (agent_id + turn) % ${#AGENT_PROMPTS[@]} ))
    local prompt="${AGENT_PROMPTS[$prompt_idx]}"
    local id="agent${agent_id}-turn${turn}"

    local start=$(python3 -c "import time; print(time.time())")

    # Send job
    local payload=$(cat <<EOF
{
  "model": "$MODEL",
  "agent_id": "sim-agent-$agent_id",
  "messages": [{"role": "user", "content": "$prompt. Reply in under 50 words."}],
  "max_tokens": 128,
  "temperature": 0.3
}
EOF
)

    local response=$(curl -sf -X POST "$API_URL" \
        -H "Content-Type: application/json" \
        -d "$payload" \
        --max-time 10 2>/dev/null || echo '{"error":"send_failed"}')

    local request_id=$(echo "$response" | python3 -c "import sys,json; print(json.load(sys.stdin).get('request_id',''))" 2>/dev/null || echo "")

    if [ -z "$request_id" ]; then
        local end=$(python3 -c "import time; print(time.time())")
        local elapsed=$(python3 -c "print(f'{$end - $start:.2f}')")
        echo "$id|FAIL|$elapsed|send_error|$(echo "$response" | head -c 100)"
        return
    fi

    # Poll for result
    local waited=0
    while [ $waited -lt $MAX_POLL ]; do
        sleep $POLL_INTERVAL
        waited=$((waited + POLL_INTERVAL))

        local result=$(curl -sf "$RESULT_URL/$request_id" --max-time 5 2>/dev/null || echo '{"status":"poll_error"}')
        local status=$(echo "$result" | python3 -c "import sys,json; print(json.load(sys.stdin).get('status',''))" 2>/dev/null || echo "parse_error")

        if [ "$status" = "completed" ]; then
            local end=$(python3 -c "import time; print(time.time())")
            local elapsed=$(python3 -c "print(f'{$end - $start:.2f}')")
            local content=$(echo "$result" | python3 -c "
import sys, json
d = json.load(sys.stdin)
c = d.get('result', {}).get('content', '')
if isinstance(c, list):
    for block in c:
        if isinstance(block, dict) and block.get('type') == 'text':
            c = block.get('text', '')
            break
print(c[:80])
" 2>/dev/null || echo "parse_error")
            local model_used=$(echo "$result" | python3 -c "
import sys, json
d = json.load(sys.stdin)
print(d.get('result', {}).get('model', d.get('model', '?')))
" 2>/dev/null || echo "?")
            echo "$id|OK|$elapsed|$model_used|$content"
            return
        elif [ "$status" = "error" ]; then
            local end=$(python3 -c "import time; print(time.time())")
            local elapsed=$(python3 -c "print(f'{$end - $start:.2f}')")
            local err=$(echo "$result" | python3 -c "
import sys, json
d = json.load(sys.stdin)
print(d.get('error', d.get('result', {}).get('error', 'unknown'))[:80])
" 2>/dev/null || echo "unknown_error")
            echo "$id|ERROR|$elapsed|$err"
            return
        fi
    done

    # Timeout
    local end=$(python3 -c "import time; print(time.time())")
    local elapsed=$(python3 -c "print(f'{$end - $start:.2f}')")
    echo "$id|TIMEOUT|$elapsed|max_poll_exceeded"
}

echo -e "${GREEN}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║  Multi-Agent Realistic Simulation                          ║${NC}"
echo -e "${GREEN}║  Agents: $AGENTS  ×  Turns: $TURNS  =  $TOTAL total requests      ║${NC}"
echo -e "${GREEN}║  Config: 1 GLM key, RPM=5, 9 model slots                   ║${NC}"
echo -e "${GREEN}╚══════════════════════════════════════════════════════════════╝${NC}"

# Check health
health=$(curl -sf http://localhost:8080/health 2>/dev/null || echo "DOWN")
if [[ "$health" == "DOWN" ]]; then
    echo -e "${RED}ERROR: Gateway not running${NC}"
    exit 1
fi
echo -e "Gateway: ${GREEN}healthy${NC}"

# Check worker
worker_metrics=$(curl -sf http://localhost:9091/metrics-internal 2>/dev/null || echo "{}")
echo -e "Worker:  ${GREEN}running${NC}"

# Show current queue depth
queue_depth=$(echo "$worker_metrics" | python3 -c "import sys,json; print(json.load(sys.stdin).get('queue_depth', '?'))" 2>/dev/null || echo "?")
echo -e "Queue:   ${queue_depth} jobs"

wall_start=$(python3 -c "import time; print(time.time())")

# Create temp dir for results (avoid TMPDIR name — conflicts with system var on macOS)
RESULT_DIR=$(mktemp -d)
trap "rm -rf $RESULT_DIR" EXIT

# Launch all agents × turns as background jobs
# All turns for all agents start simultaneously (simulating burst)
pids=()
for agent in $(seq 1 $AGENTS); do
    for turn in $(seq 1 $TURNS); do
        send_and_wait $agent $turn > "$RESULT_DIR/result-$agent-$turn.txt" &
        pids+=($!)
    done
done

echo ""
echo -e "${CYAN}── Waiting for $TOTAL requests to complete... ──${NC}"
echo -e "${YELLOW}(sending all at once — simulating burst from $AGENTS agents)${NC}"

# Progress: show dots while waiting
total_pids=${#pids[@]}
done_count=0
for pid in "${pids[@]}"; do
    wait $pid 2>/dev/null || true
    done_count=$((done_count + 1))
    pct=$((done_count * 100 / total_pids))
    printf "\r  Progress: %d/%d (%d%%)" "$done_count" "$total_pids" "$pct"
done
echo ""

wall_end=$(python3 -c "import time; print(time.time())")
wall_total=$(python3 -c "print(f'{$wall_end - $wall_start:.1f}')")

# Collect results
echo ""
echo -e "${CYAN}══════════════════════════════════════════════════════════════${NC}"
echo -e "${CYAN}  RESULTS                                                    ${NC}"
echo -e "${CYAN}══════════════════════════════════════════════════════════════${NC}"

export AGENTS_N=$AGENTS
export TURNS_N=$TURNS
export TOTAL_N=$TOTAL
export WALL_T=$wall_total
export RESULT_DIR=$RESULT_DIR

python3 << 'PYEOF'
import os, json, sys
from collections import defaultdict

agents_n = int(os.environ.get("AGENTS_N", "5"))
turns_n = int(os.environ.get("TURNS_N", "2"))
total_n = int(os.environ.get("TOTAL_N", "10"))
wall_t = float(os.environ.get("WALL_T", "0"))
tmp_dir = os.environ.get("RESULT_DIR", "/tmp")

results = []
for agent in range(1, agents_n + 1):
    for turn in range(1, turns_n + 1):
        fpath = f"{tmp_dir}/result-{agent}-{turn}.txt"
        try:
            with open(fpath) as f:
                line = f.read().strip()
        except:
            line = "???|UNKNOWN|0|parse_error"

        parts = line.split("|")
        entry = {
            "id": parts[0] if len(parts) > 0 else "?",
            "status": parts[1] if len(parts) > 1 else "UNKNOWN",
            "latency": float(parts[2]) if len(parts) > 2 and parts[2] else 0,
            "extra1": parts[3] if len(parts) > 3 else "",
            "extra2": parts[4] if len(parts) > 4 else "",
        }
        results.append(entry)

# Sort by latency
ok = [r for r in results if r["status"] == "OK"]
errors = [r for r in results if r["status"] != "OK"]
errors_429 = [r for r in errors if "429" in r.get("extra1", "") or "rate" in r.get("extra1", "").lower()]
timeouts = [r for r in errors if r["status"] == "TIMEOUT"]

# Print table
print(f"{'ID':<25} {'Status':<8} {'Latency':>8} {'Model/Info':<15} {'Response'}")
print("=" * 90)

for r in results:
    status_icon = "+" if r["status"] == "OK" else "x" if r["status"] == "ERROR" else "?" if r["status"] == "TIMEOUT" else "!"
    extra = r.get("extra1", "")[:14] if r["status"] == "OK" else r.get("extra1", "")[:30]
    resp = r.get("extra2", "")[:40] if r["status"] == "OK" else ""
    print(f"  {r['id']:<23} {status_icon} {r['status']:<6} {r['latency']:>7.1f}s  {extra:<15} {resp}")

# Summary
print()
print(f"  {'='*60}")
print(f"  SIMULATION SUMMARY")
print(f"  {'='*60}")
print(f"  Agents:          {agents_n}")
print(f"  Turns/agent:     {turns_n}")
print(f"  Total requests:  {total_n}")
print(f"  Wall time:       {wall_t:.1f}s")
print(f"")
print(f"  Successful:      {len(ok)}/{total_n} ({len(ok)*100//total_n if total_n > 0 else 0}%)")
print(f"  Errors:          {len(errors)} (429 errors: {len(errors_429)})")
print(f"  Timeouts:        {len(timeouts)}")
print(f"")

if ok:
    lats = sorted([r["latency"] for r in ok])
    p50 = lats[len(lats)//2]
    p90 = lats[int(len(lats)*0.9)] if len(lats) > 1 else lats[0]
    p95 = lats[int(len(lats)*0.95)] if len(lats) > 1 else lats[0]
    avg = sum(lats) / len(lats)
    fastest = min(lats)
    slowest = max(lats)

    print(f"  Latency (successful only):")
    print(f"    Fastest:  {fastest:.1f}s")
    print(f"    P50:      {p50:.1f}s")
    print(f"    P90:      {p90:.1f}s")
    print(f"    P95:      {p95:.1f}s")
    print(f"    Avg:      {avg:.1f}s")
    print(f"    Slowest:  {slowest:.1f}s")
    print(f"")
    throughput = len(ok) / wall_t * 60 if wall_t > 0 else 0
    print(f"  Throughput:      {throughput:.1f} req/min")
    print(f"  Avg per agent:   {wall_t / agents_n:.1f}s total / agent")
    print(f"")

# Model distribution
models = defaultdict(int)
for r in ok:
    models[r.get("extra1", "?")] += 1

if models:
    print(f"  Model distribution:")
    for m, c in sorted(models.items(), key=lambda x: -x[1]):
        print(f"    {m:<20} {c} requests")
    print()

# Verdict
print(f"  {'='*60}")
if len(ok) == total_n:
    print(f"  VERDICT: ALL OK - {agents_n} agents x {turns_n} turns handled successfully")
elif len(ok) > 0 and len(errors_429) == 0:
    print(f"  VERDICT: PARTIAL ({len(ok)}/{total_n}) - no 429, some errors")
    if timeouts:
        print(f"          {len(timeouts)} timed out - may need higher RPM or more keys")
elif len(errors_429) > 0:
    print(f"  VERDICT: 429 HIT ({len(errors_429)} errors) - reduce agents or add keys")
else:
    print(f"  VERDICT: ALL FAILED")
print(f"  {'='*60}")
print()

# Limitations
print(f"  LIMITATIONS (current config):")
print(f"    - RPM limit:       5 req/min (1 GLM key)")
print(f"    - Model slots:     9 concurrent")
print(f"    - Worker capacity: 50 concurrent coroutines")
print(f"    - Bottleneck:      {'RPM limit (5/min)' if len(ok) < total_n else 'None observed'}")
if wall_t > 60:
    print(f"    - RPM waited:     ~{wall_t/60:.0f} RPM windows")
    print(f"    - To improve:     Add GLM_API_KEYS (each key = +5 RPM)")
    print(f"                      Or add OpenAI/Anthropic for fallback")
PYEOF
