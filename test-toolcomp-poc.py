#!/usr/bin/env python3
"""POC test for Tool Compression optimizer stage."""

import json
import urllib.request
import urllib.error
import sys

BASE = "http://localhost:18080"
API_KEY = "1d63b5db6d984db1913ca9596125f06b.RHYoZWfoRZteSclW"
MODEL = "glm-5.1"

def post(path, body, headers=None):
    hdrs = {"Content-Type": "application/json"}
    if headers:
        hdrs.update(headers)
    data = json.dumps(body).encode()
    req = urllib.request.Request(f"{BASE}{path}", data=data, headers=hdrs, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            return json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        err = e.read().decode()
        print(f"HTTP {e.code}: {err[:500]}", file=sys.stderr)
        return None
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        return None

def create_profile(name, overrides):
    body = {
        "name": name,
        "target": "zai",
        "provider": "zai",
        "passthroughAuth": True,
        "optimizerOverrides": overrides
    }
    return post("/v1/profiles", body)

def send_messages(profile, messages):
    body = {
        "model": MODEL,
        "max_tokens": 512,
        "messages": messages
    }
    headers = {
        "Authorization": f"Bearer {API_KEY}",
        "X-Profile": profile
    }
    return post("/v1/messages", body, headers)

# --- Tool result payloads ---

def make_small_tool_result():
    """~200 chars of logs"""
    tool_content = "2026-05-14T10:23:01Z [INFO] Server started on port 8080\n2026-05-14T10:23:02Z [INFO] Connected to database\n2026-05-14T10:23:03Z [INFO] Health check passed\n2026-05-14T10:23:04Z [WARN] Memory usage at 75%\n2026-05-14T10:23:05Z [INFO] Request processed in 45ms"
    return [
        {"role": "user", "content": "Check the server logs for any issues"},
        {"role": "assistant", "content": [
            {"type": "text", "text": "Let me check the server logs."},
            {"type": "tool_use", "id": "toolu_small_001", "name": "read_logs", "input": {"path": "/var/log/server.log", "lines": 10}}
        ]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "toolu_small_001", "content": tool_content}
        ]},
        {"role": "assistant", "content": [
            {"type": "text", "text": "I found some log entries. Let me analyze them."},
            {"type": "tool_use", "id": "toolu_small_002", "name": "analyze", "input": {"query": "log anomalies"}}
        ]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "toolu_small_002", "content": "No critical anomalies found. Memory warning at 75% is within normal range."}
        ]}
    ]

def make_medium_tool_result():
    """~1000 chars of kubectl logs with duplicates"""
    lines = []
    for i in range(20):
        lines.append(f"2026-05-14T10:{i:02d}:01Z [INFO] pod/api-gateway-7d8f9c6b5-xk2lp Handling request from client")
        lines.append(f"2026-05-14T10:{i:02d}:01Z [INFO] pod/api-gateway-7d8f9c6b5-xk2lp Handling request from client")
        lines.append(f"2026-05-14T10:{i:02d}:02Z [DEBUG] Middleware: auth check passed for token=abc***")
        lines.append(f"2026-05-14T10:{i:02d}:03Z [INFO] Proxying to upstream zai-gateway:443")
    tool_content = "\n".join(lines)
    return [
        {"role": "user", "content": "Get the kubectl logs for api-gateway deployment"},
        {"role": "assistant", "content": [
            {"type": "text", "text": "Fetching the logs now."},
            {"type": "tool_use", "id": "toolu_med_001", "name": "kubectl_logs", "input": {"deployment": "api-gateway", "tail": 100}}
        ]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "toolu_med_001", "content": tool_content}
        ]},
        {"role": "assistant", "content": [
            {"type": "text", "text": "I see many duplicate log lines. Let me check the pod status."},
            {"type": "tool_use", "id": "toolu_med_002", "name": "kubectl_get", "input": {"resource": "pods", "selector": "app=api-gateway"}}
        ]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "toolu_med_002", "content": "NAME                             READY   STATUS    RESTARTS   AGE\napi-gateway-7d8f9c6b5-xk2lp     1/1     Running   0          2h\napi-gateway-7d8f9c6b5-mn4pq     1/1     Running   3          2h\napi-gateway-7d8f9c6b5-rs8tv     1/1     Running   0          2h"}
        ]}
    ]

