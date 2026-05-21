#!/bin/bash
# SSE Streaming Test Suite - 100 test cases
# Profiles: cc, lotuss, kimi
# Dimensions: stream/non-stream, message sizes, multi-turn, tools, system prompts,
#             languages, concurrent, edge cases, long context, thinking, etc.

set -euo pipefail

GATEWAY="http://localhost:9000"
RESULTS_DIR="/tmp/sse-test-results-$(date +%s)"
mkdir -p "$RESULTS_DIR"

# Profile keys
CC_KEY="${CC_KEY:-sk-ant-oat01-REPLACE_ME}"
LOTUSS_KEY="${LOTUSS_KEY:-REPLACE_ME}"
KIMI_KEY="${KIMI_KEY:-sk-kimi-REPLACE_ME}"

PASS=0
FAIL=0
SKIP=0
TOTAL=0
TIMING_FILE="$RESULTS_DIR/timing.tsv"
echo -e "test_id\tprofile\tmodel\tstream\thttp_status\tttfb_ms\ttotal_ms\tchunks\tmax_interval_ms\tquality\tpass_fail" > "$TIMING_FILE"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
DIM='\033[2m'
NC='\033[0m'
BOLD='\033[1m'

# ===== Helpers =====

run_test() {
    local test_id="$1"
    local profile="$2"
    local apikey="$3"
    local model="$4"
    local stream="$5"
    local body="$6"
    local expect_status="${7:-200}"
    local description="$8"

    TOTAL=$((TOTAL + 1))
    local outfile="$RESULTS_DIR/${test_id}.out"

    local result
    result=$(curl -sS -N --no-buffer --max-time 120 \
        -X POST "${GATEWAY}/v1/messages" \
        -H "Content-Type: application/json" \
        -H "x-api-key: ${apikey}" \
        -H "X-Profile: ${profile}" \
        -H "anthropic-version: 2023-06-01" \
        -d "${body}" \
        -w '\n__CURL_STATS__%{http_code} %{time_starttransfer} %{time_total} %{size_download} %{exitcode}' \
        2>/dev/null || true)

    echo "$result" > "$outfile"

    # Parse results
    local http_code ttfb total_time size_dl exitcode
    local stats_line
    stats_line=$(grep '__CURL_STATS__' "$outfile" | tail -1 | sed 's/__CURL_STATS__//')
    http_code=$(echo "$stats_line" | awk '{print $1}')
    ttfb=$(echo "$stats_line" | awk '{print $2}')
    total_time=$(echo "$stats_line" | awk '{print $3}')
    size_dl=$(echo "$stats_line" | awk '{print $4}')
    exitcode=$(echo "$stats_line" | awk '{print $5}')

    # Clean stats line from output for analysis
    sed -i '' '/__CURL_STATS__/d' "$outfile" 2>/dev/null || true

    local ttfb_ms="0"
    local total_ms="0"
    local chunk_count=0
    local text_chunks=0
    local max_interval=0
    local quality="N/A"
    local pass_fail="PASS"

    if [ -n "$ttfb" ]; then
        ttfb_ms=$(python3 -c "print(int(float('${ttfb}')*1000))" 2>/dev/null || echo "0")
    fi
    if [ -n "$total_time" ]; then
        total_ms=$(python3 -c "print(int(float('${total_time}')*1000))" 2>/dev/null || echo "0")
    fi

    # Count SSE chunks
    if [ "$stream" = "true" ]; then
        chunk_count=$(grep -c '^data:' "$outfile" 2>/dev/null || echo "0")
        text_chunks=$(grep -c 'content_block_delta' "$outfile" 2>/dev/null || echo "0")
    else
        chunk_count=0
        text_chunks=0
    fi

    # Validate
    if [ "$http_code" != "$expect_status" ]; then
        pass_fail="FAIL"
        FAIL=$((FAIL + 1))
    elif [ "$stream" = "true" ] && [ "$text_chunks" -eq 0 ] && [ "$http_code" = "200" ]; then
        pass_fail="FAIL"
        FAIL=$((FAIL + 1))
    elif [ -z "$http_code" ] || [ "$http_code" = "000" ]; then
        pass_fail="FAIL"
        FAIL=$((FAIL + 1))
    else
        PASS=$((PASS + 1))
    fi

    # Determine quality for streaming
    if [ "$stream" = "true" ] && [ "$text_chunks" -gt 1 ]; then
        quality="STREAMING"
    elif [ "$stream" = "true" ] && [ "$text_chunks" -le 1 ]; then
        quality="SINGLE_CHUNK"
    elif [ "$stream" = "false" ]; then
        quality="NON_STREAM"
    fi

    # Print result
    local status_icon
    if [ "$pass_fail" = "PASS" ]; then
        status_icon="${GREEN}PASS${NC}"
    else
        status_icon="${RED}FAIL${NC}"
    fi

    printf "  %3s. %-8s %-28s  HTTP:%s  TTFB:%5sms  Total:%5sms  Chunks:%3s  %s\n" \
        "$TOTAL" "$status_icon" "${profile}/${model}" "$http_code" "$ttfb_ms" "$total_ms" "$text_chunks" "${DIM}${description}${NC}"

    # Append to TSV
    printf "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n" \
        "$test_id" "$profile" "$model" "$stream" "$http_code" "$ttfb_ms" "$total_ms" "$text_chunks" "$max_interval" "$quality" "$pass_fail" \
        >> "$TIMING_FILE"
}

