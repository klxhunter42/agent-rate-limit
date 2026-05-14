#!/usr/bin/env python3
"""
Per-stage optimizer POC: isolate each optimizer stage and measure real token impact.
Uses profile optimizer overrides to enable/disable individual stages.

Usage:
  python3 scripts/optimizer-per-stage-poc.py
"""
import json, time, urllib.request, urllib.error, sys, os

GW = os.environ.get("GW_URL", "http://localhost:18080")
ZAI_KEY = os.environ.get("ZAI_API_KEY", "1d63b5db6d984db1913ca9596125f06b.RHYoZWfoRZteSclW")

ALL_STAGES = [
    "semantic_dedup", "chunker", "sketch", "textcomp",
    "caveman", "pordee", "toolcomp", "toolfilter",
]
# summarizer/delta excluded: summarizer needs red budget, delta is metrics-only


def api(method, path, body=None):
    data = json.dumps(body).encode() if body else None
    req = urllib.request.Request(
        f"{GW}{path}", data=data,
        headers={"Content-Type": "application/json"},
        method=method,
    )
    try:
        resp = urllib.request.urlopen(req, timeout=10)
        return json.loads(resp.read())
    except urllib.error.HTTPError as e:
        return {"error": f"HTTP {e.code}: {e.read().decode()[:200]}"}
    except Exception as e:
        return {"error": str(e)}


def create_profile(name, overrides):
    """Create profile with specific optimizer overrides."""
    api("DELETE", f"/v1/profiles", {"name": name})  # cleanup
    return api("POST", "/v1/profiles", {
        "name": name,
        "target": "zai",
        "provider": "zai",
        "passthroughAuth": True,
        "optimizerOverrides": overrides,
    })


def send(profile_name, body):
    """Send request through specific profile."""
    data = json.dumps(body, ensure_ascii=False).encode()
    req = urllib.request.Request(
        f"{GW}/v1/messages", data=data,
        headers={
            "Content-Type": "application/json",
            "x-api-key": ZAI_KEY,
            "anthropic-version": "2023-06-01",
            "X-Profile": profile_name,
        },
        method="POST",
    )
    try:
        resp = urllib.request.urlopen(req, timeout=180)
        d = json.loads(resp.read())
        u = d.get("usage", {})
        return {
            "input": u.get("input_tokens", 0),
            "output": u.get("output_tokens", 0),
        }
    except urllib.error.HTTPError as e:
        return {"error": f"HTTP {e.code}: {e.read().decode()[:200]}"}
    except Exception as e:
        return {"error": str(e)}


def overrides_only(enabled_stage):
    """All stages OFF except one."""
    return {s: False for s in ALL_STAGES if s != enabled_stage}


def overrides_all_off():
    """All stages OFF."""
    return {s: False for s in ALL_STAGES}


def overrides_all_on():
    """All stages ON (empty = use global defaults = all enabled)."""
    return {}


def overrides_pair(a, b):
    """Only two stages ON."""
    return {s: False for s in ALL_STAGES if s not in (a, b)}


# --- Test payloads ---
PAYLOAD_VERBOSE = {
    "model": "glm-5.1",
    "max_tokens": 512,
    "system": (
        "You are an expert Go developer. You write clean, idiomatic Go code. "
        "You follow best practices for error handling, concurrency, and testing. "
        "You prefer simple solutions over clever ones. You always use context.Context. "
        "You prefer table-driven tests. You use structured logging with slog. "
        "You avoid global state. You keep interfaces small. "
        "You return errors rather than panic. You use defer for cleanup. "
        "You follow best practices for error handling, concurrency, and testing. "
        "You prefer simple solutions over clever ones."
    ),
    "messages": [{"role": "user", "content": "Write a Go LRU cache with TTL."}],
}

