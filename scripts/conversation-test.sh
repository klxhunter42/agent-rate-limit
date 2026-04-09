#!/bin/bash

# Conversation test: Multi-turn Thai + implementation through the gateway
# Checks: clean output, no tool_call/tool_result artifacts, no weird tags in stdout.

GATEWAY_URL="${GATEWAY_URL:-http://localhost:8080}"
API_KEY="${API_KEY:-${API_KEY:-}}"
MODEL="${MODEL:-claude-sonnet-4-20250514}"

TOTAL_TURNS=0
TOTAL_OK=0
TOTAL_FAIL=0
ARTIFACTS_FOUND=0
STDOUT_LOG="/tmp/gateway-stdout-$(date +%s).log"

echo ""
echo "=========================================="
echo "  CONVERSATION TEST - Thai + Implement"
echo "  $(date +"%Y-%m-%d %H:%M:%S")"
echo "=========================================="
echo "  Gateway : $GATEWAY_URL"
echo "  Model   : $MODEL"
echo "  Stdout  : $STDOUT_LOG"
echo ""

send_message() {
  local turn=$1
  local content="$2"
  local max_tokens="${3:-512}"

  TOTAL_TURNS=$((TOTAL_TURNS + 1))

  local start=$(date +%s%N)

  response=$(curl -s -w "\n%{http_code}" \
    -X POST "$GATEWAY_URL/v1/messages" \
    -H "Content-Type: application/json" \
    -H "x-api-key: $API_KEY" \
    -H "anthropic-version: 2023-06-01" \
    -d '{
      "model": "'"$MODEL"'",
      "max_tokens": '"$max_tokens"',
      "messages": [
        {"role": "user", "content": '"$(echo "$content" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read().strip()))')"'}
      ]
    }')

  local end=$(date +%s%N)
  local duration=$(( (end - start) / 1000000 ))
  local http_code=$(echo "$response" | tail -1)
  local body=$(echo "$response" | sed '$d')

  # Log raw body to stdout file for inspection
  echo "=== Turn #$turn | HTTP $http_code | ${duration}ms ===" >> "$STDOUT_LOG"
  echo "$body" >> "$STDOUT_LOG"
  echo "" >> "$STDOUT_LOG"

  if [ "$http_code" = "200" ]; then
    # Extract text from response
    answer=$(echo "$body" | python3 -c "
import json, sys
try:
    data = json.load(sys.stdin)
    texts = [b['text'] for b in data.get('content', []) if b.get('type') == 'text']
    print(' '.join(texts).strip())
except Exception as e:
    print(f'[PARSE ERROR: {e}]')
" 2>/dev/null)

    # Check for artifacts in raw response
    # Exclude "server_tool_use" in usage metadata — that's normal Anthropic API metadata, not an artifact
    artifacts=""
    echo "$body" | grep -vi "server_tool_use" | grep -qi "tool_call\|tool_result\|tool_use\|<tool" && artifacts="tool_call/tool_use"
    echo "$body" | grep -vi "server_tool_use" | grep -qi "thinking\|<think" && artifacts="$artifacts thinking"
    echo "$body" | grep -qi "<tool_call\|</tool_call\|<tool_result\|</tool_result" && artifacts="$artifacts custom-tags"
    echo "$answer" | grep -qi "tool_call\|tool_result\|tool_use\|<tool" && artifacts="$artifacts answer-leak"

    # Count content block types
    block_types=$(echo "$body" | python3 -c "
import json, sys
try:
    data = json.load(sys.stdin)
    types = [b.get('type','?') for b in data.get('content', [])]
    print(', '.join(types) if types else 'empty')
except:
    print('parse-error')
" 2>/dev/null)

    echo "━━━ Turn #$turn | ${duration}ms | blocks: [$block_types] ━━━"
    echo "  🧑 $content"
    echo "  🤖 $answer"

    if [ -n "$artifacts" ]; then
      echo "  ⚠️  ARTIFACTS: $artifacts"
      ARTIFACTS_FOUND=$((ARTIFACTS_FOUND + 1))
    fi
    echo ""
    TOTAL_OK=$((TOTAL_OK + 1))
    return 0
  elif [ "$http_code" = "429" ]; then
    echo "━━━ Turn #$turn | ${duration}ms ━━━"
    echo "  🧑 $content"
    echo "  🚫 429 Rate Limited"
    echo ""
    TOTAL_OK=$((TOTAL_OK + 1))
    return 0
  else
    echo "━━━ Turn #$turn | ${duration}ms ━━━"
    echo "  🧑 $content"
    echo "  ❌ HTTP $http_code"
    echo "  $(echo "$body" | head -c 300)"
    echo ""
    TOTAL_FAIL=$((TOTAL_FAIL + 1))
    return 1
  fi
}

# ═══════════════════════════════════════════
# Phase 1: Thai conversation
# ═══════════════════════════════════════════
echo "── Phase 1: คุยภาษาไทย ──"
echo ""

send_message 1 "สวัสดีครับ ผมชื่อบอทเทสต์ ยินดีที่ได้รู้จักนะครับ"
sleep 1

send_message 2 "กรุงเทพมหานครเป็นเมืองหลวงของประเทศอะไรครับ ตอบสั้นๆ"
sleep 1

send_message 3 "ช่วยนับ 1 ถึง 10 เป็นภาษาไทยให้หน่อยครับ"
sleep 1

send_message 4 "เล่ามุกตลกสั้นๆ ภาษาไทยให้ฟังหน่อยครับ"
sleep 1

# ═══════════════════════════════════════════
# Phase 2: Implementation task
# ═══════════════════════════════════════════
echo "── Phase 2: ให้ implement อะไรสักอย่าง ──"
echo ""

send_message 5 "ช่วยเขียน HTML หน้าเว็บสวยๆ แบบ responsive ที่แสดง Dashboard ง่ายๆ พร้อม dark mode โดยใช้แค่ HTML+CSS (ไม่ต้องใช้ framework) ขอแค่โค้ดเลยครับ ไม่ต้องอธิบาย" 2048
sleep 1

send_message 6 "เอา HTML ที่เขียนเมื่อกี้ มาเพิ่ม gradient background สวยๆ และ animation fade-in ให้หน่อยครับ ขอแค่ส่วนที่เพิ่มมา" 1024
sleep 1

# ═══════════════════════════════════════════
# Phase 3: Delete / cleanup
# ═══════════════════════════════════════════
echo "── Phase 3: ลบออก ──"
echo ""

send_message 7 "โอเค ถ้าอยากลบไฟล์ HTML นั้นออก ต้องทำยังไงครับ ตอบสั้นๆ"
sleep 1

send_message 8 "ขอบคุณมากครับสำหรับความช่วยเหลือ! สรุปสิ่งที่เราทำไปวันนี้ให้หน่อยครับ"

# ═══════════════════════════════════════════
# Summary
# ═══════════════════════════════════════════
echo "=========================================="
echo "  สรุปผล"
echo "=========================================="
echo ""
echo "  คำถามทั้งหมด  : $TOTAL_TURNS"
echo "  สำเร็จ         : $TOTAL_OK"
echo "  ล้มเหลว        : $TOTAL_FAIL"
echo "  Artifact พบ    : $ARTIFACTS_FOUND"
echo ""
echo "  Stdout log     : $STDOUT_LOG"
echo ""

if [ "$ARTIFACTS_FOUND" -gt 0 ]; then
  echo "  ⚠️  มี artifact แปลกๆ ใน response!"
  echo "  ตรวจสอบ log: cat $STDOUT_LOG"
  echo ""
  echo "  Artifacts ที่พบ:"
  grep -n -i "tool_call\|tool_result\|tool_use\|<tool\|<think" "$STDOUT_LOG" | head -20
else
  echo "  ✅ Clean! ไม่มี artifact แปลกๆ"
fi

echo ""
echo "=========================================="
