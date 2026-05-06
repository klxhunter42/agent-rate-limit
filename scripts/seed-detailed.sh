#!/usr/bin/env bash
# seed-detailed.sh - Granular Privacy + Optimizer SSE Seeder
# Tests each rule/technique individually, proves savings are real
# Usage: ./scripts/seed-detailed.sh [--privacy|--optimizer|--all]
set -euo pipefail

GATEWAY_URL="${GATEWAY_URL:-http://localhost:9000}"
API_KEY="${API_KEY:-test-key}"
SELECTED_MODEL="${MODEL:-claude-sonnet-4-6}"
SELECTED_PROFILE=""

# ── ZEROTRUST Theme ──
RST='\033[0m'
NRD='\033[38;5;131m'; NMN='\033[38;5;39m'; NCN='\033[38;5;73m'
NYL='\033[38;5;108m'; NGR='\033[38;5;114m'; NOR='\033[38;5;67m'
NPK='\033[38;5;116m'; NWH='\033[38;5;252m'; BLK2='\033[38;5;238m'
BRT='\033[1m'; DIM='\033[2m'
CY="${NMN}"; CB="${NCN}"; CW="${NWH}"; CX="${NYL}"
CG="${NGR}"; CR="${NRD}"; CO="${NOR}"; CD="${BLK2}"; CP="${NPK}"

# ── Counters ──
TOTAL_REQ=0; TOTAL_OK=0; TOTAL_FAIL=0
TOTAL_INPUT_TK=0; TOTAL_OUTPUT_TK=0; TOTAL_CHARS_SAVED=0

# ── Spinner ──
SPINNER_PID=""
start_spinner() {
  tput civis 2>/dev/null || true
  ( local i=0; local frames=('*' '+' 'x' '#' '@')
    while true; do
      local ch="${frames[$((i % ${#frames[@]}))]}"
      local bar=""
      for ((j=0; j<15; j++)); do
        [[ $((RANDOM % 5)) -eq 0 ]] && bar+="${CR}${ch}${RST}" || bar+="${CB}${ch}${RST}"
      done
      printf "\r ${CY}[${RST}${bar}${CY}]${RST} ${CB}${1:-STREAMING}${RST} ${DIM}%0.s${RST}" $(seq 1 $((i % 4))) >&2
      sleep 0.06; ((i++))
    done
  ) &
  SPINNER_PID=$!
}
stop_spinner() {
  [[ -n "$SPINNER_PID" ]] && kill "$SPINNER_PID" 2>/dev/null || true
  wait "$SPINNER_PID" 2>/dev/null || true
  SPINNER_PID=""
  tput cnorm 2>/dev/null || true
  printf "\r%*s\r" 80 "" >&2
}

# ── API helpers ──
api_get() {
  curl -sf "${GATEWAY_URL}$1" -H "x-api-key: ${API_KEY}" --max-time 5 2>/dev/null || echo '{}'
}

# ── Load profiles ──
PROFILES=()
load_profiles() {
  local resp
  resp=$(api_get "/v1/profiles")
  local count
  count=$(echo "$resp" | jq '.profiles | length' 2>/dev/null || echo "0")
  if [[ "$count" == "0" || "$count" == "null" ]]; then
    PROFILES=("<no profile>"); return
  fi
  PROFILES=()
  for i in $(seq 0 $((count - 1))); do
    PROFILES+=("$(echo "$resp" | jq -r ".profiles[$i].name" 2>/dev/null || echo "unknown")")
  done
}

# ── SSE sender ──
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
  if [[ -n "${SELECTED_PROFILE:-}" ]] && [[ "${SELECTED_PROFILE:-}" != "<no profile>" ]]; then
    printf "  ${CY}>>${RST} X-Profile: ${CP}${SELECTED_PROFILE}${RST}\n"
  fi

  local user_msg
  user_msg=$(echo "$body" | jq -r '.messages[-1].content // .messages[-1].content[0].text // "?"' 2>/dev/null | head -3)
  local sys_msg
  sys_msg=$(echo "$body" | jq -r '.system // empty' 2>/dev/null | head -3)

  local tmpfile
  tmpfile=$(mktemp /tmp/sse-detailed-XXXXXX)
  local start_time
  start_time=$(python3 -c 'import time; print(int(time.time()*1000))')

  local curl_args=(-s -w '%{http_code}'
    -X POST "${GATEWAY_URL}/v1/messages"
    -H "Content-Type: application/json"
    -H "anthropic-version: 2023-06-01"
    -d "$body"
    --max-time 120
    -o "$tmpfile")

  if [[ -n "${SELECTED_PROFILE:-}" ]] && [[ "${SELECTED_PROFILE:-}" != "<no profile>" ]]; then
    curl_args+=(-H "X-Profile: ${SELECTED_PROFILE}")
  else
    curl_args+=(-H "x-api-key: ${API_KEY}")
  fi

  start_spinner "STREAMING"
  local http_code
  http_code=$(curl "${curl_args[@]}" 2>/dev/null || echo "000")
  stop_spinner

  # Show INPUT after spinner is dead (stderr like spinner to avoid cursor races)
  printf "  ${CY}${BRT}>> INPUT:${RST}\n" >&2
  if [[ -n "$sys_msg" ]]; then
    printf "  ${DIM}   system:${RST} ${CD}${sys_msg:0:120}${RST}\n" >&2
  fi
  printf "  ${DIM}   user:${RST} ${CW}${user_msg:0:150}${RST}\n" >&2

  local end_time
  end_time=$(python3 -c 'import time; print(int(time.time()*1000))')
  local elapsed_ms=$(( end_time - start_time ))
  local elapsed_sec
  elapsed_sec=$(python3 -c "print(f'{$elapsed_ms/1000:.2f}')")

  local input_tokens=0 output_tokens=0 event_count=0 msg_id="-" model_used="-" stop_reason="-"
  local first_text=""

  if [[ -f "$tmpfile" ]]; then
    event_count=$(grep -c "^event:" "$tmpfile" 2>/dev/null || true)
    event_count=${event_count:-0}

    local msg_start
    msg_start=$(grep -A1 "^event: message_start" "$tmpfile" 2>/dev/null | grep "^data:" | head -1 | sed 's/^data: //' || true)
    if [[ -n "$msg_start" ]]; then
      local id_val
      id_val=$(echo "$msg_start" | jq -r '.message.id // "-"' 2>/dev/null || echo "-")
      local msg_short="${id_val:0:22}"
      msg_id="${msg_short}"
      model_used=$(echo "$msg_start" | jq -r '.message.model // "-"' 2>/dev/null || echo "-")
      input_tokens=$(echo "$msg_start" | jq -r '.message.usage.input_tokens // 0' 2>/dev/null || echo "0")
    fi

    local msg_delta
    msg_delta=$(grep -A1 "^event: message_delta" "$tmpfile" 2>/dev/null | grep "^data:" | head -1 | sed 's/^data: //' || true)
    if [[ -n "$msg_delta" ]]; then
      output_tokens=$(echo "$msg_delta" | jq -r '.usage.output_tokens // 0' 2>/dev/null || echo "0")
      stop_reason=$(echo "$msg_delta" | jq -r '.delta.stop_reason // "-"' 2>/dev/null || echo "-")
    fi

    first_text=$(grep "^data:" "$tmpfile" 2>/dev/null | sed 's/^data: //' | jq -r 'select(.delta != null) .delta.text // empty' 2>/dev/null || true)
    if [[ -z "$first_text" ]]; then
      first_text=$(grep "^data:" "$tmpfile" 2>/dev/null | sed 's/^data: //' | jq -r 'select(.type == "content_block_delta") .delta.text // empty' 2>/dev/null || true)
    fi
  fi

  TOTAL_REQ=$((TOTAL_REQ + 1))
  TOTAL_INPUT_TK=$((TOTAL_INPUT_TK + input_tokens))
  TOTAL_OUTPUT_TK=$((TOTAL_OUTPUT_TK + output_tokens))

  if [[ "$http_code" == "200" ]]; then
    TOTAL_OK=$((TOTAL_OK + 1))
    printf "  ${CG}OK${RST} ${CD}|${RST} HTTP ${CG}200${RST} ${CD}|${RST} ${CX}${elapsed_sec}s${RST} ${CD}|${RST} ${CB}${event_count}${RST} events\n"
  else
    TOTAL_FAIL=$((TOTAL_FAIL + 1))
    printf "  ${CR}FAIL${RST} ${CD}|${RST} HTTP ${CR}${http_code}${RST} ${CD}|${RST} ${CX}${elapsed_sec}s${RST}\n"
    if [[ -f "$tmpfile" ]]; then
      local err_body
      err_body=$(head -c 200 "$tmpfile" 2>/dev/null || true)
      printf "  ${DIM}>> ${err_body}${RST}\n"
    fi
  fi

  printf "  ${CD}+${CD}--------------------------------------------------${CD}+${RST}\n"
  printf "  ${CD}|${RST} ${CB}msg_id${RST}     ${CW}${msg_id}${RST}\n"
  printf "  ${CD}|${RST} ${CB}model${RST}      ${CX}${model_used}${RST}\n"
  printf "  ${CD}|${RST} ${CB}stop${RST}       ${CP}${stop_reason}${RST}\n"
  printf "  ${CD}|${RST} ${CB}input_tk${RST}   ${CG}${input_tokens}${RST}  ${CB}output_tk${RST}  ${CG}${output_tokens}${RST}\n"
  printf "  ${CD}|${RST} ${CB}latency${RST}    ${CO}${elapsed_ms}ms${RST}\n"
  if [[ -n "$first_text" ]]; then
    printf "  ${CG}${BRT}>> OUTPUT:${RST}\n"
    while IFS= read -r line; do
      printf "  ${DIM}   %s${RST}\n" "$line"
    done <<< "$first_text"
  fi
  printf "  ${CD}+${CD}--------------------------------------------------${CD}+${RST}\n"

  rm -f "$tmpfile"
  printf "\n"
  return 0
}