def make_k8s_payload():
    logs = "\n".join(
        [f"2026-05-06 04:{i:02d}:{(i*7)%60:02d} INFO Health check passed" for i in range(25)]
        + ["2026-05-06 04:01:05 WARN Rate limit approaching for agent-123"]
        + ["2026-05-06 04:00:02 INFO Connected to Redis at localhost:6379"] * 8
    )
    return {
        "model": "glm-5.1",
        "max_tokens": 512,
        "system": "You are a DevOps engineer. Diagnose issues from pod logs.",
        "messages": [
            {"role": "user", "content": "Check pod logs"},
            {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_1", "name": "Bash", "input": {"command": "kubectl logs api-gw --tail=50"}}]},
            {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tu_1", "content": logs}]},
            {"role": "user", "content": "Root cause and fix?"},
        ],
    }

TOOLS_25 = [{"name": n, "description": d} for n, d in [
    ("Read", "Read files"), ("Edit", "Edit files"), ("Write", "Write files"),
    ("Bash", "Run commands"), ("Glob", "Find files"), ("Grep", "Search text"),
    ("WebFetch", "Fetch URLs"), ("WebSearch", "Search web"),
    ("NotebookEdit", "Edit notebooks"), ("TodoWrite", "Manage tasks"),
    ("Agent", "Spawn agents"), ("Plan", "Plan implementation"),
    ("EnterPlanMode", "Plan mode"), ("ExitPlanMode", "Exit plan"),
    ("AskUserQuestion", "Ask user"), ("CronCreate", "Create cron"),
    ("CronDelete", "Delete cron"), ("ScheduleWakeup", "Schedule wakeup"),
    ("EnterWorktree", "Enter worktree"), ("ExitWorktree", "Exit worktree"),
    ("mcp__docker_exec", "Docker exec"), ("mcp__docker_logs", "Docker logs"),
    ("mcp__docker_ps", "Docker ps"), ("mcp__docker_build", "Docker build"),
    ("mcp__docker_prune", "Docker prune"),
]]

PAYLOAD_TOOLS = {
    "model": "glm-5.1",
    "max_tokens": 256,
    "system": "You are helpful.",
    "messages": [{"role": "user", "content": "Read config.json and show DB settings"}],
    "tools": TOOLS_25,
}

PAYLOAD_THAI = {
    "model": "glm-5.1",
    "max_tokens": 256,
    "system": "You are a Thai software engineer. คุณเป็นวิศวกรซอฟต์แวร์ชาวไทย ช่วยตอบคำถามเกี่ยวกับ Kubernetes และ DevOps",
    "messages": [{"role": "user", "content": "วิธีตั้งค่า ingress controller ใน Kubernetes"}],
}


# --- Configurations to test ---
CONFIGS = [
    # Individual stages
    ("baseline",     "No optimizer",            overrides_all_off()),
    ("semantic_dedup", "Semantic Dedup",         overrides_only("semantic_dedup")),
    ("chunker",      "Chunker (topic reorder)",  overrides_only("chunker")),
    ("sketch",       "Sketch Dedup (MinHash)",   overrides_only("sketch")),
    ("textcomp",     "Text Compression",         overrides_only("textcomp")),
    ("caveman",      "Caveman (inject terse)",   overrides_only("caveman")),
    ("pordee",       "Pordee (Thai terse)",      overrides_only("pordee")),
    ("toolcomp",     "Tool Compression",         overrides_only("toolcomp")),
    ("toolfilter",   "Tool Filter",              overrides_only("toolfilter")),
    # Combined
    ("all",          "All stages ON",            overrides_all_on()),
    # Best pairs (hypothesized)
    ("pair_sem_cav", "Semantic + Caveman",       overrides_pair("semantic_dedup", "caveman")),
    ("pair_cav_tc",  "Caveman + ToolComp",       overrides_pair("caveman", "toolcomp")),
    ("pair_sem_tc",  "Semantic + ToolComp",      overrides_pair("semantic_dedup", "toolcomp")),
    ("pair_sem_sk",  "Semantic + Sketch",        overrides_pair("semantic_dedup", "sketch")),
    ("no_caveman",   "All except Caveman",       {s: False for s in ALL_STAGES if s != "caveman"}),
]

# Map config -> which payloads to use
# verbose: tests semantic_dedup, textcomp, caveman, pordee, sketch
# k8s: tests toolcomp, message_block, semantic_dedup, caveman
# tools: tests toolfilter
# thai: tests pordee
PAYLOAD_MAP = {
    "baseline":     ["verbose", "k8s", "tools", "thai"],
    "semantic_dedup": ["verbose", "k8s", "tools", "thai"],
    "chunker":      ["verbose", "k8s"],
    "sketch":       ["verbose", "k8s"],
    "textcomp":     ["verbose", "k8s"],
    "caveman":      ["verbose", "k8s", "thai"],
    "pordee":       ["verbose", "thai"],
    "toolcomp":     ["k8s"],
    "toolfilter":   ["tools"],
    "all":          ["verbose", "k8s", "tools", "thai"],
    "pair_sem_cav": ["verbose", "k8s"],
    "pair_cav_tc":  ["k8s"],
    "pair_sem_tc":  ["verbose", "k8s"],
    "pair_sem_sk":  ["verbose", "k8s"],
    "no_caveman":   ["verbose", "k8s"],
}


def main():
    print("=" * 90)
    print("PER-STAGE OPTIMIZER POC: Isolate each stage, measure real token impact")
    print("=" * 90)

    # Step 1: Create all profiles
    print("\n### Creating profiles...")
    for cfg_name, desc, overrides in CONFIGS:
        r = create_profile(f"opt-{cfg_name}", overrides)
        status = "OK" if "error" not in r else f"ERR: {r['error'][:60]}"
        print(f"  opt-{cfg_name:20s} ({desc:30s}): {status}")

    # Step 2: Run tests
    payloads = {
        "verbose": PAYLOAD_VERBOSE,
        "k8s": make_k8s_payload(),
        "tools": PAYLOAD_TOOLS,
        "thai": PAYLOAD_THAI,
    }

    results = {}  # (cfg_name, payload_name) -> {input, output}

    for cfg_name, desc, overrides in CONFIGS:
        profile = f"opt-{cfg_name}"
        active_payloads = PAYLOAD_MAP.get(cfg_name, ["verbose", "k8s"])

        for pname in active_payloads:
            body = payloads[pname]
            key = (cfg_name, pname)
            print(f"  [{profile:25s}] {pname:8s} ...", end=" ", flush=True)
            r = send(profile, body)
            if "error" in r:
                print(f"ERR: {r['error'][:80]}")
                results[key] = {"input": 0, "output": 0, "error": r["error"]}
            else:
                print(f"in={r['input']:,} out={r['output']:,}")
                results[key] = r
            time.sleep(0.5)

    # Step 3: Report
    print("\n\n" + "=" * 90)
    print("RESULTS: Per-Stage Token Impact")
    print("=" * 90)

    # For each payload, compare all configs
    for pname in ["verbose", "k8s", "tools", "thai"]:
        baseline_key = ("baseline", pname)
        if baseline_key not in results:
            continue
        bl = results[baseline_key]
        if "error" in bl:
            continue
        bl_in, bl_out = bl["input"], bl["output"]

        print(f"\n### Payload: {pname} (baseline: in={bl_in:,} out={bl_out:,})\n")
        print(f"| Config | Input | In vs Base | Output | Out vs Base | Net Cost |")
        print(f"|--------|-------|------------|--------|-------------|----------|")

        ip = 1.4 / 1e6
        op = 4.4 / 1e6
        bl_cost = bl_in * ip + bl_out * op

        for cfg_name, desc, overrides in CONFIGS:
            key = (cfg_name, pname)
            if key not in results or "error" in results[key]:
                continue
            r = results[key]
            ri, ro = r["input"], r["output"]
            di = ri - bl_in
            do = ro - bl_out
            cost = ri * ip + ro * op
            savings = bl_cost - cost
            pct = savings / bl_cost * 100 if bl_cost > 0 else 0

            in_pct = f"{di:+,} ({di/bl_in*100:+.0f}%)" if bl_in > 0 else "-"
            out_pct = f"{do:+,} ({do/bl_out*100:+.0f}%)" if bl_out > 0 else "-"
            net = f"${savings:+.4f} ({pct:+.0f}%)"

            print(f"| {cfg_name:16s} | {ri:>5,} | {in_pct:>14s} | {ro:>6,} | {out_pct:>13s} | {net:>14s} |")

    # Summary: which config saves most across all payloads
    print(f"\n### Aggregate Savings Across All Payloads\n")
    ip = 1.4 / 1e6
    op = 4.4 / 1e6

    print(f"| Config | Total In | Total Out | Total Cost | vs Baseline |")
    print(f"|--------|----------|-----------|------------|-------------|")

    config_totals = {}
    for cfg_name, desc, overrides in CONFIGS:
        total_in = total_out = 0
        for pname in ["verbose", "k8s", "tools", "thai"]:
            key = (cfg_name, pname)
            if key in results and "error" not in results[key]:
                total_in += results[key]["input"]
                total_out += results[key]["output"]
        config_totals[cfg_name] = (total_in, total_out)

    bl_in, bl_out = config_totals.get("baseline", (0, 0))
    bl_cost = bl_in * ip + bl_out * op

    sorted_configs = sorted(config_totals.items(), key=lambda x: x[1][0]*ip + x[1][1]*op)
    for cfg_name, (ti, to_) in sorted_configs:
        cost = ti * ip + to_ * op
        savings = bl_cost - cost
        pct = savings / bl_cost * 100 if bl_cost > 0 else 0
        print(f"| {cfg_name:16s} | {ti:>8,} | {to_:>9,} | ${cost:.4f} | ${savings:+.4f} ({pct:+.0f}%) |")

    # Rank
    print(f"\n### Ranking (cheapest first)\n")
    for i, (cfg_name, (ti, to_)) in enumerate(sorted_configs, 1):
        cost = ti * ip + to_ * op
        savings = bl_cost - cost
        pct = savings / bl_cost * 100 if bl_cost > 0 else 0
        desc = next(d for c, d, _ in CONFIGS if c == cfg_name)
        marker = " <-- BEST" if i == 1 else ""
        print(f"  {i:2d}. {cfg_name:16s} ({desc:30s}): ${cost:.4f} {pct:+.0f}%{marker}")


if __name__ == "__main__":
    main()
