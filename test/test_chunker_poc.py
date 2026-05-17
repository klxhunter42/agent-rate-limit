#!/usr/bin/env python3
"""POC test for Chunker optimizer stage against localhost:18080.

The chunker operates on the 'system' field of the API request, NOT on user messages.
All test patterns must send content via the 'system' field to exercise the chunker.

Sends 3 requests per pattern per profile, takes median input/output tokens.
Input tokens = input_tokens + cache_read_input_tokens (Z.AI splits them).
"""
import json
import urllib.request
import urllib.error
import time
import sys
import statistics

BASE = "http://localhost:18080"
API_KEY = "1d63b5db6d984db1913ca9596125f06b.RHYoZWfoRZteSclW"
MODEL = "glm-5.1"
HEADERS_BASE = {
    "Content-Type": "application/json",
    "Authorization": f"Bearer {API_KEY}",
}
ROUNDS = 3

PROFILES = {
    "baseline": {
        "name": "opt-ch-base",
        "target": "zai",
        "provider": "zai",
        "passthroughAuth": True,
        "optimizerOverrides": {
            "semantic_dedup": False, "chunker": False, "sketch": False,
            "textcomp": False, "caveman": False, "pordee": False,
            "toolcomp": False, "toolfilter": False,
        },
    },
    "chunker": {
        "name": "opt-ch-only",
        "target": "zai",
        "provider": "zai",
        "passthroughAuth": True,
        "optimizerOverrides": {
            "semantic_dedup": False, "sketch": False, "textcomp": False,
            "caveman": False, "pordee": False, "toolcomp": False, "toolfilter": False,
        },
    },
}

DUPLICATE_LINE = "E1012 08:23:45.678 pod/nginx-deploy-7d9f8b6c4-xk2jw container=nginx level=error msg=connection refused to upstream 10.4.3.21:8080\n"

# All patterns use 'system' field since chunker only processes system prompts.
# A user message with "Analyze the above." is always appended.
PATTERNS = [
    {
        "label": "K8s dup logs (8 identical lines)",
        "system": (
            DUPLICATE_LINE * 8
            + "E1012 08:23:46.100 pod/nginx-deploy-7d9f8b6c4-xk2jw container=nginx level=warn msg=retrying connection attempt 1\n"
            + "E1012 08:23:46.200 pod/nginx-deploy-7d9f8b6c4-xk2jw container=nginx level=info msg=health check passed\n"
            + "E1012 08:23:47.300 pod/nginx-deploy-7d9f8b6c4-xk2jw container=nginx level=warn msg=high memory usage 512MB\n"
            + "E1012 08:23:48.400 pod/nginx-deploy-7d9f8b6c4-xk2jw container=nginx level=info msg=GC completed in 120ms\n"
            + "E1012 08:23:49.500 pod/nginx-deploy-7d9f8b6c4-xk2jw container=nginx level=info msg=serving request from 10.4.0.1\n"
            + "E1012 08:23:50.600 pod/nginx-deploy-7d9f8b6c4-xk2jw container=nginx level=error msg=OOMKill threshold 90% reached\n"
            + "E1012 08:23:51.700 pod/nginx-deploy-7d9f8b6c4-xk2jw container=nginx level=info msg=metrics exported to prometheus\n"
            + "E1012 08:23:52.800 pod/nginx-deploy-7d9f8b6c4-xk2jw container=nginx level=info msg=graceful shutdown signal received\n"
            + "E1012 08:23:53.900 pod/nginx-deploy-7d9f8b6c4-xk2jw container=nginx level=warn msg=connection pool near limit 48/50\n"
            + "E1012 08:23:54.000 pod/nginx-deploy-7d9f8b6c4-xk2jw container=nginx level=info msg=config reload triggered by ConfigMap change\n"
        ),
        "user": "What went wrong in these logs? Be brief.",
    },
    {
        "label": "App logs (INFO x20, WARN x3, ERROR x2)",
        "system": (
            "2026-05-14T10:00:01Z INFO Request processed successfully path=/api/v1/users latency=12ms\n" * 20
            + "2026-05-14T10:01:00Z WARN Rate limit approaching 90% for client_id=app-service\n" * 3
            + "2026-05-14T10:01:05Z ERROR Database connection pool exhausted, retrying in 5s\n"
            + "2026-05-14T10:01:10Z ERROR Failed to acquire connection from pool after 3 retries\n"
        ),
        "user": "Summarize the issues in 3 bullet points.",
    },
    {
        "label": "Multi-topic (3 questions)",
        "system": (
            "Topic 1: How do I set up a Kubernetes Horizontal Pod Autoscaler for my deployment called 'api-server' targeting 70% CPU utilization with min 3 and max 20 replicas?\n\n"
            "Topic 2: What is the best way to configure Prometheus alerting rules for monitoring pod restart counts exceeding 5 in a 10 minute window?\n\n"
            "Topic 3: Can you explain how to implement a blue-green deployment strategy using ArgoCD with automatic rollback on failed health checks?\n\n"
        ),
        "user": "Please address all three topics above.",
    },
    {
        "label": "Clean single msg (no redundancy)",
        "system": (
            "You are a Kubernetes expert assistant. Answer questions concisely."
        ),
        "user": "Explain the difference between Kubernetes Deployments and StatefulSets in 3 bullet points.",
    },
    {
        "label": "Repeated instruction (3x)",
        "system": (
            "You are a helpful assistant. Be concise and accurate in your responses. Always provide code examples when discussing technical topics.\n\n"
            "You are a helpful assistant. Be concise and accurate in your responses. Always provide code examples when discussing technical topics.\n\n"
            "You are a helpful assistant. Be concise and accurate in your responses. Always provide code examples when discussing technical topics.\n\n"
        ),
        "user": "How do I create a ConfigMap in Kubernetes?",
    },
]


