# Agent Rate Limit Gateway -- Documentation

## Setup & Configuration
- [Getting Started](docs/01-getting-started.md) -- Architecture, traffic flow, tech stack, installation, environment variables, quick start, ports
- [Claude Code Guide](docs/02-claude-code.md) -- Setup, architecture, tool loop, compatibility, rate limits, known limitations
- [API Reference](docs/03-api-reference.md) -- All API endpoints (sync/async, auth, providers, profiles, admin, health)
- [Providers](docs/04-providers.md) -- Provider registry, Claude OAuth flow, all supported providers

## Routing & Observability
- [Profile-Based Routing](docs/05-routing.md) -- Profile configuration, model mapping, target selection
- [Dashboards](docs/06-dashboards.md) -- Grafana dashboards, rate limiter UI, gateway admin UI
- [Observability](docs/07-observability.md) -- Prometheus metrics, cost calculator, key metrics
- [Docker Operations](docs/08-docker-ops.md) -- Docker commands, build, deployment

## Features
- [Features](docs/09-features.md) -- Vision auto-routing, multi-agent modes, message body optimization
- [Privacy & Security](docs/11-privacy-security.md) -- PasteGuard PII detection, streaming unmask bugs, GLM mode isolation
- [Z.AI Vision Routing](docs/12-zai.md) -- Image format bug fix, filterUnsupportedContent changes
- [Troubleshooting](docs/13-troubleshooting.md) -- Common issues, service health, reset procedures

## Legacy Docs (superseded by numbered guides above)
- [architecture.md](docs/architecture.md) -- Original architecture documentation
- [claude-code-proxy.md](docs/claude-code-proxy.md) -- Original Claude Code proxy guide
- [claude-oauth-deep-dive.md](docs/claude-oauth-deep-dive.md) -- Detailed Claude OAuth research
- [transparent-passthrough.md](docs/transparent-passthrough.md) -- Transparent passthrough mode
- [providers.md](docs/providers.md) -- Original provider documentation
- [features.md](docs/features.md) -- Original features documentation
- [known-issues.md](docs/known-issues.md) -- Known issues and solutions
