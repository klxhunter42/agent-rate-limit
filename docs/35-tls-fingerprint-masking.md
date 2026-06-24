# TLS + HTTP/2 Fingerprint Masking for Z.AI

## Problem

Z.AI Coding Plan (api.z.ai) ToS Section 4.2 forbids proxy/third-party access. Error 1313 (429 Fair Usage Policy) indicates Z.AI detects and blocks gateway usage. Detection vectors:

| Vector | Status | Mitigation |
|---|---|---|
| TLS fingerprint (JA3/JA4) | **Fixed** | azuretls Chrome impersonation |
| HTTP/2 fingerprint (SETTINGS/WINDOW/header-order) | **Fixed** | azuretls Chrome impersonation |
| HTTP headers | Already handled | `glmFingerprintHeaders` forwarding |
| Proxy-leak headers | Already handled | Transparent passthrough |
| Request pattern (timing/concurrency) | **Mitigated** | jitter + spacing + cap (see below) |
| Source IP (datacenter) | Out of scope | Requires residential exit (infra change) |

## Architecture

```
Client -> Gateway (Go) -> SharedClient
                          |
                          +- selectiveRoundTripper
                               |- isZAIHost(host)?
                               |    YES -> azuretlsRT (Chrome TLS + HTTP/2)
                               |    NO  -> stdlib net/http.Transport
```

Only `api.z.ai` connections use azuretls. All other upstreams use standard Go TLS unchanged. Routing happens at the `http.Client` layer (selective RoundTripper), not the transport dialer.

## Implementation

### Files

| File | Role |
|---|---|
| `api-gateway/proxy/azuretls_transport.go` | `azuretlsRT` adapter (Session.Do -> RoundTripper) + `selectiveRoundTripper` |
| `api-gateway/proxy/zai_transport.go` | `isZAIHost()` host matcher only (utls dial path removed) |
| `api-gateway/proxy/shared_transport.go` | `SharedClient` wraps transport in `selectiveRoundTripper` |
| `api-gateway/proxy/zai_pacer.go` | zai concurrency semaphore + per-key spacing |
| `api-gateway/proxy/retry.go` | `jitteredBackoff` (0.5x-1.5x randomized) |
| `api-gateway/config/config.go` | env vars for pacing + fingerprint toggle |

### azuretls integration

`github.com/Noooste/azuretls-client` v1.13.2. One `Session` with `Browser = azuretls.Chrome` provides:
- Chrome TLS ClientHello (JA3/JA4)
- Chrome HTTP/2 SETTINGS/WINDOW_UPDATE/priority + header-order (Akamai fingerprint)

Verified against `tls.peet.ws/api/all`:
```
JA4:                 t13d1516h2_8daaf6152771_d8a2da3f94cd   (t13=TLS1.3, h2=HTTP/2)
HTTP/2 akamai_hash:  52d84b11737d980aef856699f885ca86       (Chrome signature)
```

`azuretlsRT.RoundTrip`:
1. Reads `req.Body` to bytes (anthropic.go rebuilds a fresh httpReq per retry, so consuming is safe).
2. Builds `azuretls.OrderedHeaders` from `req.Header` (preserves header-order fingerprint).
3. `Request.IgnoreBody = true` -> streams via `Response.RawBody` (live `io.ReadCloser`) instead of eager `[]byte` read. Critical for SSE.
4. `session.Do(areq)` -> maps to `*http.Response` (fhttp.Header copied field-by-field to stdlib http.Header, distinct named types).

### Concurrency

`Session.Do` delegates to the HTTP/2 transport's `RoundTrip`, which multiplexes streams on a single connection (HTTP/2 native concurrency). It is **safe for concurrent use without an external mutex**. An earlier per-call `mu sync.Mutex` serialized all Z.AI traffic onto one stream (throughput killer) and was removed. Verified with `go test -race`.

### Timeout (critical fix)

azuretls defaults `Session.TimeOut = 30s`, wired to BOTH `TLSHandshakeTimeout` and `ResponseHeaderTimeout`. 30s is too short for glm-5.2 thinking + long Claude Code contexts -> `502 "http2: timeout awaiting response headers"`. Fixed by `session.SetTimeout(STREAM_TIMEOUT, default 300s)` so `ResponseHeaderTimeout` matches the gateway's `StreamTimeout`.

## Request-pattern hardening (glm-* only)

All applied only when `strings.HasPrefix(model, "glm-")`, so non-zai upstreams are unaffected.

| Control | Env | Default | Where |
|---|---|---|---|
| pre-dispatch jitter | `ZAI_REQUEST_JITTER_MS` | 150 | anthropic.go before `client.Do` |
| per-key spacing | `ZAI_MIN_REQUEST_SPACING` | 800ms | zai_pacer.go (map keyed by api-key) |
| zai concurrency cap | `ZAI_CONCURRENCY_CAP` | 5 | zai_pacer.go channel semaphore |
| retry backoff jitter | `RETRY_BACKOFF_JITTER` | true | retry.go, all 4 proxy retry loops |

Per-key spacing: multiple API keys parallelize (each key has its own last-dispatch timestamp); a single key is paced. With 1 key today it equals a global pace but scales when more keys are added.

## Configuration

Defaults in BOTH `docker-compose.yml` (arl-gateway env) and `helm/ai-gateway/values.yaml` (gateway.env):

| Env Var | Default |
|---|---|
| `TLS_FINGERPRINT_ENABLED` | `true` |
| `ZAI_REQUEST_JITTER_MS` | `150` |
| `ZAI_MIN_REQUEST_SPACING` | `800ms` |
| `ZAI_CONCURRENCY_CAP` | `5` |
| `RETRY_BACKOFF_JITTER` | `true` |

Model/global/RPM limits (`UPSTREAM_MODEL_LIMITS`, `UPSTREAM_GLOBAL_LIMIT=9`, `UPSTREAM_RPM_LIMIT=40`) are UNCHANGED from prior defaults.

## Rollback

`TLS_FINGERPRINT_ENABLED=false` -> selectiveRoundTripper.zai=nil -> pure stdlib path. Pacing controls set to 0/false to disable individually.

## Gotcha: vendor + .gitignore

`.gitignore` blocks `api-gateway/vendor/`. New vendor files (azuretls + transitive deps) are NOT picked up by `git add -A` and must be force-added (`git add -f api-gateway/vendor/`) or the server-111 Docker build fails with `cannot find module ... import lookup disabled by -mod=vendor`. Same issue hit previously with utls/x-crypto vendor commits.

## Deploy History

- [2026-06-24] azuretls Chrome TLS+HTTP/2 replaces utls http/1.1 path. Verified Chrome fingerprint via tls.peet.ws.
- [2026-06-24] Fixed 502 http2 timeout (ResponseHeaderTimeout 30s -> 300s) + removed serializing mutex. Verified glm-5.2 200 OK.