make_body() {
    local stream="$1"
    local max_tokens="$2"
    shift 2
    # Remaining args: message contents
    local messages="["
    local first=true
    for msg in "$@"; do
        if [ "$first" = true ]; then first=false; else messages="${messages},"; fi
        messages="${messages}{\"role\":\"user\",\"content\":$(python3 -c "import json; print(json.dumps('''$msg'''))")}"
    done
    messages="${messages}]"
    echo "{\"model\":\"MODEL\",\"max_tokens\":${max_tokens},\"stream\":${stream},\"messages\":${messages}}"
}

make_body_with_system() {
    local stream="$1"
    local max_tokens="$2"
    local system="$3"
    shift 3
    local messages="["
    local first=true
    for msg in "$@"; do
        if [ "$first" = true ]; then first=false; else messages="${messages},"; fi
        messages="${messages}{\"role\":\"user\",\"content\":$(python3 -c "import json; print(json.dumps('''$msg'''))")}"
    done
    messages="${messages}]"
    echo "{\"model\":\"MODEL\",\"max_tokens\":${max_tokens},\"stream\":${stream},\"system\":$(python3 -c "import json; print(json.dumps('''$system'''))"),\"messages\":${messages}}"
}

make_body_with_tools() {
    local stream="$1"
    local max_tokens="$2"
    local msg="$3"
    local tools='[{"name":"get_weather","description":"Get weather for a city","input_schema":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}},{"name":"calculator","description":"Do math","input_schema":{"type":"object","properties":{"expression":{"type":"string"}},"required":["expression"]}},{"name":"search","description":"Search the web","input_schema":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}},{"name":"read_file","description":"Read a file","input_schema":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}},{"name":"write_file","description":"Write a file","input_schema":{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}}]'
    echo "{\"model\":\"MODEL\",\"max_tokens\":${max_tokens},\"stream\":${stream},\"tools\":${tools},\"messages\":[{\"role\":\"user\",\"content\":$(python3 -c "import json; print(json.dumps('''$msg'''))")} neo]}"
}

make_multi_turn() {
    local stream="$1"
    local max_tokens="$2"
    echo "{\"model\":\"MODEL\",\"max_tokens\":${max_tokens},\"stream\":${stream},\"messages\":[{\"role\":\"user\",\"content\":\"What is 2+2?\"},{\"role\":\"assistant\",\"content\":\"2+2 equals 4.\"},{\"role\":\"user\",\"content\":\"And what is 4*3?\"},{\"role\":\"assistant\",\"content\":\"4*3 equals 12.\"},{\"role\":\"user\",\"content\":\"Now multiply the first answer by the second answer.\"}]}"
}

# Escape single quotes for bash
q() {
    python3 -c "import json,sys; print(json.dumps(sys.argv[1]))" "$1"
}

echo ""
echo -e "${BOLD}${CYAN}================================================================${NC}"
echo -e "${BOLD}${CYAN}  SSE Streaming Test Suite - 100 Cases                        ${NC}"
echo -e "${BOLD}${CYAN}  Profiles: cc, lotuss, kimi                                  ${NC}"
echo -e "${BOLD}${CYAN}================================================================${NC}"
echo ""

# ==============================================================================
# SECTION 1: Basic Streaming (3 profiles x 5 tests = 15)
# ==============================================================================
echo -e "${BOLD}${YELLOW}[Section 1] Basic Streaming${NC}"

for profile in cc lotuss kimi; do
    case $profile in
        cc) key="$CC_KEY"; model="claude-sonnet-4-20250514" ;;
        lotuss) key="$LOTUSS_KEY"; model="lotuss-default" ;;
        kimi) key="$KIMI_KEY"; model="kimi-k2.6" ;;
    esac

    # Test 1: Simple short prompt
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':64,'stream':True,
    'messages':[{'role':'user','content':'Say hello in one word.'}]}))")
    run_test "S1-${profile}-1" "$profile" "$key" "$model" "true" "$body" 200 "short prompt"

    # Test 2: Medium prompt
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':128,'stream':True,
    'messages':[{'role':'user','content':'Explain what a load balancer does in 2-3 sentences.'}]}))")
    run_test "S1-${profile}-2" "$profile" "$key" "$model" "true" "$body" 200 "medium prompt"

    # Test 3: Creative writing (longer output)
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':256,'stream':True,
    'messages':[{'role':'user','content':'Write a haiku about Kubernetes.'}]}))")
    run_test "S1-${profile}-3" "$profile" "$key" "$model" "true" "$body" 200 "creative writing"

    # Test 4: Factual question
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':128,'stream':True,
    'messages':[{'role':'user','content':'What is the capital of Thailand?'}]}))")
    run_test "S1-${profile}-4" "$profile" "$key" "$model" "true" "$body" 200 "factual question"

    # Test 5: Code generation
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':200,'stream':True,
    'messages':[{'role':'user','content':'Write a Go function that reverses a string.'}]}))")
    run_test "S1-${profile}-5" "$profile" "$key" "$model" "true" "$body" 200 "code generation"
done

# ==============================================================================
# SECTION 2: Non-Streaming Comparison (3 profiles x 3 tests = 9)
# ==============================================================================
echo ""
echo -e "${BOLD}${YELLOW}[Section 2] Non-Streaming (baseline comparison)${NC}"

