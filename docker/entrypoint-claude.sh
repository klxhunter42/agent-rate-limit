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
    # Rewrite settings with API key injected.
    cat > /root/.claude/settings.json <<SETTINGS
{"env":{"ANTHROPIC_BASE_URL":"http://arl-gateway:8080","ANTHROPIC_API_KEY":"$TOKEN"}}
SETTINGS
    echo "Settings updated"
  else
    echo "WARNING: Failed to provision token"
  fi
fi

exec claude "$@"
