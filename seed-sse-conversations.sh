#!/usr/bin/env bash
# seed-sse-conversations.sh - ZEROTRUST SSE Conversation Seeder
# Usage: ./seed-sse-conversations.sh [--all|--all3]
set -euo pipefail

GATEWAY_URL="${GATEWAY_URL:-http://localhost:9000}"
API_KEY="${API_KEY:-test-key}"
SELECTED_MODEL="${MODEL:-claude-sonnet-4-6}"
SELECTED_PROFILE=""
SELECTED_PROMPT=""

# ── ZEROTRUST Theme ──
RST='\033[0m'
BLK='\033[30m'
# Core palette - green/blue
# Core palette - green/blue
NRD='\033[38;5;131m' # muted red
NMN='\033[38;5;39m' # bright blue
NCN='\033[38;5;73m' # teal cyan
NYL='\033[38;5;108m' # sage green
NGR='\033[38;5;114m' # soft green
NOR='\033[38;5;67m' # steel blue
NPK='\033[38;5;116m' # light cyan
NWH='\033[38;5;252m' # soft white
# Dim/dark variants
DRD='\033[38;5;88m' # dark red
DMN='\033[38;5;24m' # dark blue
DCN='\033[38;5;24m' # dark cyan
DGR='\033[38;5;22m' # dark green
DYL='\033[38;5;66m' # dark teal
# Styles
BRT='\033[1m'
DIM='\033[2m'
ITC='\033[3m'           # italic
BLK2='\033[38;5;238m'   # dark gray for borders
BG_BL='\033[48;5;16m'   # black bg
BG_DK='\033[48;5;233m'  # dark gray bg
# Shortcuts
CY="${NMN}"              # primary blue (primary accent)
CB="${NCN}"              # teal accent (secondary accent)
CW="${NWH}"              # text
CX="${NYL}"              # highlights (highlights)
CG="${NGR}"              # success (success)
CR="${NRD}"              # errors (errors)
CO="${NOR}"              # warnings (warnings)
CD="${BLK2}"             # borders (borders)
CP="${NPK}"              # details (details)


# ── Border helpers ──
L50_DASH() { python3 -c "print('-'*50,end='')"; }
L50_EQ()   { python3 -c "print('='*50,end='')"; }

# ── Counters ──
TOTAL_REQ=0; TOTAL_OK=0; TOTAL_FAIL=0
TOTAL_INPUT_TOKENS=0; TOTAL_OUTPUT_TOKENS=0
TOTAL_EVENTS=0; TOTAL_BYTES=0

