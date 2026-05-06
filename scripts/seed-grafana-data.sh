#!/usr/bin/env bash
# seed-grafana-data.sh
# Populates all Grafana dashboard panels with realistic data via Prometheus Pushgateway.
#
# Prerequisites:
#   - arl-pushgateway container running (port 9091)
#   - arl-prometheus scraping pushgateway
#   - API gateway running on localhost:8080
#
# Usage:
#   ./scripts/seed-grafana-data.sh           # full seed
#   ./scripts/seed-grafana-data.sh --quick    # minimal data
#
# After running, wait 30s for Prometheus scrape, then check Grafana.

set -euo pipefail

GW="${GW:-http://localhost:8080}"
PG="${PG:-http://localhost:9091}"
QUICK=false
[[ "${1:-}" == "--quick" ]] && QUICK=true

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
log()  { echo -e "${GREEN}[SEED]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
err()  { echo -e "${RED}[ERR]${NC} $*" >&2; }

# --- helpers ---
push() {
 local job="$1"; shift
 local data
 data="$(cat)"
 printf '%s\n' "$data" | curl -sf -X POST "${PG}/metrics/job/${job}" --data-binary @- 2>/dev/null || {
 err "Push failed: job=$job"
 echo "$data" | head -3
 }
}

rand() { echo $(( RANDOM % ${1:-100} + ${2:-1} )); }

# --- seed profiles & tokens ---
seed_profiles() {
  log "Registering accounts..."
  for p in anthropic openai zai; do
    curl -sf -X POST "${GW}/v1/auth/${p}/register" \
      -H "Content-Type: application/json" \
      -d "{\"api_key\":\"sk-seed-${p}-$(rand 9999 1000)\"}" >/dev/null 2>&1 || true
  done

  log "Creating seed profiles..."
  local profiles=(
    'seed-anthropic|anthropic|claude-sonnet-4-20250514|ic-001'
    'seed-openai|openai|gpt-4o|ai-001'
  )
  IFS='|'
  for entry in "${profiles[@]}"; do
    IFS="|" read -r name target model accounts <<< "$entry"
    curl -sf -X POST "${GW}/v1/profiles/" \
      -H "Content-Type: application/json" \
      -d "{\"name\":\"${name}\",\"target\":\"${target}\",\"model\":\"${model}\",\"accountIds\":[\"${accounts}\"]}" >/dev/null 2>&1 || true
    local token
    token=$(curl -sf -X POST "${GW}/v1/profiles/${name}/tokens" \
      -H "Content-Type: application/json" \
      -d "{\"keyName\":\"seed-key\"}" 2>/dev/null | python3 -c "import json,sys; print(json.load(sys.stdin).get('token',''))" 2>/dev/null || echo "")
    if [[ -n "$token" ]]; then
      echo "${name}:${token}" >> /tmp/seed-tokens
    fi
  done
  log "Profiles created."
}

# --- make real SSE requests through profiles ---
seed_real_requests() {
  log "Sending requests through profiles (SSE + non-SSE)..."
  while IFS=: read -r name token; do
    [[ -z "$token" ]] && continue
    local model
    case "$name" in
      seed-anthropic) model="claude-sonnet-4-20250514" ;;
      seed-openai)    model="gpt-4o" ;;
      *)              model="claude-sonnet-4-20250514" ;;
    esac
    curl -sf -X POST "${GW}/v1/messages" \
      -H "Content-Type: application/json" \
      -H "x-api-key: ${token}" \
      -d "{\"model\":\"${model}\",\"max_tokens\":50,\"messages\":[{\"role\":\"user\",\"content\":\"Hello\"}]}" >/dev/null 2>&1 || true
    curl -sf -X POST "${GW}/v1/messages" \
      -H "Content-Type: application/json" \
      -H "x-api-key: ${token}" \
      -H "Accept: text/event-stream" \
      -d "{\"model\":\"${model}\",\"max_tokens\":50,\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"Hi\"}]}" >/dev/null 2>&1 || true
  done < /tmp/seed-tokens 2>/dev/null
  log "Real requests sent (will get 401s with seed keys)."
}

