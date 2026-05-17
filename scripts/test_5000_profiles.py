#!/usr/bin/env python3
"""5000 test cases across 4 profiles (1250 each) - unbuffered output."""
import sys; sys.stdout.reconfigure(line_buffering=True)
"""
5000 test cases across 4 profiles (1250 each).
Generates parameterized payloads combining:
  system_prompt x tool_count x message_turns x tool_result_size x language x content_type x max_tokens
Uses concurrent execution with configurable workers.
"""
import json, urllib.request, urllib.error, time, concurrent.futures, sys, os, random, itertools
from collections import defaultdict
from datetime import datetime

# ============================================================================
# CONFIG
# ============================================================================

GW = "http://localhost:9000"
ZAI_KEY = "1d63b5db6d984db1913ca9596125f06b.RHYoZWfoRZteSclW"
CONCURRENT = 3
TESTS_PER_PROFILE = 1250
SEED = 42

PROFILES = {
    "cc": {
        "name": "cc", "provider": "claude-oauth", "model": "claude-sonnet-4-6",
        "expect_optimizer": "toolcomp+toolfilter_only", "auth_value": None,
        "concurrent_limit": 2,
    },
    "lotuss": {
        "name": "lotuss", "provider": "lotuss", "model": "glm-5.1",
        "expect_optimizer": "all_stages", "auth_value": None,
        "concurrent_limit": 2,
    },
    "kimi": {
        "name": "kimi", "provider": "kimi", "model": "kimi-latest",
        "expect_optimizer": "all_stages", "auth_value": None,
        "concurrent_limit": 2,
    },
    "zai-test": {
        "name": "zai-test", "provider": "zai", "model": "glm-5.1",
        "expect_optimizer": "SKIPPED", "auth_value": ZAI_KEY,
        "concurrent_limit": 2,
    },
}

# ============================================================================
# DIMENSION GENERATORS
# ============================================================================

SYSTEM_PROMPTS = {
    "empty": "",
    "short": "You are helpful.",
    "medium": (
        "You are a senior DevOps engineer specializing in Kubernetes, Terraform, and CI/CD. "
        "You follow GitOps principles and prefer declarative configurations. "
        "Always provide practical, production-ready solutions."
    ),
    "long": (
        "You are a senior platform engineer with 10 years of experience in cloud-native technologies. "
        "You specialize in Kubernetes, Terraform, ArgoCD, and observability stacks. "
        "You follow GitOps principles and prefer declarative infrastructure as code. "
        "You implement security best practices including least privilege RBAC, network policies, and pod security standards. "
        "You use Helm charts for application packaging and Kustomize for environment overlays. "
        "You configure horizontal pod autoscalers with custom metrics and pod disruption budgets for high availability. "
        "You implement CI/CD pipelines with GitHub Actions using reusable workflows and matrix builds. "
        "You manage secrets with HashiCorp Vault using dynamic credentials and auto-rotation. "
        "You follow the principle of least privilege for RBAC. "
        "You use ServiceMesh (Istio or Linkerd) for mTLS and traffic management. "
        "You implement disaster recovery with Velero backups. "
        "You use Kyverno or OPA Gatekeeper for policy enforcement. "
    ),
    "verbose_dedup": (
        "You are an expert Go developer. You write clean, idiomatic Go code. "
        "You follow best practices for error handling, concurrency, and testing. "
        "You prefer simple solutions over clever ones. You always use context.Context. "
        "You prefer table-driven tests. You use structured logging with slog. "
        "You avoid global state. You keep interfaces small. "
        "You return errors rather than panic. You use defer for cleanup. "
        "You follow best practices for error handling, concurrency, and testing. "
        "You prefer simple solutions over clever ones."
    ),
    "array_cache": [
        {"type": "text", "text": "You are a helpful assistant.", "cache_control": {"type": "ephemeral"}},
        {"type": "text", "text": "Always respond concisely and accurately."},
    ],
    "code_system": (
        "You are a code reviewer. Focus on:\n"
        "```go\nfunc (s *Server) handleRequest(ctx context.Context, req *Request) (*Response, error) {\n"
        "    select {\n    case <-ctx.Done():\n        return nil, ctx.Err()\n    default:\n    }\n"
        "    return s.processor.Process(ctx, req)\n}\n```\n"
        "Look for: error handling, context propagation, resource leaks, race conditions."
    ),
    "thai_system": "You are a Thai software engineer. คุณเป็นวิศวกรซอฟต์แวร์ชาวไทย ช่วยตอบคำถามเกี่ยวกับ DevOps",
    "chinese_system": "你是一位资深软件工程师，擅长 Kubernetes 和云原生技术。请用中文回答。",
}

