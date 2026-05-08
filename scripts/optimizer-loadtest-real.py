#!/usr/bin/env python3
"""
Real conversation load test - per-request per-stage optimization breakdown.
Tests with glm-5 and glm-4.5v against live Z.AI upstream.
Captures DEBUG logs to show before/after per optimization stage.
"""
import json, time, urllib.request, urllib.error, subprocess, sys, os, re

GW = "http://localhost:8080"
KEY = os.environ.get("TEST_API_KEY", "")
LOG_FILE = "/tmp/gateway-debug.log"

def send(name, model, body):
    data = json.dumps(body, ensure_ascii=False).encode()
    orig_chars = len(data)
    req = urllib.request.Request(
        f"{GW}/v1/messages", data=data,
        headers={
            "Content-Type": "application/json",
            "x-api-key": KEY,
            "anthropic-version": "2023-06-01",
        },
        method="POST",
    )
    try:
        resp = urllib.request.urlopen(req, timeout=120)
        d = json.loads(resp.read())
        u = d.get("usage", {})
        inp = u.get("input_tokens", 0)
        out = u.get("output_tokens", 0)
        c = d.get("content", [])
        chars = len(c[0].get("text", "")) if c else 0
        stop = d.get("stop_reason", "?")
        return {"name": name, "model": model, "input_tokens": inp, "output_tokens": out,
                "output_chars": chars, "stop": stop, "orig_chars": orig_chars}
    except urllib.error.HTTPError as e:
        body_text = e.read().decode()[:200]
        return {"name": name, "model": model, "input_tokens": 0, "output_tokens": 0,
                "output_chars": 0, "stop": f"HTTP {e.code}", "orig_chars": orig_chars, "error": body_text}
    except Exception as e:
        return {"name": name, "model": model, "input_tokens": 0, "output_tokens": 0,
                "output_chars": 0, "stop": "error", "orig_chars": orig_chars, "error": str(e)}

def get_metrics():
    try:
        resp = urllib.request.urlopen(f"{GW}/metrics", timeout=5)
        return resp.read().decode()
    except:
        return ""

def parse_saved(metrics_text):
    input_saved = {}
    output_saved = {}
    for line in metrics_text.split("\n"):
        if "api_gateway_optimizer_chars_saved_total" in line and not line.startswith("#"):
            parts = line.strip().split()
            if len(parts) < 2:
                continue
            val = float(parts[-1])
            if 'direction="input"' in line:
                tech = re.search(r'technique="([^"]+)"', line)
                if tech:
                    input_saved[tech.group(1)] = val
            elif 'direction="output"' in line:
                tech = re.search(r'technique="([^"]+)"', line)
                if tech:
                    output_saved[tech.group(1)] = val
    return input_saved, output_saved

def tail_log(n=100):
    try:
        with open(LOG_FILE) as f:
            lines = f.readlines()
            return lines[-n:]
    except:
        return []

def extract_debug_steps(log_lines, after_marker):
    """Extract optimizer_step debug lines after a marker timestamp"""
    steps = []
    capture = False
    for line in log_lines:
        if after_marker in line:
            capture = True
            continue
        if capture and "optimizer_step" in line:
            stage = re.search(r'"stage","([^"]+)"', line)
            before = re.search(r'"before",(\d+)', line)
            after = re.search(r'"after",(\d+)', line)
            saved = re.search(r'"saved",(\d+)', line)
            if stage and before and after and saved:
                steps.append({
                    "stage": stage.group(1),
                    "before": int(before.group(1)),
                    "after": int(after.group(1)),
                    "saved": int(saved.group(1)),
                })
            if "payload optimization" in line:
                break
        if capture and "payload optimization" in line:
            # Extract final payload stats
            before_m = re.search(r'"before_chars",(\d+)', line)
            after_m = re.search(r'"after_chars",(\d+)', line)
            saved_m = re.search(r'"saved_chars",(\d+)', line)
            pct_m = re.search(r'"saved_pct","([^"]+)"', line)
            if before_m and after_m:
                steps.append({
                    "stage": "TOTAL_PAYLOAD",
                    "before": int(before_m.group(1)),
                    "after": int(after_m.group(1)),
                    "saved": int(saved_m.group(1)) if saved_m else 0,
                    "pct": pct_m.group(1) if pct_m else "",
                })
            break
    return steps

# ============================================================
print("Resetting metrics...")
subprocess.run(["redis-cli", "FLUSHDB"], capture_output=True)

# Clear log
open(LOG_FILE, "w").write("")

print()
print("=" * 80)
print("REAL CONVERSATION LOAD TEST - PER-STAGE BREAKDOWN")
print("=" * 80)

