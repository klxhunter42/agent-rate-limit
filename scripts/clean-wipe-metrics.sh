#!/usr/bin/env bash
# clean-wipe-metrics.sh - wipe ALL seeded metrics from Prometheus + Pushgateway
# Usage: ./scripts/clean-wipe-metrics.sh
set -euo pipefail

log() { echo "[$(date +%H:%M:%S)] $*"; }

# 1. Kill any running seed processes
log "Killing seed processes..."
pkill -f continuous-seed 2>/dev/null || true
pkill -f seed-grafana-data 2>/dev/null || true
sleep 1

# 2. Wipe Pushgateway data
log "Wiping Pushgateway data..."
docker exec arl-pushgateway wget -qO- --method=PUT "http://localhost:9091/api/v1/admin/wipe" 2>/dev/null || true

# 3. Stop Prometheus
log "Stopping Prometheus..."
docker stop arl-prometheus

# 4. Wipe Prometheus TSDB data
log "Wiping Prometheus TSDB data..."
PROM_VOL=$(docker inspect arl-prometheus --format '{{range .Mounts}}{{if eq .Destination "/prometheus"}}{{.Source}}{{end}}{{end}}')
if [[ -n "$PROM_VOL" ]]; then
  docker run --rm -v "${PROM_VOL}:/data" alpine sh -c "rm -rf /data/*"
  log "Prometheus volume wiped: ${PROM_VOL}"
else
  log "WARNING: Could not find Prometheus volume, skipping TSDB wipe"
fi

# 5. Restart Prometheus
log "Starting Prometheus..."
docker start arl-prometheus

# 6. Wait for Prometheus to be healthy
log "Waiting for Prometheus to be ready..."
for i in $(seq 1 30); do
  if docker exec arl-prometheus wget -qO- "http://localhost:9090/api/v1/query?query=up" 2>/dev/null | grep -q "success"; then
    log "Prometheus is ready"
    break
  fi
  sleep 2
done

log "Restarting Grafana to clear cache..."
docker restart arl-grafana

log "Done! All seeded metrics wiped. Dashboards will show only real gateway data."