CONTENT_TYPES = {
    "prose": {
        "question": "Explain the difference between a Deployment and a StatefulSet in Kubernetes.",
        "system_append": "",
    },
    "code_go": {
        "question": "Write a Go function that retries an HTTP request with exponential backoff.",
        "system_append": " Respond with code only.",
    },
    "code_python": {
        "question": "Write a Python decorator that measures function execution time.",
        "system_append": " Respond with code only.",
    },
    "logs": {
        "question": "Diagnose the issue from these logs.",
        "system_append": " Be concise, root cause only.",
    },
    "json_config": {
        "question": "Validate this JSON config and list issues.",
        "system_append": " List issues as bullet points.",
    },
    "yaml_manifest": {
        "question": "What's wrong with this Kubernetes manifest?",
        "system_append": " Focus on security and best practices.",
    },
    "diff_review": {
        "question": "Review this diff for bugs and security issues.",
        "system_append": " Be specific about line-level issues.",
    },
    "sql": {
        "question": "Optimize this SQL query for performance.",
        "system_append": " Explain the optimization strategy.",
    },
    "terraform": {
        "question": "Review this Terraform module for best practices.",
        "system_append": " Focus on state management and security.",
    },
    "dockerfile": {
        "question": "Optimize this Dockerfile for smaller image size.",
        "system_append": " Explain each optimization.",
    },
}

LANGUAGES = {
    "en": {"user_msg": "Explain Kubernetes operators in 2 sentences."},
    "th": {"user_msg": "อธิบาย Kubernetes Operator ใน 2 ประโยค"},
    "zh": {"user_msg": "用两句话解释 Kubernetes Operator"},
    "ja": {"user_msg": "Kubernetes Operator を2文で説明してください"},
    "ko": {"user_msg": "Kubernetes Operator를 두 문장으로 설명하세요"},
    "mixed": {"user_msg": "อธิบาย HorizontalPodAutoscaler ย่อหน้าเดียว Explain briefly"},
}

SCHEMA = {"type": "object", "properties": {"input": {"type": "string"}}, "required": ["input"]}
TOOL_DEFS = [
    ("Read", "Read files"), ("Edit", "Edit files"), ("Write", "Write files"),
    ("Bash", "Execute commands"), ("Glob", "Find files"), ("Grep", "Search patterns"),
    ("WebFetch", "Fetch URLs"), ("WebSearch", "Search web"), ("NotebookEdit", "Edit notebooks"),
    ("TodoWrite", "Manage todos"), ("Agent", "Spawn agents"), ("Plan", "Create plans"),
    ("EnterPlanMode", "Enter plan mode"), ("ExitPlanMode", "Exit plan mode"),
    ("AskUserQuestion", "Ask questions"), ("CronCreate", "Schedule cron"),
    ("CronDelete", "Delete cron"), ("ScheduleWakeup", "Schedule wakeup"),
    ("EnterWorktree", "Enter worktree"), ("ExitWorktree", "Exit worktree"),
    ("mcp__docker_exec", "Docker exec"), ("mcp__docker_logs", "Docker logs"),
    ("mcp__docker_ps", "Docker list"), ("mcp__docker_build", "Docker build"),
    ("mcp__docker_prune", "Docker prune"),
]

MAX_TOKENS_OPTIONS = [16, 32, 64, 128]

# Tool result generators
def make_k8s_logs(n=25):
    lines = []
    for i in range(n):
        lines.append(f"2026-05-06 04:{i%60:02d}:{(i*7)%60:02d} INFO Health check passed req_id={i:04x}")
    lines += ["2026-05-06 04:01:05 WARN Rate limit approaching for agent-123"]
    lines += ["2026-05-06 04:02:00 ERROR Connection timeout to upstream host=db-0.svc port=5432"]
    return "\n".join(lines)