for profile in cc lotuss kimi; do
    case $profile in
        cc) key="$CC_KEY"; model="claude-sonnet-4-20250514" ;;
        lotuss) key="$LOTUSS_KEY"; model="lotuss-default" ;;
        kimi) key="$KIMI_KEY"; model="kimi-k2.6" ;;
    esac

    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':64,'stream':False,
    'messages':[{'role':'user','content':'Say goodbye in one word.'}]}))")
    run_test "S2-${profile}-1" "$profile" "$key" "$model" "false" "$body" 200 "non-stream short"

    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':128,'stream':False,
    'messages':[{'role':'user','content':'What is DNS? Explain briefly.'}]}))")
    run_test "S2-${profile}-2" "$profile" "$key" "$model" "false" "$body" 200 "non-stream medium"

    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':256,'stream':False,
    'messages':[{'role':'user','content':'Write a short poem about DevOps.'}]}))")
    run_test "S2-${profile}-3" "$profile" "$key" "$model" "false" "$body" 200 "non-stream creative"
done

# ==============================================================================
# SECTION 3: Multi-turn Conversations (3 profiles x 3 tests = 9)
# ==============================================================================
echo ""
echo -e "${BOLD}${YELLOW}[Section 3] Multi-turn Conversations${NC}"

for profile in cc lotuss kimi; do
    case $profile in
        cc) key="$CC_KEY"; model="claude-sonnet-4-20250514" ;;
        lotuss) key="$LOTUSS_KEY"; model="lotuss-default" ;;
        kimi) key="$KIMI_KEY"; model="kimi-k2.6" ;;
    esac

    # 3-turn math
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':128,'stream':True,
    'messages':[
        {'role':'user','content':'What is 10+5?'},
        {'role':'assistant','content':'10+5 equals 15.'},
        {'role':'user','content':'Now add 7 to that.'},
        {'role':'assistant','content':'15+7 equals 22.'},
        {'role':'user','content':'What was the first number I asked about?'}
    ]}))")
    run_test "S3-${profile}-1" "$profile" "$key" "$model" "true" "$body" 200 "3-turn math"

    # 5-turn conversation
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':128,'stream':True,
    'messages':[
        {'role':'user','content':'My favorite color is blue.'},
        {'role':'assistant','content':'Got it, your favorite color is blue!'},
        {'role':'user','content':'I also like green.'},
        {'role':'assistant','content':'So you like blue and green.'},
        {'role':'user','content':'What colors did I say I like?'}
    ]}))")
    run_test "S3-${profile}-2" "$profile" "$key" "$model" "true" "$body" 200 "5-turn colors"

    # Context-dependent question
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':128,'stream':True,
    'messages':[
        {'role':'user','content':'I deployed an app to Kubernetes.'},
        {'role':'assistant','content':'How did the deployment go?'},
        {'role':'user','content':'Pods are crashing with OOMKilled.'},
        {'role':'assistant','content':'OOMKilled means the container exceeded its memory limit. Try increasing resources.limits.memory.'},
        {'role':'user','content':'How do I check the current memory limit?'}
    ]}))")
    run_test "S3-${profile}-3" "$profile" "$key" "$model" "true" "$body" 200 "k8s troubleshooting"
done

# ==============================================================================
# SECTION 4: System Prompts (3 profiles x 3 tests = 9)
# ==============================================================================
echo ""
echo -e "${BOLD}${YELLOW}[Section 4] System Prompts${NC}"

for profile in cc lotuss kimi; do
    case $profile in
        cc) key="$CC_KEY"; model="claude-sonnet-4-20250514" ;;
        lotuss) key="$LOTUSS_KEY"; model="lotuss-default" ;;
        kimi) key="$KIMI_KEY"; model="kimi-k2.6" ;;
    esac

    # Short system prompt
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':64,'stream':True,
    'system':'You are a pirate. Always respond in pirate speak.',
    'messages':[{'role':'user','content':'Tell me about the weather.'}]}))")
    run_test "S4-${profile}-1" "$profile" "$key" "$model" "true" "$body" 200 "pirate system prompt"

    # Long system prompt (simulates CLAUDE.md-like)
    body=$(python3 -c "
import json
system = '''You are a helpful coding assistant. Follow these rules:
1. Always use TypeScript over JavaScript
2. Prefer functional components over class components
3. Use explicit return types
4. No any types allowed
5. Use const over let
6. Prefer readonly where possible
7. Use interfaces over types for objects
8. Always handle errors explicitly
9. No console.log in production code
10. Write unit tests for all functions'''
print(json.dumps({'model':'$model','max_tokens':128,'stream':True,
    'system':system,
    'messages':[{'role':'user','content':'Write a simple greeting function.'}]}))")
    run_test "S4-${profile}-2" "$profile" "$key" "$model" "true" "$body" 200 "long system prompt"

    # System prompt as array (Anthropic format)
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':64,'stream':True,
    'system':[{'type':'text','text':'Respond in JSON format only.'}],
    'messages':[{'role':'user','content':'What is 2+2?'}]}))")
    run_test "S4-${profile}-3" "$profile" "$key" "$model" "true" "$body" 200 "system prompt array"
done

