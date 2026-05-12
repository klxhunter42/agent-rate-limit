#!/bin/sh
set -e

# Copy mitmproxy CA cert if MITM_PROXY_URL is set and cert doesn't exist
if [ -n "$MITM_PROXY_URL" ] && [ ! -f /tmp/mitmproxy-ca-cert.pem ]; then
  # Extract mitmweb hostname from proxy URL
  MITMHOST=$(echo "$MITM_PROXY_URL" | sed -E 's|https?://([^:/]+).*|\1|')
  for i in 1 2 3; do
    if curl -sf "http://$MITMHOST:8081/cert/cert" -o /tmp/mitmproxy-ca-cert.pem 2>/dev/null; then
      echo "mitmproxy CA cert downloaded from $MITMHOST"
      export SSL_CERT_FILE=/tmp/mitmproxy-ca-cert.pem
      break
    fi
    echo "waiting for mitmweb cert... ($i)"
    sleep 2
  done
fi

# Start Node.js sidecar in background
node /app/sidecar/index.js &
SIDECAR_PID=$!
echo "sidecar started (pid $SIDECAR_PID)"

# Give sidecar a moment to bind
sleep 0.5

# Start Go gateway in foreground
exec /app/api-gateway