def make_large_tool_result():
    """~3000 chars of verbose debug output with repeated lines"""
    lines = []
    for i in range(50):
        lines.append(f"[DEBUG] [{i}] Entering request handler: method=POST path=/v1/messages content_length=2048")
        lines.append(f"[DEBUG] [{i}] Entering request handler: method=POST path=/v1/messages content_length=2048")
        lines.append(f"[DEBUG] [{i}] Auth middleware: validating bearer token against allowed scopes")
        lines.append(f"[DEBUG] [{i}] Rate limiter: token=RHYo*** remaining=99 window=60s")
        lines.append(f"[DEBUG] [{i}] Optimizer stage: semantic_dedup=false chunker=false toolcomp=true")
        lines.append(f"[DEBUG] [{i}] Optimizer stage: semantic_dedup=false chunker=false toolcomp=true")
        lines.append(f"[VERBOSE] [{i}] Reading request body from stream: bytes_read=2048 expected=2048")
        lines.append(f"[VERBOSE] [{i}] Marshaling JSON request body: fields=8")
        lines.append(f"[DEBUG] [{i}] Proxy: dialing upstream zai-gateway:443 timeout=30s")
        lines.append(f"[DEBUG] [{i}] Proxy: TLS handshake complete cipher=TLS_AES_256_GCM_SHA384")
    tool_content = "\n".join(lines)
    return [
        {"role": "user", "content": "Run the debug trace for the api-gateway service"},
        {"role": "assistant", "content": [
            {"type": "text", "text": "Starting debug trace capture."},
            {"type": "tool_use", "id": "toolu_lg_001", "name": "debug_trace", "input": {"service": "api-gateway", "level": "debug", "duration": "60s"}}
        ]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "toolu_lg_001", "content": tool_content}
        ]},
        {"role": "assistant", "content": [
            {"type": "text", "text": "I captured the debug trace. Let me also check the metrics endpoint."},
            {"type": "tool_use", "id": "toolu_lg_002", "name": "curl", "input": {"url": "http://localhost:9090/metrics"}}
        ]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "toolu_lg_002", "content": "# HELP api_gateway_requests_total Total requests processed\n# TYPE api_gateway_requests_total counter\napi_gateway_requests_total{method=\"POST\",path=\"/v1/messages\",status=\"200\"} 1234\napi_gateway_requests_total{method=\"GET\",path=\"/health\",status=\"200\"} 5678\napi_gateway_optimizer_tokens_saved_total{technique=\"toolcomp\"} 45000"}
        ]}
    ]

def make_json_tool_result():
    """~1500 chars of structured config JSON"""
    config = {
        "apiVersion": "v1",
        "kind": "ConfigMap",
        "metadata": {
            "name": "api-gateway-config",
            "namespace": "production",
            "labels": {"app": "api-gateway", "tier": "backend", "env": "production"},
            "annotations": {"kubectl.kubernetes.io/last-applied-configuration": "{\"apiVersion\":\"v1\",\"kind\":\"ConfigMap\"}"}
        },
        "data": {
            "gateway.yaml": "server:\n  port: 8080\n  read_timeout: 30s\n  write_timeout: 30s\n  idle_timeout: 120s\nupstreams:\n  zai:\n    endpoint: https://zai-gateway.example.com\n    timeout: 60s\n    max_retries: 3\n  claude:\n    endpoint: https://api.anthropic.com\n    timeout: 120s\n    max_retries: 2\nrate_limiting:\n  enabled: true\n  requests_per_minute: 60\n  burst: 10\noptimizer:\n  toolcomp: true\n  semantic_dedup: false\n  chunker: false\nlogging:\n  level: info\n  format: json\n  output: stdout",
            "nginx.conf": "upstream gateway {\n  server 127.0.0.1:8080;\n  keepalive 32;\n}\nserver {\n  listen 443 ssl;\n  server_name gateway.example.com;\n  ssl_certificate /etc/tls/cert.pem;\n  ssl_certificate_key /etc/tls/key.pem;\n  location / {\n    proxy_pass http://gateway;\n    proxy_set_header Host $host;\n  }\n}",
            "environment": "production",
            "revision": "2026-05-14-v3",
            "deployed_by": "argocd",
            "rollout_strategy": "blue-green"
        }
    }
    tool_content = json.dumps(config, indent=2)
    return [
        {"role": "user", "content": "Show me the current ConfigMap for api-gateway"},
        {"role": "assistant", "content": [
            {"type": "text", "text": "Fetching the ConfigMap."},
            {"type": "tool_use", "id": "toolu_json_001", "name": "kubectl_get", "input": {"resource": "configmap", "name": "api-gateway-config", "namespace": "production", "output": "json"}}
        ]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "toolu_json_001", "content": tool_content}
        ]},
        {"role": "assistant", "content": [
            {"type": "text", "text": "Here is the ConfigMap. Let me also check the associated secrets."},
            {"type": "tool_use", "id": "toolu_json_002", "name": "kubectl_get", "input": {"resource": "secrets", "selector": "app=api-gateway", "namespace": "production"}}
        ]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "toolu_json_002", "content": "NAME                    TYPE                                  DATA   AGE\napi-gateway-tls         kubernetes.io/tls                     2      30d\napi-gateway-auth        Opaque                                3      30d\napi-gateway-redis       Opaque                                1      30d"}
        ]}
    ]

