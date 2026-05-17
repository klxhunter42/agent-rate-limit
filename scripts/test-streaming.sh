#!/bin/bash
# SSE Streaming Test - Detailed per-chunk timing
# Tests 3 profiles: cc, lotuss, kimi
# Measures: TTFB, inter-chunk timing, total time, chunk count

GATEWAY="http://localhost:9000"
MODEL="claude-sonnet-4-20250514"
MAX_TOKENS=100
MSG='Count from 1 to 5, one number per line. Say each number then pause.'

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color
BOLD='\033[1m'

test_profile() {
    local profile="$1"
    local apikey="$2"
    local model="$3"
    local extra_headers="$4"

    echo ""
    echo -e "${BOLD}${CYAN}============================================================${NC}"
    echo -e "${BOLD}${CYAN}  Profile: ${profile} | Model: ${model}${NC}"
    echo -e "${BOLD}${CYAN}============================================================${NC}"
    echo ""

    # Temp file to capture full response with timing
    local tmpfile="/tmp/sse-test-${profile}-$(date +%s).txt"

    # Run curl with -N (no buffer) and capture timestamps
    # --no-buffer ensures curl doesn't buffer output
    local start_ms=$(python3 -c "import time; print(int(time.time()*1000))")

    curl -sS -N --no-buffer \
        -X POST "${GATEWAY}/v1/messages" \
        -H "Content-Type: application/json" \
        -H "x-api-key: ${apikey}" \
        -H "X-Profile: ${profile}" \
        -H "anthropic-version: 2023-06-01" \
        ${extra_headers} \
        -d "{\"model\":\"${model}\",\"max_tokens\":${MAX_TOKENS},\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"${MSG}\"}]}" \
        -w "\n%{http_code} %{time_starttransfer} %{time_total} %{size_download}" \
        2>/dev/null \
        | python3 -c "
import sys
import time

start_ns = time.time()
first_byte = None
chunk_count = 0
text_chunks = 0
total_bytes = 0
last_chunk_time = None
chunk_intervals = []
http_code = None
ttfb_curl = None
total_time_curl = None
size_dl = None

for raw_line in sys.stdin:
    line = raw_line.rstrip('\n')
    now = time.time()

    # Last line is curl stats
    if line.count(' ') == 3 and line.split()[0].isdigit() and len(line.split()[0]) == 3:
        parts = line.split()
        try:
            http_code = int(parts[0])
            ttfb_curl = float(parts[1])
            total_time_curl = float(parts[2])
            size_dl = int(parts[3])
        except:
            pass
        continue

    if first_byte is None and line.strip():
        first_byte = now

    chunk_count += 1
    total_bytes += len(line) + 1

    if last_chunk_time is not None:
        interval_ms = (now - last_chunk_time) * 1000
        chunk_intervals.append(interval_ms)
    last_chunk_time = now

    # Extract text content from SSE data lines
    if 'content_block_delta' in line and '\"text\"' in line:
        text_chunks += 1
        import json
        try:
            # Extract the data portion
            if line.startswith('data: '):
                data = line[6:]
                evt = json.loads(data)
                text = evt.get('delta', {}).get('text', '')
                elapsed_ms = (now - start_ns) * 1000
                print(f'  [{elapsed_ms:8.1f}ms] text: \"{text}\"', flush=True)
        except:
            pass

elapsed_ms = (time.time() - start_ns) * 1000

print()
print('=' * 58)
print(f'  HTTP Status:         {http_code}')
print(f'  Total SSE Lines:     {chunk_count}')
print(f'  Text Chunks:         {text_chunks}')
print(f'  Total Bytes:         {total_bytes}')
print(f'  Wall Time:           {elapsed_ms:.0f}ms')
if ttfb_curl is not None:
    print(f'  TTFB (curl):         {ttfb_curl*1000:.0f}ms')
if chunk_intervals:
    avg_interval = sum(chunk_intervals) / len(chunk_intervals)
    max_interval = max(chunk_intervals)
    min_interval = min(chunk_intervals)
    print(f'  Avg Chunk Interval:  {avg_interval:.1f}ms')
    print(f'  Min Chunk Interval:  {min_interval:.1f}ms')
    print(f'  Max Chunk Interval:  {max_interval:.1f}ms')

    # Streaming quality assessment
    if max_interval > 2000:
        quality = 'POOR - chunks batched, not true streaming'
    elif max_interval > 1000:
        quality = 'FAIR - some batching visible'
    elif avg_interval < 50:
        quality = 'EXCELLENT - smooth real-time streaming'
    else:
        quality = 'GOOD - streaming with minor gaps'
    print(f'  Streaming Quality:   {quality}')
print('=' * 58)
" 2>/dev/null

    echo ""
}

# ===== CC (Claude OAuth) =====
CC_KEY="${CC_KEY:-sk-ant-oat01-REPLACE_ME}"
test_profile "cc" "$CC_KEY" "claude-sonnet-4-20250514"

# ===== Lotuss =====
LOTUSS_KEY="REDACTED"
test_profile "lotuss" "$LOTUSS_KEY" "lotuss-default"

# ===== Kimi =====
KIMI_KEY="${KIMI_KEY:-sk-kimi-REPLACE_ME}"
test_profile "kimi" "$KIMI_KEY" "kimi-k2.6"

echo ""
echo -e "${BOLD}All tests complete.${NC}"
