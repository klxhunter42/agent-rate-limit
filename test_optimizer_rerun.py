#!/usr/bin/env python3
"""Re-run failed combos + baseline."""
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
    enabled = {STAGE_MAP[k] for k in combo_keys}
    overrides = {s: False for s in ALL_STAGES if s not in enabled}
    return {"name": name, "target": "zai", "provider": "zai", "passthroughAuth": True, "optimizerOverrides": overrides}

def create_profile(d):
    req = urllib.request.Request(f"{BASE}/v1/profiles", data=json.dumps(d).encode(), headers={"Content-Type": "application/json"}, method="POST")
    with urllib.request.urlopen(req, timeout=10) as r:
        b = json.loads(r.read())
        return b.get("id") or b.get("name")

def delete_profile(pid):
    try:
        urllib.request.urlopen(urllib.request.Request(f"{BASE}/v1/profiles/{pid}", method="DELETE"), timeout=5)
    except Exception:
        pass

def send(pid, payload):
    req = urllib.request.Request(f"{BASE}/v1/messages", data=json.dumps(payload).encode(), headers={"Content-Type": "application/json", "x-api-key": API_KEY, "anthropic-version": "2023-06-01", "X-Profile": pid}, method="POST")
    with urllib.request.urlopen(req, timeout=120) as r:
        b = json.loads(r.read())
        u = b.get("usage", {})
        return u.get("input_tokens", 0), u.get("output_tokens", 0)

VERBOSE_SYSTEM = "You are an expert Go developer. You write clean, idiomatic Go code. You follow best practices for error handling, concurrency, and testing. You prefer simple solutions over clever ones. You always use context.Context. You prefer table-driven tests. You use structured logging with slog. You avoid global state. You keep interfaces small. You return errors rather than panic. You use defer for cleanup. You follow best practices for error handling, concurrency, and testing. You prefer simple solutions over clever ones."

VERBOSE = {"model": "glm-5.1", "max_tokens": 512, "system": VERBOSE_SYSTEM, "messages": [{"role": "user", "content": "Write a Go LRU cache with TTL."}]}

K8S_LOGS = "E0501 12:00:01.234 1 pod.go:123] Failed to sync pod abc-123: connection refused\nE0501 12:00:01.456 1 pod.go:123] Failed to sync pod abc-123: connection refused\nE0501 12:00:02.789 1 pod.go:456] Liveness probe failed: Get http://localhost:8080/health: dial tcp 127.0.0.1:8080: connect: connection refused\nW0501 12:00:03.012 1 reflector.go:234] watch of *v1.ConfigMap ended with: too old resource version\nE0501 12:00:03.345 1 pod.go:123] Failed to sync pod abc-123: connection refused\nE0501 12:00:04.567 1 event.go:123] Unable to write event: Post https://10.0.0.1:443/api/v1/namespaces/default/events: dial tcp 10.0.0.1:443: connect: connection refused\nE0501 12:00:05.890 1 pod.go:789] Error deleting pod: apiserver not reachable\nW0501 12:00:06.123 1 reflector.go:234] watch of *v1.ConfigMap ended with: too old resource version\nE0501 12:00:06.456 1 pod.go:123] Failed to sync pod abc-123: connection refused\nE0501 12:00:07.789 1 kubelet.go:456] Container runtime not ready: NetworkPluginNotReady\nE0501 12:00:08.012 1 pod.go:123] Failed to sync pod abc-123: connection refused\nW0501 12:00:08.234 1 reflector.go:234] watch of *v1.ConfigMap ended with: too old resource version\nE0501 12:00:09.567 1 pod.go:789] Error deleting pod: apiserver not reachable\nE0501 12:00:10.890 1 pod.go:123] Failed to sync pod abc-123: connection refused\nE0501 12:00:11.123 1 kubelet.go:456] Container runtime not ready: NetworkPluginNotReady\nE0501 12:00:12.456 1 pod.go:123] Failed to sync pod abc-123: connection refused\nW0501 12:00:13.789 1 reflector.go:234] watch of *v1.ConfigMap ended with: too old resource version\nE0501 12:00:14.012 1 pod.go:789] Error deleting pod: apiserver not reachable\nE0501 12:00:15.345 1 pod.go:123] Failed to sync pod abc-123: connection refused\nE0501 12:00:16.678 1 kubelet.go:456] Container runtime not ready: NetworkPluginNotReady\nE0501 12:00:17.901 1 pod.go:123] Failed to sync pod abc-123: connection refused\nW0501 12:00:18.234 1 reflector.go:234] watch of *v1.ConfigMap ended with: too old resource version\nE0501 12:00:19.567 1 pod.go:789] Error deleting pod: apiserver not reachable\nE0501 12:00:20.890 1 pod.go:123] Failed to sync pod abc-123: connection refused\nE0501 12:00:21.123 1 kubelet.go:456] Container runtime not ready: NetworkPluginNotReady\nE0501 12:00:22.456 1 pod.go:123] Failed to sync pod abc-123: connection refused\nE0501 12:00:23.789 1 event.go:123] Unable to write event: connection refused\nW0501 12:00:24.012 1 reflector.go:234] watch of *v1.ConfigMap ended with: too old resource version\nE0501 12:00:25.345 1 pod.go:123] Failed to sync pod abc-123: connection refused\nE0501 12:00:26.678 1 kubelet.go:456] Container runtime not ready: NetworkPluginNotReady\nE0501 12:00:27.901 1 pod.go:789] Error deleting pod: apiserver not reachable\nE0501 12:00:28.234 1 pod.go:123] Failed to sync pod abc-123: connection refused\nE0501 12:00:29.567 1 event.go:123] Unable to write event: connection refused\nE0501 12:00:30.890 1 pod.go:123] Failed to sync pod abc-123: connection refused\nW0501 12:00:31.123 1 reflector.go:234] watch of *v1.ConfigMap ended with: too old resource version\nE0501 12:00:32.456 1 kubelet.go:456] Container runtime not ready: NetworkPluginNotReady\nE0501 12:00:33.789 1 pod.go:123] Failed to sync pod abc-123: connection refused"

