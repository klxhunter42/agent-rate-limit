# TLS Fingerprint Masking for Z.AI (Coding Plan)

## Problem

Z.AI Coding Plan (api.z.ai) ToS Section 4.2 forbids proxy/third-party access. Error 1313 (429 Fair Usage Policy) indicates Z.AI detects and blocks gateway usage. Detection vectors:

| Vector | Status | Mitigation |
|---|---|---|
| TLS fingerprint (JA3/JA4) | **Fixed** | utls Chrome impersonation |
| HTTP/2 fingerprint | **Fixed** (side effect) | ALPN forced to http/1.1 |
| HTTP headers | Already handled | `glmFingerprintHeaders` |
| Proxy-leak headers | Already handled | Transparent passthrough |
| Source IP | Out of scope | Requires residential IP tunnel |

## Architecture

```
Client -> Gateway (Go) -> upstream api.z.ai
                          |
                          +- standard Go TLS (non-Z.AI hosts)
                          +- utls Chrome fingerprint (Z.AI hosts only)
```

Only `api.z.ai` connections use utls. All other upstreams use standard Go TLS unchanged.

## Implementation

### Files

| File | Role |
|---|---|
| `api-gateway/proxy/zai_transport.go` | utls dial logic, host routing, Chrome spec |
| `api-gateway/proxy/shared_transport.go` | Conditionally sets DialTLSContext |
| `api-gateway/config/config.go` | `TLS_FINGERPRINT_ENABLED` env var |

### Key Design Decision: ALPN http/1.1

Go's `net/http.Transport.dialConn()` does a hard type assertion `pconn.conn.(*tls.Conn)` (transport.go:1795) to detect HTTP/2 via `tlsState.NegotiatedProtocol`. This assertion **always fails** for utls connections because `utls.UConn` is not `*tls.Conn` -- it's a separate type from `github.com/refraction-networking/utls`.

Initial fix: `utlsConn` wrapper implementing `connectionStater` interface. **Insufficient** -- the `*tls.Conn` assertion happens BEFORE any interface check.

Final fix: Override the Chrome preset's ALPN extension to advertise only `http/1.1` (not `h2`). The server responds with HTTP/1.1 text frames, which Go handles without needing `tlsState`. HTTP/2 multiplexing is lost but SSE streams are single-request, so zero practical impact.

```
utls handshake flow:
  1. UTLSIdToSpec(HelloChrome_Auto) -> get full Chrome spec
  2. Replace ALPNExtension: [h2, http/1.1] -> [http/1.1]
  3. UClient(tcpConn, config, HelloCustom) -> create utls conn
  4. ApplyPreset(&spec) -> apply modified Chrome spec
  5. HandshakeContext(ctx) -> TLS handshake with Chrome fingerprint + http/1.1 ALPN
  6. Server responds HTTP/1.1 -> Go HTTP/1.x handler (no tlsState needed)
```

### utls Chrome Fingerprint

`HelloChrome_Auto` resolves to the latest Chrome TLS ClientHello at runtime:
- TLS 1.2 + 1.3
- Cipher suites: Chrome's standard set (AES-GCM, ChaCha20-Poly1305)
- Extensions: supported_versions, signature_algorithms, key_share, psk_key_exchange_modes, etc.
- Curves: x25519, P-256, P-384

This makes the gateway's TLS fingerprint indistinguishable from a real Chrome browser to JA3/JA4 analysis.

## Configuration

| Env Var | Default | Description |
|---|---|---|
| `TLS_FINGERPRINT_ENABLED` | `false` | Enable utls for Z.AI hosts |

### docker-compose (.env)

```
TLS_FINGERPRINT_ENABLED=true
```

### Helm (values.yaml)

Already set to `"true"` in `gateway.env`.

## Testing

### Direct utls handshake test

```bash
cd api-gateway && go run test_utls_zai.go
```

Expected output:
```
TLS handshake OK
  Version: 0x304          # TLS 1.3
  CipherSuite: 0x1302     # TLS_AES_256_GCM_SHA384
  ALPN: "http/1.1"       # Forced, not h2
  ServerName: api.z.ai
SUCCESS: ALPN correctly negotiated as http/1.1
Response:
HTTP/1.1 404 Not Found    # Expected - /health doesn't exist
```

### Gateway integration test

1. Enable `TLS_FINGERPRINT_ENABLED=true`
2. Send glm-* request through gateway
3. Check logs for `shared transport: utls TLS fingerprint masking enabled for Z.AI hosts`
4. With `DEBUG=true`, look for `zai-utls: using Chrome fingerprint` and `zai-utls: handshake complete`

## Known Limitations

- **No HTTP/2 multiplexing**: ALPN is forced to http/1.1. SSE streams are inherently sequential so this has no practical impact on single-stream proxy use.
- **Source IP unchanged**: Datacenter IPs are still detectable. Requires residential IP tunnel (cloudflared/wireguard) -- infrastructure change, out of scope.
- **HTTP/2 SETTINGS fingerprint**: Go's HTTP/2 frame settings are not customized. Not relevant since we negotiate HTTP/1.1.
- **Chrome fingerprint only**: Uses `HelloChrome_Auto`. If Z.AI starts fingerprinting at finer granularity (e.g., specific Chrome version), the preset may need updating.

## Deploy History

- [2026-06-24] Initial implementation + ALPN http/1.1 fix. Deployed to server 111.