def make_code_diff():
    return (
        "diff --git a/handler.go b/handler.go\n--- a/handler.go\n+++ b/handler.go\n"
        "@@ -100,6 +100,12 @@\n+\t// FIXME: hardcoded timeout\n"
        "+\tclient := &http.Client{Timeout: 30 * time.Second}\n"
        "+\tresp, err := client.Do(req)\n+\tif err != nil {\n"
        "+\t\treturn nil, err\n+\t}\n+\tdefer resp.Body.Close()\n"
    )

def make_json_config():
    return json.dumps({
        "apiVersion": "v1", "kind": "ConfigMap",
        "metadata": {"name": "app-config"},
        "data": {
            "DB_HOST": "localhost", "DB_PORT": "5432",
            "CACHE_TTL": "300", "MAX_CONNECTIONS": "100",
            "LOG_LEVEL": "debug", "ENV": "production",
        }
    }, indent=2)

def make_yaml_manifest():
    return (
        "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api-gateway\n"
        "spec:\n  replicas: 3\n  selector:\n    matchLabels:\n      app: api-gateway\n"
        "  template:\n    metadata:\n      labels:\n        app: api-gateway\n"
        "    spec:\n      containers:\n      - name: gateway\n        image: api-gw:latest\n"
        "        ports:\n        - containerPort: 8080\n"
        "        resources:\n          limits:\n            memory: 512Mi\n"
    )

TOOL_RESULT_CONTENT = {
    "logs": lambda n=25: make_k8s_logs(n),
    "code_go": lambda: "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
    "code_python": lambda: "import asyncio\n\nasync def main():\n    print('hello')\n\nasyncio.run(main())\n",
    "json_config": lambda: make_json_config(),
    "yaml_manifest": lambda: make_yaml_manifest(),
    "diff_review": lambda: make_code_diff(),
    "sql": lambda: "SELECT u.*, COUNT(o.id) as order_count FROM users u LEFT JOIN orders o ON u.id = o.user_id WHERE u.created_at > '2025-01-01' GROUP BY u.id ORDER BY order_count DESC;\n",
    "terraform": lambda: 'resource "aws_instance" "web" {\n  ami           = "ami-12345678"\n  instance_type = "t2.micro"\n}\n',
    "dockerfile": lambda: "FROM ubuntu:latest\nRUN apt-get update\nCOPY . /app\nCMD [\"python3\", \"/app/main.py\"]\n",
}

# ============================================================================
# TEST CASE GENERATOR
# ============================================================================

