#!/usr/bin/env python3
"""Caveman optimizer POC test."""
import json
import urllib.request
import urllib.error
import time
import sys

BASE = "http://localhost:18080"
API_KEY = "1d63b5db6d984db1913ca9596125f06b.RHYoZWfoRZteSclW"
MODEL = "glm-5.1"
MAX_TOKENS = 512
HEADERS_BASE = {
    "Content-Type": "application/json",
    "Authorization": f"Bearer {API_KEY}",
}

SCENARIOS = [
    {
        "label": "Simple question",
        "messages": [{"role": "user", "content": "What is 2+2?"}],
    },
    {
        "label": "Code generation",
        "messages": [{"role": "user", "content": "Write a Python HTTP server with Flask that has endpoints for GET /health and POST /data"}],
    },
    {
        "label": "Debugging",
        "messages": [{"role": "user", "content": "Find the bug in this code:\n\ndef fibonacci(n):\n    if n <= 0:\n        return 0\n    elif n == 1:\n        return 1\n    else:\n        return fibonacci(n) + fibonacci(n-1)\n\nprint(fibonacci(10))"}],
    },
    {
        "label": "Explanation",
        "messages": [{"role": "user", "content": "Explain how Kubernetes networking works, including Services, Ingress, and pod-to-pod communication"}],
    },
    {
        "label": "Multi-step task",
        "messages": [{"role": "user", "content": "Write a deployment pipeline with stages: build, test, deploy, rollback. Include error handling and notifications for each stage."}],
    },
]

PROFILES = {
    "baseline": {
        "name": "opt-cav-base",
        "body": {
            "name": "opt-cav-base",
            "target": "zai",
            "provider": "zai",
            "passthroughAuth": True,
            "optimizerOverrides": {
                "semantic_dedup": False,
                "chunker": False,
                "sketch": False,
                "textcomp": False,
                "caveman": False,
                "pordee": False,
                "toolcomp": False,
                "toolfilter": False,
            },
        },
    },
    "caveman": {
        "name": "opt-cav-only",
        "body": {
            "name": "opt-cav-only",
            "target": "zai",
            "provider": "zai",
            "passthroughAuth": True,
            "optimizerOverrides": {
                "semantic_dedup": False,
                "chunker": False,
                "sketch": False,
                "textcomp": False,
                "pordee": False,
                "toolcomp": False,
                "toolfilter": False,
            },
        },
    },
}


