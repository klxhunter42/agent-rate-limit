# Troubleshooting

## Service Not Healthy

```bash
docker-compose ps                           # Check status
docker-compose logs <service> --tail 50     # View logs
docker-compose up -d --build <service>      # Rebuild
```

### Service Names

| Compose Service | Container Name | Role |
|---|---|---|
| `arl-gateway` | arl-gateway | API Gateway (Go) |
| `arl-rate-limiter` | arl-rate-limiter | Rate Limiter (Java/Spring) |
| `arl-dragonfly` | arl-dragonfly | Dragonfly (Redis-compatible) |
| `arl-worker` | arl-worker | AI Worker (Python) |
| `arl-prometheus` | arl-prometheus | Metrics collection |
| `arl-grafana` | arl-grafana | Dashboards |
| `arl-otel` | arl-otel | OpenTelemetry Collector |
| `arl-rl-dashboard` | arl-rl-dashboard | Rate Limiter Dashboard (React) |
| `arl-dashboard` | arl-dashboard | Main Dashboard (React/Vite) |
| `arl-proxy` | arl-proxy | Caddy reverse proxy (port 9000) |

## DOCKER_DEFAULT_PLATFORM

If you encounter error `platform (linux/amd64) does not match`:

```bash
unset DOCKER_DEFAULT_PLATFORM
# Or add to ~/.zshrc / ~/.bashrc
```

## arl-worker crash (SettingsError)

```bash
# Check .env for empty or incorrectly formatted values
cat .env | grep API_KEYS
# If not using a provider, delete that line or leave empty
```

## Rate Limiter Returns 403

```bash
# Check docker profile is active
docker exec arl-rate-limiter env | grep SPRING_PROFILES_ACTIVE
# Should return: SPRING_PROFILES_ACTIVE=docker
```

## Reset Everything

```bash
docker-compose down -v && docker-compose up -d --build
```

> **Warning**: `down -v` removes all volumes including Grafana dashboards and Dragonfly data.

## Port Reference

| Port | Service | External | Protocol |
|---|---|---|---|
| **9000** | arl-proxy (Caddy) | **Yes** | HTTP |
| 8080 | arl-gateway | No (internal) | HTTP |
| 8080 | arl-rate-limiter | No (internal) | HTTP |
| 6379 | arl-dragonfly (Redis) | No (internal) | TCP |
| 9090 | arl-prometheus | No (internal) | HTTP |
| 9090/9091 | arl-worker (metrics) | No (internal) | HTTP |
| 5173 | arl-dashboard (Vite dev) | No (internal) | HTTP |
| 3000 | arl-grafana | No (via Caddy /grafana) | HTTP |
| 4317/4318 | arl-otel (OTLP) | No (internal) | gRPC/HTTP |

All external traffic goes through arl-proxy (Caddy) on port 9000, which reverse-proxies to internal services. No other ports are published to the host.

## Testing Z.AI Vision (GLM Mode)

### Terminal Test (curl via Docker network)

Test a simple image request through the gateway from inside the Docker network:

```bash
# 1. Base64-encode an image
IMG_B64=$(base64 -i ~/Pictures/KLxPicture/devops.png | tr -d '\n')

# 2. Send via the proxy container (has access to arl-gateway)
ssh klxhunter@192.168.5.111 "docker exec arl-proxy curl -s -X POST \
  http://arl-gateway:8080/v1/messages \
  -H 'Content-Type: application/json' \
  -H 'x-api-key: YOUR_ZAI_API_KEY' \
  -H 'anthropic-version: 2023-06-01' \
  -d '{\"model\":\"glm-5.1\",\"max_tokens\":100,\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":[{\"type\":\"image\",\"source\":{\"type\":\"base64\",\"media_type\":\"image/png\",\"data\":\"$IMG_B64\"}},{\"type\":\"text\",\"text\":\"What do you see?\"}]}]}'"
```

For large payloads (>100KB), write to file first to avoid shell escaping issues:

```bash
# 1. Generate payload with Python
python3 -c "
import json, base64
img = base64.b64encode(open('$HOME/Pictures/KLxPicture/devops.png','rb').read()).decode()
payload = {
    'model': 'glm-5.1',
    'max_tokens': 100,
    'stream': True,
    'messages': [{'role': 'user', 'content': [
        {'type': 'image', 'source': {'type': 'base64', 'media_type': 'image/png', 'data': img}},
        {'type': 'text', 'text': 'What do you see?'}
    ]}]
}
json.dump(payload, open('/tmp/test_payload.json','w'))
"

# 2. Copy to server and into Docker container
scp /tmp/test_payload.json klxhunter@192.168.5.111:/tmp/test_payload.json
ssh klxhunter@192.168.5.111 "docker cp /tmp/test_payload.json arl-proxy:/tmp/test_payload.json"

# 3. Send from inside the proxy container
ssh klxhunter@192.168.5.111 'docker exec arl-proxy curl -s -X POST \
  http://arl-gateway:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: YOUR_ZAI_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -d @/tmp/test_payload.json'
```

### Terminal Test (direct to Z.AI)

Bypass the gateway to test if Z.AI itself accepts the payload:

```bash
curl -s -X POST https://api.z.ai/api/anthropic/v1/messages \
  -H 'Content-Type: application/json' \
  -H 'x-api-key: YOUR_ZAI_API_KEY' \
  -H 'anthropic-version: 2023-06-01' \
  -d '{"model":"glm-4.6v","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"Say hi"}]}'
```

### VSCode Claude Code Panel Test

1. Open VSCode with Claude Code extension configured to use the gateway proxy
2. Open the Claude Code panel (sidebar or command palette)
3. Attach an image: drag & drop or paste from clipboard (e.g., `~/Pictures/KLxPicture/*.png`)
4. Type a prompt like "Describe this image" and send
5. Check gateway logs for the result:

```bash
ssh klxhunter@192.168.5.111 "docker logs arl-gateway --tail 30 2>&1 | grep -v health\|metrics"
```

**What to look for in logs:**
- `strip debug` - confirms field stripping ran (all should be `false`)
- `content analysis` - shows all content block types and extra keys
- `vision model auto-selected` - confirms glm-5.1 -> glm-4.6v routing
- `vision via anthropic endpoint` - shows final payload_keys and body_preview
- `upstream error response` with code `1210` - Z.AI rejected the request

**Error 1210 (Invalid API parameter)** means Z.AI received a field it does not support. The gateway strips these fields automatically:
- Top-level: `tools`, `tool_choice`, `thinking`, `budget_tokens`, `effort`, `stream_options`, `metadata`, `output_config`, `context_management`, `service_tier`
- Content blocks: `server_tool_use` type, `cache_control` on any block

If 1210 still occurs, the `content analysis` log will show exactly which block types and extra keys remain in the payload.

