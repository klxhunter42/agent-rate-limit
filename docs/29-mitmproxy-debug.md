# 29 - Debugging Upstream Traffic with mitmproxy

Capture and inspect HTTPS traffic between the gateway and upstream providers
(Anthropic, Z.AI, Gemini) using mitmweb.

---

## Architecture

```
    +------------------------------------------------------+
    |               Docker Network (arl-network)            |
    |                                                       |
    |   arl-gateway (:8080)                                 |
    |        |                                              |
    |        | HTTPS_PROXY=http://arl-mitmweb:8083          |
    |        |                                              |
    |        +-----------> arl-mitmweb                      |
    |                        |                              |
    |                   :8083 (proxy)                       |
    |                   :8081 (web UI)                      |
    +--------|---------------|------------------------------+
             |               |
    +--------|---------------|------------------------------+
    |        |               |     Host Machine (macOS)     |
    |        |               +----> http://localhost:8081   |
    |        |                     (browser opens here)     |
    |        v                                              |
    |   api.anthropic.com / api.z.ai                        |
    +-------------------------------------------------------+
```

When `HTTPS_PROXY` is **not** set:

```
  arl-gateway ----HTTPS---> api.anthropic.com    (direct, no interception)
```

When `HTTPS_PROXY` **is** set (with `--profile debug`):

```
  arl-gateway ----HTTPS---> arl-mitmweb ----HTTPS---> api.anthropic.com
                               |
                               v
                            mitmweb UI at http://localhost:8081
                            shows all requests/responses
                            (headers, bodies, SSE streams, OAuth tokens)
```

---

## Quick Start

### Step 1: Start mitmweb + gateway together

```bash
HTTPS_PROXY=http://arl-mitmweb:8083 docker compose --profile debug up -d
```

This starts all normal services **plus** the `arl-mitmweb` debug container.

`arl-mitmweb` is a `mitmproxy/mitmproxy` container on the same `arl-network`,
so the gateway reaches it by container name (no host IP needed).

### Step 2: Open web UI

```bash
open http://localhost:8081
```

Password: `test`

Send any request through the gateway. All upstream traffic appears in
mitmweb in real time.

### Step 3: Stop debugging

```bash
# Stop everything including mitmweb
docker compose --profile debug down

# Restart gateway without proxy
docker compose up -d
```

---

## How It Works

### docker-compose profile

The `arl-mitmweb` service is defined with `profiles: ["debug"]`, so it only
starts when you pass `--profile debug`. Normal `docker compose up` ignores it.

### Gateway transport

`proxy/shared_transport.go` contains a shared `http.Transport` singleton. When
`HTTPS_PROXY` env var is set:

1. Go's `http.Transport` reads `HTTPS_PROXY` automatically (via
   `http.ProxyFromEnvironment`)
2. All outgoing HTTPS requests route through `arl-mitmweb:8083`
3. `TLSClientConfig` is set to `InsecureSkipVerify: true` **only when**
   `HTTPS_PROXY` is present, so mitmproxy's MITM cert is accepted
4. When `HTTPS_PROXY` is unset, TLS verification works normally (no security
   impact)

### mitmproxy TLS interception

```
  Gateway                           mitmproxy                        Anthropic
  (thinks it's                     (decrypts,                       (real server)
   talking to                       re-encrypts,
   Anthropic)                       inspects)

  ---- TLS with fake cert ---->  ---- TLS with real cert ---->
       (InsecureSkipVerify             (valid cert)
        accepts this)
```


---

## What You Can See

| Tab in mitmweb    | What it shows                                       |
|-------------------|-----------------------------------------------------|
| Request           | Full URL, headers (Authorization, anthropic-beta), body |
| Response          | Status code, headers, response body / SSE stream    |
| WebSocket         | If using streaming endpoints                        |
| Timing            | DNS, connect, TLS, TTFB, total                     |

### Useful filters

| Filter                  | Shows                              |
|--------------------------|-----------------------------------|
| `~u anthropic`          | Only Anthropic API calls          |
| `~u z.ai`               | Only Z.AI API calls              |
| `~m POST`               | Only POST requests                |
| `~s "error"`            | Responses containing "error"      |
| `~h "anthropic-beta"`   | Requests with OAuth beta header   |

---

## Alternative: mitmweb on Host

If you prefer running mitmweb on the host machine instead of a container:

### Step 0: Find your host IP

```bash
# macOS
ipconfig getifaddr en0

# Linux
hostname -I | awk '{print $1}'
```

### Step 1: Start mitmweb on host

```bash
mitmweb \
  --set web_password=test \
  --web-port 8081 \
  --listen-port 8083 \
  --ssl-insecure
```

### Step 2: Set HTTPS_PROXY in `.env`

```bash
# .env - use your host IP
HTTPS_PROXY=http://<your-host-ip>:8083
```

```bash
docker compose up -d arl-gateway
```

### Step 3: Open web UI

```bash
open http://localhost:8081
```

### Colima users

Colima does not support `host.docker.internal`. Use your `en0` IP as shown
in Step 0.

### Docker Desktop users

Docker Desktop supports `host.docker.internal`:

```bash
HTTPS_PROXY=http://host.docker.internal:8083
```

---

## Troubleshooting

### Gateway won't start / connection refused

- mitmweb is not running. Start it first
- If using profile: `docker compose --profile debug up -d`
- If using host: start mitmweb on host before restarting gateway

### No traffic appears in mitmweb

- Verify `HTTPS_PROXY` is set: `docker exec arl-gateway env | grep HTTPS_PROXY`
- Verify gateway restarted after the change
- Test connectivity: `docker exec arl-gateway curl -v http://arl-mitmweb:8083`

### TLS handshake errors

`InsecureSkipVerify` should handle this. If errors persist, check that
`shared_transport.go` has the `TLSClientConfig` conditional logic.

### Running without Docker (local dev)

```bash
# Terminal 1: start mitmweb
mitmweb --set web_password=test --web-port 8081 --listen-port 8083 --ssl-insecure

# Terminal 2: start gateway with proxy
HTTPS_PROXY=http://localhost:8083 go run main.go
```

---

## Security Notes

- `InsecureSkipVerify` is **only active** when `HTTPS_PROXY` is set
- In production (no `HTTPS_PROXY`), full TLS verification is enforced
- mitmweb password is set to `test` for local debugging only
- Never use this setup in production
