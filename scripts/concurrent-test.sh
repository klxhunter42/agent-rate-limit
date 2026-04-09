#!/bin/bash
# concurrent-test.sh — ทดสอบ concurrent requests หาจุด optimal
# Usage: ./scripts/concurrent-test.sh [concurrency]
# Default: 3, 5, 10, 15, 20

set -euo pipefail

API_URL="http://localhost:8080/v1/chat/completions"
API_KEY="${GLM_API_KEY:-1d63b5db6d984db1913ca9596125f06b.RHYoZWfoRZteSclW}"
MODEL="glm-5"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

send_request() {
    local id=$1
    local start=$(python3 -c "import time; print(time.time())")

    local payload=$(cat <<EOF
{
  "model": "$MODEL",
  "agent_id": "test-agent-$id",
  "messages": [{"role": "user", "content": "Say exactly: hello-$id"}],
  "max_tokens": 32,
  "temperature": 0.0
}
EOF
)

    local http_code=$(curl -s -o /tmp/arl-test-$id.json -w "%{http_code}" \
        -X POST "$API_URL" \
        -H "Content-Type: application/json" \
        -d "$payload" \
        --max-time 120)

    local end=$(python3 -c "import time; print(time.time())")
    local elapsed=$(python3 -c "print(f'{$end - $start:.2f}')")

    echo "$id|$http_code|$elapsed|$(cat /tmp/arl-test-$id.json 2>/dev/null | head -c 200)"
}

run_test() {
    local concurrency=$1
    echo ""
    echo -e "${CYAN}═══════════════════════════════════════════════════════${NC}"
    echo -e "${CYAN}  CONCURRENT TEST: $concurrency requests${NC}"
    echo -e "${CYAN}  Config: 1 key, RPM=5, Model slots=9${NC}"
    echo -e "${CYAN}═══════════════════════════════════════════════════════${NC}"

    local wall_start=$(python3 -c "import time; print(time.time())")

    # Launch all requests in background
    local pids=()
    for i in $(seq 1 $concurrency); do
        send_request $i &
        pids+=($!)
    done

    # Wait for all
    for pid in "${pids[@]}"; do
        wait $pid 2>/dev/null || true
    done

    local wall_end=$(python3 -c "import time; print(time.time())")
    local wall_total=$(python3 -c "print(f'{$wall_end - $wall_start:.1f}')")

    # Collect results
    local ok=0 fail=0 queued=0 total_latency=0
    echo ""
    echo -e "${YELLOW}── Results ──────────────────────────────────────────────${NC}"
    printf "%-6s %-6s %-10s %s\n" "Req#" "HTTP" "Latency" "Response"
    echo "──────────────────────────────────────────────────────────"

    for i in $(seq 1 $concurrency); do
        local result_file="/tmp/arl-test-$i.json"
        if [ -f "$result_file" ]; then
            local line=$(cat "$result_file" | tr '|' '_' | head -c 120)
            local http=$(echo "$line" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('status','?'))" 2>/dev/null || echo "?")
            echo "  #$i   | $(printf '%-4s' "$http") | see below"
        fi
    done

    # Parse with python for summary
    python3 << PYEOF
import json, os

ok = fail = queued = 0
latencies = []
errors_429 = 0

for i in range(1, $concurrency + 1):
    try:
        with open(f"/tmp/arl-test-{i}.json") as f:
            data = json.load(f)
        status = data.get("status", "?")
        if status == "queued":
            queued += 1
        elif status == "completed":
            ok += 1
            lat = data.get("latency_seconds", 0)
            latencies.append(lat)
        elif status == "error":
            fail += 1
            err = str(data.get("error", ""))
            if "429" in err or "rate" in err.lower():
                errors_429 += 1
            print(f"  req#{i}: ERROR - {err[:80]}")
        else:
            fail += 1
            print(f"  req#{i}: status={status}")
    except Exception as e:
        fail += 1
        print(f"  req#{i}: parse error - {e}")

wall = $wall_total
print()
print(f"  {'='*50}")
print(f"  SUMMARY: $concurrency concurrent requests")
print(f"  {'='*50}")
print(f"  Queued:   {queued}")
print(f"  OK:       {ok}")
print(f"  Failed:   {fail} (429 errors: {errors_429})")
print(f"  Wall:     {wall:.1f}s")

if latencies:
    latencies.sort()
    p50 = latencies[len(latencies)//2]
    p95 = latencies[int(len(latencies)*0.95)] if len(latencies) > 1 else latencies[0]
    avg = sum(latencies) / len(latencies)
    fastest = min(latencies)
    slowest = max(latencies)
    print(f"  Latency:  fastest={fastest:.1f}s  p50={p50:.1f}s  avg={avg:.1f}s  p95={p95:.1f}s  slowest={slowest:.1f}s")

if ok == $concurrency:
    print(f"  Verdict:  ALL OK - safe to run {concurrency} concurrent")
elif ok > 0 and errors_429 == 0:
    print(f"  Verdict:  PARTIAL ({ok}/{$concurrency}) - no 429, but some failures")
elif errors_429 > 0:
    print(f"  Verdict:  429 HIT ({errors_429} errors) - reduce concurrency or add keys")
else:
    print(f"  Verdict:  ALL FAILED")
print()
PYEOF

    # Cleanup
    rm -f /tmp/arl-test-*.json
}

# Run tests at different concurrency levels
if [ $# -gt 0 ]; then
    LEVELS="$@"
else
    LEVELS="3 5 10 15 20"
fi

echo -e "${GREEN}╔═══════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║  Concurrent Load Test — Finding Optimal Threshold    ║${NC}"
echo -e "${GREEN}║  Config: 1 GLM key, RPM=5, 9 model slots             ║${NC}"
echo -e "${GREEN}╚═══════════════════════════════════════════════════════╝${NC}"

# Check gateway health
health=$(curl -sf http://localhost:8080/health 2>/dev/null || echo "DOWN")
if [[ "$health" == "DOWN" ]]; then
    echo -e "${RED}ERROR: Gateway not running at http://localhost:8080${NC}"
    exit 1
fi
echo -e "Gateway: ${GREEN}$health${NC}"

for level in $LEVELS; do
    run_test $level
    if [ "$level" != "$(echo $LEVELS | awk '{print $NF}')" ]; then
        echo -e "${YELLOW}  (cooldown 5s before next test...)${NC}"
        sleep 5
    fi
done

echo -e "${GREEN}╔═══════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║  Test Complete                                        ║${NC}"
echo -e "${GREEN}╚═══════════════════════════════════════════════════════╝${NC}"
