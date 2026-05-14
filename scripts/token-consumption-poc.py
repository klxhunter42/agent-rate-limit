#!/usr/bin/env python3
"""
Token consumption PoC: Before (raw upstream) vs After (gateway optimizer).
Tests with ZAI_API_KEYS directly against upstream and through gateway.
Compares input_tokens and output_tokens to quantify real savings.

Usage:
  python3 scripts/token-consumption-poc.py raw   # baseline only
  python3 scripts/token-consumption-poc.py gw    # gateway only
  python3 scripts/token-consumption-poc.py both   # compare both
"""
import json, time, urllib.request, urllib.error, sys, os

# --- Config ---
ZAI_KEY = os.environ.get("ZAI_API_KEY", "1d63b5db6d984db1913ca9596125f06b.RHYoZWfoRZteSclW")
ZAI_UPSTREAM = os.environ.get("ZAI_UPSTREAM", "https://api.z.ai/api/anthropic")
GW_URL = os.environ.get("GW_URL", "http://localhost:18080")
GW_KEY = os.environ.get("GW_KEY", "")


# --- Static test data (must be defined before TESTS) ---
def generate_k8s_logs():
    lines = []
    for i in range(25):
        lines.append(f"2026-05-06 04:{i:02d}:{(i*7)%60:02d} INFO Health check passed")
    lines += [
        "2026-05-06 04:01:05 WARN Rate limit approaching for agent-123",
        "2026-05-06 04:02:00 ERROR Connection timeout to upstream",
    ]
    lines += ["2026-05-06 04:00:02 INFO Connected to Redis at localhost:6379"] * 8
    return "\n".join(lines)


CODE_REVIEW_DIFF = """diff --git a/cache.go b/cache.go
index abc1234..def5678 100644
--- a/cache.go
+++ b/cache.go
@@ -10,8 +10,6 @@ import (
- "old/import1"
- "old/import2"
 )
@@ -25,7 +23,8 @@ func NewCache() {
- maxSize := 1000
- log.Printf("Max size: %d", maxSize)
+ maxSize := config.GetInt("CACHE_MAX_SIZE")
+ if maxSize == 0 {
+ maxSize = 1000
+ }
+ log.Printf("Max size: %d", maxSize)
 }"""

JSON_CONFIG = json.dumps({
    "server": {"host": "10.0.1.50", "port": 8080, "timeout": 30000},
    "redis": {"host": "localhost", "port": 6379, "db": 0, "poolSize": 100},
    "upstream": {"url": "https://api.anthropic.com", "timeout": 120000},
    "rateLimit": {"global": 1000, "perAgent": 50, "window": 60},
    "optimizer": {"enabled": True, "level": "aggressive"},
}, indent=2)

K8S_DESCRIBE = """Name: api-gateway-6d8f9b7c4-x2k9m
Status: CrashLoopBackOff
Containers:
  api-gateway:
    State: Waiting (CrashLoopBackOff)
    Last State: Terminated (Error, exit code 1)
    Ready: False
    Restart Count: 5
Events:
  Warning  BackOff  2m  kubelet  Back-off restarting failed container"""

TOOLS_LIST = [
    ("Read", "Read files from disk"), ("Edit", "Edit files in place"),
    ("Write", "Write new files"), ("Bash", "Execute shell commands"),
    ("Glob", "Find files by pattern"), ("Grep", "Search for patterns in files"),
    ("WebFetch", "Fetch content from URLs"), ("WebSearch", "Search the web"),
    ("NotebookEdit", "Edit Jupyter notebooks"), ("TodoWrite", "Manage todo lists"),
    ("Agent", "Spawn sub-agents"), ("Plan", "Create implementation plans"),
    ("EnterPlanMode", "Enter plan mode"), ("ExitPlanMode", "Exit plan mode"),
    ("AskUserQuestion", "Ask user questions"), ("CronCreate", "Schedule cron jobs"),
    ("CronDelete", "Delete cron jobs"), ("ScheduleWakeup", "Schedule wakeups"),
    ("EnterWorktree", "Enter git worktree"), ("ExitWorktree", "Exit git worktree"),
    ("mcp__docker_exec", "Execute commands in Docker containers"),
    ("mcp__docker_logs", "View Docker container logs"),
    ("mcp__docker_ps", "List Docker containers"),
    ("mcp__docker_build", "Build Docker images"),
    ("mcp__docker_prune", "Prune unused Docker resources"),
]
TOOLS_25 = [{"name": n, "description": d} for n, d in TOOLS_LIST]


