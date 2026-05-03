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