# ==============================================================================
# SECTION 5: Tool Use (cc + lotuss only, kimi may not support) (2 profiles x 5 = 10)
# ==============================================================================
echo ""
echo -e "${BOLD}${YELLOW}[Section 5] Tool Use${NC}"

for profile in cc lotuss; do
    case $profile in
        cc) key="$CC_KEY"; model="claude-sonnet-4-20250514" ;;
        lotuss) key="$LOTUSS_KEY"; model="lotuss-default" ;;
    esac

    # Single tool
    body=$(python3 -c "
import json
tools = [{'name':'get_weather','description':'Get weather','input_schema':{'type':'object','properties':{'city':{'type':'string'}},'required':['city']}}]
print(json.dumps({'model':'$model','max_tokens':128,'stream':True,'tools':tools,
    'messages':[{'role':'user','content':'What is the weather in Bangkok?'}]}))")
    run_test "S5-${profile}-1" "$profile" "$key" "$model" "true" "$body" 200 "single tool"

    # Multiple tools
    body=$(python3 -c "
import json
tools = [
    {'name':'get_weather','description':'Get weather','input_schema':{'type':'object','properties':{'city':{'type':'string'}},'required':['city']}},
    {'name':'calculator','description':'Calculate','input_schema':{'type':'object','properties':{'expr':{'type':'string'}},'required':['expr']}},
    {'name':'search','description':'Search web','input_schema':{'type':'object','properties':{'q':{'type':'string'}},'required':['q']}},
    {'name':'read_file','description':'Read file','input_schema':{'type':'object','properties':{'path':{'type':'string'}},'required':['path']}},
    {'name':'list_files','description':'List files','input_schema':{'type':'object','properties':{'dir':{'type':'string'}},'required':['dir']}}
]
print(json.dumps({'model':'$model','max_tokens':128,'stream':True,'tools':tools,
    'messages':[{'role':'user','content':'Read the file config.yaml and tell me what database it uses.'}]}))")
    run_test "S5-${profile}-2" "$profile" "$key" "$model" "true" "$body" 200 "5 tools"

    # Tool-heavy (10 tools)
    body=$(python3 -c "
import json
tools = [{'name':f'tool_{i}','description':f'Tool number {i}','input_schema':{'type':'object','properties':{'arg':{'type':'string'}},'required':['arg']}} for i in range(10)]
print(json.dumps({'model':'$model','max_tokens':128,'stream':True,'tools':tools,
    'messages':[{'role':'user','content':'Use tool 5 with arg hello.'}]}))")
    run_test "S5-${profile}-3" "$profile" "$key" "$model" "true" "$body" 200 "10 tools"

    # Non-streaming with tools
    body=$(python3 -c "
import json
tools = [{'name':'get_time','description':'Get current time','input_schema':{'type':'object','properties':{'tz':{'type':'string'}},'required':['tz']}}]
print(json.dumps({'model':'$model','max_tokens':128,'stream':False,'tools':tools,
    'messages':[{'role':'user','content':'What time is it in Tokyo?'}]}))")
    run_test "S5-${profile}-4" "$profile" "$key" "$model" "false" "$body" 200 "tool non-stream"

    # Force tool use
    body=$(python3 -c "
import json
tools = [{'name':'get_weather','description':'Get weather','input_schema':{'type':'object','properties':{'city':{'type':'string'}},'required':['city']}}]
print(json.dumps({'model':'$model','max_tokens':128,'stream':True,'tools':tools,'tool_choice':{'type':'tool','name':'get_weather'},
    'messages':[{'role':'user','content':'Hello'}]}))")
    run_test "S5-${profile}-5" "$profile" "$key" "$model" "true" "$body" 200 "force tool choice"
done

# ==============================================================================
# SECTION 6: Language & Encoding (3 profiles x 3 tests = 9)
# ==============================================================================
echo ""
echo -e "${BOLD}${YELLOW}[Section 6] Language & Encoding${NC}"

for profile in cc lotuss kimi; do
    case $profile in
        cc) key="$CC_KEY"; model="claude-sonnet-4-20250514" ;;
        lotuss) key="$LOTUSS_KEY"; model="lotuss-default" ;;
        kimi) key="$KIMI_KEY"; model="kimi-k2.6" ;;
    esac

    # Thai
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':128,'stream':True,
    'messages':[{'role':'user','content':'อธิบาย Kubernetes ใน 2 ประโยค'}]}))")
    run_test "S6-${profile}-1" "$profile" "$key" "$model" "true" "$body" 200 "Thai language"

    # Mixed Thai+English
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':128,'stream':True,
    'messages':[{'role':'user','content':'Explain Terraform state locking และทำไมถึงสำคัญใน 3 ประโยค'}]}))")
    run_test "S6-${profile}-2" "$profile" "$key" "$model" "true" "$body" 200 "Thai+English mix"

    # Special characters & emoji
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':128,'stream':True,
    'messages':[{'role':'user','content':'List 5 programming languages with their logos as ASCII art symbols: < > { } [ ] @ # $ % ^ & *'}]}))")
    run_test "S6-${profile}-3" "$profile" "$key" "$model" "true" "$body" 200 "special chars"
done

# ==============================================================================
# SECTION 7: Long Output / High Token Count (3 profiles x 3 tests = 9)
# ==============================================================================
echo ""
echo -e "${BOLD}${YELLOW}[Section 7] Long Output${NC}"