# ══════════════════════════════════════════════════════════════
# PRIVACY SUB-TESTS
# ══════════════════════════════════════════════════════════════

privacy_email() {
  send_sse "Privacy: EMAIL" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Contact our team:\nCEO: thanapat@lotus.com\nCTO: somchai.dev@gmail.com\nHR: hr-team@cpaxtra.co.th\nSupport: helpdesk@cpsall.co.th\nAlso CC: admin@kmitl.ac.th and no-reply@aws.amazon.com\nPlease send the quarterly report to all of them."}]
    }')" \
    "EMAIL_ADDRESS detection - 6 emails across domains"
}

privacy_phone() {
  send_sse "Privacy: PHONE" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Call these numbers:\nOffice: +66-2-123-4567\nMobile: +1-415-555-0199\nDirect: +44-20-7946-0958\nThai mobile: 081-234-5678\nUS toll free: 1-800-555-0123\nEmergency: +66-81-987-6543"}]
    }')" \
    "PHONE_NUMBER detection - international + Thai formats"
}

privacy_credit_card() {
  send_sse "Privacy: CREDIT_CARD" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Process these payments:\nVisa: 4111-1111-1111-1111\nMastercard: 5500-0000-0000-0004\nAmex: 3400-0000-0000-009\nDiscover: 6011-0000-0000-0004\nAmount: $1,250.00 total"}]
    }')" \
    "CREDIT_CARD detection - Visa, MC, Amex, Discover"
}

privacy_ssn() {
  send_sse "Privacy: SSN" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Employee verification:\nSSN: 123-45-6789\nSSN2: 987-65-4321\nSSN3: 456-78-9012\nITIN: 900-70-1234\nPlease verify all records match."}]
    }')" \
    "SSN detection - US Social Security Numbers"
}

privacy_iban() {
  send_sse "Privacy: IBAN" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Wire transfers needed:\nTH: TH96-0000-0000-0000-0000-0001\nDE: DE89-3704-0044-0532-0130-00\nGB: GB29-NWBK-6016-1331-9268-19\nFR: FR76-3000-6000-0112-3456-7890-189"}]
    }')" \
    "IBAN detection - Thai, German, UK, French"
}

privacy_ip() {
  send_sse "Privacy: IP_ADDRESS" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Server inventory:\nWeb-01: 10.0.1.15\nDB-master: 10.0.2.100\nRedis: 10.0.3.50\nPublic LB: 52.74.128.33\nCDN: 151.101.1.69\nMonitor: 10.0.10.5"}]
    }')" \
    "IP_ADDRESS detection - private + public IPv4"
}

privacy_thai_id() {
  send_sse "Privacy: THAI_NATIONAL_ID" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Thai employee registration:\nEmp001: 1-1001-00001-23-4\nEmp002: 3-2010-54321-67-8\nEmp003: 5-3011-99999-00-1\nEmp004: 8-4012-11111-22-3\nPlease verify all IDs are valid."}]
    }')" \
    "THAI_NATIONAL_ID detection - 13-digit Thai citizen IDs"
}

privacy_thai_phone() {
  send_sse "Privacy: THAI_PHONE" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Contact list (Thai numbers):\nAIS: 081-234-5678\nDTAC: 089-876-5432\nTrue: 06-3456-7890\nOffice: 02-123-4567\nWhatsApp: +66-81-111-2222"}]
    }')" \
    "THAI_PHONE detection - Thai mobile + landline"
}

# ── Secret sub-tests ──

secret_aws_key() {
  send_sse "Secret: AWS_KEY" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Deploy config:\nAWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\nAWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY\nRegion: ap-southeast-1\nBucket: production-assets"}]
    }')" \
    "API_KEY_AWS detection - AKIA prefixed access keys"
}

secret_github() {
  send_sse "Secret: GITHUB" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"CI/CD tokens:\nGITHUB_TOKEN=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij\nGITHUB_OAUTH=gho_1234567890abcdefghijklmnopqrstuv\nActions: ghs_webhooktoken1234567890abcdefghijklmn\nPlease rotate all tokens."}]
    }')" \
    "API_KEY_GITHUB detection - ghp/ghu/ghs prefixed tokens"
}

secret_slack() {
  send_sse "Secret: SLACK" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Slack integration:\nBot token: xoxb-1234567890-1234567890123-abcdefghijklmnopqrstuvwxyz\nApp token: xapp-1-A0001-1234567890-abcdef1234567890abcdef1234567890abcdef\nWebhook: https://hooks.slack.com/services/T00/B00/XXYYZZaabbccdd"}]
    }')" \
    "API_KEY_SLACK detection - xoxb/xoxp/xapp tokens + webhooks"
}

secret_vault() {
  send_sse "Secret: VAULT_TOKEN" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Vault access:\nRoot token: hvs.abcdefghijklmnopqrstuvwxyz123456\nReviewer token: hvs.xyz789abc456def123ghi789jkl012mno345\nNamespace: admin/production\nSecret path: secret/data/db-credentials"}]
    }')" \
    "VAULT_TOKEN detection - hvs. prefixed HashiCorp tokens"
}

secret_connection_string() {
  send_sse "Secret: CONN_STRING" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Database connections:\nPostgreSQL: postgres://admin:s3cret@db.prod.internal:5432/app_db?sslmode=require\nMySQL: mysql://root:password123@10.0.1.50:3306/users\nRedis: redis://:myredispwd@cache.internal:6379/0\nMongoDB: mongodb://appUser:appPass@cluster0.internal:27017/logs"}]
    }')" \
    "CONNECTION_STRING detection - postgres/mysql/redis/mongodb URIs"
}

