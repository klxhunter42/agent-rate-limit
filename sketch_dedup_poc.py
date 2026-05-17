#!/usr/bin/env python3
"""POC: Sketch Dedup (MinHash) optimizer investigation."""

import json
import urllib.request
import urllib.error
import time
import sys

GATEWAY = "http://localhost:18080"
API_KEY = "1d63b5db6d984db1913ca9596125f06b.RHYoZWfoRZteSclW"
MODEL = "glm-5.1"

def api_call(path, method="GET", data=None, headers=None):
    hdrs = {"Content-Type": "application/json"}
    if headers:
        hdrs.update(headers)
    body = json.dumps(data).encode() if data else None
    req = urllib.request.Request(f"{GATEWAY}{path}", data=body, headers=hdrs, method=method)
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        body = e.read().decode()
        print(f"  HTTP {e.code}: {body[:300]}", file=sys.stderr)
        return {"error": e.code, "body": body}
    except Exception as e:
        print(f"  Error: {e}", file=sys.stderr)
        return {"error": str(e)}


def create_profiles():
    """Create baseline and sketch-only profiles."""
    # Delete existing if any
    for name in ["opt-sk-base", "opt-sk-only"]:
        api_call(f"/v1/profiles/{name}", method="DELETE")

    base = {
        "name": "opt-sk-base",
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
        }
    }
    sketch = {
        "name": "opt-sk-only",
        "target": "zai",
        "provider": "zai",
        "passthroughAuth": True,
        "optimizerOverrides": {
            "semantic_dedup": False,
            "chunker": False,
            "sketch": True,
            "textcomp": False,
            "caveman": False,
            "pordee": False,
            "toolcomp": False,
            "toolfilter": False,
        }
    }

    r1 = api_call("/v1/profiles", method="POST", data=base)
    r2 = api_call("/v1/profiles", method="POST", data=sketch)
    print(f"Profile base: {json.dumps(r1, indent=2)[:200]}")
    print(f"Profile sketch: {json.dumps(r2, indent=2)[:200]}")
    return r1, r2


