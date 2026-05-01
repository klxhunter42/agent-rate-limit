#!/bin/sh
set -e

# Start Node.js sidecar in background
node /app/sidecar/index.js &
SIDECAR_PID=$!
echo "sidecar started (pid $SIDECAR_PID)"

# Give sidecar a moment to bind
sleep 0.5

# Start Go gateway in foreground
exec /app/api-gateway