secret_jwt_bearer() {
  send_sse "Secret: JWT+BEARER" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Auth headers:\nAuthorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c\nAPI call with: Bearer abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"}]
    }')" \
    "JWT_TOKEN + BEARER_TOKEN detection"
}

secret_stripe_sendgrid() {
  send_sse "Secret: STRIPE+SENDGRID" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Payment integration:\nStripe live key: sk_live_abcdefghijklmnopqrstuvwxyz1234567890\nStripe restricted key: rk_live_1234567890abcdefghijklmnopqrstuvwxyz\nSendGrid: SG.abc123def456.ghi789jkl012mno345pqr678stu901vwx234\nEmail API enabled for production."}]
    }')" \
    "API_KEY_STRIPE + API_KEY_SENDGRID detection"
}

secret_gcp_tencent() {
  send_sse "Secret: GCP+TENCENT" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Cloud credentials:\nGCP API key: AIzaSyA1234567890abcdefghijklmnopqrstuv\nTencent SecretId: AKID1234567890abcdefghijklmn\nTencent SecretKey: abcdefghijklmnopqrstuvwxyz12\nRegion: ap-bangkok"}]
    }')" \
    "API_KEY_GCP (AIza) + API_KEY_TENCENT (AKID) detection"
}

secret_pem_key() {
  send_sse "Secret: PEM KEY" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Server TLS cert:\n-----BEGIN PRIVATE KEY-----\nMIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQC7VJTUt9Us8cKj\nMpu1YGI0jJqSXwJzBsOJBgqXnPZQFPQPbGlXIPePkOfJjXWoQdJBlBHBqJ3PWJBm\n-----END PRIVATE KEY-----\nAlso the RSA key:\n-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA0Z3VS5JJcds3xfn/yGaTKM3ZSSfnPqbCN0F17JqGnRCB4f3D\n-----END RSA PRIVATE KEY-----"}]
    }')" \
    "PEM_PRIVATE_KEY detection - PRIVATE KEY + RSA PRIVATE KEY blocks"
}

secret_openssh_key() {
  send_sse "Secret: OPENSSH KEY" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"SSH access key:\n-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW\nQyNTkxLWtleQAAACDEuJB4xIDyAh0QLa4bH5fqCLn3XD2yPmPTV+JDtMDXmAAAAJCkgMGMp\n-----END OPENSSH PRIVATE KEY-----\nDeploy to all bastion hosts."}]
    }')" \
    "OPENSSH_PRIVATE_KEY detection - OpenSSH ed25519 key block"
}

secret_env_password() {
  send_sse "Secret: ENV PASSWORD" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Environment variables:\nDATABASE_PASSWORD=MyS3cretP@ssword123\nREDIS_PASSWD=redis_auth_token_xyz\nADMIN_PASS=admin_super_secret_2024\nAPP_SECRET_KEY=sk_live_abc123def456ghi789\nDB_USER=root\nSERVICE_USERNAME=svc_account\nLOGIN_ID=admin@internal"}]
    }')" \
    "ENV_PASSWORD + ENV_SECRET + ENV_USER + ENV_TOKEN detection"
}

secret_gitlab() {
  send_sse "Secret: GITLAB" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"GitLab CI tokens:\nPersonal: glpat-abcdefghijklmnopqrstuvwxyz123456\nDeploy: gldt-1234567890abcdefghijklmnopqrstuv\nCI Job: glcbt-abcdefghijklmnopqrstuvwx1234567890\nPipeline trigger: glptt-zyxwvutsrqponmlkjihgfedcba0987654321\nAll need rotation."}]
    }')" \
    "API_KEY_GITLAB detection - glpat/gldt/glcbt/glptt tokens"
}

secret_basic_auth_url() {
  send_sse "Secret: BASIC_AUTH_URL" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Service endpoints:\nInternal API: https://admin:s3cret@api.internal.local:8080/v1/health\nMetrics: http://monitor:monitor123@prometheus.internal:9090/metrics\nRedis: redis://:mypassword@cache.internal:6379/0\nRabbitMQ: amqps://guest:guest123@mq.internal:5672/vhost"}]
    }')" \
    "BASIC_AUTH_URL detection - user:pass embedded in URLs"
}

secret_cli_auth() {
  send_sse "Secret: CLI AUTH FLAGS" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Deployment commands:\nmysql --password=RootP@ss123 --host=db.prod.internal\npsql --password=postgres_pwd -h pg.internal -U admin\ncurl -u admin:admin123 https://api.internal/health\ncurl --user svc:token_abc https://api.internal/v2/data\ncurl -xp http://proxy:proxy123@proxy.local:8080 https://external.com\naws --secret-key wJalrXUtnFEMI/K7MDENG/bPxRfiCY s3 ls"}]
    }')" \
    "CLI_AUTH + CURL_BASIC_AUTH detection - password flags and -u/-xp flags"
}

secret_alibaba() {
  send_sse "Secret: ALIBABA+SK" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Cloud provider keys:\nAlibaba AccessKey: LTAI5tREDACTED\nOpenAI API key: sk-proj-REDACTED\nDeepSeek: sk-deepseek-REDACTED\nThese are used in production services."}]
    }')" \
    "API_KEY_ALIBABA (LTAI) + API_KEY_SK (sk-*) detection"
}

secret_azure() {
  send_sse "Secret: AZURE CRED" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Azure service principal:\nAZURE_CLIENT_SECRET=abc123-def456-ghi789-jkl012-mno345pqr678\nAZURE_TENANT_ID=12345678-1234-1234-1234-123456789012\nAAD_CLIENT_ID=87654321-4321-4321-4321-210987654321\nSubscription: prod-sub-001"}]
    }')" \
    "AZURE_CREDENTIAL detection - client secret + tenant ID"
}

# ── Mixed privacy tests ──

privacy_mixed_pii() {
  send_sse "Privacy: MIXED PII" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 256,
      messages: [{role:"user",content:"HR report:\nName: Somchai (SSN: 123-45-6789)\nEmail: somchai@lotus.com\nPhone: +66-81-234-5678\nThai ID: 1-1001-00001-23-4\nIBAN: TH96-0000-0000-0000-0000-0001\nIP workstation: 10.0.1.50\nCC for expense: 4111-1111-1111-1111"}]
    }')" \
    "All PII types in single request - 7 entity types"
}

privacy_mixed_secrets() {
  send_sse "Privacy: MIXED SECRETS" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 256,
      messages: [{role:"user",content:"Production config dump:\nDATABASE_URL=postgres://admin:s3cret@db.internal:5432/app\nAWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\nVAULT_TOKEN=hvs.abcdefghijklmnopqrstuvwxyz123456\nSLACK_TOKEN=xoxb-1234567890-1234567890123-abcdefghijklmnopqrstuvwxyz\nSTRIPE_KEY=sk_live_abcdefghijklmnopqrstuvwxyz1234567890\nSend to: devops@lotus.com"}]
    }')" \
    "All secret types in single request - 6 secret types"
}

privacy_mixed_everything() {
  send_sse "Privacy: EVERYTHING" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 512,
      messages: [{role:"user",content:"Incident report - DO NOT SHARE:\nAffected user: somchai@lotus.com / +66-81-234-5678\nSSN on file: 123-45-6789, Thai ID: 1-1001-00001-23-4\nDB breached: postgres://admin:P@ssw0rd@10.0.1.50:5432/users\nAWS key exposed: AKIAIOSFODNN7EXAMPLE\nVault token leaked: hvs.abcdefghijklmnopqrstuvwxyz123456\nSlack webhook: https://hooks.slack.com/services/T00/B00/XXYYZZ\nExpense card: 4111-1111-1111-1111\nServer IP: 52.74.128.33 / IBAN: TH96-0000-0000-0000-0000-0001\nJWT found: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.abc123\nGitHub PAT: ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij\nGCP key: AIzaSyA1234567890abcdefghijklmnopqrstuv"}]
    }')" \
    "ALL PII + ALL secrets combined - stress test"
}

