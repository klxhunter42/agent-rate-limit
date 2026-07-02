#!/bin/sh
set -e

# Copy mitmproxy CA cert if MITM_PROXY_URL is set and cert doesn't exist
if [ -n "$MITM_PROXY_URL" ] && [ ! -f /tmp/mitmproxy-ca-cert.pem ]; then
  # The CA cert is served by the onboarding addon at http://mitm.it/cert/pem,
  # reachable only THROUGH the proxy (not as a direct HTTP request). web_password
  # auth does not cover this endpoint, so no Bearer header is needed.
  for i in 1 2 3; do
    if curl -sf --proxy "$MITM_PROXY_URL" "http://mitm.it/cert/pem" -o /tmp/mitmproxy-ca-cert.pem 2>/dev/null; then
      echo "mitmproxy CA cert downloaded via $MITM_PROXY_URL"
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
