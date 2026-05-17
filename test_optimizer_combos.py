#!/usr/bin/env python3
"""POC test: optimizer PAIR and TRIPLE combinations."""
import json
import urllib.request
import urllib.error
import time

BASE = "http://localhost:18080"
API_KEY = "1d63b5db6d984db1913ca9596125f06b.RHYoZWfoRZteSclW"

ALL_STAGES = ["semantic_dedup", "chunker", "sketch", "pordee", "toolcomp", "toolfilter"]

STAGE_MAP = {
    "textcomp": "semantic_dedup",
    "caveman": "pordee",
    "toolfilter": "toolfilter",
    "toolcomp": "toolcomp",
    "chunker": "chunker",
}

def make_profile(name, combo_keys):
    enabled_stages = {STAGE_MAP[k] for k in combo_keys}
    overrides = {}
    for s in ALL_STAGES:
        if s not in enabled_stages:
            overrides[s] = False
    return {
        "name": name,
        "target": "zai",
        "provider": "zai",
        "passthroughAuth": True,
        "optimizerOverrides": overrides,
    }

def create_profile(profile_data):
    data = json.dumps(profile_data).encode()
    req = urllib.request.Request(
        f"{BASE}/v1/profiles",
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            body = json.loads(resp.read())
            return body.get("id") or body.get("name")
    except urllib.error.HTTPError as e:
        print(f"  Profile create error {e.code}: {e.read().decode()}")
        return None

def delete_profile(profile_id):
    try:
        req = urllib.request.Request(f"{BASE}/v1/profiles/{profile_id}", method="DELETE")
        urllib.request.urlopen(req, timeout=5)
    except Exception:
        pass

def send_request(profile_id, payload):
    data = json.dumps(payload).encode()
    headers = {
        "Content-Type": "application/json",
        "x-api-key": API_KEY,
        "anthropic-version": "2023-06-01",
        "X-Profile": profile_id,
    }
    req = urllib.request.Request(f"{BASE}/v1/messages", data=data, headers=headers, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            body = json.loads(resp.read())
            usage = body.get("usage", {})
            inp = usage.get("input_tokens", 0)
            out = usage.get("output_tokens", 0)
            return inp, out, body
    except urllib.error.HTTPError as e:
        err = e.read().decode()
        print(f"  Request error {e.code}: {err[:200]}")
        return None, None, None
    except Exception as e:
        print(f"  Request exception: {e}")
        return None, None, None

# --- Payloads ---

VERBOSE_SYSTEM = (
    "You are an expert Go developer. You write clean, idiomatic Go code. "
    "You follow best practices for error handling, concurrency, and testing. "
    "You prefer simple solutions over clever ones. You always use context.Context. "
    "You prefer table-driven tests. You use structured logging with slog. "
    "You avoid global state. You keep interfaces small. You return errors rather than panic. "
    "You use defer for cleanup. You follow best practices for error handling, concurrency, and testing. "
    "You prefer simple solutions over clever ones."
)

VERBOSE_PAYLOAD = {
    "model": "glm-5.1",
    "max_tokens": 512,
    "system": VERBOSE_SYSTEM,
    "messages": [{"role": "user", "content": "Write a Go LRU cache with TTL."}],
}

K8S_LOGS = """\
E0501 12:00:01.234 1 pod.go:123] Failed to sync pod abc-123: connection refused
E0501 12:00:01.456 1 pod.go:123] Failed to sync pod abc-123: connection refused
E0501 12:00:02.789 1 pod.go:456] Liveness probe failed: Get http://localhost:8080/health: dial tcp 127.0.0.1:8080: connect: connection refused
W0501 12:00:03.012 1 reflector.go:234] watch of *v1.ConfigMap ended with: too old resource version
E0501 12:00:03.345 1 pod.go:123] Failed to sync pod abc-123: connection refused
E0501 12:00:04.567 1 event.go:123] Unable to write event: 'Post "https://10.0.0.1:443/api/v1/namespaces/default/events": dial tcp 10.0.0.1:443: connect: connection refused' (may retry)
E0501 12:00:05.890 1 pod.go:789] Error deleting pod: apiserver not reachable
W0501 12:00:06.123 1 reflector.go:234] watch of *v1.ConfigMap ended with: too old resource version
E0501 12:00:06.456 1 pod.go:123] Failed to sync pod abc-123: connection refused
E0501 12:00:07.789 1 kubelet.go:456] Container runtime not ready: NetworkPluginNotReady
E0501 12:00:08.012 1 pod.go:123] Failed to sync pod abc-123: connection refused
W0501 12:00:08.234 1 reflector.go:234] watch of *v1.ConfigMap ended with: too old resource version
E0501 12:00:09.567 1 pod.go:789] Error deleting pod: apiserver not reachable
E0501 12:00:10.890 1 pod.go:123] Failed to sync pod abc-123: connection refused
E0501 12:00:11.123 1 kubelet.go:456] Container runtime not ready: NetworkPluginNotReady
E0501 12:00:12.456 1 pod.go:123] Failed to sync pod abc-123: connection refused
W0501 12:00:13.789 1 reflector.go:234] watch of *v1.ConfigMap ended with: too old resource version
E0501 12:00:14.012 1 pod.go:789] Error deleting pod: apiserver not reachable
E0501 12:00:15.345 1 pod.go:123] Failed to sync pod abc-123: connection refused
E0501 12:00:16.678 1 kubelet.go:456] Container runtime not ready: NetworkPluginNotReady
E0501 12:00:17.901 1 pod.go:123] Failed to sync pod abc-123: connection refused
W0501 12:00:18.234 1 reflector.go:234] watch of *v1.ConfigMap ended with: too old resource version
E0501 12:00:19.567 1 pod.go:789] Error deleting pod: apiserver not reachable
E0501 12:00:20.890 1 pod.go:123] Failed to sync pod abc-123: connection refused
E0501 12:00:21.123 1 kubelet.go:456] Container runtime not ready: NetworkPluginNotReady
E0501 12:00:22.456 1 pod.go:123] Failed to sync pod abc-123: connection refused
E0501 12:00:23.789 1 event.go:123] Unable to write event: connection refused
W0501 12:00:24.012 1 reflector.go:234] watch of *v1.ConfigMap ended with: too old resource version
E0501 12:00:25.345 1 pod.go:123] Failed to sync pod abc-123: connection refused
E0501 12:00:26.678 1 kubelet.go:456] Container runtime not ready: NetworkPluginNotReady
E0501 12:00:27.901 1 pod.go:789] Error deleting pod: apiserver not reachable
E0501 12:00:28.234 1 pod.go:123] Failed to sync pod abc-123: connection refused
E0501 12:00:29.567 1 event.go:123] Unable to write event: connection refused
E0501 12:00:30.890 1 pod.go:123] Failed to sync pod abc-123: connection refused
W0501 12:00:31.123 1 reflector.go:234] watch of *v1.ConfigMap ended with: too old resource version
E0501 12:00:32.456 1 kubelet.go:456] Container runtime not ready: NetworkPluginNotReady
E0501 12:00:33.789 1 pod.go:123] Failed to sync pod abc-123: connection refused"""

K8S_PAYLOAD = {
    "model": "glm-5.1",
    "max_tokens": 512,
    "system": "You are a DevOps engineer. Diagnose issues from pod logs.",
    "messages": [
        {"role": "user", "content": f"I ran `kubectl logs abc-123 -n default --tail=50` and got:\n\n{K8S_LOGS}\n\nRoot cause and fix?"},
    ],
}

# --- Combinations ---

PAIRS = [
    ("TextComp+Caveman", ["textcomp", "caveman"]),
    ("TextComp+ToolFilter", ["textcomp", "toolfilter"]),
    ("Caveman+ToolFilter", ["caveman", "toolfilter"]),
    ("TextComp+Chunker", ["textcomp", "chunker"]),
    ("Caveman+Chunker", ["caveman", "chunker"]),
    ("ToolComp+ToolFilter", ["toolcomp", "toolfilter"]),
]

TRIPLES = [
    ("TextComp+Caveman+ToolComp", ["textcomp", "caveman", "toolcomp"]),
    ("TextComp+Caveman+Chunker", ["textcomp", "caveman", "chunker"]),
    ("TextComp+ToolComp+ToolFilter", ["textcomp", "toolcomp", "toolfilter"]),
    ("Caveman+ToolComp+Chunker", ["caveman", "toolcomp", "chunker"]),
]

ALL_COMBOS = [(n, s, "Pair") for n, s in PAIRS] + [(n, s, "Triple") for n, s in TRIPLES]

def main():
    results = []

    for idx, (combo_name, stages, ctype) in enumerate(ALL_COMBOS, 1):
        profile_name = f"opt-test-{idx}"
        print(f"\n[{idx}/{len(ALL_COMBOS)}] {ctype}: {combo_name}")

        pdata = make_profile(profile_name, stages)
        pid = create_profile(pdata)
        if not pid:
            print(f"  SKIP: could not create profile")
            results.append((idx, combo_name, ctype, "ERR", "ERR", "ERR", "ERR", "ERR"))
            continue
        print(f"  Profile: {pid}, stages: {stages}")

        time.sleep(0.3)

        # Verbose payload
        print(f"  Verbose payload...")
        v_in, v_out, _ = send_request(pid, VERBOSE_PAYLOAD)
        print(f"    in={v_in}, out={v_out}")

        time.sleep(0.5)

        # K8s payload
        print(f"  K8s payload...")
        k_in, k_out, _ = send_request(pid, K8S_PAYLOAD)
        print(f"    in={k_in}, out={k_out}")

        if v_in is not None and k_in is not None:
            total_cost = v_in + v_out + k_in + k_out
        else:
            total_cost = "ERR"

        results.append((idx, combo_name, ctype, v_in, v_out, k_in, k_out, total_cost))

        delete_profile(pid)
        time.sleep(0.3)

    # Sort by total cost
    valid = [r for r in results if isinstance(r[7], int)]
    invalid = [r for r in results if not isinstance(r[7], int)]
    valid.sort(key=lambda r: r[7])
    ranked = valid + invalid

    # Print table
    print("\n" + "=" * 130)
    print(f"| # | Combo | Type | Verbose In | Verbose Out | K8s In | K8s Out | Total Cost | Rank |")
    print(f"|---|-------|------|------------|-------------|--------|---------|------------|------|")
    for rank, r in enumerate(ranked, 1):
        idx, name, ctype, vi, vo, ki, ko, tc = r
        vi_s = f"{vi:,}" if isinstance(vi, int) else str(vi)
        vo_s = f"{vo:,}" if isinstance(vo, int) else str(vo)
        ki_s = f"{ki:,}" if isinstance(ki, int) else str(ki)
        ko_s = f"{ko:,}" if isinstance(ko, int) else str(ko)
        tc_s = f"{tc:,}" if isinstance(tc, int) else str(tc)
        print(f"| {idx} | {name} | {ctype} | {vi_s:>10} | {vo_s:>11} | {ki_s:>6} | {ko_s:>7} | {tc_s:>10} | {rank} |")

    if valid:
        winner = ranked[0]
        print(f"\nBest combo: {winner[1]} ({winner[2]}) with total cost {winner[7]:,} tokens")

if __name__ == "__main__":
    main()