privacy_dedup() {
  send_sse "Privacy: DEDUP" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 128,
      messages: [{role:"user",content:"Contact somchai@lotus.com for details.\nAlso email somchai@lotus.com for the report.\nCC somchai@lotus.com on all replies.\nForward to somchai@lotus.com when done.\nSame email appears 4 times - should get same placeholder."}]
    }')" \
    "Same value repeated 4x - should produce identical placeholder"
}

# ══════════════════════════════════════════════════════════════
# OPTIMIZER SUB-TESTS
# ══════════════════════════════════════════════════════════════

# Each test sends two requests: CONTROL (no optimization trigger) and TEST (optimized).
# Compares input_tokens to prove savings.

optim_textcomp() {
  local verbose="I would like to kindly request that you please, if you do not mind, provide a detailed and comprehensive explanation of Kubernetes pod lifecycle. It is important to note that at the end of the day, basically, the fundamental aspects are crucial. In this particular case, sort of, due to the fact that, as a matter of fact, we need to understand for all intents and purposes the underlying mechanics."
  local sys_json
  sys_json=$(echo "$verbose" | jq -Rs .)
  send_sse "Optim: TextComp" \
    "$(jq -nc --arg m "$SELECTED_MODEL" --argjson s "$sys_json" '{
      model: $m, stream: true, max_tokens: 256,
      system: $s,
      messages: [{role:"user",content:"Explain pod lifecycle phases."}]
    }')" \
    "TextComp strips filler/hedge/verbose phrases from system prompt"
}

optim_textcomp_clean() {
  local clean="Explain Kubernetes pod lifecycle phases and transitions."
  local sys_json
  sys_json=$(echo "$clean" | jq -Rs .)
  send_sse "Optim: TextComp CTRL" \
    "$(jq -nc --arg m "$SELECTED_MODEL" --argjson s "$sys_json" '{
      model: $m, stream: true, max_tokens: 256,
      system: $s,
      messages: [{role:"user",content:"Explain pod lifecycle phases."}]
    }')" \
    "CONTROL: same prompt without verbose filler - compare input_tokens"
}

optim_semantic_dedup() {
  local sys=""
  sys+="Kubernetes is a container orchestration platform that manages workloads. "
  sys+="K8s orchestrates containers and manages their workloads across clusters. "
  sys+="Pods are the smallest deployable units in Kubernetes. "
  sys+="The smallest deployable compute unit in K8s is called a pod. "
  sys+="Services provide stable networking endpoints for pods. "
  sys+="A Kubernetes Service exposes an application running in pods as a network service. "
  sys+="ConfigMaps store configuration data as key-value pairs. "
  sys+="Configuration data in K8s can be managed using ConfigMap resources."
  local sys_json
  sys_json=$(echo "$sys" | jq -Rs .)
  send_sse "Optim: SemDedup" \
    "$(jq -nc --arg m "$SELECTED_MODEL" --argjson s "$sys_json" '{
      model: $m, stream: true, max_tokens: 256,
      system: $s,
      messages: [{role:"user",content:"Summarize the key K8s concepts."}]
    }')" \
    "Semantic dedup removes near-duplicate sentences (Jaccard >= 0.7)"
}

optim_whitespace() {
  local sys="You are a helpful    assistant.    \n\n\n\n\n   Please help the user.    \n\n\n\n\n   Be thorough.    "
  local sys_json
  sys_json=$(echo "$sys" | jq -Rs .)
  send_sse "Optim: Whitespace" \
    "$(jq -nc --arg m "$SELECTED_MODEL" --argjson s "$sys_json" '{
      model: $m, stream: true, max_tokens: 128,
      system: $s,
      messages: [{role:"user",content:"Hello"}]
    }')" \
    "Whitespace optimization collapses multiple spaces/blank lines"
}

optim_caveman() {
  local sys=""
  sys+="Hello! I hope you are doing well today. "
  sys+="Thank you so much for your question. I would be delighted to help you with that. "
  sys+="Please feel free to ask me anything, I am here to assist you. "
  sys+="Let me take a moment to provide you with a comprehensive answer. "
  sys+="You are a DevOps engineer. Provide technical answers."
  local sys_json
  sys_json=$(echo "$sys" | jq -Rs .)
  send_sse "Optim: Caveman" \
    "$(jq -nc --arg m "$SELECTED_MODEL" --argjson s "$sys_json" '{
      model: $m, stream: true, max_tokens: 256,
      system: $s,
      messages: [{role:"user",content:"How to set up kubectl port-forward?"}]
    }')" \
    "Caveman strips pleasantries + injects terse output style"
}

optim_chunker() {
  local block=""
  for i in $(seq 1 30); do
    block+="You are an expert assistant. Follow all instructions carefully. "
    block+="Always provide accurate and detailed responses. "
    block+="Be thorough in your analysis and explanations. "
  done
  block+="You are a Kubernetes expert. Help with cluster operations."
  local sys_json
  sys_json=$(echo "$block" | jq -Rs .)
  send_sse "Optim: Chunker" \
    "$(jq -nc --arg m "$SELECTED_MODEL" --argjson s "$sys_json" '{
      model: $m, stream: true, max_tokens: 256,
      system: $s,
      messages: [{role:"user",content:"List K8s troubleshooting commands."}]
    }')" \
    "Chunker (Rabin-Karp) identifies stable repeating blocks"
}

optim_delta() {
  local sys=""
  for i in $(seq 1 15); do
    sys+="You are a security monitoring assistant for production systems. "
    sys+="Analyze alerts and provide remediation guidance. "
  done
  sys+="Focus on SRE best practices and runbook procedures."
  local sys_json
  sys_json=$(echo "$sys" | jq -Rs .)
  send_sse "Optim: Delta Encode" \
    "$(jq -nc --arg m "$SELECTED_MODEL" --argjson s "$sys_json" '{
      model: $m, stream: true, max_tokens: 256,
      system: $s,
      messages: [{role:"user",content:"CPU spike at 95% on node-03. What to check?"}]
    }')" \
    "Delta encoding - LCS diff vs cached baseline (send 2x to see savings)"
}

optim_delta_hit() {
  local sys=""
  for i in $(seq 1 15); do
    sys+="You are a security monitoring assistant for production systems. "
    sys+="Analyze alerts and provide remediation guidance. "
  done
  sys+="Focus on SRE best practices and runbook procedures."
  local sys_json
  sys_json=$(echo "$sys" | jq -Rs .)
  send_sse "Optim: Delta HIT" \
    "$(jq -nc --arg m "$SELECTED_MODEL" --argjson s "$sys_json" '{
      model: $m, stream: true, max_tokens: 256,
      system: $s,
      messages: [{role:"user",content:"Memory OOM kill on pod api-gateway-7d4f8. Investigate."}]
    }')" \
    "2nd request with same system prompt - delta should save chars"
}

optim_sketch() {
  local sys=""
  for i in $(seq 1 20); do
    sys+="You are a helpful assistant that provides detailed and comprehensive responses. "
    sys+="Please make sure to always be thorough in your explanations. "
    sys+="It is important to note that accuracy is paramount. "
  done
  sys+="You are a cloud architect."
  local sys_json
  sys_json=$(echo "$sys" | jq -Rs .)
  send_sse "Optim: Sketch" \
    "$(jq -nc --arg m "$SELECTED_MODEL" --argjson s "$sys_json" '{
      model: $m, stream: true, max_tokens: 256,
      system: $s,
      messages: [{role:"user",content:"Design a multi-AZ VPC architecture."}]
    }')" \
    "Sketch dedup - FNV-1a Hamming similarity check on system prompt"
}