def generate_test_cases(count=1250, seed=SEED):
    rng = random.Random(seed)
    test_cases = []

    system_keys = list(SYSTEM_PROMPTS.keys())
    content_keys = list(CONTENT_TYPES.keys())
    lang_keys = list(LANGUAGES.keys())
    tool_counts = [0, 1, 5, 10, 15, 20, 25]
    turn_counts = [1, 3, 5, 7]
    tool_result_sizes = [0, 10, 50, 120]
    max_tokens_options = MAX_TOKENS_OPTIONS

    attempts = 0
    seen = set()
    while len(test_cases) < count and attempts < count * 10:
        attempts += 1
        sys_key = rng.choice(system_keys)
        content_key = rng.choice(content_keys)
        lang_key = rng.choice(lang_keys)
        tool_count = rng.choice(tool_counts)
        turns = rng.choice(turn_counts)
        tr_size = rng.choice(tool_result_sizes)
        max_tok = rng.choice(max_tokens_options)
        stream = True  # ALL SSE

        sig = (sys_key, content_key, lang_key, tool_count, turns, tr_size, max_tok, stream)
        if sig in seen:
            continue
        seen.add(sig)

        content = CONTENT_TYPES[content_key]
        lang = LANGUAGES[lang_key]

        # Build tools
        tools = None
        if tool_count > 0:
            selected = rng.sample(TOOL_DEFS, min(tool_count, len(TOOL_DEFS)))
            tools = [{"name": n, "description": d, "input_schema": SCHEMA} for n, d in selected]

        # Build messages
        messages = []
        user_content = lang["user_msg"] if rng.random() < 0.3 else content["question"]

        if turns == 1:
            messages.append({"role": "user", "content": user_content})
        else:
            # Multi-turn with tool_use/tool_result
            messages.append({"role": "user", "content": "Start debugging the API issue."})
            for t in range(turns - 1):
                tool_name = rng.choice(["Bash", "Read", "Grep", "Glob", "WebFetch"])
                tool_id = f"tu_{t+1}"
                messages.append({
                    "role": "assistant",
                    "content": [{"type": "tool_use", "id": tool_id, "name": tool_name, "input": {"input": f"command_{t}"}}]
                })
                # Tool result content
                tr_content = "No output"
                if tr_size > 0:
                    tr_gen = TOOL_RESULT_CONTENT.get(content_key, TOOL_RESULT_CONTENT["logs"])
                    tr_content = tr_gen(tr_size) if callable(tr_gen) and content_key == "logs" else tr_gen()
                    if tr_size < 50 and isinstance(tr_content, str):
                        tr_content = "\n".join(tr_content.split("\n")[:tr_size])

                messages.append({
                    "role": "user",
                    "content": [{"type": "tool_result", "tool_use_id": tool_id, "content": tr_content}]
                })
            messages.append({"role": "user", "content": user_content})

        # Build system prompt
        system = SYSTEM_PROMPTS[sys_key]
        if isinstance(system, str) and system and content["system_append"]:
            system += content["system_append"]

        payload = {"max_tokens": max_tok, "messages": messages}
        if system:
            payload["system"] = system
        if tools:
            payload["tools"] = tools
        payload["stream"] = True

        test_cases.append({
            "id": len(test_cases) + 1,
            "sig": sig,
            "payload": payload,
            "is_stream": stream,
            "dims": {
                "system": sys_key, "content": content_key, "language": lang_key,
                "tool_count": tool_count, "turns": turns, "tool_result_size": tr_size,
                "max_tokens": max_tok, "stream": stream,
            },
        })

    return test_cases

# ============================================================================
# REQUEST FUNCTIONS
# ============================================================================

def send_request(profile_name, payload, auth_value=None):
    body = {**payload, "model": PROFILES[profile_name]["model"], "stream": True}
    return send_streaming(profile_name, body, auth_value)

def send_streaming(profile_name, body, auth_value=None):
    headers = {
        "Content-Type": "application/json", "anthropic-version": "2023-06-01",
        "X-Profile": profile_name, "Accept": "text/event-stream",
    }
    if profile_name == "cc" and auth_value:
        headers["Authorization"] = f"Bearer {auth_value}"
    elif auth_value:
        headers["x-api-key"] = auth_value

    data = json.dumps(body, ensure_ascii=False).encode()
    req = urllib.request.Request(f"{GW}/v1/messages", data=data, headers=headers, method="POST")
    try:
        start = time.time()
        resp = urllib.request.urlopen(req, timeout=180)
        input_tokens = 0
        output_tokens = 0
        cache_creation = 0
        cache_read = 0
        stopped = False
        for line in resp:
            if stopped:
                break
            line = line.decode("utf-8", errors="replace").strip()
            if line.startswith("data:"):
                try:
                    d = json.loads(line[5:])
                    if d.get("type") == "message_stop":
                        stopped = True
                    elif d.get("type") == "message_start":
                        mu = d.get("message", {}).get("usage", {})
                        if mu.get("input_tokens", 0) > 0:
                            input_tokens = mu["input_tokens"]
                    elif d.get("type") == "message_delta":
                        du = d.get("usage", {})
                        if du.get("output_tokens", 0) > 0:
                            output_tokens = du["output_tokens"]
                        if du.get("input_tokens", 0) > 0 and input_tokens == 0:
                            input_tokens = du["input_tokens"]
                except json.JSONDecodeError:
                    pass
        elapsed = time.time() - start
        return {
            "ok": True, "input": input_tokens, "output": output_tokens,
            "cache_creation": cache_creation, "cache_read": cache_read,
            "elapsed": elapsed, "stop_reason": "stream",
        }
    except urllib.error.HTTPError as e:
        return {"ok": False, "error": f"HTTP {e.code}", "elapsed": 0}
    except Exception as e:
        return {"ok": False, "error": str(e)[:150], "elapsed": 0}