# ── Glitch text effect ──
glitch_print() {
  local text="$1"
  for ((i=0; i<${#text}; i++)); do
    local ch="${text:$i:1}"
    local r=$((RANDOM % 20))
    if [[ $r -eq 0 ]]; then
      printf "${CR}%s${RST}" "$(echo "$ch" | tr '[:alnum:]' '1337@#$&!%' | head -c1)"
    else
      printf "%s" "$ch"
    fi
  done
}

# ── Scanline effect ──
scanline() {
  local width="${1:-60}"
  printf "${DIM}${DCN}"
  for ((i=0; i<width; i++)); do
    local r=$((RANDOM % 10))
    if [[ $r -eq 0 ]]; then
      printf "%s" "$(printf '\xe2\x96\x81')"  # lower one eighth block
    else
      printf "%s" "$(printf '\xe2\x94\x80')"  # horizontal dash
    fi
  done
  printf "${RST}\n"
}

# ── API helpers ──
api_get() {
  curl -sf "${GATEWAY_URL}$1" \
    -H "x-api-key: ${API_KEY}" \
    --max-time 5 2>/dev/null || echo '{}'
}

# ── Load profiles from gateway ──
PROFILES=()
PROFILE_TARGETS=()
load_profiles() {
  local resp
  resp=$(api_get "/v1/profiles")
  local count
  count=$(echo "$resp" | jq '.profiles | length' 2>/dev/null || echo "0")
  if [[ "$count" == "0" || "$count" == "null" ]]; then
    PROFILES=("<no profile>")
    PROFILE_TARGETS=("-")
    return
  fi
  PROFILES=()
  PROFILE_TARGETS=()
  for i in $(seq 0 $((count - 1))); do
    local name target
    name=$(echo "$resp" | jq -r ".profiles[$i].name" 2>/dev/null || echo "unknown")
    target=$(echo "$resp" | jq -r ".profiles[$i].target // .profiles[$i].provider // \"-\"" 2>/dev/null || echo "-")
    PROFILES+=("$name")
    PROFILE_TARGETS+=("$target")
  done
}

# ── Load models from gateway ──
MODELS=()
MODEL_SERIES=()
load_models() {
  local resp
  resp=$(api_get "/v1/models")
  local count
  count=$(echo "$resp" | jq '[.models // [] | .[]] | length' 2>/dev/null || echo "0")
  if [[ "$count" == "0" || "$count" == "null" ]]; then
  MODELS=("claude-sonnet-4-6" "claude-haiku-4-5-20251001" "claude-opus-4-7" "gemini-2.5-flash" "gemini-2.5-pro")
  MODEL_SERIES=("sonnet" "haiku" "opus" "2.5" "2.5")
    return
  fi
  MODELS=()
  MODEL_SERIES=()
  for i in $(seq 0 $((count - 1))); do
    local name series
    name=$(echo "$resp" | jq -r ".models[$i].name" 2>/dev/null || continue)
    series=$(echo "$resp" | jq -r ".models[$i].series // \"?\"" 2>/dev/null || echo "?")
    MODELS+=("$name")
    MODEL_SERIES+=("$series")
  done
}

# ── Spinner ──
SPINNER_PID=""
SPIN_FRAMES=('|' '/' '-' '\')
start_spinner() {
  local msg="${1:-STREAMING}"
  tput civis 2>/dev/null || true
  ( local i=0
    local frames=('*' '+' 'x' '#' '@' '%' '&' '$')
    while true; do
      local ch="${frames[$((i % ${#frames[@]}))]}"
      local glitch_bar=""
      for ((j=0; j<20; j++)); do
        if [[ $((RANDOM % 5)) -eq 0 ]]; then
          glitch_bar+="${CR}${ch}${RST}"
        else
          glitch_bar+="${CB}${ch}${RST}"
        fi
      done
      printf "\r  ${CY}[${RST}${glitch_bar}${CY}]${RST} ${CB}${msg}${RST} ${DIM}%0.s${RST}" $(seq 1 $((i % 4))) >&2
      sleep 0.06
      ((i++))
    done
  ) & disown 2>/dev/null
  SPINNER_PID=$!
}
stop_spinner() {
  [[ -n "$SPINNER_PID" ]] && kill "$SPINNER_PID" 2>/dev/null || true
  SPINNER_PID=""
  tput cnorm 2>/dev/null || true
  printf "\r%*s\r" 80 "" >&2
}

# ── ZEROTRUST banner ──
show_banner() {
  clear
  printf "\n"
  scanline 60
 printf "${NMN}${BRT}%s${RST}\n" \
 ' ███╗   ███╗ ███████╗ ██████╗  ██╗    ██╗'
 printf "${CB}${BRT}%s${RST}\n" \
 ' ████╗ ████║ ██╔════╝ ██╔══██╗ ██║    ██║'
 printf "${NCN}${BRT}%s${RST}\n" \
 ' ██╔████╔██║ ██████╗  ██║  ██║ ██║ █╗ ██║'
 printf "${NGR}${BRT}%s${RST}\n" \
 ' ██║╚██╔╝██║ ██╔═══╝  ██║  ██║ ██║███╗██║'
 printf "${NPK}${BRT}%s${RST}\n" \
 ' ██║ ╚═╝ ██║ ███████╗ ██████╔╝ ╚███╔███╔╝'
 printf "${NMN}${BRT}%s${RST}\n" \
 ' ╚═╝     ╚═╝ ╚══════╝ ╚═════╝   ╚══╝╚══╝ '
  printf "\n"
  printf "  ${CD}::${RST} ${CB}GATEWAY${RST}  ${CW}${GATEWAY_URL}${RST}\n"
  printf "  ${CD}::${RST} ${CY}PROFILE${RST}  ${CP}${SELECTED_PROFILE:-<none>}${RST}\n"
  printf "  ${CD}::${RST} ${CO}MODEL${RST}    ${CX}${SELECTED_MODEL}${RST}\n"
  printf "  ${CD}::${RST} ${CG}PROMPT${RST}   ${CW}${SELECTED_PROMPT:-<none>}${RST}\n"
  printf "  ${CD}::${RST} ${DIM}BOOT${RST}    ${CW}$(date '+%Y-%m-%d %H:%M:%S')${RST}\n"
  scanline 60
}

# ── SSE sender with live event parsing ──
send_sse() {
  local label="$1" body="$2" desc="$3"
  local payload_size
  payload_size=$(echo "$body" | wc -c | tr -d ' ')

  printf "\n"
  printf "  ${CD}+==================================================+${RST}\n"
  printf "  ${CD}|${RST} ${CG}>> ${CY}${BRT}${label}${RST}\n"
  printf "  ${CD}|${RST} ${DIM}${desc}${RST}\n"
  printf "  ${CD}+==================================================+${RST}\n"
  printf "  ${CB}>>${RST} POST /v1/messages ${DIM}model=${CX}${SELECTED_MODEL}${DIM} ${payload_size}B${RST}\n"
  if [[ -n "$SELECTED_PROFILE" && "$SELECTED_PROFILE" != "<no profile>" ]]; then
    printf "  ${CY}>>${RST} X-Profile: ${CP}${SELECTED_PROFILE}${RST}\n"
 fi
 # Show input prompt
 local user_msg
 user_msg=$(echo "$body" | jq -r '.messages[-1].content // .messages[-1].content[0].text // "?"' 2>/dev/null | head -3)
 printf " ${DIM}>> prompt:${RST} ${CW}${user_msg:0:80}${RST}\n"

  local tmpfile
  tmpfile=$(mktemp /tmp/sse-XXXXXX)
  local start_time
  start_time=$(python3 -c 'import time; print(int(time.time()*1000))')

  local curl_args=(-s -w '\n%{http_code}'
    -X POST "${GATEWAY_URL}/v1/messages"
    -H "Content-Type: application/json"
    -H "anthropic-version: 2023-06-01"
    -d "$body"
    --max-time 120
    -o "$tmpfile")

  if [[ -n "$SELECTED_PROFILE" && "$SELECTED_PROFILE" != "<no profile>" ]]; then
    curl_args+=(-H "X-Profile: ${SELECTED_PROFILE}")
  else
   curl_args+=(-H "x-api-key: ${API_KEY}")
  fi

  start_spinner "STREAMING"
  local http_code
  http_code=$(curl "${curl_args[@]}" 2>/dev/null || echo "000")
  stop_spinner

  local end_time
  end_time=$(python3 -c 'import time; print(int(time.time()*1000))')
  local elapsed_ms=$(( end_time - start_time ))
  local elapsed_sec
  elapsed_sec=$(python3 -c "print(f'{$elapsed_ms/1000:.2f}')")

  # Parse SSE events
  local input_tokens=0 output_tokens=0 event_count=0 msg_id="-" model_used="-"
  local stop_reason="-"

  if [[ -f "$tmpfile" ]]; then
    event_count=$(grep -c "^event:" "$tmpfile" 2>/dev/null || true)
 event_count=${event_count:-0}
    TOTAL_EVENTS=$((TOTAL_EVENTS + event_count))

    local msg_start
    msg_start=$(grep -A1 "^event: message_start" "$tmpfile" 2>/dev/null | grep "^data:" | head -1 || true)
    if [[ -n "$msg_start" ]]; then
      input_tokens=$(echo "$msg_start" | grep -o '"input_tokens":[0-9]*' | grep -o '[0-9]*' || echo "0")
      model_used=$(echo "$msg_start" | grep -o '"model":"[^"]*"' | head -1 | cut -d'"' -f4 || echo "-")
      msg_id=$(echo "$msg_start" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4 || echo "-")
    fi

    local msg_delta
    msg_delta=$(grep -A1 "^event: message_delta" "$tmpfile" 2>/dev/null | grep "^data:" | head -1 || true)
    if [[ -n "$msg_delta" ]]; then
      output_tokens=$(echo "$msg_delta" | grep -o '"output_tokens":[0-9]*' | grep -o '[0-9]*' || echo "0")
      stop_reason=$(echo "$msg_delta" | grep -o '"stop_reason":"[^"]*"' | head -1 | cut -d'"' -f4 || echo "-")
    fi

    TOTAL_BYTES=$((TOTAL_BYTES + $(wc -c < "$tmpfile" 2>/dev/null || echo 0)))
  fi

  TOTAL_REQ=$((TOTAL_REQ + 1))
  TOTAL_INPUT_TOKENS=$((TOTAL_INPUT_TOKENS + input_tokens))
  TOTAL_OUTPUT_TOKENS=$((TOTAL_OUTPUT_TOKENS + output_tokens))

  printf "\n"
  if [[ "${http_code##*$'\n'}" == "200" ]]; then
    TOTAL_OK=$((TOTAL_OK + 1))
    printf "  ${CG}${BRT}OK${RST} ${CD}|${RST} HTTP ${CG}200${RST} ${CD}|${RST} ${CX}${elapsed_sec}s${RST} ${CD}|${RST} ${CB}%d${RST} events\n" "$event_count"
  else
    TOTAL_FAIL=$((TOTAL_FAIL + 1))
    printf "  ${CR}${BRT}FAIL${RST} ${CD}|${RST} HTTP ${CR}%s${RST} ${CD}|${RST} ${CX}${elapsed_sec}s${RST}\n" "${http_code##*$'\n'}"
  fi

  local tw=50
  printf "  ${CD}+${DCN}%s${CD}+${RST}\n" "$(L50_DASH)"
  printf "  ${CD}|${RST} ${DIM}METRIC${RST}%*s${DIM}VALUE${RST}%*s${CD}|${RST}\n" 14 "" 15 ""
  printf "  ${CD}+${DCN}%s${CD}+${RST}\n" "$(L50_DASH)"
 local msg_short="${msg_id:0:22}"
 printf " ${CD}|${RST} ${CB}msg_id${RST}%*s${CW}%s${RST}%*s${CD}|${RST}\n" 15 "" "${msg_short}" $((16 - ${#msg_short})) ""
  printf "  ${CD}|${RST} ${CB}model${RST}%*s${CX}%s${RST}%*s${CD}|${RST}\n" 15 "" "${model_used}" $((16 - ${#model_used})) ""
  printf "  ${CD}|${RST} ${CB}stop${RST}%*s${CP}%s${RST}%*s${CD}|${RST}\n" 16 "" "${stop_reason}" $((16 - ${#stop_reason})) ""
  printf "  ${CD}|${RST} ${CB}input_tk${RST}%*s${CG}%s${RST}%*s${CD}|${RST}\n" 12 "" "${input_tokens}" $((16 - ${#input_tokens})) ""
  printf "  ${CD}|${RST} ${CB}output_tk${RST}%*s${CG}%s${RST}%*s${CD}|${RST}\n" 11 "" "${output_tokens}" $((16 - ${#output_tokens})) ""
  printf "  ${CD}|${RST} ${CB}latency${RST}%*s${CO}%sms${RST}%*s${CD}|${RST}\n" 13 "" "${elapsed_ms}" $((14 - ${#elapsed_ms})) ""
  printf "  ${CD}+${DCN}%s${CD}+${RST}\n" "$(L50_DASH)"

  if [[ -f "$tmpfile" ]]; then
    local sample
    sample=$(grep -A1 "^event: content_block_delta" "$tmpfile" 2>/dev/null | grep "^data:" | head -3 | grep -o '"text":"[^"]*"' | sed 's/"text":"//;s/"$//' | head -c 80 || true)
    if [[ -n "$sample" ]]; then
      printf "  ${CD}\\_${RST} ${DIM}${sample}...${RST}\n"
    fi
  fi

  rm -f "$tmpfile"
  printf "\n"
}

# ── Prompt body builders ──
build_privacy() {
  cat <<JSONEOF
{"model":"${SELECTED_MODEL}","stream":true,"max_tokens":256,"messages":[{"role":"user","content":"Review this config for security:\nDB_URL=[[CONNECTION_STRING_8]] [[EMAIL_ADDRESS_7]]\nPhone: +66-81-234-5678\nSSN: 123-45-6789\nVAULT_TOKEN=s.abc123def456\nAWS_KEY=AKIAIOSFODNN7EXAMPLE\nSlack: https://hooks.slack.com/services/T00/B00/xx"}]}
JSONEOF
}

build_optimizer_input() {
  local filler=""
  for i in $(seq 1 20); do
    filler+="You are a helpful assistant that provides detailed and comprehensive responses. "
    filler+="Please make sure to always be thorough in your explanations. "
    filler+="It is important to note that accuracy is paramount. "
    filler+="In this context, we should consider all possible implications. "
    filler+="Furthermore, additional analysis may be required. "
  done
  local sys="${filler}You are a DevOps expert specializing in Kubernetes, Terraform, and cloud infrastructure."
  local sys_json
  sys_json=$(echo "$sys" | jq -Rs .)
  cat <<JSONEOF
{"model":"${SELECTED_MODEL}","stream":true,"max_tokens":512,"system":${sys_json},"messages":[{"role":"user","content":"Explain K8s autoscaling best practices. Please make sure to always be thorough."},{"role":"assistant","content":"Here is a comprehensive guide. It is important to note that there are several components."},{"role":"user","content":"Also cover Helm best practices. Please make sure to always be thorough."}]}
JSONEOF
}

build_delta_sketch() {
  cat <<JSONEOF
{"model":"${SELECTED_MODEL}","stream":true,"max_tokens":512,"system":"You are an SRE specializing in incident response, monitoring, and on-call.","messages":[{"role":"user","content":"API returning 500 at 5%. PostgreSQL pool exhausted. CPU 45%, memory 60%."},{"role":"assistant","content":"1. Check pg max_connections\n2. Verify pool settings\n3. Find slow queries\n4. Temp limit increase"},{"role":"user","content":"max_connections=100, 97 active. 3 queries taking 30s+."},{"role":"assistant","content":"Kill slow queries. Reduce max_open to 60, add 10s timeout."},{"role":"user","content":"Done. Connections at 45. How to prevent recurrence?"}]}
JSONEOF
}

build_output_trim() {
  cat <<JSONEOF
{"model":"${SELECTED_MODEL}","stream":true,"max_tokens":1024,"messages":[{"role":"user","content":"What is the capital of France?"}]}
JSONEOF
}

build_combined() {
  local sys=""
  for i in $(seq 1 10); do
    sys+="You are a security audit assistant. Provide thorough analysis. Accuracy is paramount. "
  done
  sys+="You are a security auditor specializing in compliance."
  local sys_json
  sys_json=$(echo "$sys" | jq -Rs .)
  cat <<JSONEOF
{"model":"${SELECTED_MODEL}","stream":true,"max_tokens":512,"system":${sys_json},"messages":[{"role":"user","content":"Audit these credentials:\nDB: [[CONNECTION_STRING_9]] [[EMAIL_ADDRESS_8]] / +66-89-123-4567\nVAULT_TOKEN=s.xyz789abc\nSlack: https://hooks.slack.com/services/T00/B00/yy"}]}
JSONEOF
}

build_tool_compress() {
  cat <<JSONEOF
{"model":"${SELECTED_MODEL}","stream":true,"max_tokens":512,"tools":[{"name":"bash","description":"Execute shell commands","input_schema":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}},{"name":"read_file","description":"Read file contents","input_schema":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}},{"name":"search","description":"Search patterns","input_schema":{"type":"object","properties":{"pattern":{"type":"string"},"path":{"type":"string"}},"required":["pattern"]}}],"messages":[{"role":"user","content":"Search for TODO comments in Go files"},{"role":"assistant","content":[{"type":"tool_use","id":"toolu_01","name":"search","input":{"pattern":"TODO|FIXME|HACK","path":"/app"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01","content":"main.go:45:// TODO: graceful shutdown\nhandler.go:123:// FIXME: context cancel\nproxy.go:200:// HACK: retry 429"}]}]}
JSONEOF
}

build_vision() {
  local img="iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg=="
  cat <<JSONEOF
{"model":"${SELECTED_MODEL}","stream":true,"max_tokens":256,"messages":[{"role":"user","content":[{"type":"text","text":"Describe this image."},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"${img}"}}]}]}
JSONEOF
}

build_custom() {
  printf "\n  ${CY}>> ${CW}Enter your prompt (empty line to end):${RST}\n"
  local lines=() line
  while IFS= read -r line; do
    [[ -z "$line" ]] && break
    lines+=("$line")
  done
  local prompt
  prompt=$(printf '%s\n' "${lines[@]}")
  local prompt_json
  prompt_json=$(echo "$prompt" | jq -Rs .)
  cat <<JSONEOF
{"model":"${SELECTED_MODEL}","stream":true,"max_tokens":1024,"messages":[{"role":"user","content":${prompt_json}}]}
JSONEOF
}

# ── Prompt registry ──
PROMPT_NAMES=()
PROMPT_DESCS=()
PROMPT_METRICS=()
PROMPT_BUILD=()
PROMPT_GLYPHS=()

reg() {
  PROMPT_NAMES+=("$1")
  PROMPT_DESCS+=("$2")
  PROMPT_METRICS+=("$3")
  PROMPT_BUILD+=("$4")
  PROMPT_GLYPHS+=("${5:-}")
}

reg "Privacy Guard"    "PII + Secret masking"              "mask_requests, pii_detected, secrets_detected"  "build_privacy"        "${CR}"
reg "Optimizer Input"  "Large repetitive system prompt"    "optimizer_runs, chars_saved (chunker, dedup)"    "build_optimizer_input" "${CO}"
reg "Delta/Sketch"     "Multi-turn conversation dedup"     "delta, sketch_dedup, packer, warmstart"          "build_delta_sketch"    "${CB}"
reg "Output Trim"      "Verbose response trimming"         "output optimizer, token_output"                  "build_output_trim"     "${CG}"
reg "Privacy+Optimizer" "Combined pipeline"                  "privacy + optimizer metrics"                     "build_combined"        "${CY}"
reg "Tool Compress"    "Tool use + toolcomp"               "optimizer_runs (toolcomp)"                       "build_tool_compress"   "${CX}"
reg "Vision"           "Image analysis"                    "image_compressions, vision_preanalysis"          "build_vision"          "${CP}"
reg "Custom"           "Write your own prompt"             "whatever the gateway processes"                  "build_custom"          "${NMN}"

fire_prompt() {
  local idx="$1"
  local body
  body=$("${PROMPT_BUILD[$idx]}")
  send_sse "${PROMPT_NAMES[$idx]}" "$body" "${PROMPT_METRICS[$idx]}"
}

run_all() {
  local round="${1:-1}"
  if [[ "$round" -gt 1 ]]; then
    printf "\n  ${CY}${BRT}>> ROUND %d${RST}\n" "$round"
  fi
  for i in $(seq 0 $(( ${#PROMPT_NAMES[@]} - 2 )) ); do
    fire_prompt "$i"
  done
}

# ── Session summary ──
show_summary() {
  local runtime=$(( $(date +%s) - SESSION_START ))
  printf "\n"
  scanline 52
  printf "  ${CD}+${NMN}%s${CD}+${RST}\n" "$(L50_EQ)"
  printf "  ${CD}|${RST} ${CG}${BRT}>> SESSION SUMMARY${RST}%*s${CD}|${RST}\n" 29 ""
  printf "  ${CD}+${NMN}%s${CD}+${RST}\n" "$(L50_EQ)"
  printf "  ${CD}|${RST} ${CB}Requests${RST}%*s${CW}%d${RST} ${CD}(${CG}%d ok${CD} / ${CR}%d fail${CD})${RST}%*s${CD}|${RST}\n" 10 "" "$TOTAL_REQ" "$TOTAL_OK" "$TOTAL_FAIL" $((6 - ${#TOTAL_FAIL})) ""
  printf "  ${CD}|${RST} ${CB}Input tk${RST}%*s${CG}%s${RST}%*s${CD}|${RST}\n" 8 "" "${TOTAL_INPUT_TOKENS}" $((16 - ${#TOTAL_INPUT_TOKENS})) ""
  printf "  ${CD}|${RST} ${CB}Output tk${RST}%*s${CG}%s${RST}%*s${CD}|${RST}\n" 7 "" "${TOTAL_OUTPUT_TOKENS}" $((16 - ${#TOTAL_OUTPUT_TOKENS})) ""
  printf "  ${CD}|${RST} ${CB}SSE events${RST}%*s${CX}%d${RST}%*s${CD}|${RST}\n" 6 "" "$TOTAL_EVENTS" $((16 - ${#TOTAL_EVENTS})) ""
  printf "  ${CD}|${RST} ${CB}Runtime${RST}%*s${CW}%ds${RST}%*s${CD}|${RST}\n" 11 "" "$runtime" $((15 - ${#runtime})) ""
  printf "  ${CD}|${RST} ${CB}Data in${RST}%*s${CW}%s${RST}%*s${CD}|${RST}\n" 9 "" "$(numfmt --to=iec "$TOTAL_BYTES" 2>/dev/null || echo "${TOTAL_BYTES}B")" $((15 - ${#TOTAL_BYTES})) ""
  printf "  ${CD}+${NMN}%s${CD}+${RST}\n" "$(L50_EQ)"
  scanline 52
  printf "\n"
}

# ── Select profile ──
select_profile() {
  printf "\n"
  scanline 52
  printf "  ${CD}+${DCN}%s${CD}+${RST}\n" "$(L50_DASH)"
  printf "  ${CD}|${RST} ${CY}${BRT}>> SELECT PROFILE${RST}%*s${CD}|${RST}\n" 30 ""
  printf "  ${CD}+${DCN}%s${CD}+${RST}\n" "$(L50_DASH)"

  for i in $(seq 0 $(( ${#PROFILES[@]} - 1 ))); do
    local marker=" "
    [[ "${PROFILES[$i]}" == "$SELECTED_PROFILE" ]] && marker="*"
    printf "  ${CD}|${RST} ${CG}%2d${RST} ${marker} ${CW}%-24s${RST} ${DIM}%s${RST}%*s${CD}|${RST}\n" \
      "$((i+1))" "${PROFILES[$i]}" "${PROFILE_TARGETS[$i]}" $((10 - ${#PROFILE_TARGETS[$i]})) ""
  done

  printf "  ${CD}|${RST} ${CR} 0${RST}   ${DIM}Clear (no profile)${RST}%*s${CD}|${RST}\n" 24 ""
  printf "  ${CD}+${DCN}%s${CD}+${RST}\n" "$(L50_DASH)"

  read -rp $'  \033[38;5;39m\342\235\266\033[0m ' pchoice
  if [[ "$pchoice" =~ ^[0-9]+$ ]] && [[ "$pchoice" -ge 1 ]] && [[ "$pchoice" -le "${#PROFILES[@]}" ]]; then
    SELECTED_PROFILE="${PROFILES[$((pchoice-1))]}"
    printf "  ${CG}>> Profile: ${CW}${SELECTED_PROFILE}${RST}\n"
  elif [[ "$pchoice" == "0" ]]; then
    SELECTED_PROFILE=""
    printf "  ${CO}>> Profile cleared${RST}\n"
  fi
}

# ── Select model ──
select_model() {
  printf "\n"
  scanline 52
  printf "  ${CD}+${DCN}%s${CD}+${RST}\n" "$(L50_DASH)"
  printf "  ${CD}|${RST} ${CB}${BRT}>> SELECT MODEL${RST}%*s${CD}|${RST}\n" 32 ""
  printf "  ${CD}+${DCN}%s${CD}+${RST}\n" "$(L50_DASH)"

  local seen_series=()
  for i in $(seq 0 $(( ${#MODELS[@]} - 1 ))); do
    local series="${MODEL_SERIES[$i]}"
    if [[ ! " ${seen_series[*]} " =~ " ${series} " ]]; then
      if [[ ${#seen_series[@]} -gt 0 ]]; then
        printf "  ${CD}|${RST}%*s${CD}|${RST}\n" 50 ""
      fi
      printf "  ${CD}|${RST} ${CY}${BRT}%s${RST}%*s${CD}|${RST}\n" "${series^^}" $((50 - ${#series} - 3)) ""
      seen_series+=("$series")
    fi
    local marker=" "
    if [[ "${MODELS[$i]}" == "$SELECTED_MODEL" ]]; then
      marker="${CG}*${RST}"
    fi
    printf "  ${CD}|${RST} ${CG}%2d${RST} ${marker} ${CW}%-32s${RST}${CD}|${RST}\n" "$((i+1))" "${MODELS[$i]}"
  done

  printf "  ${CD}+${DCN}%s${CD}+${RST}\n" "$(L50_DASH)"
  printf "  ${DIM} * = current${RST}\n"

  read -rp $'  \033[38;5;116m\342\235\266\033[0m ' mchoice
  if [[ "$mchoice" =~ ^[0-9]+$ ]] && [[ "$mchoice" -ge 1 ]] && [[ "$mchoice" -le "${#MODELS[@]}" ]]; then
    SELECTED_MODEL="${MODELS[$((mchoice-1))]}"
    printf "  ${CG}>> Model: ${CX}${SELECTED_MODEL}${RST}\n"
  fi
}

# ── Select prompt ──
select_prompt() {
  printf "\n"
  scanline 52
  printf "  ${CD}+${DCN}%s${CD}+${RST}\n" "$(L50_DASH)"
  printf "  ${CD}|${RST} ${CX}${BRT}>> SELECT PROMPT${RST}%*s${CD}|${RST}\n" 30 ""
  printf "  ${CD}+${DCN}%s${CD}+${RST}\n" "$(L50_DASH)"

  for i in $(seq 0 $(( ${#PROMPT_NAMES[@]} - 1 ))); do
    local glyph="${PROMPT_GLYPHS[$i]}"
    printf "  ${CD}|${RST} ${glyph}%2d${RST} ${CW}%-18s${RST} ${DIM}%s${RST}%*s${CD}|${RST}\n" \
      "$((i+1))" "${PROMPT_NAMES[$i]}" "${PROMPT_DESCS[$i]}" $((18 - ${#PROMPT_DESCS[$i]})) ""
  done

  printf "  ${CD}|${RST}%*s${CD}|${RST}\n" 50 ""
  printf "  ${CD}|${RST} ${CY}${BRT} A${RST} ${CW}Run ALL prompts${RST}%*s${CD}|${RST}\n" 29 ""
  printf "  ${CD}|${RST} ${CY}${BRT}A3${RST} ${CW}Run ALL x3 (stress)${RST}%*s${CD}|${RST}\n" 24 ""
  printf "  ${CD}+${DCN}%s${CD}+${RST}\n" "$(L50_DASH)"

  read -rp $'  \033[38;5;108m\342\235\266\033[0m ' pchoice
  case "$pchoice" in
    [1-9])
      if [[ "$pchoice" -le "${#PROMPT_NAMES[@]}" ]]; then
        SELECTED_PROMPT="${PROMPT_NAMES[$((pchoice-1))]}"
        fire_prompt "$((pchoice-1))"
      fi
      ;;
    A|a)
      SELECTED_PROMPT="ALL"
      run_all
      ;;
    A3|a3)
      SELECTED_PROMPT="ALL x3"
      for i in 1 2 3; do run_all "$i"; done
      ;;
  esac
}

# ── Main menu ──
show_main_menu() {
  printf "\n"
  printf "  ${CD}+${NMN}%s${CD}+${RST}\n" "$(L50_EQ)"
  printf "  ${CD}|${RST} ${CY}${BRT}>> COMMAND${RST}%*s${CD}|${RST}\n" 36 ""
  printf "  ${CD}+${NMN}%s${CD}+${RST}\n" "$(L50_EQ)"
  printf "  ${CD}|${RST} ${CG}P${RST} ${CW}Profile${RST}%*s${CD}|${RST}\n" $((50 - 13)) ""
  printf "  ${CD}|${RST} ${CG}M${RST} ${CW}Model${RST}%*s${CD}|${RST}\n" $((50 - 11)) ""
  printf "  ${CD}|${RST} ${CX}F${RST} ${CW}Full flow${RST}%*s${CD}|${RST}\n" $((50 - 14)) ""
  printf "  ${CD}|${RST} ${CO}1-8${RST} ${CW}Fire prompt${RST}%*s${CD}|${RST}\n" $((50 - 17)) ""
  printf "  ${CD}|${RST} ${CB}A${RST} ${CW}Run ALL${RST}%*s${CD}|${RST}\n" $((50 - 12)) ""
  printf "  ${CD}|${RST} ${CP}S${RST} ${CW}Summary${RST}%*s${CD}|${RST}\n" $((50 - 13)) ""
  printf "  ${CD}|${RST} ${CW}R${RST} ${CW}Refresh${RST}%*s${CD}|${RST}\n" $((50 - 13)) ""
  printf "  ${CD}|${RST} ${CR}Q${RST} ${CW}Quit${RST}%*s${CD}|${RST}\n" $((50 - 10)) ""
  printf "  ${CD}+${NMN}%s${CD}+${RST}\n" "$(L50_EQ)"
  printf "  ${CD}|${RST} ${CY}Profile:${RST} ${CP}${SELECTED_PROFILE:-<none>}${RST}  ${CB}Model:${RST} ${CX}${SELECTED_MODEL}${RST}\n"
 # Prompt legend
 local i
 for i in $(seq 0 $(( ${#PROMPT_NAMES[@]} - 1 ))); do
   printf " ${DIM}${CD}|${RST} ${PROMPT_GLYPHS[$i]:-}${CW}%d${RST} ${DIM}%-24s${RST} ${CD}|${RST}\n" "$((i+1))" "${PROMPT_DESCS[$i]}"
 done
  scanline 52
}

# ── Init ──
SESSION_START=$(date +%s)

main() {
  if ! command -v jq &>/dev/null; then
    printf "${CR}jq is required${RST}\n" >&2; exit 1
  fi

  printf "  ${CB}>>${RST} ${DIM}Loading profiles...${RST}"
  load_profiles
  printf " ${CG}%d${RST}\n" "${#PROFILES[@]}"

  printf "  ${CB}>>${RST} ${DIM}Loading models...${RST}"
  load_models
  printf " ${CG}%d${RST}\n" "${#MODELS[@]}"

  show_banner

  if [[ "${1:-}" == "--all" ]]; then
	if [[ -z "$SELECTED_PROFILE" ]] && [[ "${#PROFILES[@]}" -gt 0 ]] && [[ "${PROFILES[0]}" != "<no profile>" ]]; then
		SELECTED_PROFILE="${PROFILES[0]}"
		printf " ${CG}>> Auto-profile: ${CW}${SELECTED_PROFILE}${RST}\n"
	fi
    run_all; show_summary; exit 0
  fi
  if [[ "${1:-}" == "--all3" ]]; then
	if [[ -z "$SELECTED_PROFILE" ]] && [[ "${#PROFILES[@]}" -gt 0 ]] && [[ "${PROFILES[0]}" != "<no profile>" ]]; then
		SELECTED_PROFILE="${PROFILES[0]}"
		printf " ${CG}>> Auto-profile: ${CW}${SELECTED_PROFILE}${RST}\n"
	fi
    for i in 1 2 3; do run_all "$i"; done
    show_summary; exit 0
  fi

  while true; do
    show_main_menu
    read -rp $'  \033[38;5;39m\342\235\266\033[0m ' cmd
    case "$cmd" in
      [pP]) select_profile ;;
      [mM]) select_model ;;
      [fF]) select_profile; select_model; select_prompt ;;
      [sS]) show_summary ;;
      [rR])
        printf "  ${CB}>>${RST} ${DIM}Refreshing...${RST}"
        load_profiles; load_models
        printf " ${CG}done${RST}\n"
        ;;
      [qQ]|[eE]|0)
        show_summary
        printf "  ${DIM}>> Connection terminated.${RST}\n\n"
        exit 0
        ;;
      [aA])
SELECTED_PROMPT="ALL"; if [[ -z "$SELECTED_PROFILE" ]] && [[ "${#PROFILES[@]}" -gt 0 ]] && [[ "${PROFILES[0]}" != "<no profile>" ]]; then SELECTED_PROFILE="${PROFILES[0]}"; printf " ${CG}>> Auto-profile: ${CW}${SELECTED_PROFILE}${RST}\n"; fi; run_all
        ;;
      [1-8])
        local idx=$((cmd - 1))
        if [[ "$idx" -lt "${#PROMPT_NAMES[@]}" ]]; then
          SELECTED_PROMPT="${PROMPT_NAMES[$idx]}"
          fire_prompt "$idx"
        fi
        ;;
      *) printf "  ${CR}>> Unknown: ${cmd}${RST}\n" ;;
    esac
  done
}

main "$@"
