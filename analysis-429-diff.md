# 429 Analysis: mitmproxy Flows

## Summary

One 429 response found in 28 total flows. The 429 was NOT caused by request header differences. All MCP requests use identical headers and the same Bearer token. The 429 was caused by **rate limiting on the MCP proxy endpoint** - 11 MCP `initialize` requests were fired in parallel within ~200ms.

## Status Code Breakdown

| Status | Count | Endpoint Pattern |
|--------|-------|-----------------|
| 200    | 13    | /api/hello, /v1/oauth/*, /api/claude_code/*, /v1/mcp/* (after retry), /v1/messages |
| 401    | 9     | /v1/mcp/mcpsrv_* (MCP servers requiring OAuth) |
| 502    | 3     | /v1/mcp/mcpsrv_* (Cloudflare bad gateway) |
| 429    | 1     | /v1/mcp/mcpsrv_01WWRm1Vv89C83sk5dRf1G3L |
| 404    | 1     | /v1/mcp_servers?limit=1000 |
| 202    | 1     | /v1/mcp/mcpsrv_* (notifications) |

## 429 Request Details

```
POST https://mcp-proxy.anthropic.com/v1/mcp/mcpsrv_01WWRm1Vv89C83sk5dRf1G3L
Timestamp: 2026-04-30T02:59:48.525Z (request sent at 02:59:47.542Z)
```

### Request Headers

```
Accept: application/json, text/event-stream
Accept-Encoding: identity
Authorization: Bearer sk-ant-oaitkn-...[REDACTED, same as all other MCP requests]
Content-Type: application/json
User-Agent: claude-code/2.1.123 (cli)
X-Mcp-Client-Session-Id: 04fa8597-8c70-4a57-b32a-2d73d27310e7
Connection: keep-alive
Host: mcp-proxy.anthropic.com
Content-Length: 305
```

### Request Body

```json
{
  "method": "initialize",
  "params": {
    "protocolVersion": "2025-11-25",
    "capabilities": {"roots": {}, "elicitation": {}},
    "clientInfo": {
      "name": "claude-code",
      "title": "Claude Code",
      "version": "2.1.123",
      "description": "Anthropic's agentic coding tool",
      "websiteUrl": "https://claude.com/claude-code"
    }
  },
  "jsonrpc": "2.0",
  "id": 0
}
```

### Response Headers

```
HTTP/1.1 429 Too Many Requests
Content-Type: text/event-stream
Transfer-Encoding: chunked
x-mcp-client-session-id: 04fa8597-8c70-4a57-b32a-2d73d27310e7
x-mcp-upstream-content-type: other          <-- KEY DIFFERENTIATOR (200s have "json")
Cache-Control: no-cache
request-id: req_011CaZD2LCUMWupSLVposhWL
via: 1.1 google
Server: cloudflare
CF-RAY: 9f434adf7d5be428-SIN
```

### Response Body

```
event: message
data: {"jsonrpc":"2.0","id":0,"error":{"code":-32600,"message":"Anthropic Proxy: Invalid content from server","data":null}}
```

## 200 MCP Request (Comparison)

Identical request headers, body, and auth token to the 429. Only differences are the MCP server ID in the URL path and timing.

```
POST https://mcp-proxy.anthropic.com/v1/mcp/mcpsrv_01UJGQtHJk1YBjKiMtHZDERM
Timestamp: 2026-04-30T02:59:54.599Z (request sent at 02:59:53.953Z)
Status: 200 OK
x-mcp-upstream-content-type: json           <-- 200 returns "json"
```

## Root Cause: Concurrent MCP Initialize Burst

Claude Code fires 11 MCP `initialize` requests in parallel at startup:

```
Timeline (relative to first request):
+0.000s  401  mcpsrv_01YZmL4BDjVaqZR67jsFdQWy
+0.011s  401  mcpsrv_01NQA7QLTqeMy69e7Pm3G4gb
+0.041s  401  mcpsrv_01APuW9EqwPYT4PrpaYhr59o
+0.044s  502  mcpsrv_01UJGQtHJk1YBjKiMtHZDERM   (Cloudflare 502, retried later -> 200)
+0.069s  401  mcpsrv_01WW2mQz77qS7zjENGYCZwM8
+0.087s  401  mcpsrv_01Lju3vF8NPn59G5BNwCGqbF
+0.121s  401  mcpsrv_019L9vLxX7NFKBbjQnYwE5Dd
+0.141s  401  mcpsrv_016SyKtqB9ppsMV3rnC5SnL8
+0.146s  401  mcpsrv_01KGmn4JzA6QpePyVUyufa7b
+0.149s  429  mcpsrv_01WWRm1Vv89C83sk5dRf1G3L   <-- RATE LIMITED
+0.200s  401  mcpsrv_017YPXpFWZcz5c7hRg835X7G
```

### Key Findings

1. **Identical headers**: 429 and 200 requests have identical request headers and body. The auth token is the same.

2. **Not a per-server limit**: The 429 hit `mcpsrv_01WWRm1Vv89C83sk5dRf1G3L`, a different server than the ones that returned 401. No prior request was made to this server.

3. **Rate limit triggered at ~10th request in ~150ms**: 10 requests were sent before the 429, all within 146ms. The 11th request (at +0.149s) triggered the rate limit. This suggests Anthropic's MCP proxy enforces a ~10 req/150ms rate limit (roughly 10 concurrent connections).

4. **The 429 response is unusual**: It returns `text/event-stream` content type with a JSON-RPC error (`-32600`, "Anthropic Proxy: Invalid content from server") rather than a standard Anthropic rate limit error. This suggests the MCP proxy's rate limiter returns a generic error rather than a proper rate-limit response.

5. **x-mcp-upstream-content-type**: The 429 returns `other` while 200s return `json`. This indicates the upstream MCP server never responded -- the proxy itself generated the error before forwarding to the upstream.

6. **401s are unrelated**: The 401 responses come from MCP servers that "require authentication but no OAuth token is configured" (`mcp_unauthorized_no_token`). These are server-side config issues, not rate limiting.

7. **Post-burst recovery works**: The same MCP server that got 502 (`mcpsrv_01UJGQtHJk1YBjKiMtHZDERM`) was retried 6.5s later and succeeded with 200.

## Recommendation

The gateway should stagger MCP initialization requests (e.g., 50-100ms delay between each, or batch into groups of 5) to avoid hitting the MCP proxy's concurrent connection rate limit during startup.
