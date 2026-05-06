# Retry Logic & Z.AI MCP Server Reference

## 1. Upstream Retry System

Gateway has a multi-layer retry system for upstream failures. All retries use quadratic backoff capped at 5 minutes.

### 1.1 Retry Configuration

| Parameter             | Env Var                  | Default | Source                                       |
|-----------------------|--------------------------|---------|----------------------------------------------|
| Max 429 retries       | `UPSTREAM_MAX_RETRIES`   | 3       | [config.go](../api-gateway/config/config.go) |
| Backoff base          | `UPSTREAM_RETRY_BACKOFF` | 500ms   | [config.go](../api-gateway/config/config.go) |
| Max transient retries | `TRANSIENT_RETRY_MAX`    | 3       | [config.go](../api-gateway/config/config.go) |
| Auto-truncate         | `ENABLE_AUTO_TRUNCATE`   | true    | [config.go](../api-gateway/config/config.go) |

Backoff formula: `backoff = base * attempt^2` (capped at 5 min)

With defaults (500ms base): retry 1 = 500ms, retry 2 = 2s, retry 3 = 4.5s

### 1.2 Retry Categories

#### A. 429 Rate Limit Retry

- Up to `UPSTREAM_MAX_RETRIES` (3) retries on HTTP 429
- On each 429, calls `OnRateLimitError` to rotate API key from pool
- Implemented in: [anthropic.go](../api-gateway/proxy/anthropic.go), [openai.go](../api-gateway/proxy/openai.go), [gemini-apikey.go](../api-gateway/proxy/gemini-apikey.go)

#### B. Transient Error Retry

- Up to `TRANSIENT_RETRY_MAX` (3) retries on HTTP 500, 502, 503, 529
- Also retries HTTP 400 with body containing `"code":"1234"` or `"internal network failure"` (Z.AI transient glitch)
- Also retries HTTP 200 with empty or malformed body
- Implemented in: [anthropic.go](../api-gateway/proxy/anthropic.go), [openai.go](../api-gateway/proxy/openai.go), [gemini-apikey.go](../api-gateway/proxy/gemini-apikey.go)

#### C. Context Window Truncation Retry

- HTTP 413, or 400/422 with context-length error messages
- Auto-truncates conversation (preserves tool_use/tool_result pair boundaries)
- Limited to 1 truncation attempt per request
- Error classification: [recovery.go](../api-gateway/proxy/recovery.go)

#### D. OAuth Token Refresh Retry

- HTTP 401 triggers single token refresh via `OnAuthError`, then retries once
- Implemented in: [anthropic.go](../api-gateway/proxy/anthropic.go)
- Callback wiring: [handler.go](../api-gateway/handler/handler.go)

### 1.3 Total Retry Budget

```
maxAttempts = UpstreamMaxRetries + 1 + TransientRetryMax
            = 3 + 1 + 3 = 7 total attempts
```

### 1.4 Retry Budget Per Proxy

| Proxy                   | 429 Retries | Transient Retries | Truncate | Special                     |
|-------------------------|-------------|-------------------|----------|-----------------------------|
| Anthropic (transparent) | 3           | 3                 | 1        | Key rotation, token refresh |
| OpenAI                  | 3           | 3                 | 0        | max_tokens reduction        |
| GeminiAPI               | 3           | 3                 | 0        | none                        |
| Vision                  | 3           | 0                 | 0        | none                        |

### 1.5 Metrics

```
upstream_retries_total{...}       -- counts all retry attempts
upstream_429_total{...}           -- counts 429 responses
upstream_transient_retries{...}   -- counts transient retry attempts
```

Source: [metrics.go](../api-gateway/metrics/metrics.go)

---

## 2. Z.AI MCP Servers

Z.AI provides 4 MCP servers under the GLM Coding Plan. Three are remote (hosted on Z.AI infrastructure), one is local (runs via npx).

### 2.1 Server Summary

| Server         | Transport         | Endpoint / Package                              | Tools            | Status                      |
|----------------|-------------------|-------------------------------------------------|------------------|-----------------------------|
| **Web Search** | Remote (HTTP/SSE) | `https://api.z.ai/api/mcp/web_search_prime/mcp` | `webSearchPrime` | Broken (upstream)           |
| **Web Reader** | Remote (HTTP/SSE) | `https://api.z.ai/api/mcp/web_reader/mcp`       | `webReader`      | Working                     |
| **Vision**     | Local (stdio/npx) | `@z_ai/mcp-server` (npm)                        | 8 vision tools   | Working (needs Node.js 22+) |
| **Zread**      | Remote (HTTP/SSE) | `https://api.z.ai/api/mcp/zread/mcp`            | 3 GitHub tools   | Working                     |

### 2.2 Web Search (`web_search_prime`)

- **Type**: Remote MCP server on Z.AI infrastructure
- **Endpoint**: `https://api.z.ai/api/mcp/web_search_prime/mcp`
- **Auth**: Bearer token (Z.AI API key)
- **Tool**: `webSearchPrime` - returns page titles, URLs, summaries, site names, icons
- **Status**: BROKEN - all queries return `"JSON Parse error: Unexpected EOF"`. Upstream provider issue.
- **Doc ref**: https://docs.z.ai/devpack/mcp/search-mcp-server.md
- **Note**: Not supported in Goose client (known issue)

### 2.3 Web Reader (`web_reader`)