# --- push synthetic data for all dashboard metrics ---
seed_synthetic() {
  log "Pushing synthetic metrics to pushgateway..."

  local PROFILES="seed-anthropic,seed-openai,test-anthropic,test-openai"
  local MODELS="claude-sonnet-4-20250514,gpt-4o,glm-5.1,glm-5,glm-4.7"
  local ACCOUNTS="ic-001,ai-001"
  IFS=',' read -ra PROFS <<< "$PROFILES"
  IFS=',' read -ra MODS <<< "$MODELS"
  IFS=',' read -ra ACCTS <<< "$ACCOUNTS"

  for model in "${MODS[@]}"; do
    local inp=$(( RANDOM % 50000 + 5000 ))
    local out=$(( RANDOM % 15000 + 1000 ))
    push "seed_tokens" <<PUSH
# TYPE api_gateway_token_input_total counter
api_gateway_token_input_total{model="${model}"} ${inp}
# TYPE api_gateway_token_output_total counter
api_gateway_token_output_total{model="${model}"} ${out}
# TYPE api_gateway_cost_total counter
api_gateway_cost_total{model="${model}",type="input"} $(echo "scale=6;${inp}*3/1000000" | bc)
api_gateway_cost_total{model="${model}",type="output"} $(echo "scale=6;${out}*15/1000000" | bc)
PUSH
  done

  for prof in "${PROFS[@]}"; do
    local model="${MODS[$((RANDOM % ${#MODS[@]}))]}"
    local inp=$(( RANDOM % 20000 + 1000 ))
    local out=$(( RANDOM % 8000 + 500 ))
    push "seed_profile_${prof}" <<PUSH
# TYPE api_gateway_profile_requests_total counter
api_gateway_profile_requests_total{profile="${prof}",model="${model}"} $(( RANDOM % 50 + 5 ))
# TYPE api_gateway_profile_token_input_total counter
api_gateway_profile_token_input_total{profile="${prof}",model="${model}"} ${inp}
# TYPE api_gateway_profile_token_output_total counter
api_gateway_profile_token_output_total{profile="${prof}",model="${model}"} ${out}
# TYPE api_gateway_profile_cost_total counter
api_gateway_profile_cost_total{profile="${prof}",model="${model}",type="input"} $(echo "scale=6;${inp}*3/1000000" | bc)
api_gateway_profile_cost_total{profile="${prof}",model="${model}",type="output"} $(echo "scale=6;${out}*15/1000000" | bc)
# TYPE api_gateway_profile_optimizer_chars_saved_total counter
api_gateway_profile_optimizer_chars_saved_total{profile="${prof}",technique="chunker"} $(( RANDOM % 5000 ))
api_gateway_profile_optimizer_chars_saved_total{profile="${prof}",technique="delta"} $(( RANDOM % 3000 ))
api_gateway_profile_optimizer_chars_saved_total{profile="${prof}",technique="disclosure"} $(( RANDOM % 2000 ))
api_gateway_profile_optimizer_chars_saved_total{profile="${prof}",technique="sketch"} $(( RANDOM % 4000 ))
PUSH
  done

  for acct in "${ACCTS[@]}"; do
    local model="${MODS[$((RANDOM % ${#MODS[@]}))]}"
    push "seed_account_${acct}" <<PUSH
# TYPE api_gateway_account_token_input_total counter
api_gateway_account_token_input_total{account_id="${acct}",model="${model}"} $(( RANDOM % 30000 + 2000 ))
# TYPE api_gateway_account_token_output_total counter
api_gateway_account_token_output_total{account_id="${acct}",model="${model}"} $(( RANDOM % 10000 + 500 ))
PUSH
  done

  for bpath in go_direct sidecar direct billing_rejected; do
    for model in "${MODS[@]:0:3}"; do
      for prof in "${PROFS[@]:0:3}"; do
        push "seed_billing_${bpath}" <<PUSH
# TYPE api_gateway_billing_path_requests_total counter
api_gateway_billing_path_requests_total{path="${bpath}",model="${model}",profile="${prof}"} $(( RANDOM % 20 + 1 ))
PUSH
      done
    done
  done

  for bpath in go_direct sidecar direct; do
    push "seed_billing_lat_${bpath}" <<PUSH
# TYPE api_gateway_billing_path_latency_seconds histogram
api_gateway_billing_path_latency_seconds_bucket{path="${bpath}",model="claude-sonnet-4-20250514",le="0.05"} $(( RANDOM % 10 ))
api_gateway_billing_path_latency_seconds_bucket{path="${bpath}",model="claude-sonnet-4-20250514",le="0.1"} $(( RANDOM % 20 + 10 ))
api_gateway_billing_path_latency_seconds_bucket{path="${bpath}",model="claude-sonnet-4-20250514",le="0.25"} $(( RANDOM % 30 + 20 ))
api_gateway_billing_path_latency_seconds_bucket{path="${bpath}",model="claude-sonnet-4-20250514",le="0.5"} $(( RANDOM % 40 + 30 ))
api_gateway_billing_path_latency_seconds_bucket{path="${bpath}",model="claude-sonnet-4-20250514",le="1"} $(( RANDOM % 50 + 40 ))
api_gateway_billing_path_latency_seconds_bucket{path="${bpath}",model="claude-sonnet-4-20250514",le="2.5"} $(( RANDOM % 60 + 50 ))
api_gateway_billing_path_latency_seconds_bucket{path="${bpath}",model="claude-sonnet-4-20250514",le="5"} $(( RANDOM % 65 + 55 ))
api_gateway_billing_path_latency_seconds_bucket{path="${bpath}",model="claude-sonnet-4-20250514",le="+Inf"} $(( RANDOM % 70 + 60 ))
api_gateway_billing_path_latency_seconds_sum{path="${bpath}",model="claude-sonnet-4-20250514"} $(echo "scale=3;$(rand 5000 500)/1000" | bc)
api_gateway_billing_path_latency_seconds_count{path="${bpath}",model="claude-sonnet-4-20250514"} $(( RANDOM % 70 + 60 ))
PUSH
  done

  for tech in chunker delta disclosure sketch; do
    push "seed_optim_${tech}" <<PUSH
# TYPE api_gateway_optimizer_runs_total counter
api_gateway_optimizer_runs_total{technique="${tech}"} $(( RANDOM % 100 + 20 ))
# TYPE api_gateway_optimizer_chars_saved_total counter
api_gateway_optimizer_chars_saved_total{technique="${tech}",direction="input"} $(( RANDOM % 8000 + 500 ))
# TYPE api_gateway_optimizer_tokens_saved_total counter
api_gateway_optimizer_tokens_saved_total{direction="input"} $(( RANDOM % 5000 + 500 ))
api_gateway_optimizer_tokens_saved_total{direction="output"} $(( RANDOM % 2000 + 200 ))
# TYPE api_gateway_optimizer_duration_seconds histogram
api_gateway_optimizer_duration_seconds_bucket{technique="${tech}",le="0.001"} $(( RANDOM % 10 ))
api_gateway_optimizer_duration_seconds_bucket{technique="${tech}",le="0.005"} $(( RANDOM % 30 + 10 ))
api_gateway_optimizer_duration_seconds_bucket{technique="${tech}",le="0.01"} $(( RANDOM % 50 + 30 ))
api_gateway_optimizer_duration_seconds_bucket{technique="${tech}",le="0.05"} $(( RANDOM % 70 + 50 ))
api_gateway_optimizer_duration_seconds_bucket{technique="${tech}",le="0.1"} $(( RANDOM % 80 + 70 ))
api_gateway_optimizer_duration_seconds_bucket{technique="${tech}",le="+Inf"} $(( RANDOM % 90 + 80 ))
api_gateway_optimizer_duration_seconds_sum{technique="${tech}"} $(echo "scale=4;$(rand 500 50)/10000" | bc)
api_gateway_optimizer_duration_seconds_count{technique="${tech}"} $(( RANDOM % 90 + 80 ))
PUSH
  done

  for tech in chunker delta disclosure sketch; do
    push "seed_tech_${tech}" <<PUSH
# TYPE api_gateway_${tech}_chars_saved_total counter
api_gateway_${tech}_chars_saved_total{technique="${tech}"} $(( RANDOM % 8000 + 1000 ))
PUSH
  done

  push "seed_savings" <<PUSH
# TYPE api_gateway_cost_savings_total counter
api_gateway_cost_savings_total 0.045
PUSH

  push "seed_upstream" <<PUSH
# TYPE api_gateway_upstream_429_total counter
api_gateway_upstream_429_total $(( RANDOM % 10 + 1 ))
# TYPE api_gateway_upstream_retries_total counter
api_gateway_upstream_retries_total $(( RANDOM % 15 + 2 ))
# TYPE api_gateway_model_fallback_total counter
api_gateway_model_fallback_total{requested="claude-opus-4-20250514",selected="claude-sonnet-4-20250514"} $(( RANDOM % 5 ))
# TYPE api_gateway_error_total counter
api_gateway_error_total{type="upstream_timeout"} $(( RANDOM % 3 ))
api_gateway_error_total{type="upstream_5xx"} $(( RANDOM % 2 ))
# TYPE api_gateway_rate_limit_hits_total counter
api_gateway_rate_limit_hits_total{key="ip:10.0.0.1"} $(( RANDOM % 20 ))
api_gateway_rate_limit_hits_total{key="profile:seed-anthropic"} $(( RANDOM % 10 ))
# TYPE api_gateway_transient_retry_total counter
api_gateway_transient_retry_total{status="503",model="claude-sonnet-4-20250514"} $(( RANDOM % 5 ))
# TYPE api_gateway_context_truncation_total counter
api_gateway_context_truncation_total{model="claude-sonnet-4-20250514"} $(( RANDOM % 3 ))
PUSH

  push "seed_ttfb" <<PUSH
# TYPE api_gateway_ttfb_seconds histogram
api_gateway_ttfb_seconds_bucket{model="claude-sonnet-4-20250514",le="0.1"} 5
api_gateway_ttfb_seconds_bucket{model="claude-sonnet-4-20250514",le="0.3"} 15
api_gateway_ttfb_seconds_bucket{model="claude-sonnet-4-20250514",le="0.5"} 30
api_gateway_ttfb_seconds_bucket{model="claude-sonnet-4-20250514",le="1"} 50
api_gateway_ttfb_seconds_bucket{model="claude-sonnet-4-20250514",le="2"} 65
api_gateway_ttfb_seconds_bucket{model="claude-sonnet-4-20250514",le="+Inf"} 70
api_gateway_ttfb_seconds_sum{model="claude-sonnet-4-20250514"} 35.2
api_gateway_ttfb_seconds_count{model="claude-sonnet-4-20250514"} 70
api_gateway_ttfb_seconds_bucket{model="gpt-4o",le="0.1"} 3
api_gateway_ttfb_seconds_bucket{model="gpt-4o",le="0.3"} 12
api_gateway_ttfb_seconds_bucket{model="gpt-4o",le="0.5"} 25
api_gateway_ttfb_seconds_bucket{model="gpt-4o",le="1"} 40
api_gateway_ttfb_seconds_bucket{model="gpt-4o",le="2"} 55
api_gateway_ttfb_seconds_bucket{model="gpt-4o",le="+Inf"} 60
api_gateway_ttfb_seconds_sum{model="gpt-4o"} 28.5
api_gateway_ttfb_seconds_count{model="gpt-4o"} 60
PUSH

  push "seed_privacy" <<PUSH
# TYPE api_gateway_mask_requests_total counter
api_gateway_mask_requests_total{has_secrets="true"} $(( RANDOM % 30 + 5 ))
api_gateway_mask_requests_total{has_secrets="false"} $(( RANDOM % 100 + 20 ))
api_gateway_mask_requests_total{has_pii="true"} $(( RANDOM % 20 + 3 ))
api_gateway_mask_requests_total{has_pii="false"} $(( RANDOM % 100 + 20 ))
# TYPE api_gateway_pii_detected_total counter
api_gateway_pii_detected_total{type="EMAIL_ADDRESS"} $(( RANDOM % 15 + 2 ))
api_gateway_pii_detected_total{type="PHONE_NUMBER"} $(( RANDOM % 10 + 1 ))
api_gateway_pii_detected_total{type="api_key"} $(( RANDOM % 8 ))
api_gateway_pii_detected_total{type="aws_secret"} $(( RANDOM % 3 ))
api_gateway_pii_detected_total{type="private_key"} $(( RANDOM % 2 ))
api_gateway_pii_detected_total{type="github_token"} $(( RANDOM % 4 ))
# TYPE api_gateway_secrets_detected_total counter
api_gateway_secrets_detected_total{type="api_key"} $(( RANDOM % 10 ))
api_gateway_secrets_detected_total{type="aws_secret"} $(( RANDOM % 5 ))
api_gateway_secrets_detected_total{type="private_key"} $(( RANDOM % 3 ))
PUSH

  local MD="api_gateway_mask_duration_seconds"
  push "seed_mask_dur" <<PUSH
# TYPE ${MD} histogram
${MD}_bucket{phase="scan",le="0.001"} 20
${MD}_bucket{phase="scan",le="0.005"} 50
${MD}_bucket{phase="scan",le="0.01"} 70
${MD}_bucket{phase="scan",le="0.05"} 85
${MD}_bucket{phase="scan",le="+Inf"} 90
${MD}_sum{phase="scan"} 0.45
${MD}_count{phase="scan"} 90
${MD}_bucket{phase="mask",le="0.001"} 15
${MD}_bucket{phase="mask",le="0.005"} 40
${MD}_bucket{phase="mask",le="0.01"} 60
${MD}_bucket{phase="mask",le="0.05"} 75
${MD}_bucket{phase="mask",le="+Inf"} 80
${MD}_sum{phase="mask"} 0.32
${MD}_count{phase="mask"} 80
${MD}_bucket{phase="unmask",le="0.001"} 10
${MD}_bucket{phase="unmask",le="0.005"} 30
${MD}_bucket{phase="unmask",le="0.01"} 50
${MD}_bucket{phase="unmask",le="+Inf"} 60
${MD}_sum{phase="unmask"} 0.18
${MD}_count{phase="unmask"} 60
PUSH

  push "seed_waste" <<PUSH
# TYPE api_gateway_waste_findings_total counter
api_gateway_waste_findings_total{detector="repeated_phrases",severity="low"} $(( RANDOM % 10 ))
api_gateway_waste_findings_total{detector="repeated_phrases",severity="medium"} $(( RANDOM % 5 ))
api_gateway_waste_findings_total{detector="empty_context",severity="high"} $(( RANDOM % 3 ))
api_gateway_waste_findings_total{detector="oversized_system_prompt",severity="medium"} $(( RANDOM % 8 ))
# TYPE api_gateway_waste_tokens_wasted_total counter
api_gateway_waste_tokens_wasted_total{detector="repeated_phrases"} $(( RANDOM % 5000 ))
api_gateway_waste_tokens_wasted_total{detector="empty_context"} $(( RANDOM % 2000 ))
api_gateway_waste_tokens_wasted_total{detector="oversized_system_prompt"} $(( RANDOM % 8000 ))
# TYPE api_gateway_waste_scan_duration_seconds histogram
api_gateway_waste_scan_duration_seconds_bucket{le="0.001"} 30
api_gateway_waste_scan_duration_seconds_bucket{le="0.005"} 60
api_gateway_waste_scan_duration_seconds_bucket{le="0.01"} 80
api_gateway_waste_scan_duration_seconds_bucket{le="0.05"} 90
api_gateway_waste_scan_duration_seconds_bucket{le="+Inf"} 95
api_gateway_waste_scan_duration_seconds_sum 0.25
api_gateway_waste_scan_duration_seconds_count 95
PUSH

  push "seed_vision" <<PUSH
# TYPE api_gateway_vision_preanalysis_total counter
api_gateway_vision_preanalysis_total{status="success"} $(( RANDOM % 20 + 5 ))
api_gateway_vision_preanalysis_total{status="fallback"} $(( RANDOM % 5 ))
# TYPE api_gateway_image_compressions_total counter
api_gateway_image_compressions_total{model="glm-5.1"} $(( RANDOM % 15 + 3 ))
# TYPE api_gateway_image_bytes_saved_total counter
api_gateway_image_bytes_saved_total{model="glm-5.1"} $(( RANDOM % 500000 + 50000 ))
# TYPE api_gateway_image_bytes_original_total counter
api_gateway_image_bytes_original_total{model="glm-5.1"} $(( RANDOM % 1000000 + 100000 ))
PUSH

  push "seed_mcp" <<PUSH
# TYPE api_gateway_mcp_calls_total counter
api_gateway_mcp_calls_total{server="web-reader",tool="fetch",status="success"} $(( RANDOM % 30 + 5 ))
api_gateway_mcp_calls_total{server="web-reader",tool="fetch",status="error"} $(( RANDOM % 3 ))
api_gateway_mcp_calls_total{server="analyzer",tool="analyze",status="success"} $(( RANDOM % 20 + 3 ))
# TYPE api_gateway_mcp_cache_hits_total counter
api_gateway_mcp_cache_hits_total{server="web-reader",tool="fetch"} $(( RANDOM % 15 ))
# TYPE api_gateway_mcp_cache_misses_total counter
api_gateway_mcp_cache_misses_total{server="web-reader",tool="fetch"} $(( RANDOM % 25 ))
# TYPE api_gateway_mcp_quota_usage gauge
api_gateway_mcp_quota_usage{account_id="ic-001"} 0.35
PUSH

  for rpath in /v1/messages /v1/auth/accounts /v1/profiles; do
    local method="POST"
    [[ "$rpath" == "/v1/auth/accounts" || "$rpath" == "/v1/profiles" ]] && method="GET"
    push "seed_lat_${rpath//\//_}" <<PUSH
# TYPE api_gateway_request_latency_seconds histogram
api_gateway_request_latency_seconds_bucket{method="${method}",path="${rpath}",status="200",le="0.05"} $(( RANDOM % 20 ))
api_gateway_request_latency_seconds_bucket{method="${method}",path="${rpath}",status="200",le="0.1"} $(( RANDOM % 40 + 20 ))
api_gateway_request_latency_seconds_bucket{method="${method}",path="${rpath}",status="200",le="0.25"} $(( RANDOM % 60 + 40 ))
api_gateway_request_latency_seconds_bucket{method="${method}",path="${rpath}",status="200",le="0.5"} $(( RANDOM % 80 + 60 ))
api_gateway_request_latency_seconds_bucket{method="${method}",path="${rpath}",status="200",le="1"} $(( RANDOM % 90 + 80 ))
api_gateway_request_latency_seconds_bucket{method="${method}",path="${rpath}",status="200",le="2.5"} $(( RANDOM % 95 + 90 ))
api_gateway_request_latency_seconds_bucket{method="${method}",path="${rpath}",status="200",le="+Inf"} $(( RANDOM % 100 + 95 ))
api_gateway_request_latency_seconds_sum{method="${method}",path="${rpath}",status="200"} $(echo "scale=3;$(rand 5000 500)/1000" | bc)
api_gateway_request_latency_seconds_count{method="${method}",path="${rpath}",status="200"} $(( RANDOM % 100 + 95 ))
api_gateway_request_latency_seconds_count{method="${method}",path="${rpath}",status="401"} $(( RANDOM % 10 ))
api_gateway_request_latency_seconds_count{method="${method}",path="${rpath}",status="429"} $(( RANDOM % 5 ))
PUSH
  done

  for model in "${MODS[@]}"; do
    push "seed_budget_${model//./_}" <<PUSH
# TYPE api_gateway_budget_level gauge
api_gateway_budget_level{model="${model}"} $(( RANDOM % 3 ))
PUSH
  done

  log "Synthetic metrics pushed."
}


