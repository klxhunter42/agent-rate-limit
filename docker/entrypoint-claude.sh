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
    export ANTHROPIC_AUTH_TOKEN="$TOKEN"
    echo "Token provisioned: ${TOKEN:0:12}..."
    CLAUDE_DIR="$HOME/.claude"
    if [ ! -f "$HOME/.claude.json" ]; then
      echo '{"hasCompletedOnboarding":true}' > "$HOME/.claude.json"
    else
      python3 -c "
import json
with open('$HOME/.claude.json','r') as f: d=json.load(f)
d['hasCompletedOnboarding']=True
with open('$HOME/.claude.json','w') as f: json.dump(d,f)
" 2>/dev/null || true
    fi
    MODEL="${DEFAULT_MODEL:-}"
    if [ -z "$MODEL" ]; then
      MODEL=$(python3 -c "import json; print(json.load(open('$HOME/.claude/settings.json')).get('model',''))" 2>/dev/null || true)
    fi
    THINKING=$(python3 -c "import json; v=json.load(open('$HOME/.claude/settings.json')).get('alwaysThinkingEnabled',''); print(str(v).lower() if v!='' else '')" 2>/dev/null || true)
    MODEL_JSON=""
    if [ -n "$MODEL" ]; then MODEL_JSON=",\"model\":\"$MODEL\""; fi
    THINKING_JSON=""
    if [ -n "$THINKING" ]; then THINKING_JSON=",\"alwaysThinkingEnabled\":$THINKING"; fi
    AUTH_TOKEN_JSON=""
    USER_OAUTH=""
    if [ -n "$CLAUDE_OAUTH_TOKEN" ]; then
      USER_OAUTH="$CLAUDE_OAUTH_TOKEN"
    elif [ -f "$HOME/.claude/credentials.json" ]; then
      USER_OAUTH=$(python3 -c "
import json
try:
    d = json.load(open('$HOME/.claude/credentials.json'))
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
    mkdir -p "$HOME/.claude"
    BASE_URL="${ANTHROPIC_BASE_URL:-http://arl-proxy:9000}"
    export _CFG_TOKEN="$TOKEN" _CFG_BASE_URL="$BASE_URL" _CFG_HOME="$HOME"
    export _CFG_AUTH="$AUTH_TOKEN_JSON" _CFG_MODEL="$MODEL" _CFG_THINKING="$THINKING"
    python3 <<PYEOF
import json, os, re

# Read existing mounted settings.json and merge
home = os.environ["_CFG_HOME"]
settings_path = os.path.join(home, ".claude", "settings.json")
try:
    with open(settings_path, "r") as f:
        s = json.load(f)
except:
    s = {}

# Ensure env dict exists and merge our values
if "env" not in s:
    s["env"] = {}
s["env"]["ANTHROPIC_BASE_URL"] = os.environ["_CFG_BASE_URL"]
s["env"]["ANTHROPIC_API_KEY"] = os.environ["_CFG_TOKEN"]
auth = os.environ.get("_CFG_AUTH", "")
if auth:
    m = re.match(r',?"([^"]+)":"([^"]*)"', auth)
    if m:
        s["env"][m.group(1)] = m.group(2)

model = os.environ.get("_CFG_MODEL", "")
if model:
    s["model"] = model
thinking = os.environ.get("_CFG_THINKING", "")
if thinking:
    s["alwaysThinkingEnabled"] = thinking.lower() == "true"

with open(settings_path, "w") as f:
    json.dump(s, f, indent=2)
    f.write("\n")
PYEOF
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
