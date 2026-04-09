#!/bin/bash

# Stress test: 10 concurrent requests to API Gateway (sync /v1/messages)
# Tests the Anthropic-compatible sync proxy endpoint for real Q&A experience.

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
API_KEY="${API_KEY:-}"
CONCURRENT=10
MODEL="${MODEL:-claude-sonnet-4-20250514}"

echo ""
echo "=========================================="
echo "  AGENT RATE LIMIT - STRESS TEST"
echo "  $(date +"%Y-%m-%d %H:%M:%S")"
echo "=========================================="
echo ""
echo "  Target   : $GATEWAY_URL/v1/messages"
echo "  Model    : $MODEL"
echo "  Concur.  : $CONCURRENT"
echo ""

# Different questions for each agent to make it feel like real conversations
QUESTIONS=(
  "What is 2+2? Answer in one word."
  "Name a primary color."
  "Is the sky blue? Yes or no."
  "What planet do we live on? One word."
  "Is water wet? Yes or no."
  "What season comes after spring? One word."
  "How many legs does a cat have? Just the number."
  "What is the opposite of hot? One word."
  "Does the sun rise in the east? Yes or no."
  "What color is grass? One word."
)

success=0
fail=0
rate_limited=0
total_time=0
pids=()

TMPDIR_OUT=$(mktemp -d)
trap "rm -rf $TMPDIR_OUT" EXIT

run_request() {
  local id=$1
  local question="$2"
  local outfile="$TMPDIR_OUT/agent_${id}.json"
  local timefile="$TMPDIR_OUT/agent_${id}_time.txt"

  start_ns=$(date +%s%N)

  http_code=$(curl -s -o "$outfile" -w "%{http_code}" \
    -X POST "$GATEWAY_URL/v1/messages" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    -d '{
      "model": "'"$MODEL"'",
      "max_tokens": 128,
      "messages": [
        {"role": "user", "content": "'"$question"'"}
      ]
    }')

  end_ns=$(date +%s%N)
  duration=$(( (end_ns - start_ns) / 1000000 ))

  echo "$duration" > "$timefile"

  if [ "$http_code" = "200" ]; then
    # Extract the text content from Anthropic response format
    answer=$(cat "$outfile" | python3 -c "
import json, sys
try:
    data = json.load(sys.stdin)
    texts = [b['text'] for b in data.get('content', []) if b.get('type') == 'text']
    print(' '.join(texts).strip()[:100])
except:
    print('(could not parse response)')
" 2>/dev/null)
    echo "[Agent #$id] 200 OK (${duration}ms)"
    echo "  Q: $question"
    echo "  A: $answer"
    echo "ok" > "$TMPDIR_OUT/agent_${id}_status.txt"
  elif [ "$http_code" = "429" ]; then
    echo "[Agent #$id] 429 Rate Limited (${duration}ms)"
    echo "ok" > "$TMPDIR_OUT/agent_${id}_status.txt"
  elif [ "$http_code" = "000" ]; then
    echo "[Agent #$id] CONNECTION REFUSED (${duration}ms)"
    echo "fail" > "$TMPDIR_OUT/agent_${id}_status.txt"
  else
    echo "[Agent #$id] $http_code (${duration}ms)"
    echo "  $(cat "$outfile" | head -c 200)"
    echo "fail" > "$TMPDIR_OUT/agent_${id}_status.txt"
  fi
}

for i in $(seq 1 $CONCURRENT); do
  run_request $i "${QUESTIONS[$((i-1))]}" &
  pids+=($!)
done

for pid in "${pids[@]}"; do
  wait $pid
done

# Aggregate results
for i in $(seq 1 $CONCURRENT); do
  status=$(cat "$TMPDIR_OUT/agent_${i}_status.txt" 2>/dev/null)
  dur=$(cat "$TMPDIR_OUT/agent_${i}_time.txt" 2>/dev/null || echo "0")
  case "$status" in
    "ok") success=$((success + 1)) ;;
    "fail") fail=$((fail + 1)) ;;
    *) fail=$((fail + 1)) ;;
  esac
  total_time=$((total_time + dur))
done

avg_time=$((total_time / CONCURRENT))

echo ""
echo "=========================================="
echo "  RESULTS"
echo "=========================================="
echo ""
echo "  Total     : $CONCURRENT"
echo "  Success   : $success"
echo "  Rate Ltd  : $(grep -rl "429" "$TMPDIR_OUT"/*.json 2>/dev/null | wc -l | tr -d ' ')"
echo "  Failed    : $fail"
echo "  Avg Time  : ${avg_time}ms"
echo "=========================================="
echo ""