- **Type**: Remote MCP server on Z.AI infrastructure
- **Endpoint**: `https://api.z.ai/api/mcp/web_reader/mcp`
- **Auth**: Bearer token (Z.AI API key)
- **Tool**: `webReader` - fetches complete webpage content (title, body, metadata, links)
- **Status**: Working
- **Limitations**: Anti-scraping pages may return empty results
- **Doc ref**: https://docs.z.ai/devpack/mcp/reader-mcp-server.md

### 2.4 Vision (`@z_ai/mcp-server`)

- **Type**: Local MCP server (runs via npx)
- **NPM Package**: `@z_ai/mcp-server` (version >= 0.1.2)
- **Requires**: Node.js >= v22.0.0, `Z_AI_API_KEY` env var, `Z_AI_MODE=ZAI` env var
- **Status**: Working (local execution)

**8 Tools:**

| Tool                           | Purpose                                        |
|--------------------------------|------------------------------------------------|
| `ui_to_artifact`               | Convert UI screenshots to code, prompts, specs |
| `extract_text_from_screenshot` | OCR for code, terminals, docs                  |
| `diagnose_error_screenshot`    | Analyze error snapshots, propose fixes         |
| `understand_technical_diagram` | Architecture, flow, UML, ER diagrams           |
| `analyze_data_visualization`   | Charts and dashboards                          |
| `ui_diff_check`                | Compare two UI screenshots for drift           |
| `image_analysis`               | General-purpose image understanding            |
| `video_analysis`               | Video inspection (max 8MB, MP4/MOV/M4V)        |

- **Quota**: 5-hour prompt resource pool (all plans)
- **Doc ref**: https://docs.z.ai/devpack/mcp/vision-mcp-server.md

### 2.5 Zread (`zread`)

- **Type**: Remote MCP server on Z.AI infrastructure
- **Endpoint**: `https://api.z.ai/api/mcp/zread/mcp`
- **Auth**: Bearer token (Z.AI API key)
- **Backend**: Powered by zread.ai
- **Status**: Working

**3 Tools:**

| Tool                 | Purpose                                            |
|----------------------|----------------------------------------------------|
| `search_doc`         | Search docs, code, issues, PRs for a GitHub repo   |
| `get_repo_structure` | Directory structure and file list of a GitHub repo |
| `read_file`          | Read file contents from a GitHub repo              |

- **Limitation**: Public GitHub repos only, repo must exist on zread.ai
- **Doc ref**: https://docs.z.ai/devpack/mcp/zread-mcp-server.md

### 2.6 Quota (Shared Pool)

Web Search, Web Reader, and Zread share a pooled call quota:

| Plan | Shared Quota (search + reader + zread) | Vision             |
|------|----------------------------------------|--------------------|
| Lite | 100 calls                              | 5-hour prompt pool |
| Pro  | 1,000 calls                            | 5-hour prompt pool |
| Max  | 4,000 calls                            | 5-hour prompt pool |

### 2.7 Can webReader Replace webSearchPrime?

**No.** They serve different purposes:

|          | webSearchPrime                          | webReader                   |
|----------|-----------------------------------------|-----------------------------|
| Input    | Search query (text)                     | URL                         |
| Output   | Search results (titles, URLs, snippets) | Full page content           |
| Use case | "Find information about X"              | "Read the content at URL Y" |

A web search discovers URLs. A web reader consumes URLs. You cannot replace a search query with a URL read because you don't have the URLs yet.

**Potential workarounds** while web_search_prime is broken:
1. Use Z.AI REST API `POST /paas/v4/web_search` with `search_engine: "search-prime"` (separate from MCP server)
2. Use `webReader` to read a search engine URL (e.g., Google search page) - unreliable, may hit anti-scraping
3. Wait for Z.AI to fix the upstream MCP server

---

## 3. Gateway Code References

| Component              | File                                                            | Key Lines                                                             |
|------------------------|-----------------------------------------------------------------|-----------------------------------------------------------------------|
| Retry config           | [config/config.go](../api-gateway/config/config.go)             | `UpstreamMaxRetries`, `TransientRetryMax`, `UpstreamRetryBaseBackoff` |
| Error classification   | [proxy/recovery.go](../api-gateway/proxy/recovery.go)           | `ClassifyError()`, `ActionRetryTransient`, `ActionTruncateAndRetry`   |
| Context truncation     | [proxy/recovery.go](../api-gateway/proxy/recovery.go)           | `TruncateMessages()`, `fixToolPairBoundary()`                         |
| Anthropic retry loop   | [proxy/anthropic.go](../api-gateway/proxy/anthropic.go)         | 429 retry, transient retry, empty body retry                          |
| OpenAI retry loop      | [proxy/openai.go](../api-gateway/proxy/openai.go)               | 429 retry, transient retry, max_tokens reduction                      |
| Gemini retry loop      | [proxy/gemini-apikey.go](../api-gateway/proxy/gemini-apikey.go) | 429 retry, transient retry                                            |
| OAuth refresh callback | [handler/handler.go](../api-gateway/handler/handler.go)         | `OnAuthError` callback                                                |
| Key rotation callback  | [handler/handler.go](../api-gateway/handler/handler.go)         | `OnRateLimitError` callback                                           |
| Retry metrics          | [metrics/metrics.go](../api-gateway/metrics/metrics.go)         | `upstream_retries_total`, `upstream_429_total`                        |
| Spec: Error Recovery   | [docs/spec/01-proxy-layer.md](spec/01-proxy-layer.md)           | Section 11, Appendix B                                                |
