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
- [Retry & MCP Servers](docs/14-retry-mcp-servers.md) -- Upstream retry logic, Z.AI MCP server reference, quota, fallback strategies

## Implementation Specs (re-implementable from these)
- [01 Proxy Layer](docs/spec/01-proxy-layer.md) -- All proxy types, SSE streaming, format conversion, retry/error matrix, auto-continuation
- [02 Handler & Routing](docs/spec/02-handler-routing.md) -- Request lifecycle, auth flow, profile resolution, rate limiting, provider fallback
- [03 Optimizer & Privacy](docs/spec/03-optimizer-privacy.md) -- 13-stage optimizer pipeline, PII/secret detection, streaming unmask, budget management
- [04 Infra & Config](docs/spec/04-infra-config.md) -- Startup sequence, 50+ env vars, Docker services, sidecar, UI, metrics
- [05 Data Models & API](docs/spec/05-data-models-api.md) -- All structs/schemas, API contracts, 22 Redis key patterns, rate limit data structures

## Legacy Docs (superseded by numbered guides above)
- [architecture.md](docs/architecture.md) -- Original architecture documentation
- [claude-code-proxy.md](docs/claude-code-proxy.md) -- Original Claude Code proxy guide
- [claude-oauth-deep-dive.md](docs/claude-oauth-deep-dive.md) -- Detailed Claude OAuth research
- [transparent-passthrough.md](docs/transparent-passthrough.md) -- Transparent passthrough mode
- [providers.md](docs/providers.md) -- Original provider documentation
- [features.md](docs/features.md) -- Original features documentation
- [known-issues.md](docs/known-issues.md) -- Known issues and solutions