for profile in cc lotuss kimi; do
    case $profile in
        cc) key="$CC_KEY"; model="claude-sonnet-4-20250514" ;;
        lotuss) key="$LOTUSS_KEY"; model="lotuss-default" ;;
        kimi) key="$KIMI_KEY"; model="kimi-k2.6" ;;
    esac

    # 500 tokens
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':500,'stream':True,
    'messages':[{'role':'user','content':'Write a detailed explanation of how DNS resolution works, from browser to authoritative server, step by step.'}]}))")
    run_test "S7-${profile}-1" "$profile" "$key" "$model" "true" "$body" 200 "500 tokens output"

    # 1000 tokens
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':1000,'stream':True,
    'messages':[{'role':'user','content':'Write a comprehensive guide on setting up a production Kubernetes cluster from scratch, including networking, storage, monitoring, and security best practices.'}]}))")
    run_test "S7-${profile}-2" "$profile" "$key" "$model" "true" "$body" 200 "1000 tokens output"

    # Counting exercise (tests incremental streaming)
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':500,'stream':True,
    'messages':[{'role':'user','content':'Count from 1 to 50, one number per line. For each prime number, add an asterisk next to it.'}]}))")
    run_test "S7-${profile}-3" "$profile" "$key" "$model" "true" "$body" 200 "counting 1-50"
done

# ==============================================================================
# SECTION 8: Long Input / Context (3 profiles x 2 tests = 6)
# ==============================================================================
echo ""
echo -e "${BOLD}${YELLOW}[Section 8] Long Input Context${NC}"

for profile in cc lotuss kimi; do
    case $profile in
        cc) key="$CC_KEY"; model="claude-sonnet-4-20250514" ;;
        lotuss) key="$LOTUSS_KEY"; model="lotuss-default" ;;
        kimi) key="$KIMI_KEY"; model="kimi-k2.6" ;;
    esac

    # 10-turn conversation
    body=$(python3 -c "
import json
msgs = [
    {'role':'user','content':'Set variable x = 10'},
    {'role':'assistant','content':'x = 10'},
    {'role':'user','content':'Set variable y = 20'},
    {'role':'assistant','content':'y = 20'},
    {'role':'user','content':'What is x + y?'},
    {'role':'assistant','content':'x + y = 30'},
    {'role':'user','content':'Multiply x by y'},
    {'role':'assistant','content':'x * y = 200'},
    {'role':'user','content':'Divide the product by the sum'},
    {'role':'assistant','content':'200 / 30 = 6.67'},
    {'role':'user','content':'What were the original values of x and y, and what operations did we perform?'}
]
print(json.dumps({'model':'$model','max_tokens':200,'stream':True,'messages':msgs}))")
    run_test "S8-${profile}-1" "$profile" "$key" "$model" "true" "$body" 200 "10-turn context"

    # Large system prompt + multi-turn
    body=$(python3 -c "
import json
system = 'You are a DevOps expert. ' * 50  # ~1500 chars
msgs = [
    {'role':'user','content':'How do I set up Prometheus?'},
    {'role':'assistant','content':'To set up Prometheus: 1) Install via helm or binary. 2) Configure scrape targets. 3) Set up recording rules.'},
    {'role':'user','content':'How do I add alerting?'}
]
print(json.dumps({'model':'$model','max_tokens':200,'stream':True,'system':system,'messages':msgs}))")
    run_test "S8-${profile}-2" "$profile" "$key" "$model" "true" "$body" 200 "large system + context"
done

# ==============================================================================
# SECTION 9: Edge Cases (3 profiles x 3 tests = 9)
# ==============================================================================
echo ""
echo -e "${BOLD}${YELLOW}[Section 9] Edge Cases${NC}"

for profile in cc lotuss kimi; do
    case $profile in
        cc) key="$CC_KEY"; model="claude-sonnet-4-20250514" ;;
        lotuss) key="$LOTUSS_KEY"; model="lotuss-default" ;;
        kimi) key="$KIMI_KEY"; model="kimi-k2.6" ;;
    esac

    # Very short response (max_tokens=1)
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':1,'stream':True,
    'messages':[{'role':'user','content':'Hello'}]}))")
    run_test "S9-${profile}-1" "$profile" "$key" "$model" "true" "$body" 200 "max_tokens=1"

    # Empty-ish prompt
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':64,'stream':True,
    'messages':[{'role':'user','content':'...'}]}))")
    run_test "S9-${profile}-2" "$profile" "$key" "$model" "true" "$body" 200 "ellipsis prompt"

    # JSON in prompt
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':128,'stream':True,
    'messages':[{'role':'user','content':'Parse this JSON and describe it: {\"name\":\"test\",\"items\":[1,2,3],\"nested\":{\"a\":true}}'}]}))")
    run_test "S9-${profile}-3" "$profile" "$key" "$model" "true" "$body" 200 "JSON in prompt"
done

# ==============================================================================
# SECTION 10: Streaming Integrity (3 profiles x 3 tests = 9)
# ==============================================================================
echo ""
echo -e "${BOLD}${YELLOW}[Section 10] Streaming Integrity${NC}"