tests = []

# ---- Test 1: Go coding task (verbose system prompt) - glm-5 ----
marker1 = f"TEST1_{int(time.time())}"
slog.info(marker1) if False else None
print(f"\n--- Test 1: Go coding task (glm-5) [{marker1}] ---")
tests.append(send("Go LRU cache", "glm-5", {
    "model": "glm-5",
    "max_tokens": 1024,
    "system": "You are an expert Go developer. You write clean, idiomatic Go code. You follow best practices for error handling, concurrency, and testing. You prefer simple solutions over clever ones. You always use context.Context for cancellation. You prefer table-driven tests. You use structured logging with slog. You avoid global state. You keep interfaces small. You return errors rather than panic. You use defer for cleanup. You prefer composition over inheritance. You keep packages focused. You avoid premature optimization. You write documentation for exported functions. You use meaningful variable names. You keep functions short and focused.",
    "messages": [{"role": "user", "content": "Write a Go function that implements a generic thread-safe LRU cache with TTL expiration."}],
}))

# ---- Test 2: K8s debug with tool_result logs - glm-5 ----
marker2 = f"TEST2_{int(time.time())}"
print(f"\n--- Test 2: K8s debug with logs (glm-5) [{marker2}] ---")
log_lines = []
for i in range(25):
    log_lines.append(f"2026-05-06 04:{i:02d}:{(i*7)%60:02d} INFO Health check passed")
log_lines += ["2026-05-06 04:01:05 WARN Rate limit approaching for agent-123",
              "2026-05-06 04:02:00 ERROR Connection timeout to upstream"]