optim_toolcomp() {
  send_sse "Optim: ToolComp" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 256,
      tools: [
        {name:"bash",description:"Execute shell commands",input_schema:{type:"object",properties:{command:{type:"string"}},required:["command"]}},
        {name:"read_file",description:"Read file contents",input_schema:{type:"object",properties:{path:{type:"string"}},required:["path"]}},
        {name:"search",description:"Search patterns",input_schema:{type:"object",properties:{pattern:{type:"string"},path:{type:"string"}},required:["pattern"]}}
      ],
      messages: [
        {role:"user",content:"Search for all Go source files in the project"},
        {role:"assistant",content:[{type:"tool_use",id:"toolu_01",name:"search",input:{pattern:"*.go",path:"/app"}}]},
        {role:"user",content:[{type:"tool_result",tool_use_id:"toolu_01",content:"api-gateway/main.go\napi-gateway/handler/handler.go\napi-gateway/handler/handler_test.go\napi-gateway/proxy/proxy.go\napi-gateway/proxy/shared_transport.go\napi-gateway/metrics/metrics.go\napi-gateway/privacy/pii/detect.go\napi-gateway/privacy/secrets/patterns.go\napi-gateway/privacy/masking/context.go\napi-gateway/privacy/pipeline.go\napi-gateway/optimizer/optimizers.go\napi-gateway/tokenizer/optimizer.go\napi-gateway/tokenizer/similarity.go\napi-gateway/chunker/chunker.go\napi-gateway/delta/delta.go\napi-gateway/sketch/sketch.go\napi-gateway/summarizer/summarizer.go\napi-gateway/filter/filter.go\napi-gateway/textcomp/textcomp.go\napi-gateway/caveman/caveman.go\napi-gateway/toolcomp/toolcomp.go\napi-gateway/disclosure/disclosure.go\napi-gateway/packer/packer.go\napi-gateway/warmstart/warmstart.go\napi-gateway/bandit/bandit.go\napi-gateway/prefetcher/prefetcher.go\napi-gateway/waste/waste.go\napi-gateway/compcache/compcache.go\ninternal/config/config.go\ncmd/server/main.go"}]}
      ]
    }')" \
    "ToolComp compresses verbose tool_result file listings"
}

optim_toolcomp_log() {
  send_sse "Optim: ToolComp LOG" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 256,
      tools: [{name:"bash",description:"Execute commands",input_schema:{type:"object",properties:{command:{type:"string"}},required:["command"]}}],
      messages: [
        {role:"user",content:"Check nginx error logs"},
        {role:"assistant",content:[{type:"tool_use",id:"toolu_02",name:"bash",input:{command:"tail -50 /var/log/nginx/error.log"}}]},
        {role:"user",content:[{type:"tool_result",tool_use_id:"toolu_02",content:"2026-05-07 01:23:45 [error] upstream timed out (110)\n2026-05-07 01:23:45 [error] upstream timed out (110)\n2026-05-07 01:23:46 [error] upstream timed out (110)\n2026-05-07 01:23:46 [warn] client sent invalid request\n2026-05-07 01:23:47 [error] upstream timed out (110)\n2026-05-07 01:23:47 [error] upstream timed out (110)\n2026-05-07 01:23:48 [error] connect() failed (111)\n2026-05-07 01:23:48 [error] connect() failed (111)\n2026-05-07 01:23:49 [error] upstream timed out (110)\n2026-05-07 01:23:49 [warn] client sent invalid request\n2026-05-07 01:23:50 [error] upstream timed out (110)\n2026-05-07 01:23:50 [error] upstream timed out (110)\n2026-05-07 01:23:51 [error] connect() failed (111)\n2026-05-07 01:23:51 [error] connect() failed (111)"}]}
      ]
    }')" \
    "ToolComp log dedup - strips timestamps, deduplicates consecutive lines"
}

optim_disclosure() {
  local sys=""
  for i in $(seq 1 50); do
    sys+="Section ${i}: This is a detailed documentation paragraph about infrastructure component ${i}. It contains important information about configuration, deployment, and monitoring of service-${i}. Please refer to this documentation when troubleshooting issues related to this service. "
  done
  local sys_json
  sys_json=$(echo "$sys" | jq -Rs .)
  send_sse "Optim: Disclosure" \
    "$(jq -nc --arg m "$SELECTED_MODEL" --argjson s "$sys_json" '{
      model: $m, stream: true, max_tokens: 256,
      system: $s,
      messages: [{role:"user",content:"Summarize the key sections."}]
    }')" \
    "Progressive disclosure - L1/L2/L3 layers based on budget"
}

optim_combined() {
  local sys=""
  for i in $(seq 1 20); do
    sys+="You are a helpful assistant.    Please be thorough.     "
    sys+="I would like to note that basically, at the end of the day, accuracy matters.  "
  done
  sys+="You are a security audit assistant. Contact somchai@lotus.com for findings. AWS key AKIAIOSFODNN7EXAMPLE found."
  local sys_json
  sys_json=$(echo "$sys" | jq -Rs .)
  send_sse "Optim: COMBINED" \
    "$(jq -nc --arg m "$SELECTED_MODEL" --argjson s "$sys_json" '{
      model: $m, stream: true, max_tokens: 256,
      system: $s,
      messages: [{role:"user",content:"Audit this config and email results to admin@lotus.com"}]
    }')" \
    "Combined: TextComp + SemDedup + Whitespace + Privacy masking"
}

optim_multi_turn_delta() {
  send_sse "Optim: MultiTurn Delta" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 256,
      system: "You are an SRE specializing in incident response, monitoring, and on-call runbooks.",
      messages: [
        {role:"user",content:"API latency P99 at 12s. Normal is 200ms."},
        {role:"assistant",content:"Check: 1) DB query time 2) Cache hit rate 3) Network latency 4) Recent deployments"},
        {role:"user",content:"DB queries normal. Cache hit rate dropped from 95% to 30%. No recent deploys."},
        {role:"assistant",content:"Cache miss storm. Check: 1) Cache TTL config 2) Key eviction policy 3) Memory pressure on Redis"},
        {role:"user",content:"Redis memory at 98%. Eviction policy is volatile-lru. TTL set to 5min."}
      ]
    }')" \
    "Multi-turn conversation delta - incremental context encoding"
}

optim_output_trim() {
  send_sse "Optim: Output Trim" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 1024,
      messages: [{role:"user",content:"Write a 500-word essay about cloud computing benefits for enterprise."}]
    }')" \
    "Output trim - caveman style injection constrains verbose output"
}

optim_summarizer() {
  local sys=""
  for i in $(seq 1 40); do
    sys+="Paragraph ${i}: Kubernetes production operations require careful attention to resource management, monitoring, alerting, and incident response procedures. Each component must be configured with appropriate resource requests and limits to prevent noisy neighbor problems. "
    sys+="Historical data shows that improper resource allocation accounts for approximately 60% of production incidents in containerized environments. "
  done
  local sys_json
  sys_json=$(echo "$sys" | jq -Rs .)
  send_sse "Optim: Summarizer" \
    "$(jq -nc --arg m "$SELECTED_MODEL" --argjson s "$sys_json" '{
      model: $m, stream: true, max_tokens: 256,
      system: $s,
      messages: [{role:"user",content:"Summarize K8s production best practices."}]
    }')" \
    "Summarizer (TextRank) extracts key sentences, keeps ~30% at budget >= 2"
}

optim_intent_filter() {
  send_sse "Optim: Intent Filter" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 512,
      messages: [{role:"user",content:"Write a Go function that sorts a slice of structs by a date field. Include error handling."}]
    }')" \
    "Intent filter classifies code intent, strips prose from code responses"
}