def api_call(method, path, body=None, extra_headers=None):
    headers = dict(HEADERS_BASE)
    if extra_headers:
        headers.update(extra_headers)
    data = json.dumps(body).encode() if body else None
    req = urllib.request.Request(f"{BASE}{path}", data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            return json.loads(resp.read().decode()), resp.status
    except urllib.error.HTTPError as e:
        err_body = e.read().decode()
        return {"error": err_body, "status": e.code}, e.code
    except Exception as e:
        return {"error": str(e)}, 0


def send_message(profile_name, messages):
    body = {
        "model": MODEL,
        "max_tokens": MAX_TOKENS,
        "messages": messages,
    }
    headers = {"X-Profile": profile_name}
    return api_call("POST", "/v1/messages", body, headers)


def count_tokens_from_usage(resp):
    if "usage" in resp:
        u = resp["usage"]
        return u.get("input_tokens", u.get("prompt_tokens", 0)), u.get("output_tokens", u.get("completion_tokens", 0))
    return 0, 0


def extract_text(resp):
    if "choices" in resp:
        # OpenAI-style
        text = ""
        for c in resp["choices"]:
            msg = c.get("message", {})
            text += msg.get("content", "")
        return text
    if "content" in resp:
        # Anthropic-style
        text = ""
        for block in resp["content"]:
            if block.get("type") == "text":
                text += block.get("text", "")
        return text
    return str(resp)[:200]


def delete_profile(name):
    try:
        api_call("DELETE", f"/v1/profiles/{name}")
    except:
        pass


def main():
    # Delete existing profiles if any
    for p in PROFILES.values():
        delete_profile(p["name"])
    time.sleep(0.5)

    # Create profiles
    print("Creating profiles...")
    for key, p in PROFILES.items():
        resp, status = api_call("POST", "/v1/profiles", p["body"])
        if status in (200, 201):
            print(f"  Created profile: {p['name']}")
        else:
            print(f"  Failed to create {p['name']}: {status} {resp}")
            sys.exit(1)

    time.sleep(1)

    # Run scenarios
    results = []
    for i, scenario in enumerate(SCENARIOS):
        print(f"\nScenario {i+1}: {scenario['label']}")

        # Baseline
        b_resp, b_status = send_message(PROFILES["baseline"]["name"], scenario["messages"])
        b_in, b_out = count_tokens_from_usage(b_resp)
        b_text = extract_text(b_resp)
        b_len = len(b_text)
        print(f"  Baseline: status={b_status}, in={b_in}, out={b_out}, text_len={b_len}")
        if "error" in b_resp:
            print(f"    Error: {b_resp['error'][:300]}")

        time.sleep(2)

        # Caveman
        c_resp, c_status = send_message(PROFILES["caveman"]["name"], scenario["messages"])
        c_in, c_out = count_tokens_from_usage(c_resp)
        c_text = extract_text(c_resp)
        c_len = len(c_text)
        print(f"  Caveman:  status={c_status}, in={c_in}, out={c_out}, text_len={c_len}")
        if "error" in c_resp:
            print(f"    Error: {c_resp['error'][:300]}")

        time.sleep(2)

        in_delta = c_in - b_in
        out_delta = c_out - b_out
        # cost delta: approximate as (in * 0.001 + out * 0.003) ratio
        b_cost = b_in * 0.001 + b_out * 0.003
        c_cost = c_in * 0.001 + c_out * 0.003
        cost_delta_pct = ((c_cost - b_cost) / b_cost * 100) if b_cost > 0 else 0

        if out_delta < 0:
            verdict = "SAVED"
        elif out_delta == 0:
            verdict = "NEUTRAL"
        else:
            verdict = "OVERHEAD"

        results.append({
            "num": i + 1,
            "label": scenario["label"],
            "b_in": b_in, "c_in": c_in, "in_delta": in_delta,
            "b_out": b_out, "c_out": c_out, "out_delta": out_delta,
            "cost_delta_pct": cost_delta_pct,
            "verdict": verdict,
            "b_text_len": b_len,
            "c_text_len": c_len,
        })

    # Output table
    print("\n" + "=" * 120)
    print("| # | Scenario          | Baseline In | Caveman In | In Δ  | Baseline Out | Caveman Out | Out Δ  | Cost Δ   | Verdict  |")
    print("|---|-------------------|-------------|------------|-------|--------------|-------------|--------|----------|----------|")
    for r in results:
        in_d = f"{r['in_delta']:+d}"
        out_d = f"{r['out_delta']:+d}"
        cost_d = f"{r['cost_delta_pct']:+.1f}%"
        print(f"| {r['num']} | {r['label']:<17} | {r['b_in']:>11} | {r['c_in']:>10} | {in_d:>5} | {r['b_out']:>12} | {r['c_out']:>11} | {out_d:>6} | {cost_d:>8} | {r['verdict']:<8} |")

    # Summary
    total_b_out = sum(r["b_out"] for r in results)
    total_c_out = sum(r["c_out"] for r in results)
    total_out_delta = total_c_out - total_b_out
    saved_pct = (total_out_delta / total_b_out * 100) if total_b_out > 0 else 0
    print(f"\nTotal output tokens: Baseline={total_b_out}, Caveman={total_c_out}, Δ={total_out_delta} ({saved_pct:+.1f}%)")

    total_b_in = sum(r["b_in"] for r in results)
    total_c_in = sum(r["c_in"] for r in results)
    total_in_delta = total_c_in - total_b_in
    in_pct = (total_in_delta / total_b_in * 100) if total_b_in > 0 else 0
    print(f"Total input tokens:  Baseline={total_b_in}, Caveman={total_c_in}, Δ={total_in_delta} ({in_pct:+.1f}%)")

    # Show sample outputs for comparison
    print("\n--- Sample Output Comparison (Scenario 1: Simple question) ---")
    # Re-fetch is expensive, just note from first run
    print("(See text_len in results above for char-level comparison)")


if __name__ == "__main__":
    main()
