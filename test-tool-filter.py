#!/usr/bin/env python3
"""Tool Filter optimizer POC test."""

import json
import urllib.request
import urllib.error
import sys

BASE = "http://localhost:18080"
API_KEY = "1d63b5db6d984db1913ca9596125f06b.RHYoZWfoRZteSclW"
MODEL = "glm-5.1"
MAX_TOKENS = 256
USER_MSG = "Read config.json and show DB settings"

TOOLS = [
    {"name": "Read", "description": "Read a file from the filesystem and return its contents."},
    {"name": "Edit", "description": "Perform exact string replacements in files."},
    {"name": "Write", "description": "Write a file to the local filesystem, overwriting if exists."},
    {"name": "Bash", "description": "Execute a bash command and return its output."},
    {"name": "Glob", "description": "Search for files matching a glob pattern."},
    {"name": "Grep", "description": "Search file contents for patterns using regex."},
    {"name": "WebFetch", "description": "Fetch content from a URL."},
    {"name": "WebSearch", "description": "Search the web and return results."},
    {"name": "NotebookEdit", "description": "Edit a Jupyter notebook cell."},
    {"name": "TodoWrite", "description": "Create and manage a structured task list."},
    {"name": "Agent", "description": "Execute a skill or sub-agent task."},
    {"name": "Plan", "description": "Create an execution plan for a task."},
    {"name": "EnterPlanMode", "description": "Switch to planning mode for complex tasks."},
    {"name": "ExitPlanMode", "description": "Exit planning mode and resume execution."},
    {"name": "AskUserQuestion", "description": "Ask the user a clarifying question."},
    {"name": "CronCreate", "description": "Create a scheduled cron job."},
    {"name": "CronDelete", "description": "Delete an existing cron job."},
    {"name": "ScheduleWakeup", "description": "Schedule a wakeup call for later."},
    {"name": "EnterWorktree", "description": "Create or enter a git worktree."},
    {"name": "ExitWorktree", "description": "Exit a git worktree session."},
    {"name": "mcp__docker_exec", "description": "Execute a command inside a Docker container."},
    {"name": "mcp__docker_logs", "description": "Fetch logs from a Docker container."},
    {"name": "mcp__docker_ps", "description": "List running Docker containers."},
    {"name": "mcp__docker_build", "description": "Build a Docker image from a Dockerfile."},
    {"name": "mcp__docker_prune", "description": "Remove unused Docker resources."},
]

TOOL_COUNTS = [5, 10, 15, 20, 25]

PROFILES = [
    {
        "name": "opt-tf-base",
        "payload": {
            "name": "opt-tf-base",
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
    {
        "name": "opt-tf-only",
        "payload": {
            "name": "opt-tf-only",
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
            },
        },
    },
]


def api(method, path, body=None, headers=None):
    url = f"{BASE}{path}"
    data = json.dumps(body).encode() if body else None
    hdrs = {"Content-Type": "application/json"}
    if headers:
        hdrs.update(headers)
    req = urllib.request.Request(url, data=data, headers=hdrs, method=method)
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            raw = resp.read().decode()
            return json.loads(raw) if raw else {}
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        print(f"HTTP {e.code} on {method} {path}: {raw[:500]}", file=sys.stderr)
        try:
            return json.loads(raw)
        except Exception:
            return {"error": raw[:500], "status": e.code}
    except Exception as e:
        print(f"Error on {method} {path}: {e}", file=sys.stderr)
        return {"error": str(e)}


def create_profiles():
    for p in PROFILES:
        r = api("POST", "/v1/profiles", p["payload"])
        print(f"Profile '{p['name']}': {r.get('name', r.get('error', 'ok'))}")


def send_request(profile_name, tool_count):
    tools = TOOLS[:tool_count]
    body = {
        "model": MODEL,
        "max_tokens": MAX_TOKENS,
        "messages": [{"role": "user", "content": USER_MSG}],
        "tools": tools,
    }
    headers = {
        "x-api-key": API_KEY,
        "X-Profile": profile_name,
    }
    return api("POST", "/v1/messages", body, headers)


def extract_token_counts(resp):
    usage = resp.get("usage", {})
    inp = usage.get("input_tokens", 0)
    out = usage.get("output_tokens", 0)
    return inp, out


def run_test(tool_count):
    # Baseline
    base_resp = send_request("opt-tf-base", tool_count)
    base_in, base_out = extract_token_counts(base_resp)

    # Filtered
    filt_resp = send_request("opt-tf-only", tool_count)
    filt_in, filt_out = extract_token_counts(filt_resp)

    # Check for errors
    if "error" in base_resp:
        print(f"  Baseline error: {base_resp['error'][:200]}")
    if "error" in filt_resp:
        print(f"  Filtered error: {filt_resp['error'][:200]}")

    in_saved = base_in - filt_in
    out_saved = base_out - filt_out
    verdict = "PASS" if in_saved > 0 else "NOOP"

    return {
        "tool_count": tool_count,
        "base_in": base_in,
        "filt_in": filt_in,
        "in_saved": in_saved,
        "base_out": base_out,
        "filt_out": filt_out,
        "out_saved": out_saved,
        "verdict": verdict,
    }


def main():
    print("=== Tool Filter Optimizer POC ===\n")

    # Step 1: Create profiles
    print("--- Creating profiles ---")
    create_profiles()
    print()

    # Step 2: Run tests
    print("--- Running tests ---")
    results = []
    for tc in TOOL_COUNTS:
        print(f"  Testing {tc} tools...", end=" ", flush=True)
        r = run_test(tc)
        results.append(r)
        print(f"verdict={r['verdict']} in_saved={r['in_saved']}")

    # Step 3: Print table
    print()
    print(f"| # | Tool Count | Baseline In | Filtered In | In Saved | Baseline Out | Filtered Out | Out Saved | Verdict |")
    print(f"|---|-----------|-------------|-------------|----------|-------------|-------------|----------|---------|")
    for i, r in enumerate(results, 1):
        print(
            f"| {i} | {r['tool_count']:>9} | {r['base_in']:>11} | {r['filt_in']:>11} | {r['in_saved']:>8} | {r['base_out']:>11} | {r['filt_out']:>11} | {r['out_saved']:>8} | {r['verdict']:>7} |"
        )

    # Summary
    total_in_saved = sum(r["in_saved"] for r in results)
    total_tools = sum(r["tool_count"] for r in results)
    print(f"\nTotal input tokens saved across all tests: {total_in_saved}")
    if total_in_saved > 0:
        print("Tool Filter is removing irrelevant tools and saving input tokens.")
    else:
        print("Tool Filter did not reduce input tokens. Check stage logic.")


if __name__ == "__main__":
    main()