optim_warmstart() {
  local sys="You are a Kubernetes troubleshooting assistant. Help debug cluster issues with clear runbook steps."
  local sys_json
  sys_json=$(echo "$sys" | jq -Rs .)
  send_sse "Optim: WarmStart" \
    "$(jq -nc --arg m "$SELECTED_MODEL" --argjson s "$sys_json" '{
      model: $m, stream: true, max_tokens: 256,
      system: $s,
      messages: [{role:"user",content:"Pod CrashLoopBackOff on production namespace. Logs show OOMKilled."}]
    }')" \
    "WarmStart - cosine similarity on session features, reuses past optimizer state"
}

optim_bandit() {
  local sys="You are a code review assistant. Analyze pull requests for security vulnerabilities."
  local sys_json
  sys_json=$(echo "$sys" | jq -Rs .)
  send_sse "Optim: Bandit" \
    "$(jq -nc --arg m "$SELECTED_MODEL" --argjson s "$sys_json" '{
      model: $m, stream: true, max_tokens: 256,
      system: $s,
      messages: [{role:"user",content:"Review this code for SQL injection:\nquery := fmt.Sprintf(\"SELECT * FROM users WHERE id = %s\", userID)"}]
    }')" \
    "LinUCB bandit selects best optimization arm per context"
}

optim_prefetcher() {
  send_sse "Optim: Prefetcher" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 256,
      tools: [
        {name:"read_file",description:"Read file",input_schema:{type:"object",properties:{path:{type:"string"}},required:["path"]}},
        {name:"bash",description:"Run command",input_schema:{type:"object",properties:{command:{type:"string"}},required:["command"]}},
        {name:"search",description:"Search",input_schema:{type:"object",properties:{pattern:{type:"string"}},required:["pattern"]}},
        {name:"write_file",description:"Write file",input_schema:{type:"object",properties:{path:{type:"string"},content:{type:"string"}},required:["path","content"]}},
        {name:"edit",description:"Edit file",input_schema:{type:"object",properties:{path:{type:"string"},old:{type:"string"},new:{type:"string"}},required:["path","old","new"]}}
      ],
      messages: [
        {role:"user",content:"Find all TODO comments in the codebase"},
        {role:"assistant",content:[{type:"tool_use",id:"t1",name:"search",input:{pattern:"TODO"}}]},
        {role:"user",content:[{type:"tool_result",tool_use_id:"t1",content:"main.go:5: TODO: add graceful shutdown\nhandler.go:99: TODO: add timeout"}]},
        {role:"assistant",content:[{type:"tool_use",id:"t2",name:"read_file",input:{path:"main.go"}}]},
        {role:"user",content:[{type:"tool_result",tool_use_id:"t2",content:"package main\n// TODO: add graceful shutdown\nfunc main() { }"}]},
        {role:"assistant",content:"Found 2 TODOs. Let me fix main.go first."}
      ]
    }')" \
    "Prefetcher - Markov chain predicts next tool call from sequence"
}

optim_packer() {
  local sys=""
  for i in $(seq 1 20); do
    sys+="Document section ${i}: This covers infrastructure component ${i} including setup, monitoring, and troubleshooting. Refer to the runbook for detailed procedures. "
  done
  local sys_json
  sys_json=$(echo "$sys" | jq -Rs .)
  send_sse "Optim: Packer" \
    "$(jq -nc --arg m "$SELECTED_MODEL" --argjson s "$sys_json" '{
      model: $m, stream: true, max_tokens: 256,
      system: $s,
      messages: [{role:"user",content:"Which sections are most critical for production?"}]
    }')" \
    "Packer (0/1 knapsack) selects highest-utility items within token budget"
}

optim_toolcomp_diff() {
  send_sse "Optim: ToolComp DIFF" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 256,
      tools: [{name:"bash",description:"Execute",input_schema:{type:"object",properties:{command:{type:"string"}},required:["command"]}}],
      messages: [
        {role:"user",content:"Show me what changed in the config"},
        {role:"assistant",content:[{type:"tool_use",id:"t3",name:"bash",input:{command:"git diff HEAD~1 config.yaml"}}]},
        {role:"user",content:[{type:"tool_result",tool_use_id:"t3",content:"diff --git a/config.yaml b/config.yaml\nindex abc1234..def5678 100644\n--- a/config.yaml\n+++ b/config.yaml\n@@ -1,5 +1,5 @@\n-replicas: 3\n+replicas: 5\n-image: app:v1.2.0\n+image: app:v1.3.0\n-memory: 512Mi\n+memory: 1024Mi\n-cpu: 500m\n+cpu: 1000m\n # unchanged comment\n another unchanged line\n yet another unchanged line\n fourth unchanged context line\n fifth unchanged line"}]}
      ]
    }')" \
    "ToolComp diff format - keeps header/hunk/changes, omits unchanged context"
}

optim_waste_detector() {
  send_sse "Optim: Waste Detect" \
    "$(jq -nc --arg m "$SELECTED_MODEL" '{
      model: $m, stream: true, max_tokens: 16,
      messages: [{role:"user",content:"What is 2+2?"}]
    }')" \
    "Waste detector - empty_response/low_value detectors track tiny outputs"
}

optim_tool_filter() {
  local tools=""
  for name in bash read_file write_file edit search glob grep sed curl docker kubectl helm terraform ansible vault jq yq python3 git; do
    [[ -n "$tools" ]] && tools+=","
    tools+="{\"name\":\"${name}\",\"description\":\"Execute ${name}\",\"input_schema\":{\"type\":\"object\",\"properties\":{\"arg\":{\"type\":\"string\"}},\"required\":[\"arg\"]}}"
  done
  send_sse "Optim: ToolFilter" \
    "$(jq -nc --arg m "$SELECTED_MODEL" --argjson t "[$tools]" '{
      model: $m, stream: true, max_tokens: 128,
      tools: $t,
      messages: [{role:"user",content:"Search for API endpoint definitions in Go files"}]
    }')" \
    "ToolFilter - narrows 20 tools to top 15 by intent match"
}

# ══════════════════════════════════════════════════════════════
# REGISTRIES
# ══════════════════════════════════════════════════════════════

# Privacy tests
PRIV_NAMES=(); PRIV_FUNCS=(); PRIV_DESCS=()
preg() { PRIV_NAMES+=("$1"); PRIV_FUNCS+=("$2"); PRIV_DESCS+=("$3"); }