# ============================================================================
# RUNNER
# ============================================================================

def run_profile(profile_key, test_cases):
    pcfg = PROFILES[profile_key]
    auth_val = pcfg.get("auth_value")
    workers = pcfg.get("concurrent_limit", 3)

    results = []
    pass_count = 0
    fail_count = 0
    total_input = 0
    total_output = 0
    latencies = []
    error_types = defaultdict(int)
    dim_stats = defaultdict(lambda: {"pass": 0, "fail": 0, "input": 0, "output": 0, "latency": []})

    print(f"\n  Running {len(test_cases)} tests with {workers} workers...")
    start_time = time.time()

    def execute_test(tc):
        return tc, send_request(profile_key, tc["payload"], auth_value=auth_val)

    batch_size = 25
    for batch_start in range(0, len(test_cases), batch_size):
        batch = test_cases[batch_start:batch_start + batch_size]
        with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as ex:
            futs = {ex.submit(execute_test, tc): tc for tc in batch}
            for f in concurrent.futures.as_completed(futs):
                tc, r = f.result()
                r["dims"] = tc["dims"]
                r["test_id"] = tc["id"]
                results.append(r)

                if r["ok"]:
                    pass_count += 1
                    total_input += r.get("input", 0)
                    total_output += r.get("output", 0)
                    latencies.append(r["elapsed"])
                    for dk, dv in tc["dims"].items():
                        key = f"{dk}={dv}"
                        dim_stats[key]["pass"] += 1
                        dim_stats[key]["input"] += r.get("input", 0)
                        dim_stats[key]["output"] += r.get("output", 0)
                        dim_stats[key]["latency"].append(r["elapsed"])
                else:
                    fail_count += 1
                    err_short = r.get("error", "unknown")[:40]
                    error_types[err_short] += 1
                    for dk, dv in tc["dims"].items():
                        dim_stats[f"{dk}={dv}"]["fail"] += 1

                done = pass_count + fail_count
                if done % 100 == 0:
                    elapsed = time.time() - start_time
                    rate = done / elapsed if elapsed > 0 else 0
                    eta = (len(test_cases) - done) / rate if rate > 0 else 0
                    print(f"    {done}/{len(test_cases)} ({pass_count} ok, {fail_count} fail) "
                          f"rate={rate:.1f}/s eta={eta:.0f}s")

    wall = time.time() - start_time
    return {
        "results": results, "pass": pass_count, "fail": fail_count,
        "total_input": total_input, "total_output": total_output,
        "latencies": latencies, "wall": wall,
        "error_types": dict(error_types), "dim_stats": dict(dim_stats),
    }

# ============================================================================
# REPORT
# ============================================================================