def make_diff_tool_result():
    """~2000 chars of git diff output"""
    diff_content = """diff --git a/api-gateway/internal/optimizer/toolcomp.go b/api-gateway/internal/optimizer/toolcomp.go
new file mode 100644
index 0000000..a1b2c3d
--- /dev/null
+++ b/api-gateway/internal/optimizer/toolcomp.go
@@ -0,0 +1,85 @@
+package optimizer
+
+import (
+\t"encoding/json"
+\t"fmt"
+\t"strings"
+)
+
+// ToolComp compresses tool_result content by removing duplicates,
+// truncating verbose output, and summarizing structured data.
+type ToolComp struct {
+\tmaxLen      int
+\tdedupLines  bool
+\tsummarize   bool
+}
+
+// NewToolComp creates a new ToolComp with default settings.
+func NewToolComp() *ToolComp {
+\treturn &ToolComp{
+\t\tmaxLen:     2000,
+\t\tdedupLines: true,
+\t\tsummarize:  true,
+\t}
+}
+
+// Compress applies tool result compression to the content.
+func (tc *ToolComp) Compress(content string, mimeType string) (string, error) {
+\tif len(content) == 0 {
+\t\treturn content, nil
+\t}
+
+\t// Step 1: Remove duplicate consecutive lines
+\tif tc.dedupLines {
+\t\tcontent = tc.deduplicateLines(content)
+\t}
+
+\t// Step 2: Truncate if exceeds max length
+\tif len(content) > tc.maxLen {
+\t\tcontent = content[:tc.maxLen] + "\\n... [truncated]"
+\t}
+
+\t// Step 3: Summarize structured data
+\tif tc.summarize && (mimeType == "application/json" || strings.HasPrefix(content, "{")) {
+\t\tcontent = tc.summarizeJSON(content)
+\t}
+
+\treturn content, nil
+}
+
+// deduplicateLines removes consecutive duplicate lines.
+func (tc *ToolComp) deduplicateLines(content string) string {
+\tlines := strings.Split(content, "\\n")
+\tvar result []string
+\tprev := ""
+\tfor _, line := range lines {
+\t\tif line != prev {
+\t\t\tresult = append(result, line)
+\t\t\tprev = line
+\t\t}
+\t}
+\treturn strings.Join(result, "\\n")
+}
+
+// summarizeJSON produces a compact summary of JSON content.
+func (tc *ToolComp) summarizeJSON(content string) string {
+\tvar data interface{}
+\tif err := json.Unmarshal([]byte(content), &data); err != nil {
+\t\treturn content
+\t}
+\tsummary, _ := json.Marshal(data)
+\treturn string(summary)
+}
diff --git a/api-gateway/internal/optimizer/stages.go b/api-gateway/internal/optimizer/stages.go
index d4e5f6a..b7c8d9e 100644
--- a/api-gateway/internal/optimizer/stages.go
+++ b/api-gateway/internal/optimizer/stages.go
@@ -15,6 +15,7 @@ var stageOrder = []string{
 \t"toolcomp",
 \t"toolfilter",
+\t"toolcomp",
 \t"semantic_dedup",
 }
"""
    return [
        {"role": "user", "content": "Show me the git diff for the toolcomp feature branch"},
        {"role": "assistant", "content": [
            {"type": "text", "text": "Getting the diff."},
            {"type": "tool_use", "id": "toolu_diff_001", "name": "git_diff", "input": {"branch": "feature/toolcomp", "stat": False}}
        ]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "toolu_diff_001", "content": diff_content}
        ]},
        {"role": "assistant", "content": [
            {"type": "text", "text": "I see the diff. Let me check the file stats as well."},
            {"type": "tool_use", "id": "toolu_diff_002", "name": "git_diff", "input": {"branch": "feature/toolcomp", "stat": True}}
        ]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "toolu_diff_002", "content": " api-gateway/internal/optimizer/toolcomp.go | 85 +++++++++++\n api-gateway/internal/optimizer/stages.go     |  1 +\n 2 files changed, 86 insertions(+)\n"}
        ]}
    ]

