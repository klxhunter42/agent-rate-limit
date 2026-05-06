#!/usr/bin/env bash
# continuous-seed.sh - pushes incrementing counters every N seconds
# Usage: ./scripts/continuous-seed.sh [INTERVAL] [RUNS]
set -euo pipefail

PG="${PG:-http://localhost:9091}"
INTERVAL="${1:-15}"
MAX_RUNS="${2:-0}"
RUN=0

log() { echo "[$(date +%H:%M:%S)] $*"; }

push() {
  local job="$1"; shift
  local data
  data="$(cat)"
  printf '%s\n' "$data" | curl -sf -X POST "${PG}/metrics/job/${job}" --data-binary @- 2>/dev/null || true
}

TOKENS_INP=0 TOKENS_OUT=0 COST_IN=0 COST_OUT=0
PROF_REQ_A=0 PROF_REQ_O=0
PROF_INP_A=0 PROF_INP_O=0
PROF_OUT_A=0 PROF_OUT_O=0
MASK_ALL=0 MASK_SEC=0 MASK_PII=0
OPTIM_C=0 OPTIM_D=0 OPTIM_S=0 OPTIM_K=0
CHARS_C=0 CHARS_D=0 CHARS_S=0 CHARS_K=0
TOKSAV_IN=0 TOKSAV_OUT=0
WASTE_RP=0 WASTE_EC=0 WASTE_OSP=0
WASTETOK_RP=0 WASTETOK_EC=0 WASTETOK_OSP=0
VISION_OK=0 VISION_FB=0 MCP_OK=0 MCP_ERR=0
FALLBACK_RL=0 FALLBACK_UNAVAIL=0
UPSTREAM_429=0 UPSTREAM_RETRIES=0

log "Starting continuous seed (interval=${INTERVAL}s, max=${MAX_RUNS})"

while true; do
  RUN=$((RUN + 1))
  TOKENS_INP=$((TOKENS_INP + RANDOM % 2000 + 100))
  TOKENS_OUT=$((TOKENS_OUT + RANDOM % 800 + 50))
  COST_IN=$((COST_IN + RANDOM % 20 + 1))
  COST_OUT=$((COST_OUT + RANDOM % 15 + 1))

  model_idx=$((RUN % 2))
  model="claude-sonnet-4-20250514"
  case $model_idx in 1) model="gpt-4o" ;; esac

  COST_I_VAL=$(printf "%.6f" "$(echo "$COST_IN / 1000000" | bc -l)")
  COST_O_VAL=$(printf "%.6f" "$(echo "$COST_OUT / 1000000" | bc -l)")

  push "live_tokens" <<PUSH
# TYPE api_gateway_token_input_total counter
api_gateway_token_input_total{model="${model}"} ${TOKENS_INP}
# TYPE api_gateway_token_output_total counter
api_gateway_token_output_total{model="${model}"} ${TOKENS_OUT}
# TYPE api_gateway_cost_total counter
api_gateway_cost_total{model="${model}",type="input"} ${COST_I_VAL}
api_gateway_cost_total{model="${model}",type="output"} ${COST_O_VAL}
PUSH

  PROF_REQ_A=$((PROF_REQ_A + RANDOM % 3 + 1))
  PROF_REQ_O=$((PROF_REQ_O + RANDOM % 3 + 1))
  PROF_INP_A=$((PROF_INP_A + RANDOM % 1500 + 50))
  PROF_INP_O=$((PROF_INP_O + RANDOM % 1200 + 40))
  PROF_OUT_A=$((PROF_OUT_A + RANDOM % 500 + 20))
  PROF_OUT_O=$((PROF_OUT_O + RANDOM % 400 + 15))

  for prof in "seed-anthropic:${PROF_REQ_A}:${PROF_INP_A}:${PROF_OUT_A}" \
              "seed-openai:${PROF_REQ_O}:${PROF_INP_O}:${PROF_OUT_O}"; do
    IFS=: read -r p_name p_req p_inp p_out <<< "$prof"
    pm="${model}"
    push "live_prof_${p_name}" <<PUSH
