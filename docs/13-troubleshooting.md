# Troubleshooting

## Service Not Healthy

```bash
docker-compose ps                           # Check status
docker-compose logs <service> --tail 50     # View logs
docker-compose up -d --build <service>      # Rebuild
```

## DOCKER_DEFAULT_PLATFORM

If you encounter error `platform (linux/amd64) does not match`:

```bash
unset DOCKER_DEFAULT_PLATFORM
# Or add to ~/.zshrc / ~/.bashrc
```

## ai-worker crash (SettingsError)

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
| **8080** | API Gateway | Yes | HTTP |
| **8081** | Rate Limiter UI | Yes | HTTP |
| **3000** | Grafana | Yes | HTTP |
| **6379** | Dragonfly (Redis) | No | TCP |
| **6380** | Dragonfly (Redis) Admin | No | HTTP |
| **9090** | Prometheus | No | HTTP |
| **6479** | Dragonfly Sentinel | No | TCP |