def extract_usage(resp):
    """Extract input/output token counts from response."""
    if not resp:
        return None, None
    usage = resp.get("usage", {})
    inp = usage.get("input_tokens", 0)
    out = usage.get("output_tokens", 0)
    return inp, out

def count_tool_result_chars(messages):
    """Count total chars in tool_result content blocks."""
    total = 0
    for msg in messages:
        content = msg.get("content", "")
        if isinstance(content, list):
            for block in content:
                if isinstance(block, dict) and block.get("type") == "tool_result":
                    total += len(block.get("content", ""))
    return total

# --- Main ---

print("=== Creating profiles ===")

# Baseline: all off
base_overrides = {
    "semantic_dedup": False, "chunker": False, "sketch": False,
    "textcomp": False, "caveman": False, "pordee": False,
    "toolcomp": False, "toolfilter": False
}
r1 = create_profile("opt-tc-base", base_overrides)
print(f"  Baseline: {r1}")

# ToolComp only: omit toolcomp from overrides so it defaults enabled
tc_overrides = {
    "semantic_dedup": False, "chunker": False, "sketch": False,
    "textcomp": False, "caveman": False, "pordee": False,
    "toolfilter": False
}
r2 = create_profile("opt-tc-only", tc_overrides)
print(f"  ToolComp: {r2}")

# Test payloads
tests = [
    ("Small (~200 chars logs)",       make_small_tool_result()),
    ("Medium (~1000 chars kubectl)",  make_medium_tool_result()),
    ("Large (~3000 chars debug)",     make_large_tool_result()),
    ("JSON (~1500 chars config)",     make_json_tool_result()),
    ("Diff (~2000 chars git diff)",   make_diff_tool_result()),
]

results = []

for label, messages in tests:
    tool_chars = count_tool_result_chars(messages)
    print(f"\n--- {label} (tool_result chars: {tool_chars}) ---")

    print(f"  Sending baseline...", end=" ", flush=True)
    base_resp = send_messages("opt-tc-base", messages)
    base_in, base_out = extract_usage(base_resp)
    print(f"in={base_in} out={base_out}")
    if base_resp is None:
        print("  BASELINE FAILED, skipping")
        results.append((label, tool_chars, "ERR", "ERR", "ERR", "ERR", "ERR", "ERR", "FAIL"))
        continue

    print(f"  Sending toolcomp...", end=" ", flush=True)
    tc_resp = send_messages("opt-tc-only", messages)
    tc_in, tc_out = extract_usage(tc_resp)
    print(f"in={tc_in} out={tc_out}")
    if tc_resp is None:
        print("  TOOLCOMP FAILED, skipping")
        results.append((label, tool_chars, base_in, base_out, "ERR", "ERR", "ERR", "ERR", "FAIL"))
        continue

    # Calculate savings
    in_saved = ((base_in - tc_in) / base_in * 100) if base_in and base_in > 0 else 0
    out_saved = ((base_out - tc_out) / base_out * 100) if base_out and base_out > 0 else 0

    verdict = "PASS" if in_saved > 0 else "NO CHANGE"
    if in_saved < 0:
        verdict = "REGRESSION"

    results.append((label, tool_chars, base_in, tc_in, f"{in_saved:.1f}%", base_out, tc_out, f"{out_saved:.1f}%", verdict))

# Print table
print("\n")
print("| # | Test | Tool Result Size | Baseline In | Compressed In | In Saved | Baseline Out | Compressed Out | Out Saved | Verdict |")
print("|---|------|-----------------|-------------|---------------|----------|--------------|----------------|-----------|---------|")
for i, r in enumerate(results, 1):
    label, tsize, b_in, c_in, in_sv, b_out, c_out, out_sv, verdict = r
    print(f"| {i} | {label} | {tsize} | {b_in} | {c_in} | {in_sv} | {b_out} | {c_out} | {out_sv} | {verdict} |")