# --- Test cases ---
TESTS = [
    {
        "name": "Verbose system prompt (Go expert)",
        "model": "glm-5.1",
        "body": {
            "model": "glm-5.1",
            "max_tokens": 512,
            "system": (
                "You are an expert Go developer. You write clean, idiomatic Go code. "
                "You follow best practices for error handling, concurrency, and testing. "
                "You prefer simple solutions over clever ones. You always use context.Context for cancellation. "
                "You prefer table-driven tests. You use structured logging with slog. You avoid global state. "
                "You keep interfaces small. You return errors rather than panic. You use defer for cleanup. "
                "You prefer composition over inheritance. You keep packages focused. "
                "You avoid premature optimization. You write documentation for exported functions. "
                "You use meaningful variable names. You keep functions short and focused."
            ),
            "messages": [
                {"role": "user", "content": "Write a Go function that implements a generic thread-safe LRU cache with TTL expiration."}
            ],
        },
    },
    {
        "name": "K8s debug with repeated logs",
        "model": "glm-5.1",
        "body_fn": lambda: {
            "model": "glm-5.1",
            "max_tokens": 512,
            "system": "You are a DevOps engineer helping debug Kubernetes issues. Check for error patterns, rate limits, and connection issues.",
            "messages": [
                {"role": "user", "content": "Check the pod logs"},
                {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_1", "name": "Bash", "input": {"command": "kubectl logs api-gateway --tail=50"}}]},
                {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tu_1", "content": generate_k8s_logs()}]},
                {"role": "user", "content": "What is the root cause and how would you fix it?"},
            ],
        },
    },
    {
        "name": "25-tool manifest (toolfilter test)",
        "model": "glm-5.1",
        "body": {
            "model": "glm-5.1",
            "max_tokens": 256,
            "system": "You are helpful.",
            "messages": [
                {"role": "user", "content": "Read the config.json file and show me the database connection settings"}
            ],
            "tools": TOOLS_25,
        },
    },
    {
        "name": "Code review with diff (toolcomp)",
        "model": "glm-5.1",
        "body": {
            "model": "glm-5.1",
            "max_tokens": 512,
            "system": "You are a senior code reviewer. Analyze diffs for bugs, security issues, and best practice violations.",
            "messages": [
                {"role": "user", "content": "Review this code change"},
                {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_2", "name": "Bash", "input": {"command": "git diff HEAD~1"}}]},
                {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tu_2", "content": CODE_REVIEW_DIFF}]},
                {"role": "user", "content": "Is this change safe for production? What could go wrong?"},
            ],
        },
    },
    {
        "name": "JSON config with redundancy",
        "model": "glm-5.1",
        "body": {
            "model": "glm-5.1",
            "max_tokens": 256,
            "system": "You are a site reliability engineer. Analyze configurations for security and reliability issues.",
            "messages": [
                {"role": "user", "content": "Check the config"},
                {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_3", "name": "Read", "input": {"file_path": "config.json"}}]},
                {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tu_3", "content": JSON_CONFIG}]},
                {"role": "user", "content": "Any security issues? What would you change?"},
            ],
        },
    },
    {
        "name": "Dedup system prompt (semantic dedup)",
        "model": "glm-5.1",
        "body": {
            "model": "glm-5.1",
            "max_tokens": 256,
            "system": (
                "You are an expert Go developer. You follow best practices for error handling, concurrency, and testing. "
                "You prefer simple solutions over clever ones. You follow best practices for error handling, concurrency, and testing. "
                "You prefer simple solutions over clever ones."
            ),
            "messages": [
                {"role": "user", "content": "Write a simple HTTP middleware that logs request duration and status code."}
            ],
        },
    },
    {
        "name": "Multi-turn with repeated context",
        "model": "glm-5.1",
        "body_fn": lambda: {
            "model": "glm-5.1",
            "max_tokens": 512,
            "system": (
                "You are a Kubernetes troubleshooting assistant. Help diagnose cluster issues, pod failures, and networking problems. "
                "Check logs, events, and resource status. Suggest fixes with kubectl commands."
            ),
            "messages": [
                {"role": "user", "content": "My pod is CrashLoopBackOff"},
                {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_5", "name": "Bash", "input": {"command": "kubectl describe pod api-gateway-6d8f9b7c4-x2k9m"}}]},
                {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tu_5", "content": K8S_DESCRIBE}]},
                {"role": "assistant", "content": "The pod is in CrashLoopBackOff with exit code 1 and 5 restarts. Let me check the logs."},
                {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tu_6", "content": generate_k8s_logs()}]},
                {"role": "user", "content": "What should I do?"},
            ],
        },
    },
]


# --- HTTP helpers ---
def send_raw(body):
    """Send directly to Z.AI upstream (no optimizer)."""
    data = json.dumps(body, ensure_ascii=False).encode()
    req = urllib.request.Request(
        f"{ZAI_UPSTREAM}/v1/messages", data=data,
        headers={
            "Content-Type": "application/json",
            "x-api-key": ZAI_KEY,
            "anthropic-version": "2023-06-01",
        },
        method="POST",
    )
    try:
        resp = urllib.request.urlopen(req, timeout=180)
        d = json.loads(resp.read())
        u = d.get("usage", {})
        return {
            "input_tokens": u.get("input_tokens", 0),
            "output_tokens": u.get("output_tokens", 0),
            "cache_creation": u.get("cache_creation_input_tokens", 0),
            "cache_read": u.get("cache_read_input_tokens", 0),
        }
    except urllib.error.HTTPError as e:
        return {"error": f"HTTP {e.code}: {e.read().decode()[:300]}"}
    except Exception as e:
        return {"error": str(e)}


def send_gw(body):
    """Send through gateway (with optimizer pipeline)."""
    data = json.dumps(body, ensure_ascii=False).encode()
    headers = {
        "Content-Type": "application/json",
        "anthropic-version": "2023-06-01",
        "x-api-key": GW_KEY or ZAI_KEY,
    }
    req = urllib.request.Request(
        f"{GW_URL}/v1/messages", data=data,
        headers=headers, method="POST",
    )
    try:
        resp = urllib.request.urlopen(req, timeout=180)
        d = json.loads(resp.read())
        u = d.get("usage", {})
        return {
            "input_tokens": u.get("input_tokens", 0),
            "output_tokens": u.get("output_tokens", 0),
            "cache_creation": u.get("cache_creation_input_tokens", 0),
            "cache_read": u.get("cache_read_input_tokens", 0),
        }
    except urllib.error.HTTPError as e:
        return {"error": f"HTTP {e.code}: {e.read().decode()[:300]}"}
    except Exception as e:
        return {"error": str(e)}


def resolve_body(test):
    b = test.get("body")
    if b is None:
        b = test["body_fn"]()
    return b


# --- Main ---
def main():
    mode = sys.argv[1] if len(sys.argv) > 1 else "both"
    if mode not in ("raw", "gw", "both"):
        print(f"Usage: {sys.argv[0]} [raw|gw|both]")
        sys.exit(1)

    print("=" * 90)
    print("TOKEN CONSUMPTION POC: Before (raw) vs After (gateway optimizer)")
    print(f"Mode: {mode} | Upstream: {ZAI_UPSTREAM} | GW: {GW_URL}")
    print("=" * 90)

    results = []

    for i, test in enumerate(TESTS, 1):
        body = resolve_body(test)
        model = test["model"]
        chars = len(json.dumps(body, ensure_ascii=False))

        print(f"\n--- Test {i}/{len(TESTS)}: {test['name']} (model={model}, payload={chars:,} chars) ---")

        raw_result = {"input_tokens": 0, "output_tokens": 0}
        gw_result = {"input_tokens": 0, "output_tokens": 0}

        if mode in ("raw", "both"):
            print(f"  [RAW] Sending to upstream...", end=" ", flush=True)
            raw_result = send_raw(body)
            if "error" in raw_result:
                print(f"ERROR: {raw_result['error'][:100]}")
            else:
                print(f"input={raw_result['input_tokens']:,} output={raw_result['output_tokens']:,}")

        if mode in ("gw", "both"):
            print(f"  [GW]  Sending to gateway...", end=" ", flush=True)
            gw_result = send_gw(body)
            if "error" in gw_result:
                print(f"ERROR: {gw_result['error'][:100]}")
            else:
                print(f"input={gw_result['input_tokens']:,} output={gw_result['output_tokens']:,}")

        results.append({
            "name": test["name"],
            "model": model,
            "payload_chars": chars,
            "raw": raw_result,
            "gw": gw_result,
        })

        time.sleep(1)

    # --- Summary ---
    print("\n\n" + "=" * 90)
    print("RESULTS SUMMARY")
    print("=" * 90)

    # Per-test table
    print("\n### Per-Test Token Comparison\n")
    hdr = f"| # | Test | Payload | Raw In | GW In | In Saved | Raw Out | GW Out | Out Saved |"
    sep = f"|---|------|---------|--------|-------|----------|---------|--------|-----------|"
    print(hdr)
    print(sep)

    total_raw_in = total_gw_in = total_raw_out = total_gw_out = 0

    for i, r in enumerate(results, 1):
        ri = r["raw"].get("input_tokens", 0)
        gi = r["gw"].get("input_tokens", 0)
        ro = r["raw"].get("output_tokens", 0)
        go = r["gw"].get("output_tokens", 0)
        is_ = ri - gi
        os_ = ro - go
        e_r = " *" if "error" in r["raw"] else ""
        e_g = " *" if "error" in r["gw"] else ""
        ip = f"{is_/ri*100:.1f}%" if ri > 0 else "-"
        op = f"{os_/ro*100:.1f}%" if ro > 0 else "-"
        total_raw_in += ri
        total_gw_in += gi
        total_raw_out += ro
        total_gw_out += go
        print(f"| {i} | {r['name'][:28]} | {r['payload_chars']:>6,} | "
              f"{ri:>6,}{e_r} | {gi:>5,}{e_g} | {is_:>+5} ({ip}) | "
              f"{ro:>6,} | {go:>5,} | {os_:>+5} ({op}) |")

    ti = total_raw_in - total_gw_in
    to = total_raw_out - total_gw_out
    tip = f"{ti/total_raw_in*100:.1f}%" if total_raw_in > 0 else "-"
    top = f"{to/total_raw_out*100:.1f}%" if total_raw_out > 0 else "-"
    print(f"| | **TOTAL** | | **{total_raw_in:,}** | **{total_gw_in:,}** | **{ti:+,} ({tip})** | "
          f"**{total_raw_out:,}** | **{total_gw_out:,}** | **{to:+,} ({top})** |")

    # Cost table
    print("\n### Cost Comparison (glm-5.1 pricing: $1.4/M in, $4.4/M out)\n")
    ip = 1.4 / 1_000_000
    op = 4.4 / 1_000_000
    rc_i = total_raw_in * ip
    rc_o = total_raw_out * op
    gc_i = total_gw_in * ip
    gc_o = total_gw_out * op
    rt = rc_i + rc_o
    gt = gc_i + gc_o
    st = rt - gt

    print(f"| Metric | Before (Raw) | After (GW) | Saved |")
    print(f"|--------|-------------|------------|-------|")
    print(f"| Input tokens | {total_raw_in:,} | {total_gw_in:,} | {ti:,} |")
    print(f"| Output tokens | {total_raw_out:,} | {total_gw_out:,} | {to:,} |")
    print(f"| Input cost | ${rc_i:.4f} | ${gc_i:.4f} | ${rc_i-gc_i:.4f} |")
    print(f"| Output cost | ${rc_o:.4f} | ${gc_o:.4f} | ${rc_o-gc_o:.4f} |")
    print(f"| **Total cost** | **${rt:.4f}** | **${gt:.4f}** | **${st:.4f}** |")
    if rt > 0:
        print(f"| Savings % | | | **{st/rt*100:.1f}%** |")

    # Extrapolation
    print("\n### Monthly Extrapolation\n")
    n = len(results)
    for daily in [10_000, 50_000]:
        m = daily * 30
        mr = rt / n * m
        mg = gt / n * m
        ms = mr - mg
        print(f"| {daily:,} req/day | Before: ${mr:.2f}/mo | After: ${mg:.2f}/mo | Saved: ${ms:.2f}/mo |")

    # Findings
    print("\n### Key Findings\n")
    if ti > 0:
        print(f"- Input tokens saved: **{ti:,}** ({tip}) across {n} test requests")
    if to > 0:
        print(f"- Output tokens saved: **{to:,}** ({top}) via caveman/pordee output injection")
    if ti == 0 and to == 0:
        print("- No savings detected - ensure gateway is running with optimizers enabled")
    if rt > 0:
        print(f"- Cost: ${rt:.4f} -> ${gt:.4f} = **${st:.4f} saved ({st/rt*100:.1f}%)**")


if __name__ == "__main__":
    main()