preg "P01 EMAIL"       "privacy_email"            "EMAIL_ADDRESS - 6 emails, various domains"
preg "P02 PHONE"       "privacy_phone"            "PHONE_NUMBER - international + Thai formats"
preg "P03 CREDIT_CARD" "privacy_credit_card"      "CREDIT_CARD - Visa, MC, Amex, Discover"
preg "P04 SSN"         "privacy_ssn"              "SSN - US Social Security Numbers"
preg "P05 IBAN"        "privacy_iban"             "IBAN - Thai, German, UK, French"
preg "P06 IP_ADDR"     "privacy_ip"               "IP_ADDRESS - private + public IPv4"
preg "P07 THAI_ID"     "privacy_thai_id"          "THAI_NATIONAL_ID - 13-digit citizen IDs"
preg "P08 THAI_PHONE"  "privacy_thai_phone"       "THAI_PHONE - Thai mobile + landline"
preg "P09 AWS_KEY"     "secret_aws_key"           "API_KEY_AWS - AKIA prefixed keys"
preg "P10 GITHUB"      "secret_github"            "API_KEY_GITHUB - ghp/ghu/ghs tokens"
preg "P11 SLACK"       "secret_slack"             "API_KEY_SLACK - xoxb/xapp + webhooks"
preg "P12 VAULT"       "secret_vault"             "VAULT_TOKEN - hvs. prefixed tokens"
preg "P13 CONN_STR"    "secret_connection_string" "CONNECTION_STRING - postgres/mysql/redis/mongodb"
preg "P14 JWT+BEARER"  "secret_jwt_bearer"        "JWT_TOKEN + BEARER_TOKEN detection"
preg "P15 STRIPE+SG"   "secret_stripe_sendgrid"   "API_KEY_STRIPE + API_KEY_SENDGRID"
preg "P16 GCP+TENCENT" "secret_gcp_tencent"       "API_KEY_GCP + API_KEY_TENCENT"
preg "P17 PEM_KEY"     "secret_pem_key"           "PEM_PRIVATE_KEY - RSA + PKCS8 key blocks"
preg "P18 OPENSSH_KEY" "secret_openssh_key"       "OPENSSH_PRIVATE_KEY - ed25519 key blocks"
preg "P19 ENV_PASS"    "secret_env_password"      "ENV_PASSWORD + ENV_SECRET + ENV_USER"
preg "P20 GITLAB"      "secret_gitlab"            "API_KEY_GITLAB - glpat/gldt/glcbt/glptt"
preg "P21 BASIC_URL"   "secret_basic_auth_url"    "BASIC_AUTH_URL - user:pass in URLs"
preg "P22 CLI_AUTH"    "secret_cli_auth"          "CLI_AUTH + CURL_BASIC_AUTH - --password/-u flags"
preg "P23 ALIBABA+SK"  "secret_alibaba"           "API_KEY_ALIBABA (LTAI) + API_KEY_SK (sk-*)"
preg "P24 AZURE"       "secret_azure"             "AZURE_CREDENTIAL - client secret + tenant ID"
preg "P25 MIXED_PII"   "privacy_mixed_pii"        "ALL 7 PII types in single request"
preg "P26 MIXED_SEC"   "privacy_mixed_secrets"    "6 secret types in single request"
preg "P27 EVERYTHING"  "privacy_mixed_everything" "ALL PII + ALL secrets - stress test"
preg "P28 DEDUP"       "privacy_dedup"            "Same value 4x - same placeholder"

# Optimizer tests
OPTIM_NAMES=(); OPTIM_FUNCS=(); OPTIM_DESCS=()
oreg() { OPTIM_NAMES+=("$1"); OPTIM_FUNCS+=("$2"); OPTIM_DESCS+=("$3"); }

oreg "O01 TextComp"       "optim_textcomp"         "Strip filler/hedge/verbose phrases"
oreg "O02 TextComp CTRL"  "optim_textcomp_clean"   "CONTROL - compare input_tokens with O01"
oreg "O03 SemDedup"       "optim_semantic_dedup"   "Near-duplicate sentence removal (Jaccard 0.7)"
oreg "O04 Whitespace"     "optim_whitespace"       "Collapse multi-space/blank-lines"
oreg "O05 Caveman"        "optim_caveman"          "Strip pleasantries + terse output style"
oreg "O06 Chunker"        "optim_chunker"          "Rabin-Karp stable block reorder"
oreg "O07 Delta COLD"     "optim_delta"            "Delta encode - 1st request (cache miss)"
oreg "O08 Delta HIT"      "optim_delta_hit"        "Delta encode - 2nd request (cache hit!)"
oreg "O09 Sketch"         "optim_sketch"           "FNV-1a Hamming similarity check"
oreg "O10 ToolComp FILE"  "optim_toolcomp"         "Compress verbose file listing results"
oreg "O11 ToolComp LOG"   "optim_toolcomp_log"     "Log dedup - strip timestamps, dedup lines"
oreg "O12 ToolComp DIFF"  "optim_toolcomp_diff"    "Diff format - keep changes, drop context"
oreg "O13 Disclosure"     "optim_disclosure"       "Progressive L1/L2/L3 layers"
oreg "O14 Summarizer"     "optim_summarizer"       "TextRank extractive summarization"
oreg "O15 IntentFilter"   "optim_intent_filter"    "Code intent - strip prose from code responses"
oreg "O16 COMBINED"       "optim_combined"         "TextComp + SemDedup + Privacy all at once"
oreg "O17 MultiTurn"      "optim_multi_turn_delta" "5-turn conversation delta encoding"
oreg "O18 Output Trim"    "optim_output_trim"      "Caveman style constrains verbose output"
oreg "O19 WarmStart"      "optim_warmstart"        "Cosine similarity reuses past optimizer state"
oreg "O20 Bandit"         "optim_bandit"           "LinUCB selects best optimization arm"
oreg "O21 Prefetcher"     "optim_prefetcher"       "Markov chain predicts next tool call"
oreg "O22 Packer"         "optim_packer"           "0/1 knapsack selects high-utility items"
oreg "O23 WasteDetect"    "optim_waste_detector"   "Low-value/tiny-output waste detection"
oreg "O24 ToolFilter"     "optim_tool_filter"      "Narrows 20 tools to top 15 by intent"

# ══════════════════════════════════════════════════════════════
# RUN FUNCTIONS
# ══════════════════════════════════════════════════════════════

