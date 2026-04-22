#!/bin/bash
set -e

echo "Waiting for gateway..."
until curl -sf http://arl-gateway:8080/health > /dev/null 2>&1; do
  sleep 2
done
echo "Gateway is up"

if [ -n "$PROFILE_NAME" ]; then
  echo "Auto-provisioning token for profile: $PROFILE_NAME"
  RESP=$(curl -sf -X POST "http://arl-gateway:8080/v1/profiles/${PROFILE_NAME}/tokens" \
    -H 'Content-Type: application/json' \
    -d "{\"keyName\":\"docker-$(hostname)\",\"expiresIn\":86400}" 2>/dev/null || echo '{}')
  TOKEN=$(echo "$RESP" | grep -o 'arl_[a-f0-9]*' | head -1)
  if [ -n "$TOKEN" ]; then
    export ANTHROPIC_API_KEY="$TOKEN"
    echo "Token provisioned: ${TOKEN:0:12}..."
    # Preserve model and thinking settings from original settings, rewrite API key.
    MODEL=$(python3 -c "import json; print(json.load(open('/root/.claude/settings.json')).get('model',''))" 2>/dev/null || true)
    THINKING=$(python3 -c "import json; v=json.load(open('/root/.claude/settings.json')).get('alwaysThinkingEnabled',''); print(str(v).lower() if v!='' else '')" 2>/dev/null || true)
    MODEL_JSON=""
    if [ -n "$MODEL" ]; then MODEL_JSON=",\"model\":\"$MODEL\""; fi
    THINKING_JSON=""
    if [ -n "$THINKING" ]; then THINKING_JSON=",\"alwaysThinkingEnabled\":$THINKING"; fi
    cat > /root/.claude/settings.json <<SETTINGS
{"env":{"ANTHROPIC_BASE_URL":"http://arl-gateway:8080","ANTHROPIC_API_KEY":"$TOKEN","CLAUDE_CODE_USE_BEDROCK":"0","CLAUDE_CODE_USE_VERTEX":"0"}${MODEL_JSON}${THINKING_JSON}}
SETTINGS
    echo "Settings updated"
  else
    echo "WARNING: Failed to provision token"
  fi
fi

exec claude "$@"