K8S = {"model": "glm-5.1", "max_tokens": 512, "system": "You are a DevOps engineer. Diagnose issues from pod logs.", "messages": [{"role": "user", "content": f"I ran `kubectl logs abc-123 -n default --tail=50` and got:\n\n{K8S_LOGS}\n\nRoot cause and fix?"}]}

def test_combo(name, stages):
    print(f"\n--- {name} ({stages}) ---")
    pid = create_profile(make_profile(f"retest-{name}", stages))
    print(f"  Profile: {pid}")
    time.sleep(0.3)
    try:
        vi, vo = send(pid, VERBOSE)
        print(f"  Verbose: in={vi}, out={vo}")
        time.sleep(0.5)
        ki, ko = send(pid, K8S)
        print(f"  K8s: in={ki}, out={ko}")
        total = vi + vo + ki + ko
        print(f"  TOTAL: {total:,}")
        return (name, vi, vo, ki, ko, total)
    except Exception as e:
        print(f"  Error: {e}")
        return None
    finally:
        delete_profile(pid)

# Baseline: all optimizers OFF
def test_baseline():
    print("\n--- BASELINE (all off) ---")
    overrides = {s: False for s in ALL_STAGES}
    pid = create_profile({"name": "retest-baseline", "target": "zai", "provider": "zai", "passthroughAuth": True, "optimizerOverrides": overrides})
    print(f"  Profile: {pid}")
    time.sleep(0.3)
    try:
        vi, vo = send(pid, VERBOSE)
        print(f"  Verbose: in={vi}, out={vo}")
        time.sleep(0.5)
        ki, ko = send(pid, K8S)
        print(f"  K8s: in={ki}, out={ko}")
        total = vi + vo + ki + ko
        print(f"  TOTAL: {total:,}")
        return ("Baseline", vi, vo, ki, ko, total)
    except Exception as e:
        print(f"  Error: {e}")
        return None
    finally:
        delete_profile(pid)

results = []
results.append(test_baseline())
results.append(test_combo("TextComp+Chunker", ["textcomp", "chunker"]))
results.append(test_combo("TextComp+ToolComp+ToolFilter", ["textcomp", "toolcomp", "toolfilter"]))
results.append(test_combo("Caveman+ToolComp+Chunker", ["caveman", "toolcomp", "chunker"]))

# Print summary
print("\n" + "=" * 100)
valid = [r for r in results if r]
valid.sort(key=lambda r: r[5])
print(f"| Combo | Verbose In | Verbose Out | K8s In | K8s Out | Total | Rank |")
print(f"|-------|------------|-------------|--------|---------|-------|------|")
for rank, r in enumerate(valid, 1):
    print(f"| {r[0]:30s} | {r[1]:>10,} | {r[2]:>11,} | {r[3]:>6,} | {r[4]:>7,} | {r[5]:>7,} | {rank} |")

if len(valid) >= 2:
    baseline_total = [r for r in valid if r[0] == "Baseline"][0][5]
    best = valid[0] if valid[0][0] != "Baseline" else valid[1]
    savings = (1 - best[5] / baseline_total) * 100
    print(f"\nBest: {best[0]} = {best[5]:,} tokens vs baseline {baseline_total:,} = {savings:.1f}% savings")