run_privacy_all() {
  printf "\n  ${CR}${BRT}>> PRIVACY SUITE: ${#PRIV_NAMES[@]} tests${RST}\n"
  for i in $(seq 0 $(( ${#PRIV_NAMES[@]} - 1 ))); do
    "${PRIV_FUNCS[$i]}"
  done
}

run_optimizer_all() {
  printf "\n  ${CG}${BRT}>> OPTIMIZER SUITE: ${#OPTIM_NAMES[@]} tests${RST}\n"
  for i in $(seq 0 $(( ${#OPTIM_NAMES[@]} - 1 ))); do
    "${OPTIM_FUNCS[$i]}"
  done
}

run_all_detailed() {
  run_privacy_all
  run_optimizer_all
}

# ══════════════════════════════════════════════════════════════
# MENUS
# ══════════════════════════════════════════════════════════════

show_summary() {
  printf "\n"
  printf "  ${CD}+==================================================+${RST}\n"
  printf "  ${CD}|${RST} ${CG}${BRT}>> SUMMARY${RST}%*s${CD}|${RST}\n" 37 ""
  printf "  ${CD}+==================================================+${RST}\n"
  printf "  ${CD}|${RST} ${CB}Requests${RST}    ${CW}%d${RST} ${CD}(${CG}%d ok${RST} / ${CR}%d fail${CD})${RST}\n" "$TOTAL_REQ" "$TOTAL_OK" "$TOTAL_FAIL"
  printf "  ${CD}|${RST} ${CB}Input tk${RST}   ${CG}%d${RST}\n" "$TOTAL_INPUT_TK"
  printf "  ${CD}|${RST} ${CB}Output tk${RST}  ${CG}%d${RST}\n" "$TOTAL_OUTPUT_TK"
  printf "  ${CD}+==================================================+${RST}\n"
}

show_privacy_menu() {
  printf "\n  ${CR}${BRT}>> PRIVACY SUB-TESTS${RST}\n"
  printf "  ${CD}+--------------------------------------------------+${RST}\n"
  for i in $(seq 0 $(( ${#PRIV_NAMES[@]} - 1 ))); do
    printf "  ${CD}|${RST} ${CW}%2d${RST} ${DIM}%-14s${RST} ${PRIV_DESCS[$i]}${RST}\n" "$((i+1))" "${PRIV_NAMES[$i]}"
  done
  printf "  ${CD}+--------------------------------------------------+${RST}\n"
  printf "  ${CD}|${RST} ${CG}A${RST}  Run all privacy tests\n"
  printf "  ${CD}|${RST} ${CO}0${RST}  Back\n"
  printf "  ${CD}+--------------------------------------------------+${RST}\n"
}

show_optimizer_menu() {
  printf "\n  ${CG}${BRT}>> OPTIMIZER SUB-TESTS${RST}\n"
  printf "  ${CD}+--------------------------------------------------+${RST}\n"
  for i in $(seq 0 $(( ${#OPTIM_NAMES[@]} - 1 ))); do
    printf "  ${CD}|${RST} ${CW}%2d${RST} ${DIM}%-16s${RST} ${OPTIM_DESCS[$i]}${RST}\n" "$((i+1))" "${OPTIM_NAMES[$i]}"
  done
  printf "  ${CD}+--------------------------------------------------+${RST}\n"
  printf "  ${CD}|${RST} ${CG}A${RST}  Run all optimizer tests\n"
  printf "  ${CD}|${RST} ${CO}0${RST}  Back\n"
  printf "  ${CD}+--------------------------------------------------+${RST}\n"
}

select_profile() {
  if [[ ${#PROFILES[@]} -eq 0 ]]; then load_profiles; fi
  printf "\n  ${CY}>> SELECT PROFILE${RST}\n"
  for i in $(seq 0 $(( ${#PROFILES[@]} - 1 ))); do
    local marker=" "
    [[ "${PROFILES[$i]}" == "${SELECTED_PROFILE:-}" ]] && marker="*"
    printf "  ${CD}|${RST} ${CG}%d${RST} ${marker} ${CW}%s${RST}\n" "$((i+1))" "${PROFILES[$i]}"
  done
  printf "  ${CD}|${RST} ${CR}0${RST}   Clear\n"
  read -rp $'  \033[38;5;39m>>\033[0m ' pchoice
  if [[ "$pchoice" =~ ^[0-9]+$ ]] && [[ "$pchoice" -ge 1 ]] && [[ "$pchoice" -le "${#PROFILES[@]}" ]]; then
    SELECTED_PROFILE="${PROFILES[$((pchoice-1))]}"
    printf "  ${CG}>> Profile: ${CW}${SELECTED_PROFILE}${RST}\n"
  elif [[ "$pchoice" == "0" ]]; then
    SELECTED_PROFILE=""
    printf "  ${CO}>> Profile cleared${RST}\n"
  fi
}

show_main_menu() {
  printf "\n"
  printf "  ${CD}+==================================================+${RST}\n"
  printf "  ${CD}|${RST} ${CY}${BRT}ZEROTRUST DETAILED SEEDER${RST}%*s${CD}|${RST}\n" 24 ""
  printf "  ${CD}|${RST} ${CB}Profile:${RST} ${CP}${SELECTED_PROFILE:-<none>}${RST}  ${CB}Model:${RST} ${CX}${SELECTED_MODEL}${RST}\n"
  printf "  ${CD}+==================================================+${RST}\n"
  printf "  ${CD}|${RST}\n"
  printf "  ${CD}|${RST} ${CR}${BRT}P${RST}  Privacy tests  (${#PRIV_NAMES[@]} sub-tests)${RST}\n"
  printf "  ${CD}|${RST} ${CG}${BRT}O${RST}  Optimizer tests (${#OPTIM_NAMES[@]} sub-tests)${RST}\n"
  printf "  ${CD}|${RST} ${CY}${BRT}A${RST}  Run ALL (${CB}$(( ${#PRIV_NAMES[@]} + ${#OPTIM_NAMES[@]} ))${RST} tests)${RST}\n"
  printf "  ${CD}|${RST}\n"
  printf "  ${CD}|${RST} ${CB}F${RST}  Select Profile     ${CB}M${RST}  Select Model${RST}\n"
  printf "  ${CD}|${RST} ${CO}S${RST}  Summary            ${CO}Q${RST}  Quit${RST}\n"
  printf "  ${CD}+==================================================+${RST}\n"
}

# ══════════════════════════════════════════════════════════════
# MAIN
# ══════════════════════════════════════════════════════════════

main() {
  SESSION_START=$(date +%s)

  printf "  ${CB}>>${RST} ${DIM}Loading profiles...${RST}"
  load_profiles
  printf " ${CG}%d${RST}\n" "${#PROFILES[@]}"

  if [[ "${1:-}" == "--privacy" ]]; then
    if [[ -z "${SELECTED_PROFILE:-}" ]] && [[ "${#PROFILES[@]}" -gt 0 ]] && [[ "${PROFILES[0]}" != "<no profile>" ]]; then
      SELECTED_PROFILE="${PROFILES[0]}"
      printf "  ${CG}>> Auto-profile: ${CW}${SELECTED_PROFILE}${RST}\n"
    fi
    run_privacy_all; show_summary; exit 0
  fi

  if [[ "${1:-}" == "--optimizer" ]]; then
    if [[ -z "${SELECTED_PROFILE:-}" ]] && [[ "${#PROFILES[@]}" -gt 0 ]] && [[ "${PROFILES[0]}" != "<no profile>" ]]; then
      SELECTED_PROFILE="${PROFILES[0]}"
      printf "  ${CG}>> Auto-profile: ${CW}${SELECTED_PROFILE}${RST}\n"
    fi
    run_optimizer_all; show_summary; exit 0
  fi

  if [[ "${1:-}" == "--all" ]]; then
    if [[ -z "${SELECTED_PROFILE:-}" ]] && [[ "${#PROFILES[@]}" -gt 0 ]] && [[ "${PROFILES[0]}" != "<no profile>" ]]; then
      SELECTED_PROFILE="${PROFILES[0]}"
      printf "  ${CG}>> Auto-profile: ${CW}${SELECTED_PROFILE}${RST}\n"
    fi
    run_all_detailed; show_summary; exit 0
  fi

  while true; do
    show_main_menu
    read -rp $'  \033[38;5;39m>>\033[0m ' cmd
    case "$cmd" in
      [pP])
        while true; do
          show_privacy_menu
          read -rp $'  \033[38;5;39m>>\033[0m ' pcmd
          case "$pcmd" in
            [aA]) run_privacy_all ;;
            0) break ;;
            [0-9]*)
              _idx=$((pcmd - 1))
              if [[ "$_idx" -ge 0 ]] && [[ "$_idx" -lt "${#PRIV_NAMES[@]}" ]]; then
                "${PRIV_FUNCS[$_idx]}"
              fi ;;
            *) printf "  ${CR}>> Unknown${RST}\n" ;;
          esac
        done ;;
      [oO])
        while true; do
          show_optimizer_menu
          read -rp $'  \033[38;5;39m>>\033[0m ' ocmd
          case "$ocmd" in
            [aA]) run_optimizer_all ;;
            0) break ;;
            [0-9]*)
              _idx=$((ocmd - 1))
              if [[ "$_idx" -ge 0 ]] && [[ "$_idx" -lt "${#OPTIM_NAMES[@]}" ]]; then
                "${OPTIM_FUNCS[$_idx]}"
              fi ;;
            *) printf "  ${CR}>> Unknown${RST}\n" ;;
          esac
        done ;;
      [aA])
        if [[ -z "${SELECTED_PROFILE:-}" ]] && [[ "${#PROFILES[@]}" -gt 0 ]] && [[ "${PROFILES[0]}" != "<no profile>" ]]; then
          SELECTED_PROFILE="${PROFILES[0]}"
          printf "  ${CG}>> Auto-profile: ${CW}${SELECTED_PROFILE}${RST}\n"
        fi
        run_all_detailed ;;
      [fF]) select_profile ;;
      [sS]) show_summary ;;
      [qQ]|[eE]|0)
        show_summary
        printf "  ${DIM}>> Connection terminated.${RST}\n\n"
        exit 0 ;;
      *) printf "  ${CR}>> Unknown: ${cmd}${RST}\n" ;;
    esac
  done
}

main "$@"
