# Agent Rate Limit - Complete System Documentation

> Generated: 2026-05-04 | 10 agents | 8,994 lines across 10 documents

---

## System Architecture Overview

```
                              +------------------+
                              |    Client SDK    |
                              | (Claude Code,    |
                              |  API consumers)  |
                              +--------+---------+
                                       |
                                       v
+====================================================================+
|                        API Gateway (Go)                             |
|                        :8080 / :8443                                |
|                                                                    |
|  +------------------+    +------------------+    +---------------+ |
|  | Middleware Chain  |    | 13-Stage         |    | Privacy Guard | |
|  | (14 middleware)   |    | Optimizer        |    | (PasteGuard)  | |
|  | - Auth           |    | Pipeline         |    | - Secrets     | |
|  | - Rate Limit     |    | - Chunker        |    | - PII         | |
|  | - CORS           |    | - Delta          |    +---------------+ |
|  | - Metrics        |    | - Summarizer     |                      |
|  | - Adaptive Limit |    | - Caveman        |    +---------------+ |
|  | - Anomaly Detect |    | - Packer         |    | Rate Limiter  | |
|  +--------+---------+    | - Sketch         |    | (Bandit/      | |
|           |              | - Bandit         |    |  Sketch/      | |
|           v              | - Prefetcher     |    |  Queue)       | |
|  +--------+---------+    | - Warm Start     |    +---------------+ |
|  | Provider Resolver  |  | - Waste Detect   |                      |
|  | (19 providers)     |  +--------+---------+                      |
|  | - Anthropic        |           |                                |
|  | - OpenAI           |           v                                |
|  | - Gemini           |  +--------+---------+    +---------------+ |
|  | - Z.AI/GLM         |  | Upstream Proxy    |    | Dragonfly     | |
|  | - OpenRouter       |  | (8 proxy types)   |    | (Redis-compat)| |
|  +--------+---------+  | - Stream SSE      |    | - Queue       | |
|           |             | - Non-stream      |    | - Cache       | |
|           v             | - Vision/images   |    | - State store | |
|  +--------+---------+  +-------------------+    +---------------+ |
|  | 30+ HTTP Routes    |                                           |
|  | /v1/messages       |    +-------------------+                  |
|  | /v1/chat/completions   | Metrics (42+)     |                  |
|  | /health, /metrics   |   | Prometheus        |                  |
|  +--------------------+   +--------+----------+                  |
+=====================================|==============================+
                                      |
                    +-----------------+-----------------+
                    |                 |                 |
                    v                 v                 v
          +--------+------+  +------+------+  +-------+--------+
          |  Anthropic    |  |   OpenAI   |  |    Gemini      |
          |  API          |  |   API      |  |    API         |
          +---------------+  +------------+  +----------------+
                    ^
                    |  (also Z.AI, OpenRouter)
                    +

+====================================================================+
|                    Supporting Services                              |
|                                                                    |
|  +-----------------+  +------------------+  +------------------+   |
|  | AI Worker       |  | Distributed      |  | UI Dashboard     |   |
|  | (Python)        |  | Rate Limiter     |  | (Vite + React 19)|   |
|  | - 6 providers   |  | (Java/Spring)    |  | - 14 pages       |   |
|  | - Key rotation  |  | - 5 algorithms   |  | - 12 hooks       |   |
|  | - Prometheus    |  | - 30+ endpoints  |  | - WebSocket      |   |
|  +-----------------+  +------------------+  +------------------+   |
|                                                                    |
|  +-----------------+  +------------------+  +------------------+   |
|  | OAuth/PKCE      |  | Grafana (10      |  | OTEL Collector   |   |
|  | - Token refresh |  | dashboards)      |  | - Traces         |   |
|  | - Billing route |  | - Provisioned    |  | - Metrics        |   |
|  +-----------------+  +------------------+  +------------------+   |
+====================================================================+

+====================================================================+
|                    Infrastructure                                   |
|                                                                    |
|  Docker Compose (13 services)                                      |
|  Helm Chart (12 components, K8s ready)                             |
|  CI/CD: GitHub Actions (6 images, amd64+arm64)                     |
|  Registry: GHCR                                                    |
+====================================================================+
```

---

## Documentation Index