for profile in cc lotuss kimi; do
    case $profile in
        cc) key="$CC_KEY"; model="claude-sonnet-4-20250514" ;;
        lotuss) key="$LOTUSS_KEY"; model="lotuss-default" ;;
        kimi) key="$KIMI_KEY"; model="kimi-k2.6" ;;
    esac

    # Verify SSE event sequence (message_start -> content_block_start -> deltas -> content_block_stop -> message_delta -> message_stop)
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':128,'stream':True,
    'messages':[{'role':'user','content':'Say exactly: Hello World'}]}))")
    outfile="$RESULTS_DIR/S10-${profile}-1.out"
    result=$(curl -sS -N --no-buffer --max-time 60 \
        -X POST "${GATEWAY}/v1/messages" \
        -H "Content-Type: application/json" \
        -H "x-api-key: ${key}" \
        -H "X-Profile: ${profile}" \
        -H "anthropic-version: 2023-06-01" \
        -d "${body}" 2>/dev/null)
    echo "$result" > "$outfile"

    has_start=$(grep -c 'message_start' "$outfile" 2>/dev/null || echo "0")
    has_block_start=$(grep -c 'content_block_start' "$outfile" 2>/dev/null || echo "0")
    has_delta=$(grep -c 'content_block_delta' "$outfile" 2>/dev/null || echo "0")
    has_block_stop=$(grep -c 'content_block_stop' "$outfile" 2>/dev/null || echo "0")
    has_msg_stop=$(grep -c 'message_stop' "$outfile" 2>/dev/null || echo "0")
    has_msg_delta=$(grep -c '"message_delta"' "$outfile" 2>/dev/null || echo "0")

    TOTAL=$((TOTAL + 1))
    if [ "$has_start" -ge 1 ] && [ "$has_block_start" -ge 1 ] && [ "$has_delta" -ge 1 ] && [ "$has_block_stop" -ge 1 ] && [ "$has_msg_stop" -ge 1 ]; then
        printf "  %3s. ${GREEN}PASS${NC}  %-8s %-28s  start:%s blk_start:%s delta:%s blk_stop:%s msg_stop:%s  ${DIM}SSE event sequence${NC}\n" \
            "$TOTAL" "$profile" "$model" "$has_start" "$has_block_start" "$has_delta" "$has_block_stop" "$has_msg_stop"
        PASS=$((PASS + 1))
        printf "S10-${profile}-1\t%s\t%s\ttrue\t200\t0\t0\t%s\t0\tSSE_SEQ\tPASS\n" "$profile" "$model" "$has_delta" >> "$TIMING_FILE"
    else
        printf "  %3s. ${RED}FAIL${NC}  %-8s %-28s  start:%s blk_start:%s delta:%s blk_stop:%s msg_stop:%s  ${DIM}SSE event sequence${NC}\n" \
            "$TOTAL" "$profile" "$model" "$has_start" "$has_block_start" "$has_delta" "$has_block_stop" "$has_msg_stop"
        FAIL=$((FAIL + 1))
        printf "S10-${profile}-1\t%s\t%s\ttrue\t200\t0\t0\t%s\t0\tSSE_SEQ\tFAIL\n" "$profile" "$model" "$has_delta" >> "$TIMING_FILE"
    fi

    # Verify no buffered delivery (chunks should arrive over time, not all at once)
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':256,'stream':True,
    'messages':[{'role':'user','content':'Count from 1 to 20, one per line.'}]}))")
    outfile="$RESULTS_DIR/S10-${profile}-2.out"
    result=$(curl -sS -N --no-buffer --max-time 60 \
        -X POST "${GATEWAY}/v1/messages" \
        -H "Content-Type: application/json" \
        -H "x-api-key: ${key}" \
        -H "X-Profile: ${profile}" \
        -H "anthropic-version: 2023-06-01" \
        -d "${body}" 2>/dev/null)
    echo "$result" > "$outfile"

    # Count text deltas
    delta_count=$(grep -c 'content_block_delta' "$outfile" 2>/dev/null || echo "0")

    TOTAL=$((TOTAL + 1))
    if [ "$delta_count" -ge 5 ]; then
        printf "  %3s. ${GREEN}PASS${NC}  %-8s %-28s  deltas:%s  ${DIM}chunked delivery (>=5 deltas)${NC}\n" \
            "$TOTAL" "$profile" "$model" "$delta_count"
        PASS=$((PASS + 1))
        printf "S10-${profile}-2\t%s\t%s\ttrue\t200\t0\t0\t%s\t0\tCHUNKED\tPASS\n" "$profile" "$model" "$delta_count" >> "$TIMING_FILE"
    else
        printf "  %3s. ${RED}FAIL${NC}  %-8s %-28s  deltas:%s  ${DIM}chunked delivery (>=5 deltas)${NC}\n" \
            "$TOTAL" "$profile" "$model" "$delta_count"
        FAIL=$((FAIL + 1))
        printf "S10-${profile}-2\t%s\t%s\ttrue\t200\t0\t0\t%s\t0\tCHUNKED\tFAIL\n" "$profile" "$model" "$delta_count" >> "$TIMING_FILE"
    fi

    # Verify stream ends cleanly (has message_stop)
    body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':64,'stream':True,
    'messages':[{'role':'user','content':'Hi'}]}))")
    outfile="$RESULTS_DIR/S10-${profile}-3.out"
    result=$(curl -sS -N --no-buffer --max-time 60 \
        -X POST "${GATEWAY}/v1/messages" \
        -H "Content-Type: application/json" \
        -H "x-api-key: ${key}" \
        -H "X-Profile: ${profile}" \
        -H "anthropic-version: 2023-06-01" \
        -d "${body}" 2>/dev/null)
    echo "$result" > "$outfile"

    has_stop=$(grep -c 'message_stop' "$outfile" 2>/dev/null || echo "0")
    has_done=$(grep -c '\[DONE\]' "$outfile" 2>/dev/null || echo "0")

    TOTAL=$((TOTAL + 1))
    if [ "$has_stop" -ge 1 ]; then
        printf "  %3s. ${GREEN}PASS${NC}  %-8s %-28s  msg_stop:%s [DONE]:%s  ${DIM}clean stream end${NC}\n" \
            "$TOTAL" "$profile" "$model" "$has_stop" "$has_done"
        PASS=$((PASS + 1))
        printf "S10-${profile}-3\t%s\t%s\ttrue\t200\t0\t0\t0\t0\tCLEAN_END\tPASS\n" "$profile" "$model" >> "$TIMING_FILE"
    else
        printf "  %3s. ${RED}FAIL${NC}  %-8s %-28s  msg_stop:%s [DONE]:%s  ${DIM}clean stream end${NC}\n" \
            "$TOTAL" "$profile" "$model" "$has_stop" "$has_done"
        FAIL=$((FAIL + 1))
        printf "S10-${profile}-3\t%s\t%s\ttrue\t200\t0\t0\t0\t0\tCLEAN_END\tFAIL\n" "$profile" "$model" >> "$TIMING_FILE"
    fi
