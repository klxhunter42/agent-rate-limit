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
    echo "Token provisioned: ${TOKEN:0:12}..."

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

    mkdir -p "$HOME/.claude"
    BASE_URL="${ANTHROPIC_BASE_URL:-http://arl-proxy:9000}"
    export _CFG_TOKEN="$TOKEN"

    python3 -c "
import json, os
settings_path = os.path.join(os.path.expanduser('~'), '.claude', 'settings.json')
with open(settings_path) as f: s = json.load(f)
if 'env' not in s: s['env'] = {}
s['env']['ANTHROPIC_BASE_URL'] = '$BASE_URL'
s['env']['ANTHROPIC_AUTH_TOKEN'] = os.environ.get('_CFG_TOKEN', '')
for k in ('ANTHROPIC_API_KEY',): s['env'].pop(k, None)
with open(settings_path, 'w') as f: json.dump(s, f, indent=2); f.write('\n')
print('Settings updated')
" 2>&1
    export ANTHROPIC_AUTH_TOKEN="$TOKEN" ANTHROPIC_BASE_URL="$BASE_URL"
    echo "Done with entrypoint"
  else
    echo "WARNING: Failed to provision token"
  fi
fi

if [ ! -t 0 ] && [ "${CLAUDE_CODE_SIMPLE:-}" != "0" ]; then
  export CLAUDE_CODE_SIMPLE=1
fi

case "$1" in
  claude|-*|"")
    exec claude "$@"
    ;;
  *)
    exec "$@"
    ;;
esac
