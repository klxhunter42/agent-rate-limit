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
    if [ ! -f /root/.claude.json ]; then
      echo '{"hasCompletedOnboarding":true}' > /root/.claude.json
    else
      python3 -c "
import json
with open('/root/.claude.json','r') as f: d=json.load(f)
d['hasCompletedOnboarding']=True
with open('/root/.claude.json','w') as f: json.dump(d,f)
" 2>/dev/null || true
    fi
    MODEL=$(python3 -c "import json; print(json.load(open('/root/.claude/settings.json')).get('model',''))" 2>/dev/null || true)
    THINKING=$(python3 -c "import json; v=json.load(open('/root/.claude/settings.json')).get('alwaysThinkingEnabled',''); print(str(v).lower() if v!='' else '')" 2>/dev/null || true)
    MODEL_JSON=""
    if [ -n "$MODEL" ]; then MODEL_JSON=",\"model\":\"$MODEL\""; fi
    THINKING_JSON=""
    if [ -n "$THINKING" ]; then THINKING_JSON=",\"alwaysThinkingEnabled\":$THINKING"; fi
    AUTH_TOKEN_JSON=""
    USER_OAUTH=""
    if [ -n "$CLAUDE_OAUTH_TOKEN" ]; then
      USER_OAUTH="$CLAUDE_OAUTH_TOKEN"
    elif [ -f /root/.claude/credentials.json ]; then
      USER_OAUTH=$(python3 -c "
import json
try:
    d = json.load(open('/root/.claude/credentials.json'))
    for k in ('oauthToken','accessToken','token'):
        if d.get(k): print(d[k]); break
except: pass
" 2>/dev/null || true)
    fi
    if [ -n "$USER_OAUTH" ] && [ "${USER_OAUTH:0:4}" != "arl_" ]; then
      AUTH_TOKEN_JSON=",\"ANTHROPIC_AUTH_TOKEN\":\"$USER_OAUTH\""
      export CLAUDE_CODE_OAUTH_TOKEN="$USER_OAUTH"
      echo "User OAuth token mounted for passthrough auth"
    fi
    mkdir -p /root/.claude
    cat > /root/.claude/settings.json <<SETTINGS
{"env":{"ANTHROPIC_BASE_URL":"http://arl-proxy:9000","ANTHROPIC_API_KEY":"$TOKEN"${AUTH_TOKEN_JSON}}${MODEL_JSON}${THINKING_JSON}}
SETTINGS
    echo "Settings updated"
  else
    echo "WARNING: Failed to provision token"
  fi
fi

# Detect TTY: use full interactive mode only when TTY is attached.
if [ ! -t 0 ] && [ "${CLAUDE_CODE_SIMPLE:-}" != "0" ]; then
  export CLAUDE_CODE_SIMPLE=1
fi

# If first arg is a command other than claude flags, run it directly.
case "$1" in
  claude|-*|"")
    exec claude "$@"
    ;;
  *)
    exec "$@"
    ;;
esac