def send_request(profile_name, system_text, user_msg, round_num):
    payload = {
        "model": MODEL,
        "max_tokens": 512,
        "system": system_text,
        "messages": [{"role": "user", "content": user_msg}],
    }
    data = json.dumps(payload).encode()
    headers = dict(HEADERS_BASE)
    headers["X-Profile"] = profile_name
    req = urllib.request.Request(
        f"{BASE}/v1/messages", data=data, headers=headers, method="POST"
    )
    try:
        resp = urllib.request.urlopen(req, timeout=120)
        body = json.loads(resp.read().decode())
        usage = body.get("usage", {})
        total_in = usage.get("input_tokens", 0) + usage.get("cache_read_input_tokens", 0)
        return {
            "input": total_in,
            "output": usage.get("output_tokens", 0),
            "status": resp.status,
            "error": None,
        }
    except urllib.error.HTTPError as e:
        body = e.read().decode()
        return {"input": 0, "output": 0, "status": e.code, "error": body[:200]}
    except Exception as e:
        return {"input": 0, "output": 0, "status": 0, "error": str(e)[:200]}


def median_of(profile, system_text, user_msg, rounds):
    inputs, outputs = [], []
    last_err = None
    for r in range(rounds):
        result = send_request(profile, system_text, user_msg, r)
        if result["error"]:
            last_err = result["error"]
            continue
        inputs.append(result["input"])
        outputs.append(result["output"])
        time.sleep(0.3)
    if not inputs:
        return 0, 0, last_err
    return statistics.median(inputs), statistics.median(outputs), None


def pct_saved(baseline, optimized):
    if baseline == 0:
        return "N/A"
    return f"{((baseline - optimized) / baseline) * 100:.1f}%"


def main():
    print("=== Chunker Optimizer POC ===", file=sys.stderr)
    print(f"Rounds per pattern per profile: {ROUNDS}", file=sys.stderr)
    print("Note: Chunker operates on 'system' field only.\n", file=sys.stderr)

    # Warmup
    print("Warming up...", file=sys.stderr)
    send_request("opt-ch-base", "Be brief.", "Hello", 0)
    send_request("opt-ch-only", "Be brief.", "Hello", 0)
    time.sleep(0.5)
    print("Warmup done.\n", file=sys.stderr)

    results = []
    for i, pattern in enumerate(PATTERNS, 1):
        label = pattern["label"]
        sys_text = pattern["system"]
        user_msg = pattern["user"]
        sys_len = len(sys_text)
        print(f"Test #{i}: {label} (system={sys_len} chars)", file=sys.stderr)

        base_in, base_out, base_err = median_of("opt-ch-base", sys_text, user_msg, ROUNDS)
        print(f"  Baseline: in={base_in} out={base_out}", file=sys.stderr)

        chk_in, chk_out, chk_err = median_of("opt-ch-only", sys_text, user_msg, ROUNDS)
        print(f"  Chunker:  in={chk_in} out={chk_out}", file=sys.stderr)

        in_saved = pct_saved(base_in, chk_in)
        out_saved = pct_saved(base_out, chk_out)

        if base_err or chk_err:
            verdict = "ERROR"
        elif chk_in < base_in:
            verdict = "SAVED"
        elif chk_in == base_in:
            verdict = "NO CHANGE"
        else:
            verdict = "OVERHEAD"

        results.append({
            "#": i, "Pattern": label,
            "Base In": base_in, "Chk In": chk_in, "In Saved": in_saved,
            "Base Out": base_out, "Chk Out": chk_out, "Out Saved": out_saved,
            "Verdict": verdict,
        })

    print()
    header = f"| # | {'Pattern':<36} | {'Base In':>8} | {'Chk In':>7} | {'In Saved':>9} | {'Base Out':>8} | {'Chk Out':>7} | {'Out Saved':>9} | {'Verdict':<10} |"
    sep =    f"|---| {'':-<36} | {'':->8} | {'':->7} | {'':->9} | {'':->8} | {'':->7} | {'':->9} | {'':-<10} |"
    print(header)
    print(sep)
    for r in results:
        print(f"| {r['#']} | {r['Pattern']:<36} | {r['Base In']:>8} | {r['Chk In']:>7} | {r['In Saved']:>9} | {r['Base Out']:>8} | {r['Chk Out']:>7} | {r['Out Saved']:>9} | {r['Verdict']:<10} |")


if __name__ == "__main__":
    main()