| # | Document | Lines | Description |
|---|----------|-------|-------------|
| 15 | [System Index](15-SYSTEM-INDEX.md) | 130 | Master index + full system architecture diagram |
| 16 | [API Gateway Core](16-api-gateway-core.md) | 1,086 | Main entry, HTTP handler (30+ routes), proxy layer (8 types), middleware chain (14), config (50+ env vars), request flow diagrams |
| 17 | [Provider System](17-provider-system.md) | 1,031 | 19 built-in providers, resolver algorithm, OAuth flows (PKCE), token refresh (30min cycle), Redis key schema, 22 HTTP endpoints |
| 18 | [Token Pipeline](18-token-pipeline.md) | 775 | Chunker (Rabin-Karp), tokenizer (content-aware estimation), packer (knapsack), delta (LCS), summarizer, waste (7 heuristics), prefetcher (Markov), warmstart (cosine sim) |
| 19 | [Rate Limiting](19-rate-limiting.md) | 633 | Bandit (LinUCB), sketch (SimHash), queue (Dragonfly), caveman (4-tier injection), adaptive limiter (Envoy gradient), 3-layer architecture |
| 20 | [Cache, Privacy, Filter](20-cache-privacy-filter.md) | 682 | Cache (ROI eviction), PasteGuard (secrets + PII masking), filter (intent classification), disclosure (progressive), integration diagram |
| 21 | [AI Worker](21-ai-worker.md) | 561 | Python worker, Dragonfly producer/consumer, 6 providers, key manager, retry with backoff, 10 Prometheus metrics |
| 22 | [Distributed Rate Limiter](22-distributed-rate-limiter.md) | 1,118 | Java/Spring Boot, 5 rate limiting algorithms, Redis Lua scripts, 30+ endpoints, K8s HPA/PDB |
| 23 | [UI Dashboard](23-ui-dashboard.md) | 1,014 | Vite + React 19 SPA, 14 pages, 4 context providers, 12 hooks, polling + WebSocket, Prometheus analytics |
| 24 | [Infra & Deployment](24-infra-deployment.md) | 1,095 | Docker Compose (13 services), Helm chart (12 components), observability stack (Prometheus/OTEL/Grafana), CI/CD, sidecar, 80+ env vars |
| 25 | [OAuth, Metrics, Improvements](25-oauth-metrics-improvements.md) | 999 | OAuth PKCE flow (28 requests), 42+ metrics catalog, 7 improvement projects, billing path routing, integration diagrams |

### Thai Summary

| File | Description |
|------|-------------|
| [token-optimization-th.md](token-optimization-th.md) | สรุปภาษาไทย 16 module + pipeline flow diagram + 3-layer rate limiting |

---

## Quick Reference

### Tech Stack

| Component | Technology | Port |
|-----------|-----------|------|
| API Gateway | Go 1.24 | 8080, 8443 |
| AI Worker | Python 3.12 | 8090 |
| Distributed Rate Limiter | Java 21 / Spring Boot 3 | 8081 |
| UI Dashboard | Vite + React 19 | 3000 (dev) |
| Cache/Queue | Dragonfly (Redis-compat) | 6379 |
| Metrics | Prometheus | 9090 |
| Dashboards | Grafana | 3001 |
| Tracing | OpenTelemetry Collector | 4317 |
| PII Detection | Microsoft Presidio | 5001 |
| Proxy | Squid | 3128 |

### Key Metrics (42+)

Prefix: `api_gateway_*`

- `bandit_selections_total`, `bandit_reward_total`
- `chunker_chunks_total`, `chunker_cache_hit_rate`
- `delta_encodes_total`, `delta_chars_saved_total`
- `caveman_compressions_total`, `caveman_compression_ratio`
- `sketch_checks_total`, `sketch_hamming_similarity`
- `waste_findings_total`, `waste_tokens_wasted_total`
- `prefetcher_predictions_total`
- `warmstart_sessions_warmed_total`
- `packer_items_packed_total`, `packer_tokens_saved_total`
- `summarizer_calls_total`, `summarizer_chars_saved_total`
- + 25 more

### Redis Key Patterns

| Pattern | TTL | Module |
|---------|-----|--------|
| `bandit:state:<armID>` | 24h | Bandit |
| `sketch:recent:<session>` | 24h | Sketch |
| `chunker:stable:<hash>` | 24h | Chunker |
| `delta:baseline:<key>` | 24h | Delta |
| `summarizer:cache:<hash>` | 1h | Summarizer |
| `prefetcher:chain:<session>` | 4h | Prefetcher |
| `warmstart:sig:<project>:<session>` | 7d | Warm Start |
| `result:<requestID>` | 10m | Queue |
| `token:*`, `idx:*` | varies | Provider/OAuth |