def print_profile_report(pkey, stats, test_cases):
    pcfg = PROFILES[pkey]
    C = {
        "G": "\033[32m", "R": "\033[31m", "Y": "\033[33m", "C": "\033[36m",
        "M": "\033[35m", "B": "\033[1m", "D": "\033[2m", "X": "\033[0m",
    }

    total = stats["pass"] + stats["fail"]
    rate = total / stats["wall"] if stats["wall"] > 0 else 0
    pass_pct = stats["pass"] / total * 100 if total > 0 else 0

    print(f"\n{'='*90}")
    print(f"{C['B']}{C['M']}  PROFILE: {pcfg['name']} ({pcfg['provider']}, model={pcfg['model']}){C['X']}")
    print(f"{C['B']}  Optimizer: {pcfg['expect_optimizer']}{C['X']}")
    print(f"{'='*90}")

    # Summary
    print(f"\n  {C['B']}SUMMARY{C['X']}")
    print(f"  {'Tests:':<20s} {total:,}")
    print(f"  {'Pass:':<20s} {C['G']}{stats['pass']:,} ({pass_pct:.1f}%){C['X']}")
    print(f"  {'Fail:':<20s} {C['R']}{stats['fail']:,}{C['X']}")
    print(f"  {'Wall time:':<20s} {stats['wall']:.1f}s")
    print(f"  {'Throughput:':<20s} {rate:.1f} req/s")
    print(f"  {'Total input tokens:':<20s} {stats['total_input']:,}")
    print(f"  {'Total output tokens:':<20s} {stats['total_output']:,}")

    # Latency
    if stats["latencies"]:
        lats = sorted(stats["latencies"])
        n = len(lats)
        print(f"\n  {C['B']}LATENCY{C['X']}")
        print(f"  {'Min:':<20s} {lats[0]:.2f}s")
        print(f"  {'P50:':<20s} {lats[n//2]:.2f}s")
        print(f"  {'P90:':<20s} {lats[int(n*0.9)]:.2f}s")
        print(f"  {'P95:':<20s} {lats[int(n*0.95)]:.2f}s")
        print(f"  {'P99:':<20s} {lats[int(n*0.99)]:.2f}s")
        print(f"  {'Max:':<20s} {lats[-1]:.2f}s")
        print(f"  {'Avg:':<20s} {sum(lats)/n:.2f}s")

    # Token stats
    if stats["latencies"]:
        inputs = [r.get("input", 0) for r in stats["results"] if r.get("ok")]
        outputs = [r.get("output", 0) for r in stats["results"] if r.get("ok")]
        if inputs:
            si = sorted(inputs)
            so = sorted(outputs)
            print(f"\n  {C['B']}TOKEN DISTRIBUTION{C['X']}")
            print(f"  {'Input min/avg/max:':<20s} {si[0]:,} / {sum(si)//len(si):,} / {si[-1]:,}")
            print(f"  {'Output min/avg/max:':<20s} {so[0]:,} / {sum(so)//len(so):,} / {so[-1]:,}")

    # Dimension breakdown
    print(f"\n  {C['B']}DIMENSION BREAKDOWN{C['X']}")
    for dim_name in ["system", "content", "language", "tool_count", "turns", "tool_result_size", "max_tokens", "stream"]:
        rows = []
        for key, ds in stats["dim_stats"].items():
            if not key.startswith(f"{dim_name}="):
                continue
            val = key.split("=", 1)[1]
            total_dim = ds["pass"] + ds["fail"]
            pass_pct = ds["pass"] / total_dim * 100 if total_dim > 0 else 0
            avg_lat = sum(ds["latency"]) / len(ds["latency"]) if ds["latency"] else 0
            rows.append((val, total_dim, ds["pass"], pass_pct, ds["input"], ds["output"], avg_lat))
        if rows:
            rows.sort(key=lambda x: x[0])
            print(f"\n    {C['Y']}{dim_name.upper()}{C['X']}")
            print(f"    {'Value':<15s} {'Total':>6s} {'Pass':>6s} {'%':>7s} {'AvgIn':>8s} {'AvgOut':>8s} {'AvgLat':>8s}")
            print(f"    {'-'*15} {'-'*6} {'-'*6} {'-'*7} {'-'*8} {'-'*8} {'-'*8}")
            for val, total_dim, passed, pct, inp, out, lat in rows:
                avg_in = inp // max(passed, 1)
                avg_out = out // max(passed, 1)
                pct_color = C['G'] if pct >= 95 else (C['Y'] if pct >= 80 else C['R'])
                print(f"    {val:<15s} {total_dim:>6,} {passed:>6,} {pct_color}{pct:>6.1f}%{C['X']} {avg_in:>8,} {avg_out:>8,} {lat:>7.2f}s")

    # Errors
    if stats["error_types"]:
        print(f"\n  {C['B']}{C['R']}ERROR TYPES{C['X']}")
        for err, count in sorted(stats["error_types"].items(), key=lambda x: -x[1]):
            print(f"    {C['R']}{count:>5,}x{C['X']} {err}")

    return {
        "profile": pkey, "provider": pcfg["provider"], "model": pcfg["model"],
        "optimizer": pcfg["expect_optimizer"],
        "total": total, "pass": stats["pass"], "fail": stats["fail"],
        "wall": stats["wall"], "rate": rate,
        "total_input": stats["total_input"], "total_output": stats["total_output"],
        "latency_p50": sorted(stats["latencies"])[len(stats["latencies"])//2] if stats["latencies"] else 0,
        "latency_p99": sorted(stats["latencies"])[int(len(stats["latencies"])*0.99)] if stats["latencies"] else 0,
    }

# ============================================================================
# MAIN
# ============================================================================

def main():
    import argparse
    p = argparse.ArgumentParser(description="5000 test cases across profiles")
    p.add_argument("--profile", "-P", help="Run only this profile", default=None)
    p.add_argument("--gateway", "-g", help="Gateway URL", default=None)
    p.add_argument("--count", "-n", type=int, default=TESTS_PER_PROFILE, help="Tests per profile")
    p.add_argument("--concurrent", "-c", type=int, default=None, help="Override concurrent workers")
    p.add_argument("--seed", "-s", type=int, default=SEED, help="Random seed")
    p.add_argument("--json", action="store_true", help="Output JSON summary")
    args = p.parse_args()

    if args.gateway:
        global GW
        GW = args.gateway
    if args.concurrent:
        for k in PROFILES:
            PROFILES[k]["concurrent_limit"] = args.concurrent

    target = {k: v for k, v in PROFILES.items() if args.profile is None or k == args.profile}
    if args.profile and not target:
        print(f"Unknown profile: {args.profile}. Available: {', '.join(PROFILES.keys())}")
        sys.exit(1)

    print(f"\n{'='*90}")
    print(f"  5000 TEST SUITE: {args.count} tests x {len(target)} profile(s) = {args.count * len(target)} total")
    print(f"  Gateway: {GW}  Seed: {args.seed}  Started: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    print(f"{'='*90}")

    test_cases = generate_test_cases(count=args.count, seed=args.seed)
    print(f"  Generated {len(test_cases)} unique test cases")

    summaries = []
    for pkey in target:
        pcfg = PROFILES[pkey]
        print(f"\n{'#'*90}")
        print(f"  RUNNING: {pkey} ({pcfg['provider']}, {pcfg['model']})")
        print(f"  Expected optimizer: {pcfg['expect_optimizer']}")
        print(f"  Workers: {pcfg.get('concurrent_limit', 3)}")
        print(f"{'#'*90}")
        stats = run_profile(pkey, test_cases)
        summary = print_profile_report(pkey, stats, test_cases)
        summaries.append(summary)

    # Cross-profile comparison
    if len(summaries) > 1:
        print(f"\n\n{'='*90}")
        print(f"  {'' if not args.json else ''}CROSS-PROFILE COMPARISON")
        print(f"{'='*90}\n")
        print(f"  {'Profile':<10s} {'Provider':<14s} {'Pass':>8s} {'Fail':>6s} {'Rate':>8s} "
              f"{'P50':>8s} {'P99':>8s} {'TotalIn':>10s} {'TotalOut':>10s} {'Wall':>8s}")
        print(f"  {'-'*10} {'-'*14} {'-'*8} {'-'*6} {'-'*8} {'-'*8} {'-'*8} {'-'*10} {'-'*10} {'-'*8}")
        for s in summaries:
            pct = s['pass'] / s['total'] * 100 if s['total'] > 0 else 0
            print(f"  {s['profile']:<10s} {s['provider']:<14s} "
                  f"{s['pass']:>7,} {s['fail']:>6,} {s['rate']:>7.1f}/s "
                  f"{s['latency_p50']:>7.2f}s {s['latency_p99']:>7.2f}s "
                  f"{s['total_input']:>10,} {s['total_output']:>10,} {s['wall']:>7.1f}s")

    if args.json:
        print(f"\n--- JSON ---")
        print(json.dumps(summaries, indent=2))

    print(f"\n  Completed: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    print(f"  Verify: docker logs arl-gateway 2>&1 | grep -E 'optimizer_step|zai_provider_skip' | tail -50")

if __name__ == "__main__":
    main()