# --- simulate counter growth for rate()/increase() panels ---
seed_simulate() {
local ITERS="${1:-20}"
local DELAY="${2:-5}"
log "Simulating traffic: ${ITERS} iterations, ${DELAY}s apart..."
local PROFILES="seed-anthropic,seed-openai"
local MODELS="claude-sonnet-4-20250514,gpt-4o"
IFS=',' read -ra PROFS <<< "$PROFILES"
IFS=',' read -ra MODS <<< "$MODELS"

local req_total=0 inp_total=0 out_total=0
local prof_req=() prof_inp=() prof_out=()
for i in "${!PROFS[@]}"; do prof_req[$i]=0; prof_inp[$i]=0; prof_out[$i]=0; done
local mask_req=0 mask_pii=0 mask_secrets=0
local optim_runs_chunker=$(( RANDOM % 20 + 10 )) optim_runs_delta=$(( RANDOM % 20 + 10 ))
local optim_runs_disclosure=$(( RANDOM % 20 + 10 )) optim_runs_sketch=$(( RANDOM % 20 + 10 ))
local optim_chars_chunker=$(( RANDOM % 3000 + 500 )) optim_chars_delta=$(( RANDOM % 3000 + 500 ))
local optim_chars_disclosure=$(( RANDOM % 3000 + 500 )) optim_chars_sketch=$(( RANDOM % 3000 + 500 ))
local waste_det_rp=$(( RANDOM % 5 )) waste_det_ec=$(( RANDOM % 5 )) waste_det_osp=$(( RANDOM % 5 ))
local waste_tok_rp=$(( RANDOM % 1000 )) waste_tok_ec=$(( RANDOM % 1000 )) waste_tok_osp=$(( RANDOM % 1000 ))
local vision_success=0 vision_fallback=0
local mcp_success=0 mcp_error=0

for ((iter=1; iter<=ITERS; iter++)); do
  local delta_req=$(( RANDOM % 5 + 1 ))
  local delta_inp=$(( RANDOM % 2000 + 100 ))
  local delta_out=$(( RANDOM % 800 + 50 ))
  req_total=$((req_total + delta_req))
  inp_total=$((inp_total + delta_inp))
  out_total=$((out_total + delta_out))

  local model="${MODS[$((RANDOM % ${#MODS[@]}))]}"
  push "sim_tokens" <<PUSH
# TYPE api_gateway_token_input_total counter
api_gateway_token_input_total{model="${model}"} ${inp_total}
# TYPE api_gateway_token_output_total counter
api_gateway_token_output_total{model="${model}"} ${out_total}
PUSH

  for i in "${!PROFS[@]}"; do
    local dr=$(( RANDOM % 3 + 1 ))
    local di=$(( RANDOM % 1500 + 50 ))
    local do_=$(( RANDOM % 600 + 30 ))
    prof_req[$i]=$((prof_req[$i] + dr))
    prof_inp[$i]=$((prof_inp[$i] + di))
    prof_out[$i]=$((prof_out[$i] + do_))
    local m="${MODS[$((RANDOM % ${#MODS[@]}))]}"
    push "sim_prof_${PROFS[$i]}" <<PUSH
# TYPE api_gateway_profile_requests_total counter
api_gateway_profile_requests_total{profile="${PROFS[$i]}",model="${m}"} ${prof_req[$i]}
# TYPE api_gateway_profile_token_input_total counter
api_gateway_profile_token_input_total{profile="${PROFS[$i]}",model="${m}"} ${prof_inp[$i]}
# TYPE api_gateway_profile_token_output_total counter
api_gateway_profile_token_output_total{profile="${PROFS[$i]}",model="${m}"} ${prof_out[$i]}
PUSH
  done

  mask_req=$((mask_req + RANDOM % 8 + 1))
  mask_pii=$((mask_pii + RANDOM % 3))
  mask_secrets=$((mask_secrets + RANDOM % 2))
  push "sim_privacy" <<PUSH
# TYPE api_gateway_mask_requests_total counter
api_gateway_mask_requests_total{has_secrets="true"} ${mask_secrets}
api_gateway_mask_requests_total{has_secrets="false"} ${mask_req}
api_gateway_mask_requests_total{has_pii="true"} ${mask_pii}
api_gateway_mask_requests_total{has_pii="false"} ${mask_req}
# TYPE api_gateway_pii_detected_total counter
api_gateway_pii_detected_total{type="EMAIL_ADDRESS"} $(( mask_pii + RANDOM % 5 ))
api_gateway_pii_detected_total{type="PHONE_NUMBER"} $(( mask_pii + RANDOM % 3 ))
api_gateway_pii_detected_total{type="api_key"} $(( mask_secrets + RANDOM % 2 ))
# TYPE api_gateway_secrets_detected_total counter
api_gateway_secrets_detected_total{type="api_key"} ${mask_secrets}
api_gateway_secrets_detected_total{type="aws_secret"} $(( RANDOM % 2 ))
api_gateway_secrets_detected_total{type="private_key"} $(( RANDOM % 2 ))
PUSH

  optim_runs_chunker=$((optim_runs_chunker + RANDOM % 5 + 1))
  optim_chars_chunker=$((optim_chars_chunker + RANDOM % 500 + 50))
  optim_runs_delta=$((optim_runs_delta + RANDOM % 5 + 1))
  optim_chars_delta=$((optim_chars_delta + RANDOM % 500 + 50))
  optim_runs_disclosure=$((optim_runs_disclosure + RANDOM % 5 + 1))
  optim_chars_disclosure=$((optim_chars_disclosure + RANDOM % 500 + 50))
  optim_runs_sketch=$((optim_runs_sketch + RANDOM % 5 + 1))
  optim_chars_sketch=$((optim_chars_sketch + RANDOM % 500 + 50))
  push "sim_optim" <<PUSH
# TYPE api_gateway_optimizer_runs_total counter
api_gateway_optimizer_runs_total{technique="chunker"} ${optim_runs_chunker}
api_gateway_optimizer_runs_total{technique="delta"} ${optim_runs_delta}
api_gateway_optimizer_runs_total{technique="disclosure"} ${optim_runs_disclosure}
api_gateway_optimizer_runs_total{technique="sketch"} ${optim_runs_sketch}
# TYPE api_gateway_optimizer_chars_saved_total counter
api_gateway_optimizer_chars_saved_total{technique="chunker",direction="input"} ${optim_chars_chunker}
api_gateway_optimizer_chars_saved_total{technique="delta",direction="input"} ${optim_chars_delta}
api_gateway_optimizer_chars_saved_total{technique="disclosure",direction="input"} ${optim_chars_disclosure}
api_gateway_optimizer_chars_saved_total{technique="sketch",direction="input"} ${optim_chars_sketch}
PUSH

  push "sim_tech_chunker" <<PUSH
# TYPE api_gateway_chunker_chars_saved_total counter
api_gateway_chunker_chars_saved_total{technique="chunker"} ${optim_chars_chunker}
PUSH
  push "sim_tech_delta" <<PUSH
# TYPE api_gateway_delta_chars_saved_total counter
api_gateway_delta_chars_saved_total{technique="delta"} ${optim_chars_delta}
PUSH
  push "sim_tech_disclosure" <<PUSH
# TYPE api_gateway_disclosure_chars_saved_total counter
api_gateway_disclosure_chars_saved_total{technique="disclosure"} ${optim_chars_disclosure}
PUSH
  push "sim_tech_sketch" <<PUSH
# TYPE api_gateway_sketch_chars_saved_total counter
api_gateway_sketch_chars_saved_total{technique="sketch"} ${optim_chars_sketch}
PUSH

  waste_det_rp=$((waste_det_rp + RANDOM % 3))
  waste_tok_rp=$((waste_tok_rp + RANDOM % 300))
  waste_det_ec=$((waste_det_ec + RANDOM % 3))
  waste_tok_ec=$((waste_tok_ec + RANDOM % 300))
  waste_det_osp=$((waste_det_osp + RANDOM % 3))
  waste_tok_osp=$((waste_tok_osp + RANDOM % 300))
  push "sim_waste" <<PUSH
# TYPE api_gateway_waste_findings_total counter
api_gateway_waste_findings_total{detector="repeated_phrases",severity="low"} ${waste_det_rp}
api_gateway_waste_findings_total{detector="repeated_phrases",severity="medium"} $(( waste_det_rp / 2 ))
api_gateway_waste_findings_total{detector="empty_context",severity="high"} ${waste_det_ec}
api_gateway_waste_findings_total{detector="oversized_system_prompt",severity="medium"} ${waste_det_osp}
# TYPE api_gateway_waste_tokens_wasted_total counter
api_gateway_waste_tokens_wasted_total{detector="repeated_phrases"} ${waste_tok_rp}
api_gateway_waste_tokens_wasted_total{detector="empty_context"} ${waste_tok_ec}
api_gateway_waste_tokens_wasted_total{detector="oversized_system_prompt"} ${waste_tok_osp}
PUSH

  vision_success=$((vision_success + RANDOM % 3 + 1))
  vision_fallback=$((vision_fallback + RANDOM % 2 ))
  push "sim_vision" <<PUSH
# TYPE api_gateway_vision_preanalysis_total counter
api_gateway_vision_preanalysis_total{status="success"} ${vision_success}
api_gateway_vision_preanalysis_total{status="fallback"} ${vision_fallback}
PUSH

  mcp_success=$((mcp_success + RANDOM % 4 + 1))
  mcp_error=$((mcp_error + RANDOM % 2))
  push "sim_mcp" <<PUSH
# TYPE api_gateway_mcp_calls_total counter
api_gateway_mcp_calls_total{server="web-reader",tool="fetch",status="success"} ${mcp_success}
api_gateway_mcp_calls_total{server="web-reader",tool="fetch",status="error"} ${mcp_error}
# TYPE api_gateway_mcp_cache_hits_total counter
api_gateway_mcp_cache_hits_total{server="web-reader",tool="fetch"} $(( mcp_success / 2 ))
# TYPE api_gateway_mcp_cache_misses_total counter
api_gateway_mcp_cache_misses_total{server="web-reader",tool="fetch"} $(( mcp_success / 2 + 1 ))
PUSH

  # Cost savings - accumulate from optimizer chars
local cost_savings=$(echo "scale=6;(${optim_chars_chunker}+${optim_chars_delta}+${optim_chars_disclosure}+${optim_chars_sketch})*0.75/1000000" | bc)
push "sim_savings" <<PUSH
# TYPE api_gateway_cost_savings_total counter
api_gateway_cost_savings_total ${cost_savings}
PUSH

(( iter < ITERS )) && sleep "$DELAY"
done
log "Simulation done: ${ITERS} iterations over $((ITERS * DELAY))s"
}

# --- main ---
main() {
log "Seeding Grafana dashboard data..."
log "Gateway: ${GW}"
log "Pushgateway: ${PG}"
rm -f /tmp/seed-tokens
seed_profiles
seed_real_requests
seed_synthetic
if [[ "$QUICK" != "true" ]]; then
  seed_simulate 10 3
fi
log "Done! Wait ~30s for Prometheus scrape, then refresh Grafana."
log "Cleanup: docker stop arl-pushgateway && docker rm arl-pushgateway"
}

main "$@"
