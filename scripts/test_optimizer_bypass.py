#!/usr/bin/env python3
"""Test optimizer bypass: Z.AI skips optimizer, Claude OAuth uses toolcomp+toolfilter only."""
import json, urllib.request, urllib.error, sys

GW = "http://localhost:18080"
ZAI_KEY = "1d63b5db6d984db1913ca9596125f06b.RHYoZWfoRZteSclW"

def send_zai(body_dict):
    data = json.dumps(body_dict, ensure_ascii=False).encode()
    req = urllib.request.Request(f"{GW}/v1/messages", data=data, headers={
        "Content-Type": "application/json",
        "x-api-key": ZAI_KEY,
        "anthropic-version": "2023-06-01",
    }, method="POST")
    try:
        resp = urllib.request.urlopen(req, timeout=120)
        return json.loads(resp.read())
    except urllib.error.HTTPError as e:
        return {"error": f"HTTP {e.code}: {e.read().decode()[:300]}"}
    except Exception as e:
        return {"error": str(e)}

# Test 1: Z.AI - should skip optimizer entirely
print("=" * 70)
print("TEST 1: Z.AI glm-5.1 - optimizer should be SKIPPED")
print("=" * 70)

r = send_zai({
    "model": "glm-5.1",
    "max_tokens": 128,
    "system": "You are an expert Go developer. You follow best practices. " * 5,
    "messages": [{"role": "user", "content": "Say hello in exactly 5 words"}],
})

if "error" in r:
    print(f"  ERROR: {r['error']}")
else:
    u = r.get("usage", {})
    print(f"  Input: {u.get('input_tokens', 0):,}")
    print(f"  Output: {u.get('output_tokens', 0):,}")
    content = ""
    for block in r.get("content", []):
        if block.get("type") == "text":
            content += block.get("text", "")
    print(f"  Response: {content[:120]}")
    print(f"  [Z.AI bypass works if no optimizer_step logs in gateway]")

# Test 2: Z.AI with verbose prompt (dedup target)
print("\n" + "=" * 70)
print("TEST 2: Z.AI verbose prompt - no semantic_dedup should run")
print("=" * 70)

verbose_sys = (
    "You are an expert Go developer. You write clean, idiomatic Go code. "
    "You follow best practices for error handling, concurrency, and testing. "
    "You prefer simple solutions over clever ones. You always use context.Context. "
    "You prefer table-driven tests. You use structured logging with slog. "
    "You avoid global state. You keep interfaces small. "
    "You return errors rather than panic. You use defer for cleanup. "
    "You follow best practices for error handling, concurrency, and testing. "
    "You prefer simple solutions over clever ones."
)

r2 = send_zai({
    "model": "glm-5.1",
    "max_tokens": 128,
    "system": verbose_sys,
    "messages": [{"role": "user", "content": "Write a simple hello world"}],
})

if "error" in r2:
    print(f"  ERROR: {r2['error']}")
else:
    u = r2.get("usage", {})
    print(f"  Input: {u.get('input_tokens', 0):,}")
    print(f"  Output: {u.get('output_tokens', 0):,}")
    content = ""
    for block in r2.get("content", []):
        if block.get("type") == "text":
            content += block.get("text", "")
    print(f"  Response: {content[:120]}")

# Test 3: Z.AI with 25 tools - no toolfilter should run
print("\n" + "=" * 70)
print("TEST 3: Z.AI with 25 tools - toolfilter should NOT run")
print("=" * 70)

TOOLS_25 = [
    {"name": "Read", "description": "Read files from disk"},
    {"name": "Edit", "description": "Edit files in place"},
    {"name": "Write", "description": "Write new files"},
    {"name": "Bash", "description": "Execute shell commands"},
    {"name": "Glob", "description": "Find files by pattern"},
    {"name": "Grep", "description": "Search for patterns in files"},
    {"name": "WebFetch", "description": "Fetch content from URLs"},
    {"name": "WebSearch", "description": "Search the web"},
    {"name": "NotebookEdit", "description": "Edit Jupyter notebooks"},
    {"name": "TodoWrite", "description": "Manage todo lists"},
    {"name": "Agent", "description": "Spawn sub-agents"},
    {"name": "Plan", "description": "Create implementation plans"},
    {"name": "EnterPlanMode", "description": "Enter plan mode"},
    {"name": "ExitPlanMode", "description": "Exit plan mode"},
    {"name": "AskUserQuestion", "description": "Ask user questions"},
    {"name": "CronCreate", "description": "Schedule cron jobs"},
    {"name": "CronDelete", "description": "Delete cron jobs"},
    {"name": "ScheduleWakeup", "description": "Schedule wakeups"},
    {"name": "EnterWorktree", "description": "Enter git worktree"},
    {"name": "ExitWorktree", "description": "Exit git worktree"},
    {"name": "mcp__docker_exec", "description": "Execute in Docker containers"},
    {"name": "mcp__docker_logs", "description": "View Docker container logs"},
    {"name": "mcp__docker_ps", "description": "List Docker containers"},
    {"name": "mcp__docker_build", "description": "Build Docker images"},
    {"name": "mcp__docker_prune", "description": "Prune unused Docker resources"},
]

r3 = send_zai({
    "model": "glm-5.1",
    "max_tokens": 128,
    "system": "You are helpful.",
    "messages": [{"role": "user", "content": "Read config.json"}],
    "tools": TOOLS_25,
})

if "error" in r3:
    print(f"  ERROR: {r3['error']}")
else:
    u = r3.get("usage", {})
    print(f"  Input: {u.get('input_tokens', 0):,}")
    print(f"  Output: {u.get('output_tokens', 0):,}")
    # If toolfilter ran, input would be ~40% less. If bypass works, all 25 tools sent.
    print(f"  [If bypass works, all 25 tools sent upstream - higher input tokens]")

# Test 4: Concurrent SSE test - 3 parallel Z.AI requests
print("\n" + "=" * 70)
print("TEST 4: Concurrent SSE - 3 parallel Z.AI requests")
print("=" * 70)

import concurrent.futures
import time

def timed_request(label):
    start = time.time()
    r = send_zai({
        "model": "glm-5.1",
        "max_tokens": 64,
        "messages": [{"role": "user", "content": f"Say '{label}' in 3 words"}],
    })
    elapsed = time.time() - start
    if "error" in r:
        return f"  {label}: ERROR {r['error'][:60]} ({elapsed:.1f}s)"
    u = r.get("usage", {})
    return f"  {label}: in={u.get('input_tokens', 0):,} out={u.get('output_tokens', 0):,} ({elapsed:.1f}s)"

start_all = time.time()
with concurrent.futures.ThreadPoolExecutor(max_workers=3) as ex:
    futs = [ex.submit(timed_request, f"parallel-{i}") for i in range(3)]
    for f in concurrent.futures.as_completed(futs):
        print(f.result())
total = time.time() - start_all
print(f"  Total wall time: {total:.1f}s (should be ~max of 3, not sum)")

print("\n" + "=" * 70)
print("SUMMARY")
print("=" * 70)
print("Check gateway logs for:")
print("  - NO 'optimizer_step' logs for glm-5.1 requests")
print("  - NO 'optimize_system_prompt_entry' logs for glm-5.1")
print("  - 'isZAIProvider=true' should skip entire optimizer block")
print("\nVerify with: docker logs arl-gateway 2>&1 | grep -i 'optim' | tail -20")