def make_request(profile, system_text, messages, max_tokens=512):
    """Send a /v1/messages request with given profile and payload."""
    data = {
        "model": MODEL,
        "max_tokens": max_tokens,
        "system": system_text,
        "messages": messages,
    }
    headers = {
        "x-api-key": API_KEY,
        "content-type": "application/json",
        "anthropic-version": "2023-06-01",
    }
    req = urllib.request.Request(
        f"{GATEWAY}/v1/messages?profile={profile}",
        data=json.dumps(data).encode(),
        headers=headers,
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            body = resp.read().decode()
            # Calculate input/output bytes
            input_bytes = len(json.dumps(data))
            result = json.loads(body)
            output_bytes = len(body)
            return {
                "input_bytes": input_bytes,
                "output_bytes": output_bytes,
                "result": result,
            }
    except urllib.error.HTTPError as e:
        body = e.read().decode()
        print(f"  HTTP {e.code}: {body[:500]}", file=sys.stderr)
        return {"error": e.code, "input_bytes": len(json.dumps(data)), "output_bytes": 0}
    except Exception as e:
        print(f"  Error: {e}", file=sys.stderr)
        return {"error": str(e), "input_bytes": 0, "output_bytes": 0}


def get_metrics():
    """Fetch sketch metrics from /metrics endpoint."""
    req = urllib.request.Request(f"{GATEWAY}/metrics")
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            text = resp.read().decode()
            metrics = {}
            for line in text.split("\n"):
                if line.startswith("#") or not line.strip():
                    continue
                if "sketch" in line:
                    parts = line.split()
                    metrics[parts[0]] = parts[1]
            return metrics
    except Exception as e:
        print(f"  Metrics error: {e}", file=sys.stderr)
        return {}


# ---- PAYLOADS ----

# Payload 1: System prompt with 3 paragraphs saying the same thing differently
PAYLOAD_1_SYSTEM = """You are a helpful coding assistant. You must always write clean, well-structured code.

You are an assistant that helps developers write good code. Always produce clean and well-organized code that follows best practices.

As a coding helper, your job is to generate high-quality code. Make sure the code you write is clean, follows proper structure, and adheres to established coding conventions and best practices.

When writing code, prioritize readability and maintainability. Use meaningful variable names, add comments where logic is unclear, and follow the DRY principle.

Remember: clean code is essential. Write code that others can easily understand and maintain. Follow best practices at all times."""

PAYLOAD_1_MSGS = [
    {"role": "user", "content": "Write a hello world in Python."}
]

# Payload 2: K8s logs with same message, different timestamps
PAYLOAD_2_SYSTEM = """You are a Kubernetes log analyzer. Analyze the following logs and identify patterns.

Pod: api-gateway-7d9f8c6b5-xk2j4
Namespace: production

2024-01-15 08:01:23.456 [INFO] Request processed successfully in 145ms - GET /api/v1/users
2024-01-15 08:01:24.789 [INFO] Request processed successfully in 132ms - GET /api/v1/users
2024-01-15 08:01:25.123 [INFO] Request processed successfully in 198ms - GET /api/v1/users
2024-01-15 08:01:26.456 [INFO] Request processed successfully in 156ms - GET /api/v1/users
2024-01-15 08:01:27.789 [INFO] Request processed successfully in 143ms - GET /api/v1/users
2024-01-15 08:01:28.123 [INFO] Request processed successfully in 167ms - GET /api/v1/users
2024-01-15 08:01:29.456 [INFO] Request processed successfully in 189ms - GET /api/v1/users
2024-01-15 08:01:30.789 [INFO] Request processed successfully in 134ms - GET /api/v1/users
2024-01-15 08:01:31.123 [INFO] Request processed successfully in 178ms - GET /api/v1/users
2024-01-15 08:01:32.456 [INFO] Request processed successfully in 145ms - GET /api/v1/users
2024-01-15 08:01:33.789 [INFO] Request processed successfully in 201ms - GET /api/v1/users
2024-01-15 08:01:34.123 [INFO] Request processed successfully in 155ms - GET /api/v1/users
2024-01-15 08:01:35.456 [INFO] Request processed successfully in 144ms - GET /api/v1/users
2024-01-15 08:01:36.789 [INFO] Request processed successfully in 167ms - GET /api/v1/users
2024-01-15 08:01:37.123 [INFO] Request processed successfully in 134ms - GET /api/v1/users"""

PAYLOAD_2_MSGS = [
    {"role": "user", "content": "What patterns do you see in these logs?"}
]

# Payload 3: Error variations
PAYLOAD_3_SYSTEM = """You are an error analysis assistant. Review these errors and categorize them.

Error Report - Service: payment-gateway

[ERROR] 2024-01-15T10:23:45Z - Connection timeout to database primary host db-primary.internal:5432 after 30s
[ERROR] 2024-01-15T10:23:46Z - Connection timeout to database replica host db-replica-1.internal:5432 after 30s
[ERROR] 2024-01-15T10:23:47Z - Connection timeout to database replica host db-replica-2.internal:5432 after 30s
[ERROR] 2024-01-15T10:24:01Z - Failed to establish connection to Redis cluster at redis-1.internal:6379 - timeout
[ERROR] 2024-01-15T10:24:02Z - Failed to establish connection to Redis cluster at redis-2.internal:6379 - timeout
[ERROR] 2024-01-15T10:24:03Z - Failed to establish connection to Redis cluster at redis-3.internal:6379 - timeout
[WARN]  2024-01-15T10:24:15Z - Circuit breaker OPEN for payment-service downstream
[WARN]  2024-01-15T10:24:16Z - Circuit breaker OPEN for fraud-detection downstream
[WARN]  2024-01-15T10:24:17Z - Circuit breaker OPEN for notification-service downstream
[ERROR] 2024-01-15T10:24:30Z - Health check failed for all upstream providers
[ERROR] 2024-01-15T10:24:31Z - Health check failed for load balancer pool"""

PAYLOAD_3_MSGS = [
    {"role": "user", "content": "Categorize these errors by severity and root cause."}
]

# Payload 4: Two user messages asking the same thing differently
PAYLOAD_4_SYSTEM = "You are a helpful assistant."

PAYLOAD_4_MSGS = [
    {"role": "user", "content": "How do I deploy a containerized application to a Kubernetes cluster using Helm charts and ArgoCD for GitOps-based continuous deployment?"},
    {"role": "assistant", "content": "To deploy a containerized application to Kubernetes using Helm and ArgoCD:\n\n1. Create a Helm chart for your application\n2. Push the chart to a Helm repository\n3. Create an ArgoCD Application manifest pointing to the Helm chart\n4. ArgoCD will sync and deploy automatically\n\nWould you like more details on any step?"},
    {"role": "user", "content": "Can you explain how to use Helm charts with ArgoCD to deploy containers on Kubernetes? I want to set up GitOps continuous deployment."},
    {"role": "assistant", "content": "Here's how to set up GitOps deployment with Helm and ArgoCD:\n\n1. Package your app as a Helm chart\n2. Store the chart in a Git repository\n3. Configure ArgoCD to watch the repo\n4. ArgoCD handles automated deployments\n\nLet me know if you need specifics."},
    {"role": "user", "content": "I need a walkthrough for deploying containerized apps to K8s with Helm and ArgoCD for automated GitOps deployment."},
]

# Payload 5: Control - unique content, no dupes
PAYLOAD_5_SYSTEM = """You are a research assistant specializing in marine biology.

The ocean covers approximately 71% of Earth's surface and contains 97% of Earth's water.
The Mariana Trench reaches a depth of approximately 11,034 meters (36,201 feet).
Marine ecosystems include coral reefs, deep-sea hydrothermal vents, kelp forests, and open ocean pelagic zones.
Phytoplankton produce approximately 50-80% of Earth's oxygen through photosynthesis.
The Great Barrier Reef is the world's largest coral reef system, spanning over 2,300 kilometers.
Bioluminescence occurs in approximately 76% of deep-sea organisms.
The ocean's average depth is about 3,688 meters (12,100 feet).
Cephalopods like octopuses have three hearts and blue, copper-based blood."""

PAYLOAD_5_MSGS = [
    {"role": "user", "content": "Tell me about the deepest part of the ocean and what lives there."}
]


def run_test(test_num, name, system, messages, is_near_dupe):
    """Run a single test: send request with both profiles and compare."""
    print(f"\n--- Test {test_num}: {name} (near-dupes: {is_near_dupe}) ---")

    # For sketch dedup to trigger, we need to send MULTIPLE requests with the SAME system prompt
    # The sketch is stored per-model in Redis, so the 2nd+ request should detect the dupe

    # Step 1: Send with baseline (no sketch) - just measure
    r_base = make_request("opt-sk-base", system, messages)
    base_in = r_base.get("input_bytes", 0)
    base_out = r_base.get("output_bytes", 0)
    print(f"  Baseline: in={base_in}, out={base_out}")

    if "error" in r_base and r_base.get("output_bytes", 0) == 0:
        print(f"  Baseline FAILED: {r_base.get('error')}, {r_base.get('body', '')[:200]}")
        base_in = len(system) + sum(len(str(m)) for m in messages)

    # Step 2: Send 1st request with sketch profile (stores sketch)
    r_sk1 = make_request("opt-sk-only", system, messages)
    sk1_in = r_sk1.get("input_bytes", 0)
    sk1_out = r_sk1.get("output_bytes", 0)
    print(f"  Sketch req#1: in={sk1_in}, out={sk1_out}")

    # Step 3: Send 2nd request with SAME system prompt (should detect near-dupe)
    time.sleep(0.3)
    r_sk2 = make_request("opt-sk-only", system, messages)
    sk2_in = r_sk2.get("input_bytes", 0)
    sk2_out = r_sk2.get("output_bytes", 0)
    print(f"  Sketch req#2: in={sk2_in}, out={sk2_out}")

    # Step 4: Send 3rd request (slightly modified system prompt for near-dupe test)
    # Add a small variation to the system prompt
    modified_system = system + "\n\nAdditional note: always be concise in responses."
    r_sk3 = make_request("opt-sk-only", modified_system, messages)
    sk3_in = r_sk3.get("input_bytes", 0)
    sk3_out = r_sk3.get("output_bytes", 0)
    print(f"  Sketch req#3 (modified): in={sk3_in}, out={sk3_out}")

    # Check metrics after each test
    time.sleep(0.3)
    metrics = get_metrics()
    print(f"  Sketch metrics: {json.dumps(metrics, indent=2)}")

    return {
        "test_num": test_num,
        "name": name,
        "near_dupes": is_near_dupe,
        "base_in": base_in,
        "base_out": base_out,
        "sk1_in": sk1_in,
        "sk1_out": sk1_out,
        "sk2_in": sk2_in,
        "sk2_out": sk2_out,
        "sk3_in": sk3_in,
        "sk3_out": sk3_out,
        "metrics": metrics,
    }


def main():
    print("=== Sketch Dedup (MinHash) POC Test ===\n")

    # Create profiles
    print("Creating profiles...")
    create_profiles()
    time.sleep(0.5)

    # Get initial metrics
    print("\nInitial sketch metrics:")
    initial_metrics = get_metrics()
    print(f"  {json.dumps(initial_metrics, indent=2)}")

    payloads = [
        (1, "Paraphrased paragraphs", PAYLOAD_1_SYSTEM, PAYLOAD_1_MSGS, "Yes"),
        (2, "K8s logs (timestamp variants)", PAYLOAD_2_SYSTEM, PAYLOAD_2_MSGS, "Yes"),
        (3, "Error variations", PAYLOAD_3_SYSTEM, PAYLOAD_3_MSGS, "Yes"),
        (4, "Repeated questions (messages)", PAYLOAD_4_SYSTEM, PAYLOAD_4_MSGS, "Yes"),
        (5, "Control (unique content)", PAYLOAD_5_SYSTEM, PAYLOAD_5_MSGS, "No"),
    ]

    results = []
    for num, name, sys_text, msgs, is_dupe in payloads:
        r = run_test(num, name, sys_text, msgs, is_dupe)
        results.append(r)
        time.sleep(0.5)

    # Final metrics
    print("\n=== Final sketch metrics ===")
    final_metrics = get_metrics()
    print(json.dumps(final_metrics, indent=2))

    # Summary table
    print("\n=== RESULTS TABLE ===")
    print(f"| # | Payload | Near-Dupes? | Base In | Sk1 In | Sk2 In | Sk3 In | Base Out | Sk1 Out | Sk2 Out | Sk3 Out | Verdict |")
    print(f"|---|---------|-------------|---------|--------|--------|--------|----------|---------|---------|---------|---------|")
    for r in results:
        # Calculate if sketch actually saved anything
        # Sketch dedup records chars_saved but doesn't modify the text (it's metrics-only!)
        verdict = "metrics-only"  # Based on code analysis
        if r["metrics"]:
            verdict += f" | metrics: {r['metrics']}"
        print(f"| {r['test_num']} | {r['name']} | {r['near_dupes']} | {r['base_in']} | {r['sk1_in']} | {r['sk2_in']} | {r['sk3_in']} | {r['base_out']} | {r['sk1_out']} | {r['sk2_out']} | {r['sk3_out']} | {verdict} |")

    print("\n=== ANALYSIS ===")
    print("Key finding from code review:")
    print("- Sketch dedup calls CheckAndStore() on the SYSTEM PROMPT")
    print("- It stores sketches in Redis keyed by model name: sketch:recent:{model}")
    print("- On duplicate detection, it records metrics but the code shows:")
    print('  slog.Info("optimizer_step", ..., "before", len(text), "after", len(text), "saved", saved)')
    print('  m.RecordOptimization("sketch_dedup", saved, "input")')
    print("- CRITICAL: text is NOT modified! before == after == len(text)")
    print("- It records savings as len(content) but never actually removes content")
    print("- This is a METRICS-ONLY optimizer - it measures potential savings without realizing them")

    # Check delta between baseline and sketch profile
    print("\n=== INPUT SIZE COMPARISON (base vs sketch) ===")
    for r in results:
        diff = r["base_in"] - r["sk2_in"]  # 2nd sketch req should show savings if it worked
        pct = (diff / r["base_in"] * 100) if r["base_in"] > 0 else 0
        print(f"  Test {r['test_num']}: base_in={r['base_in']}, sk2_in={r['sk2_in']}, diff={diff}, pct={pct:.1f}%")


if __name__ == "__main__":
    main()