log_lines += ["2026-05-06 04:00:02 INFO Connected to Redis at localhost:6379"] * 8
log_content = "\n".join(log_lines)
tests.append(send("K8s debug logs", "glm-5", {
    "model": "glm-5",
    "max_tokens": 512,
    "system": "You are a DevOps engineer helping debug Kubernetes issues. Check for error patterns, rate limits, and connection issues.",
    "messages": [
        {"role": "user", "content": "Check the pod logs"},
        {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_1", "name": "Bash", "input": {"command": "kubectl logs api-gateway --tail=50"}}]},
        {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tu_1", "content": log_content}]},
        {"role": "user", "content": "What is the root cause and how would you fix it?"},
    ],
}))

# ---- Test 3: Tool filtering (25 tools) - glm-5 ----
marker3 = f"TEST3_{int(time.time())}"
print(f"\n--- Test 3: Tool filtering (glm-5) [{marker3}] ---")
tools = [{"name": n, "description": d} for n, d in [
    ("Read", "Read files"), ("Edit", "Edit files"), ("Write", "Write files"),
    ("Bash", "Execute shell commands"), ("Glob", "Find files"),
    ("Grep", "Search patterns"), ("WebFetch", "Fetch URLs"), ("WebSearch", "Search web"),
    ("NotebookEdit", "Edit notebooks"), ("TodoWrite", "Manage todos"),
    ("Agent", "Spawn agents"), ("Plan", "Create plans"),
    ("EnterPlanMode", "Enter plan mode"), ("ExitPlanMode", "Exit plan mode"),
    ("AskUserQuestion", "Ask questions"), ("CronCreate", "Schedule cron"),
    ("CronDelete", "Delete cron"), ("ScheduleWakeup", "Schedule wakeup"),
    ("EnterWorktree", "Enter worktree"), ("ExitWorktree", "Exit worktree"),
    ("mcp__docker_exec", "Execute in Docker"), ("mcp__docker_logs", "Docker logs"),
    ("mcp__docker_ps", "List containers"), ("mcp__docker_build", "Build image"),
    ("mcp__docker_prune", "Prune resources"),
]]
tests.append(send("Tool filter 25", "glm-5", {
    "model": "glm-5",
    "max_tokens": 256,
    "system": "You are helpful.",
    "messages": [{"role": "user", "content": "Read the config.json file and show me the database connection settings"}],
    "tools": tools,
}))

# ---- Test 4: Code review with diff - glm-4.5v ----
marker4 = f"TEST4_{int(time.time())}"
print(f"\n--- Test 4: Code review (glm-4.5v) [{marker4}] ---")
diff_content = """diff --git a/cache.go b/cache.go
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
tests.append(send("Code review diff", "glm-4.5v", {
    "model": "glm-4.5v",
    "max_tokens": 512,
    "system": "You are a senior code reviewer. Analyze diffs for bugs, security issues, and best practice violations.",
    "messages": [
        {"role": "user", "content": "Review this code change"},
        {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_2", "name": "Bash", "input": {"command": "git diff HEAD~1"}}]},
        {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tu_2", "content": diff_content}]},
        {"role": "user", "content": "Is this change safe for production? What could go wrong?"},
    ],
}))

# ---- Test 5: JSON config - glm-4.5v ----
marker5 = f"TEST5_{int(time.time())}"
print(f"\n--- Test 5: JSON config (glm-4.5v) [{marker5}] ---")
json_config = json.dumps({
    "server": {"host": "10.0.0.1", "port": 8080, "timeout": 30000},
    "redis": {"host": "localhost", "port": 6379, "db": 0, "poolSize": 100},
    "upstream": {"url": "https://api.anthropic.com", "timeout": 120000},
    "rateLimit": {"global": 1000, "perAgent": 50, "window": 60},
    "optimizer": {"enabled": True, "level": "aggressive"},
}, indent=2)
tests.append(send("JSON config", "glm-4.5v", {
    "model": "glm-4.5v",
    "max_tokens": 256,
    "system": "You are a site reliability engineer.",
    "messages": [
        {"role": "user", "content": "Check the config"},
        {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_3", "name": "Read", "input": {"file_path": "config.json"}}]},
        {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tu_3", "content": json_config}]},
        {"role": "user", "content": "Any security issues? What would you change?"}],
}))

# ---- Test 6: Dedup system prompt (cache hit from Test 1) - glm-5 ----
marker6 = f"TEST6_{int(time.time())}"
print(f"\n--- Test 6: Dedup system (glm-5) [{marker6}] ---")
tests.append(send("Dedup system", "glm-5", {
    "model": "glm-5",
    "max_tokens": 256,
    "system": "You are an expert Go developer. You write clean, idiomatic Go code. You follow best practices for error handling, concurrency, and testing. You prefer simple solutions over clever ones.",
    "messages": [{"role": "user", "content": "Write a simple HTTP middleware that logs request duration and status code."}],
}))

# ---- Test 7: Multi-turn with repeated context - glm-5 ----
marker7 = f"TEST7_{int(time.time())}"
print(f"\n--- Test 7: Multi-turn repeated context (glm-5) [{marker7}] ---")
tests.append(send("Multi-turn repeat", "glm-5", {
    "model": "glm-5",
    "max_tokens": 512,
    "system": "You are a Kubernetes troubleshooting assistant. Help diagnose cluster issues, pod failures, and networking problems. Check logs, events, and resource status. Suggest fixes with kubectl commands.",
    "messages": [
        {"role": "user", "content": "My pod is CrashLoopBackOff"},
        {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_5", "name": "Bash", "input": {"command": "kubectl describe pod api-gateway-6d8f9b7c4-x2k9m"}}]},
        {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tu_5", "content": "Name: api-gateway-6d8f9b7c4-x2k9m\nStatus: CrashLoopBackOff\nContainers:\n  api-gateway:\n    State: Waiting (CrashLoopBackOff)\n    Last State: Terminated (Error, exit code 1)\n    Ready: False\n    Restart Count: 5\nEvents:\n  Warning  BackOff  2m  kubelet  Back-off restarting failed container"}]},
        {"role": "assistant", "content": "The pod is in CrashLoopBackOff with exit code 1 and 5 restarts. Let me check the logs."},
        {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tu_6", "content": log_content}]},
        {"role": "user", "content": "What should I do?"},
    ],
}))

# ============================================================
# Read debug log for per-stage breakdown
print("\n\n" + "=" * 80)
print("RESULTS")
print("=" * 80)

time.sleep(2)

# Get metrics
metrics_text = get_metrics()
input_saved, output_saved = parse_saved(metrics_text)

# Read debug log
log_lines = tail_log(500)

# Print per-test results
print("\n### Per-Test Token Usage (upstream response)\n")
print(f"| # | Test | Model | Before (chars) | Input Tokens | Output Tokens | Stop |")
print(f"|---|------|-------|---------------|-------------|--------------|------|")
total_input = 0
total_output = 0
for i, t in enumerate(tests, 1):
    inp = t["input_tokens"]
    out = t["output_tokens"]
    total_input += inp
    total_output += out
    err = " *" if "error" in t else ""
    print(f"| {i} | {t['name']}{err} | {t['model']} | {t['orig_chars']:,} | {inp:,} | {out:,} | {t['stop']} |")
print(f"| | **TOTAL** | | | **{total_input:,}** | **{total_output:,}** | |")

# Print per-stage INPUT savings
print("\n### INPUT Optimization Per Stage (aggregate)\n")
print(f"| Stage | Chars Saved | Est. Tokens | Description |")
print(f"|-------|------------|-------------|-------------|")
total_chars = 0
for tech in sorted(input_saved.keys()):
    val = input_saved[tech]
    if val > 0:
        est_tok = int(val / 4)
        total_chars += val
        desc = {
            "semantic_dedup": "Remove exact/near-duplicate system prompt content",
            "sketch_dedup": "Detect near-duplicate prompts across requests (MinHash)",
            "toolcomp": "Format-aware compression (JSON, logs, diff, table, shell)",
            "toolfilter": "Filter tools manifest to relevant subset by intent",
            "summarizer": "Extractive summarization on red budget",
            "textcomp": "Regex filler/verbose removal from system prompt",
            "message_text": "Whitespace + sentence dedup in message strings",
            "message_textcomp": "TextComp on message string content",
            "message_block_tool_result": "Whitespace dedup in tool_result blocks",
            "message_block_text": "Whitespace dedup in text blocks",
            "chunker": "Chunk and reorder content by topic",
            "delta": "Delta encoding of repeated prompts",
            "intent_filter": "Filter content by classified intent",
            "caveman": "Token-preserving compression",
        }.get(tech, tech)
        print(f"| {tech} | {int(val):,} | {est_tok:,} | {desc} |")
print(f"| **TOTAL** | **{int(total_chars):,}** | **{int(total_chars/4):,}** | |")

# Print OUTPUT savings
print("\n### OUTPUT Optimization Per Stage (aggregate)\n")
print(f"| Stage | Chars Saved | Est. Tokens | Description |")
print(f"|-------|------------|-------------|-------------|")
total_out_chars = 0
for tech in sorted(output_saved.keys()):
    val = output_saved[tech]
    if val > 0:
        total_out_chars += val
        print(f"| {tech} | {int(val):,} | {int(val/4):,} | Post-proxy response trimming |")
if total_out_chars == 0:
    print("| (none) | 0 | 0 | Needs sustained traffic for bandit/waste/caching |")
print(f"| **TOTAL** | **{int(total_out_chars):,}** | **{int(total_out_chars/4):,}** | |")

# Summary
est_tokens_saved = int(total_chars / 4)
est_out_saved = int(total_out_chars / 4)
print(f"\n### Summary\n")
print(f"| Metric | INPUT | OUTPUT | Total |")
print(f"|--------|-------|--------|-------|")
print(f"| Original (chars) | {total_input*4 + est_tokens_saved:,} | {total_output*4:,} | |")
print(f"| Optimized (chars) | {total_chars:,} saved | {total_out_chars:,} saved | {total_chars+total_out_chars:,} saved |")
print(f"| Tokens consumed | {total_input:,} | {total_output:,} | {total_input+total_output:,} |")
print(f"| Tokens saved | ~{est_tokens_saved:,} | ~{est_out_saved:,} | ~{est_tokens_saved+est_out_saved:,} |")
pct = est_tokens_saved / (total_input + est_tokens_saved) * 100 if (total_input + est_tokens_saved) > 0 else 0
print(f"| Savings % | ~{pct:.1f}% | - | - |")
print(f"| Cost (at $3/$15 per M) | ${total_input*3/1e6:.4f} | ${total_output*15/1e6:.4f} | ${total_input*3/1e6+total_output*15/1e6:.4f} |")
print(f"| Cost without opt | ${(total_input+est_tokens_saved)*3/1e6:.4f} | - | |")

# Print debug log steps
print(f"\n### Debug Log - Per-Stage Breakdown\n")
print("```")
# Find optimizer_step lines
for line in log_lines:
    if "optimizer_step" in line or "payload optimization" in line or "debug tokens" in line:
        # Clean up for readability
        clean = line.strip()
        # Extract key fields
        if "optimizer_step" in clean:
            stage = re.search(r'"stage","([^"]+)"', clean)
            before = re.search(r'"before",(\d+)', clean)
            after = re.search(r'"after",(\d+)', clean)
            saved = re.search(r'"saved",(\d+)', clean)
            if all([stage, before, after, saved]):
                print(f"  {stage.group(1):30s} before={int(before.group(1)):>6,}  after={int(after.group(1)):>6,}  saved={int(saved.group(1)):>6,}")
        elif "payload optimization" in clean:
            before = re.search(r'"before_chars",(\d+)', clean)
            after = re.search(r'"after_chars",(\d+)', clean)
            saved = re.search(r'"saved_chars",(\d+)', clean)
            pct = re.search(r'"saved_pct","([^"]+)"', clean)
            if all([before, after, saved]):
                print(f"  {'TOTAL PAYLOAD':30s} before={int(before.group(1)):>6,}  after={int(after.group(1)):>6,}  saved={int(saved.group(1)):>6,}  ({pct.group(1) if pct else ''})")
print("```")