# TYPE api_gateway_profile_requests_total counter
api_gateway_profile_requests_total{profile="${p_name}",model="${pm}"} ${p_req}
# TYPE api_gateway_profile_token_input_total counter
api_gateway_profile_token_input_total{profile="${p_name}",model="${pm}"} ${p_inp}
# TYPE api_gateway_profile_token_output_total counter
api_gateway_profile_token_output_total{profile="${p_name}",model="${pm}"} ${p_out}
# TYPE api_gateway_profile_cost_total counter
api_gateway_profile_cost_total{profile="${p_name}",model="${pm}",type="input"} $(printf "%.6f" "$(echo "${p_inp}*3/1000000" | bc -l)")
api_gateway_profile_cost_total{profile="${p_name}",model="${pm}",type="output"} $(printf "%.6f" "$(echo "${p_out}*15/1000000" | bc -l)")
PUSH
  done

  MASK_ALL=$((MASK_ALL + RANDOM % 8 + 1))
  MASK_SEC=$((MASK_SEC + RANDOM % 3))
  MASK_PII=$((MASK_PII + RANDOM % 3))
  push "live_privacy" <<PUSH
# TYPE api_gateway_mask_requests_total counter
api_gateway_mask_requests_total{has_secrets="true"} ${MASK_SEC}
api_gateway_mask_requests_total{has_secrets="false"} ${MASK_ALL}
api_gateway_mask_requests_total{has_pii="true"} ${MASK_PII}
api_gateway_mask_requests_total{has_pii="false"} ${MASK_ALL}
# TYPE api_gateway_pii_detected_total counter
api_gateway_pii_detected_total{type="EMAIL_ADDRESS"} $(( MASK_PII + RANDOM % 5 ))
api_gateway_pii_detected_total{type="PHONE_NUMBER"} $(( MASK_PII + RANDOM % 3 ))
api_gateway_pii_detected_total{type="api_key"} $(( MASK_SEC + RANDOM % 2 ))
api_gateway_pii_detected_total{type="aws_secret"} $(( RANDOM % 3 ))
api_gateway_pii_detected_total{type="private_key"} $(( RANDOM % 2 ))
api_gateway_pii_detected_total{type="github_token"} $(( RANDOM % 4 ))
# TYPE api_gateway_secrets_detected_total counter
api_gateway_secrets_detected_total{type="api_key"} ${MASK_SEC}
api_gateway_secrets_detected_total{type="aws_secret"} $(( RANDOM % 2 ))
api_gateway_secrets_detected_total{type="private_key"} $(( RANDOM % 2 ))
PUSH

  OPTIM_C=$((OPTIM_C + RANDOM % 5 + 1))
  OPTIM_D=$((OPTIM_D + RANDOM % 5 + 1))
  OPTIM_S=$((OPTIM_S + RANDOM % 5 + 1))
  OPTIM_K=$((OPTIM_K + RANDOM % 5 + 1))
  CHARS_C=$((CHARS_C + RANDOM % 500 + 50))
  CHARS_D=$((CHARS_D + RANDOM % 400 + 40))
  CHARS_S=$((CHARS_S + RANDOM % 300 + 30))
  CHARS_K=$((CHARS_K + RANDOM % 450 + 45))
  TOKSAV_IN=$((TOKSAV_IN + RANDOM % 300 + 30))
  TOKSAV_OUT=$((TOKSAV_OUT + RANDOM % 100 + 10))

  push "live_optim" <<PUSH
# TYPE api_gateway_optimizer_runs_total counter
api_gateway_optimizer_runs_total{technique="chunker"} ${OPTIM_C}
api_gateway_optimizer_runs_total{technique="delta"} ${OPTIM_D}
api_gateway_optimizer_runs_total{technique="disclosure"} ${OPTIM_S}
api_gateway_optimizer_runs_total{technique="sketch"} ${OPTIM_K}
# TYPE api_gateway_optimizer_chars_saved_total counter
api_gateway_optimizer_chars_saved_total{technique="chunker",direction="input"} ${CHARS_C}
api_gateway_optimizer_chars_saved_total{technique="delta",direction="input"} ${CHARS_D}
api_gateway_optimizer_chars_saved_total{technique="disclosure",direction="input"} ${CHARS_S}
api_gateway_optimizer_chars_saved_total{technique="sketch",direction="input"} ${CHARS_K}
# TYPE api_gateway_optimizer_tokens_saved_total counter
api_gateway_optimizer_tokens_saved_total{direction="input"} ${TOKSAV_IN}
api_gateway_optimizer_tokens_saved_total{direction="output"} ${TOKSAV_OUT}
PUSH

  push "live_tech_chunker" <<PUSH
# TYPE api_gateway_chunker_chars_saved_total counter
api_gateway_chunker_chars_saved_total{technique="chunker"} ${CHARS_C}
PUSH
  push "live_tech_delta" <<PUSH
# TYPE api_gateway_delta_chars_saved_total counter
api_gateway_delta_chars_saved_total{technique="delta"} ${CHARS_D}
PUSH
  push "live_tech_disclosure" <<PUSH