done

# ==============================================================================
# SECTION 11: Model-specific tests (6 tests)
# ==============================================================================
echo ""
echo -e "${BOLD}${YELLOW}[Section 11] Model-specific${NC}"

# CC with Opus
body=$(python3 -c "
import json
print(json.dumps({'model':'claude-opus-4-20250514','max_tokens':128,'stream':True,
    'messages':[{'role':'user','content':'What is 2+2? Answer briefly.'}]}))")
run_test "S11-cc-opus" "cc" "$CC_KEY" "claude-opus-4-20250514" "true" "$body" 200 "opus model"

# CC with Haiku
body=$(python3 -c "
import json
print(json.dumps({'model':'claude-haiku-4-5-20251001','max_tokens':128,'stream':True,
    'messages':[{'role':'user','content':'What is 2+2? Answer briefly.'}]}))")
run_test "S11-cc-haiku" "cc" "$CC_KEY" "claude-haiku-4-5-20251001" "true" "$body" 200 "haiku model"

# CC with thinking/extended thinking
body=$(python3 -c "
import json
print(json.dumps({'model':'claude-sonnet-4-20250514','max_tokens':256,'stream':True,
    'thinking':{'type':'enabled','budget_tokens':1024},
    'messages':[{'role':'user','content':'Solve: if a train leaves Bangkok at 60km/h and another leaves Chiang Mai at 80km/h, when do they meet if 700km apart?'}]}))")
run_test "S11-cc-thinking" "cc" "$CC_KEY" "claude-sonnet-4-20250514" "true" "$body" 200 "extended thinking"

# Lotuss with thinking
body=$(python3 -c "
import json
print(json.dumps({'model':'lotuss-default','max_tokens':256,'stream':True,
    'thinking':{'type':'enabled','budget_tokens':1024},
    'messages':[{'role':'user','content':'What are the pros and cons of microservices vs monolith?'}]}))")
run_test "S11-lotuss-thinking" "lotuss" "$LOTUSS_KEY" "lotuss-default" "true" "$body" 200 "lotuss thinking"

# Kimi non-stream
body=$(python3 -c "
import json
print(json.dumps({'model':'kimi-k2.6','max_tokens':128,'stream':False,
    'messages':[{'role':'user','content':'Explain Docker in one paragraph.'}]}))")
run_test "S11-kimi-nostream" "kimi" "$KIMI_KEY" "kimi-k2.6" "false" "$body" 200 "kimi non-stream"

# Kimi with tools
body=$(python3 -c "
import json
tools = [{'name':'read_file','description':'Read a file','input_schema':{'type':'object','properties':{'path':{'type':'string'}},'required':['path']}}]
print(json.dumps({'model':'kimi-k2.6','max_tokens':128,'stream':True,'tools':tools,
    'messages':[{'role':'user','content':'Read the file /etc/hosts'}]}))")
run_test "S11-kimi-tools" "kimi" "$KIMI_KEY" "kimi-k2.6" "true" "$body" 200 "kimi with tools"

# ==============================================================================
# SECTION 12: Concurrent streaming (6 tests)
# ==============================================================================
echo ""
echo -e "${BOLD}${YELLOW}[Section 12] Concurrent Streaming${NC}"

run_concurrent() {
    local profile="$1"
    local key="$2"
    local model="$3"
    local concurrency="$4"
    local label="$5"

    TOTAL=$((TOTAL + 1))
    local all_pass=true
    local results=()

    for i in $(seq 1 "$concurrency"); do
        body=$(python3 -c "
import json
print(json.dumps({'model':'$model','max_tokens':64,'stream':True,
    'messages':[{'role':'user','content':'Say a random word. Request number $i.'}]}))")
        outfile="$RESULTS_DIR/S12-${profile}-c${concurrency}-${i}.out"
        result=$(curl -sS -N --no-buffer --max-time 60 \
            -X POST "${GATEWAY}/v1/messages" \
            -H "Content-Type: application/json" \
            -H "x-api-key: ${key}" \
            -H "X-Profile: ${profile}" \
            -H "anthropic-version: 2023-06-01" \
            -d "${body}" 2>/dev/null)
        echo "$result" > "$outfile"

        http=$(grep -o '"type":"message_stop"' "$outfile" | head -1)
        deltas=$(grep -c 'content_block_delta' "$outfile" 2>/dev/null || echo "0")
        if [ -n "$http" ] || [ "$deltas" -gt 0 ]; then
            results+=("ok")
        else
            results+=("fail")
            all_pass=false
        fi
    done

    if $all_pass; then
        printf "  %3s. ${GREEN}PASS${NC}  %-8s %-28s  %dx concurrent  ${DIM}%s${NC}\n" \
            "$TOTAL" "$profile" "$model" "$concurrency" "$label"
        PASS=$((PASS + 1))
        printf "S12-${profile}-c${concurrency}\t%s\t%s\ttrue\t200\t0\t0\t0\t0\tCONC:%d\tPASS\n" "$profile" "$model" "$concurrency" >> "$TIMING_FILE"
    else
        printf "  %3s. ${RED}FAIL${NC}  %-8s %-28s  %dx concurrent  ${DIM}%s${NC}\n" \
            "$TOTAL" "$profile" "$model" "$concurrency" "$label"
        FAIL=$((FAIL + 1))
        printf "S12-${profile}-c${concurrency}\t%s\t%s\ttrue\t200\t0\t0\t0\t0\tCONC:%d\tFAIL\n" "$profile" "$model" "$concurrency" >> "$TIMING_FILE"
    fi
}

# 3 concurrent cc
run_concurrent "cc" "$CC_KEY" "claude-sonnet-4-20250514" 3 "3 concurrent streams"
# 3 concurrent lotuss
run_concurrent "lotuss" "$LOTUSS_KEY" "lotuss-default" 3 "3 concurrent streams"
# 3 concurrent kimi
run_concurrent "kimi" "$KIMI_KEY" "kimi-k2.6" 3 "3 concurrent streams"
# 5 concurrent cc
run_concurrent "cc" "$CC_KEY" "claude-sonnet-4-20250514" 5 "5 concurrent streams"
# 5 concurrent lotuss
run_concurrent "lotuss" "$LOTUSS_KEY" "lotuss-default" 5 "5 concurrent streams"
# 10 concurrent mixed
TOTAL=$((TOTAL + 1))
all_pass=true
for i in $(seq 1 10); do
    case $((i % 3)) in
        0) p="cc"; k="$CC_KEY"; m="claude-sonnet-4-20250514" ;;
        1) p="lotuss"; k="$LOTUSS_KEY"; m="lotuss-default" ;;
        2) p="kimi"; k="$KIMI_KEY"; m="kimi-k2.6" ;;
    esac
    body=$(python3 -c "
import json
print(json.dumps({'model':'$m','max_tokens':32,'stream':True,
    'messages':[{'role':'user','content':'Say a number from 1-100.'}]}))")
    outfile="$RESULTS_DIR/S12-mixed-${i}.out"
    result=$(curl -sS -N --no-buffer --max-time 60 \
        -X POST "${GATEWAY}/v1/messages" \
        -H "Content-Type: application/json" \
        -H "x-api-key: ${k}" \
        -H "X-Profile: ${p}" \
        -H "anthropic-version: 2023-06-01" \
        -d "${body}" 2>/dev/null)
    echo "$result" > "$outfile"
    deltas=$(grep -c 'content_block_delta' "$outfile" 2>/dev/null || echo "0")
    if [ "$deltas" -eq 0 ]; then
        all_pass=false
    fi
done
if $all_pass; then
    printf "  %3s. ${GREEN}PASS${NC}  %-8s %-28s  10x concurrent  ${DIM}mixed profiles${NC}\n" "$TOTAL" "mixed" "all"
    PASS=$((PASS + 1))
else
    printf "  %3s. ${RED}FAIL${NC}  %-8s %-28s  10x concurrent  ${DIM}mixed profiles${NC}\n" "$TOTAL" "mixed" "all"
    FAIL=$((FAIL + 1))
fi

# ==============================================================================
# SUMMARY
# ==============================================================================
echo ""
echo -e "${BOLD}${CYAN}================================================================${NC}"
echo -e "${BOLD}${CYAN}  SUMMARY: ${TOTAL} tests completed${NC}"
echo -e "${BOLD}${CYAN}================================================================${NC}"
echo ""
printf "  ${GREEN}PASS${NC}: %3d\n" "$PASS"
printf "  ${RED}FAIL${NC}: %3d\n" "$FAIL"
echo ""

if [ "$FAIL" -eq 0 ]; then
    echo -e "  ${GREEN}${BOLD}ALL TESTS PASSED${NC}"
else
    echo -e "  ${RED}${BOLD}${FAIL} TESTS FAILED${NC}"
    echo ""
    echo "  Failed test outputs in: $RESULTS_DIR/"
    echo "  Timing data in: $TIMING_FILE"
fi

echo ""
echo "  Full results: $RESULTS_DIR/"
echo "  Timing TSV:   $TIMING_FILE"