# TYPE api_gateway_disclosure_chars_saved_total counter
api_gateway_disclosure_chars_saved_total{technique="disclosure"} ${CHARS_S}
PUSH
  push "live_tech_sketch" <<PUSH
# TYPE api_gateway_sketch_chars_saved_total counter
api_gateway_sketch_chars_saved_total{technique="sketch"} ${CHARS_K}
PUSH

  cost_savings=$(printf "%.6f" "$(echo "(${CHARS_C}+${CHARS_D}+${CHARS_S}+${CHARS_K})*0.75/1000000" | bc -l)")
  push "live_savings" <<PUSH
# TYPE api_gateway_cost_savings_total counter
api_gateway_cost_savings_total ${cost_savings}
PUSH

  WASTE_RP=$((WASTE_RP + RANDOM % 3))
  WASTE_EC=$((WASTE_EC + RANDOM % 2))
  WASTE_OSP=$((WASTE_OSP + RANDOM % 2))
  WASTETOK_RP=$((WASTETOK_RP + RANDOM % 300))
  WASTETOK_EC=$((WASTETOK_EC + RANDOM % 200))
  WASTETOK_OSP=$((WASTETOK_OSP + RANDOM % 500))
  push "live_waste" <<PUSH
# TYPE api_gateway_waste_findings_total counter
api_gateway_waste_findings_total{detector="repeated_phrases",severity="low"} ${WASTE_RP}
api_gateway_waste_findings_total{detector="repeated_phrases",severity="medium"} $(( WASTE_RP / 2 ))
api_gateway_waste_findings_total{detector="empty_context",severity="high"} ${WASTE_EC}
api_gateway_waste_findings_total{detector="oversized_system_prompt",severity="medium"} ${WASTE_OSP}
# TYPE api_gateway_waste_tokens_wasted_total counter
api_gateway_waste_tokens_wasted_total{detector="repeated_phrases"} ${WASTETOK_RP}
api_gateway_waste_tokens_wasted_total{detector="empty_context"} ${WASTETOK_EC}
api_gateway_waste_tokens_wasted_total{detector="oversized_system_prompt"} ${WASTETOK_OSP}
PUSH

  VISION_OK=$((VISION_OK + RANDOM % 3 + 1))
  VISION_FB=$((VISION_FB + RANDOM % 2))
  push "live_vision" <<PUSH
# TYPE api_gateway_vision_preanalysis_total counter
api_gateway_vision_preanalysis_total{status="success"} ${VISION_OK}
api_gateway_vision_preanalysis_total{status="fallback"} ${VISION_FB}
PUSH

  MCP_OK=$((MCP_OK + RANDOM % 4 + 1))
  MCP_ERR=$((MCP_ERR + RANDOM % 2))
  push "live_mcp" <<PUSH
# TYPE api_gateway_mcp_calls_total counter
api_gateway_mcp_calls_total{server="web-reader",tool="fetch",status="success"} ${MCP_OK}
api_gateway_mcp_calls_total{server="web-reader",tool="fetch",status="error"} ${MCP_ERR}
# TYPE api_gateway_mcp_cache_hits_total counter
api_gateway_mcp_cache_hits_total{server="web-reader",tool="fetch"} $(( MCP_OK / 2 ))
# TYPE api_gateway_mcp_cache_misses_total counter
api_gateway_mcp_cache_misses_total{server="web-reader",tool="fetch"} $(( MCP_OK / 2 + 1 ))
PUSH

  for bpath in go_direct sidecar direct; do
    push "live_billing_${bpath}" <<PUSH
# TYPE api_gateway_billing_path_requests_total counter
api_gateway_billing_path_requests_total{path="${bpath}",model="${model}",profile="seed-anthropic"} $(( RUN * 2 + RANDOM % 5 ))
PUSH
  done

  for rpath in /v1/messages /v1/auth/accounts /v1/profiles; do
    method="POST"
    [[ "$rpath" == "/v1/auth/accounts" || "$rpath" == "/v1/profiles" ]] && method="GET"
    push "live_lat_${rpath//\//_}" <<PUSH
# TYPE api_gateway_request_latency_seconds_count counter
api_gateway_request_latency_seconds_count{method="${method}",path="${rpath}",status="200"} $(( RUN * 3 ))
PUSH
  done

  [[ "${MAX_RUNS}" -gt 0 ]] && [[ "$RUN" -ge "$MAX_RUNS" ]] && break
  sleep "$INTERVAL"
done

log "Continuous seed stopped after ${RUN} iterations"
