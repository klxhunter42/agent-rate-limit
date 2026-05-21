#!/usr/bin/env python3
"""
Comprehensive optimizer + routing test for all profiles: cc, kimi, zai-test.
20 test cases covering: system prompts, tools, multi-turn, languages, streaming, concurrency.
Verifies optimizer behavior per provider with detailed comparison tables.
"""
import json, urllib.request, urllib.error, time, concurrent.futures, sys, os

GW = "http://localhost:9000"
ZAI_KEY = "1d63b5db6d984db1913ca9596125f06b.RHYoZWfoRZteSclW"

# ANSI colors
C_RESET = "\033[0m"
C_BOLD = "\033[1m"
C_DIM = "\033[2m"
C_GREEN = "\033[32m"
C_RED = "\033[31m"
C_YELLOW = "\033[33m"
C_CYAN = "\033[36m"
C_MAGENTA = "\033[35m"

# ============================================================================
# DATA GENERATORS
# ============================================================================

VERBOSE_SYSTEM = (
    "You are an expert Go developer. You write clean, idiomatic Go code. "
    "You follow best practices for error handling, concurrency, and testing. "
    "You prefer simple solutions over clever ones. You always use context.Context. "
    "You prefer table-driven tests. You use structured logging with slog. "
    "You avoid global state. You keep interfaces small. "
    "You return errors rather than panic. You use defer for cleanup. "
    "You follow best practices for error handling, concurrency, and testing. "
    "You prefer simple solutions over clever ones."
)

VERY_LONG_SYSTEM = (
    "You are a senior platform engineer. You specialize in Kubernetes, Terraform, and Go. "
    "You follow GitOps principles. You use ArgoCD for deployments. "
    "You write Terraform modules with variables, outputs, and remote state. "
    "You prefer Helm charts over raw YAML. You use Kustomize for overlays. "
    "You implement health checks, readiness probes, and liveness probes. "
    "You use NetworkPolicies for pod security. You implement PodSecurityPolicies. "
    "You use HorizontalPodAutoscalers for scaling. You configure PodDisruptionBudgets. "
    "You implement CI/CD pipelines with GitHub Actions or GitLab CI. "
    "You use container registry with image scanning. You sign images with cosign. "
    "You implement secrets management with HashiCorp Vault or AWS Secrets Manager. "
    "You use external-dns for DNS management. You use cert-manager for TLS. "
    "You implement observability with Prometheus, Grafana, and Loki. "
    "You use OpenTelemetry for distributed tracing. You configure Jaeger or Tempo. "
    "You implement SLOs and SLIs with Prometheus rules and Grafana dashboards. "
    "You use Thanos or Cortex for long-term Prometheus storage. "
    "You follow the principle of least privilege for RBAC. "
    "You use ServiceMesh (Istio or Linkerd) for mTLS and traffic management. "
    "You implement disaster recovery with Velero backups. "
    "You use Kyverno or OPA Gatekeeper for policy enforcement. "
    "You follow the principle of least privilege for RBAC. "
    "You use ServiceMesh (Istio or Linkerd) for mTLS and traffic management. "
    "You implement disaster recovery with Velero backups. "
    "You use Kyverno or OPA Gatekeeper for policy enforcement. "
)

CODE_SYSTEM = (
    "You are a code reviewer. Focus on:\n"
    "```go\n"
    "func (s *Server) handleRequest(ctx context.Context, req *Request) (*Response, error) {\n"
    "    // Always check context cancellation first\n"
    "    select {\n"
    "    case <-ctx.Done():\n"
    "        return nil, ctx.Err()\n"
    "    default:\n"
    "    }\n"
    "    return s.processor.Process(ctx, req)\n"
    "}\n"
    "```\n"
    "Look for: error handling, context propagation, resource leaks, race conditions."
)

SCHEMA = {"type": "object", "properties": {"input": {"type": "string"}}, "required": ["input"]}

TOOLS_25 = [
    {"name": n, "description": d, "input_schema": SCHEMA} for n, d in [
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
        ("mcp__docker_exec", "Execute in Docker containers"),
        ("mcp__docker_logs", "View Docker container logs"),
        ("mcp__docker_ps", "List Docker containers"),
        ("mcp__docker_build", "Build Docker images"),
        ("mcp__docker_prune", "Prune unused Docker resources"),
    ]
]

TOOLS_10 = TOOLS_25[:10]

TOOL_SINGLE = [{"name": "Bash", "description": "Execute shell commands", "input_schema": SCHEMA}]

def make_k8s_logs(n=25):
    lines = []
    for i in range(n):
        lines.append(f"2026-05-06 04:{i:02d}:{(i*7)%60:02d} INFO Health check passed")
    lines += [
        "2026-05-06 04:01:05 WARN Rate limit approaching for agent-123",
        "2026-05-06 04:02:00 ERROR Connection timeout to upstream",
    ]
    lines += ["2026-05-06 04:00:02 INFO Connected to Redis at localhost:6379"] * 8
    return "\n".join(lines)

def make_large_logs(n=120):
    lines = []
    for i in range(n):
        level = "ERROR" if i % 37 == 0 else ("WARN" if i % 19 == 0 else "INFO")
        msgs = {
            "INFO": f"Request processed successfully req_id={i:04x} latency={(i%50)+10}ms",
            "WARN": f"High memory usage detected: {85+i%15}% threshold=80%",
            "ERROR": f"Connection refused to upstream host=db-{i%3}.svc port=5432",
        }
        lines.append(f"2026-05-06 04:{(i*3)//60:02d}:{(i*3)%60:02d} {level} {msgs[level]}")
    return "\n".join(lines)

def make_code_review_messages():
    return [
        {"role": "user", "content": "Review this PR diff"},
        {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_1", "name": "Bash", "input": {"command": "git diff HEAD~1"}}]},
        {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tu_1", "content": (
            "diff --git a/handler.go b/handler.go\n"
            "--- a/handler.go\n"
            "+++ b/handler.go\n"
            "@@ -100,6 +100,12 @@\n"
            "+\t// FIXME: hardcoded timeout\n"
            "+\tclient := &http.Client{Timeout: 30 * time.Second}\n"
            "+\tresp, err := client.Do(req)\n"
            "+\tif err != nil {\n"
            "+\t\treturn nil, err\n"
            "+\t}\n"
            "+\tdefer resp.Body.Close()\n"
        )}]},
        {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_2", "name": "Read", "input": {"file": "handler.go"}}]},
        {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tu_2", "content": (
            "package handler\n\n"
            "import (\n\t\"net/http\"\n\t\"time\"\n)\n\n"
            "type Handler struct {\n\tclient *http.Client\n}\n\n"
            "// NewHandler creates a new handler with configurable timeout\n"
            "func NewHandler(timeout time.Duration) *Handler {\n"
            "\treturn &Handler{\n\t\tclient: &http.Client{Timeout: timeout},\n\t}\n}\n"
        )}]},
        {"role": "user", "content": "What issues do you see? Be specific."},
    ]

def make_multi_tool_turns():
    return [
        {"role": "user", "content": "Debug why the API is slow"},
        {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_1", "name": "Bash", "input": {"command": "kubectl top pods"}}]},
        {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tu_1", "content": "NAME CPU MEM\napi-gw-7d8f9 450m 512Mi\napi-gw-7d8fa 120m 256Mi\nworker-5c3a1 800m 1Gi"}]},
        {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_2", "name": "Bash", "input": {"command": "kubectl logs api-gw-7d8f9 --tail=20"}}]},
        {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tu_2", "content": make_k8s_logs(20)}]},
        {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_3", "name": "Grep", "input": {"pattern": "ERROR|WARN", "path": "/var/log/app.log"}}]},
        {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tu_3", "content": "2026-05-06 04:02:00 ERROR Connection timeout to upstream\n2026-05-06 04:01:05 WARN Rate limit approaching"}]},
        {"role": "user", "content": "Root cause analysis?"},
    ]

# ============================================================================
# TEST DEFINITIONS
# ============================================================================

TESTS = [
    # --- System Prompt Tests ---
    {
        "name": "Verbose system (dedup target)",
        "category": "system",
        "payload": {
            "max_tokens": 128,
            "system": VERBOSE_SYSTEM,
            "messages": [{"role": "user", "content": "Write a Go LRU cache with TTL in one sentence."}],
        },
    },
    {
        "name": "Array system + cache_control",
        "category": "system",
        "payload": {
            "max_tokens": 64,
            "system": [
                {"type": "text", "text": "You are a helpful assistant.", "cache_control": {"type": "ephemeral"}},
                {"type": "text", "text": "Always respond concisely."},
            ],
            "messages": [{"role": "user", "content": "What is 2+2?"}],
        },
    },
    {
        "name": "Very long system (budget target)",
        "category": "system",
        "payload": {
            "max_tokens": 128,
            "system": VERY_LONG_SYSTEM,
            "messages": [{"role": "user", "content": "Summarize your role in 2 sentences."}],
        },
    },
    {
        "name": "No system prompt",
        "category": "system",
        "payload": {
            "max_tokens": 64,
            "messages": [{"role": "user", "content": "What is Kubernetes?"}],
        },
    },
    {
        "name": "System with code blocks",
        "category": "system",
        "payload": {
            "max_tokens": 128,
            "system": CODE_SYSTEM,
            "messages": [{"role": "user", "content": "Review this code for issues."}],
        },
    },

    # --- Tool Tests ---
    {
        "name": "25-tool manifest (toolfilter)",
        "category": "tools",
        "payload": {
            "max_tokens": 64,
            "system": "You are helpful.",
            "messages": [{"role": "user", "content": "Read config.json and show DB settings"}],
            "tools": TOOLS_25,
        },
    },
    {
        "name": "10-tool manifest (below threshold)",
        "category": "tools",
        "payload": {
            "max_tokens": 64,
            "system": "You are helpful.",
            "messages": [{"role": "user", "content": "Find all Go files with TODO comments"}],
            "tools": TOOLS_10,
        },
    },
    {
        "name": "Single tool",
        "category": "tools",
        "payload": {
            "max_tokens": 64,
            "system": "You are helpful.",
            "messages": [{"role": "user", "content": "Run date command"}],
            "tools": TOOL_SINGLE,
        },
    },

    # --- Multi-turn Conversation Tests ---
    {
        "name": "K8s logs tool_result (toolcomp)",
        "category": "conversation",
        "payload_fn": lambda: {
            "max_tokens": 256,
            "system": "You are a DevOps engineer. Diagnose issues from pod logs.",
            "messages": [
                {"role": "user", "content": "Check pod logs"},
                {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_1", "name": "Bash", "input": {"command": "kubectl logs api-gw --tail=50"}}]},
                {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tu_1", "content": make_k8s_logs()}]},
                {"role": "user", "content": "Root cause? Be brief."},
            ],
        },
    },
    {
        "name": "Code review multi-turn",
        "category": "conversation",
        "payload_fn": lambda: {
            "max_tokens": 256,
            "system": "You are a senior code reviewer. Be specific about issues.",
            "messages": make_code_review_messages(),
        },
    },
    {
        "name": "Large tool_result (120 logs)",
        "category": "conversation",
        "payload_fn": lambda: {
            "max_tokens": 256,
            "system": "You are an SRE. Analyze application logs for patterns.",
            "messages": [
                {"role": "user", "content": "Check logs"},
                {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_1", "name": "Bash", "input": {"command": "tail -120 /var/log/app.log"}}]},
                {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "tu_1", "content": make_large_logs(120)}]},
                {"role": "user", "content": "Summarize: error patterns, frequency, root cause."},
            ],
        },
    },
    {
        "name": "3-turn tool chain",
        "category": "conversation",
        "payload_fn": lambda: {
            "max_tokens": 256,
            "system": "You are a DevOps engineer. Be concise.",
            "messages": make_multi_tool_turns(),
        },
    },

    # --- Language Tests ---
    {
        "name": "Thai content (pordee target)",
        "category": "language",
        "payload": {
            "max_tokens": 128,
            "system": "You are a Thai software engineer. คุณเป็นวิศวกรซอฟต์แวร์ชาวไทย ช่วยตอบคำถามเกี่ยวกับ Kubernetes และ DevOps",
            "messages": [{"role": "user", "content": "วิธีตั้งค่า ingress controller"}],
        },
    },
    {
        "name": "Mixed Thai+English",
        "category": "language",
        "payload": {
            "max_tokens": 128,
            "system": "You are a Thai-English bilingual engineer.",
            "messages": [{"role": "user", "content": "อธิบาย HorizontalPodAutoscaler ย่อหน้าเดียว ตอบเป็นภาษาไทย"}],
        },
    },
    {
        "name": "Chinese content",
        "category": "language",
        "payload": {
            "max_tokens": 128,
            "system": "You are a Chinese software engineer. 你是一位资深软件工程师，擅长 Kubernetes 和云原生技术。",
            "messages": [{"role": "user", "content": "如何配置 Kubernetes 的 Resource Quota？简短回答"}],
        },
    },

    # --- Basic / Edge ---
    {
        "name": "Simple ping",
        "category": "basic",
        "payload": {
            "max_tokens": 32,
            "messages": [{"role": "user", "content": "Say OK"}],
        },
    },
    {
        "name": "Streaming SSE",
        "category": "basic",
        "payload": {
            "max_tokens": 64,
            "stream": True,
            "messages": [{"role": "user", "content": "Count from 1 to 5, one per line."}],
        },
        "is_stream": True,
    },
]

# ============================================================================
# PROFILE CONFIGS
# ============================================================================

PROFILES = {
    "cc": {
        "name": "cc",
        "provider": "claude-oauth",
        "model": "claude-sonnet-4-6",
        "description": "Claude OAuth (claude-sonnet-4-6)",
        "expect_optimizer": "toolcomp+toolfilter_only",
        "auth_value": None,
    },
    "kimi": {
        "name": "kimi",
        "provider": "kimi",
        "model": "kimi-latest",
        "description": "Kimi (kimi-latest)",
        "expect_optimizer": "all_stages",
        "auth_value": None,
    },
    "zai-test": {
        "name": "zai-test",
        "provider": "zai",
        "model": "glm-5.1",
        "description": "Z.AI direct (glm-5.1)",
        "expect_optimizer": "SKIPPED",
        "auth_value": ZAI_KEY,
    },
}

# ============================================================================
# REQUEST FUNCTIONS
# ============================================================================

def send_request(profile_name, payload_dict, auth_value=None):
    body = payload_dict.copy()
    body["model"] = PROFILES[profile_name]["model"]

    data = json.dumps(body, ensure_ascii=False).encode()
    headers = {
        "Content-Type": "application/json",
        "anthropic-version": "2023-06-01",
        "X-Profile": profile_name,
    }
    if profile_name == "cc":
        if auth_value:
            headers["Authorization"] = f"Bearer {auth_value}"
    elif auth_value:
        headers["x-api-key"] = auth_value

    req = urllib.request.Request(f"{GW}/v1/messages", data=data, headers=headers, method="POST")
    try:
        start = time.time()
        resp = urllib.request.urlopen(req, timeout=180)
        elapsed = time.time() - start
        d = json.loads(resp.read())
        u = d.get("usage", {})
        return {
            "ok": True, "model": d.get("model", ""),
            "input": u.get("input_tokens", 0),
            "output": u.get("output_tokens", 0),
            "cache_creation": u.get("cache_creation_input_tokens", 0),
            "cache_read": u.get("cache_read_input_tokens", 0),
            "elapsed": elapsed,
            "content": "".join(b.get("text", "") for b in d.get("content", []) if b.get("type") == "text")[:120],
            "stop_reason": d.get("stop_reason", ""),
        }
    except urllib.error.HTTPError as e:
        return {"ok": False, "error": f"HTTP {e.code}: {e.read().decode()[:300]}", "elapsed": 0}
    except Exception as e:
        return {"ok": False, "error": str(e)[:200], "elapsed": 0}


def send_streaming_request(profile_name, payload_dict, auth_value=None):
    body = payload_dict.copy()
    body["model"] = PROFILES[profile_name]["model"]

    data = json.dumps(body, ensure_ascii=False).encode()
    headers = {
        "Content-Type": "application/json",
        "anthropic-version": "2023-06-01",
        "X-Profile": profile_name,
        "Accept": "text/event-stream",
    }
    if profile_name == "cc":
        if auth_value:
            headers["Authorization"] = f"Bearer {auth_value}"
    elif auth_value:
        headers["x-api-key"] = auth_value

    req = urllib.request.Request(f"{GW}/v1/messages", data=data, headers=headers, method="POST")
    try:
        start = time.time()
        resp = urllib.request.urlopen(req, timeout=180)
        events = 0
        chunks = 0
        usage = {}
        for line in resp:
            line = line.decode("utf-8", errors="replace").strip()
            if line.startswith("event:"):
                events += 1
            elif line.startswith("data:"):
                chunks += 1
                try:
                    d = json.loads(line[5:])
                    if "usage" in d and d["usage"]:
                        usage = d["usage"]
                    if d.get("type") == "message_stop":
                        break
                except json.JSONDecodeError:
                    pass
        elapsed = time.time() - start
        u = usage
        return {
            "ok": True, "model": "",
            "input": u.get("input_tokens", 0),
            "output": u.get("output_tokens", 0),
            "cache_creation": u.get("cache_creation_input_tokens", 0),
            "cache_read": u.get("cache_read_input_tokens", 0),
            "elapsed": elapsed,
            "content": f"SSE events={events}, chunks={chunks}",
            "stop_reason": "stream",
        }
    except urllib.error.HTTPError as e:
        return {"ok": False, "error": f"HTTP {e.code}: {e.read().decode()[:300]}", "elapsed": 0}
    except Exception as e:
        return {"ok": False, "error": str(e)[:200], "elapsed": 0}


def run_concurrent_test(profile_name, count=3, auth_value=None):
    def single(idx):
        return send_request(
            profile_name,
            {"max_tokens": 32, "messages": [{"role": "user", "content": f"Say hello number {idx}"}]},
            auth_value=auth_value,
        )
    start = time.time()
    results = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=count) as ex:
        futs = [ex.submit(single, i) for i in range(count)]
        for f in concurrent.futures.as_completed(futs):
            results.append(f.result())
    wall = time.time() - start
    return results, wall


def run_burst_test(profile_name, count=10, auth_value=None):
    latencies = []
    errors = 0
    for i in range(count):
        start = time.time()
        r = send_request(
            profile_name,
            {"max_tokens": 16, "messages": [{"role": "user", "content": f"Say {i}"}]},
            auth_value=auth_value,
        )
        lat = time.time() - start
        if r["ok"]:
            latencies.append(lat)
        else:
            errors += 1
    return latencies, errors

# ============================================================================
# OUTPUT HELPERS
# ============================================================================

def fmt_result(r, profile_width=10):
    if not r["ok"]:
        return f"{C_RED}ERROR: {r['error'][:100]}{C_RESET}"
    cache = ""
    if r["cache_creation"] > 0:
        cache += f" cw={r['cache_creation']:,}"
    if r["cache_read"] > 0:
        cache += f" cr={r['cache_read']:,}"
    return (
        f"{C_GREEN}in={r['input']:>5,} out={r['output']:>4,}{cache} "
        f"({r['elapsed']:.1f}s){C_RESET} [{C_CYAN}{r['model']}{C_RESET}]"
    )

def fmt_concurrent(oks, errs, wall):
    if oks:
        avg_in = sum(r["input"] for r in oks) / len(oks)
        avg_out = sum(r["output"] for r in oks) / len(oks)
        max_t = max(r["elapsed"] for r in oks)
        return f"{C_GREEN}{len(oks)} OK{C_RESET}, wall={wall:.1f}s, max={max_t:.1f}s, avg_in={avg_in:.0f}"
    return f"{C_RED}{len(errs)} errors{C_RESET}"

# ============================================================================
# MAIN
# ============================================================================

def parse_args():
    import argparse
    p = argparse.ArgumentParser(description="Comprehensive profile test")
    p.add_argument("--profile", "-p", help="Run only this profile (cc, kimi, zai-test)", default=None)
    p.add_argument("--gateway", "-g", help="Gateway URL", default=None)
    return p.parse_args()

def main():
    args = parse_args()
    if args.gateway:
        global GW
        GW = args.gateway
    target_profiles = {k: v for k, v in PROFILES.items() if args.profile is None or k == args.profile}
    if args.profile and not target_profiles:
        print(f"{C_RED}Unknown profile: {args.profile}. Available: {', '.join(PROFILES.keys())}{C_RESET}")
        sys.exit(1)
    total_start = time.time()
    run_profiles = target_profiles if args.profile else PROFILES
    print(f"\n{C_BOLD}{'='*110}{C_RESET}")
    print(f"{C_BOLD} COMPREHENSIVE PROFILE TEST: {' | '.join(run_profiles.keys())} ({len(TESTS)} tests + concurrent){C_RESET}")
    print(f"{C_BOLD}{'='*110}{C_RESET}")

    all_results = {}
    all_errors = {}

    for pkey, pcfg in run_profiles.items():
        print(f"\n{C_BOLD}{C_MAGENTA}{'='*110}{C_RESET}")
        print(f"{C_BOLD}{C_MAGENTA} PROFILE: {pcfg['name']} ({pcfg['description']}){C_RESET}")
        print(f"{C_BOLD}{C_MAGENTA} Expected optimizer: {pcfg['expect_optimizer']}{C_RESET}")
        print(f"{C_BOLD}{C_MAGENTA}{'='*110}{C_RESET}")

        profile_results = []
        profile_errors = []
        auth_val = pcfg.get("auth_value")
        current_cat = ""

        for test in TESTS:
            cat = test.get("category", "misc")
            if cat != current_cat:
                current_cat = cat
                cat_labels = {
                    "system": "SYSTEM PROMPT TESTS",
                    "tools": "TOOL MANIFEST TESTS",
                    "conversation": "MULTI-TURN CONVERSATION TESTS",
                    "language": "LANGUAGE / CONTENT TESTS",
                    "basic": "BASIC / EDGE CASE TESTS",
                }
                print(f"\n  {C_BOLD}{C_YELLOW}--- {cat_labels.get(cat, cat.upper())} ---{C_RESET}")

            tname = test["name"]
            print(f"  [{pcfg['name']:10s}] {C_DIM}{tname}{C_RESET} ... ", end="", flush=True)

            payload = test.get("payload")
            if "payload_fn" in test:
                payload = test["payload_fn"]()

            is_stream = test.get("is_stream", False)
            if is_stream:
                r = send_streaming_request(pcfg["name"], payload, auth_value=auth_val)
            else:
                r = send_request(pcfg["name"], payload, auth_value=auth_val)

            print(fmt_result(r))

            if r["ok"]:
                cache_note = ""
                if r.get("cache_read", 0) > 0:
                    cache_note = f" cache_read={r['cache_read']:,}"
                print(f"{'':14s} stop={r['stop_reason']} resp={r['content'][:80]}")
            else:
                profile_errors.append(tname)
                print(f"{'':14s} {C_RED}{r.get('error', 'unknown')[:100]}{C_RESET}")

            profile_results.append({"test": tname, "category": cat, **r})
            time.sleep(0.3)

        # --- Concurrent SSE 3x ---
        print(f"\n  {C_BOLD}{C_YELLOW}--- CONCURRENCY TESTS ---{C_RESET}")
        print(f"  [{pcfg['name']:10s}] Concurrent SSE 3x ... ", end="", flush=True)
        conc3, wall3 = run_concurrent_test(pcfg["name"], count=3, auth_value=auth_val)
        oks3 = [r for r in conc3 if r.get("ok")]
        errs3 = [r for r in conc3 if not r.get("ok")]
        print(fmt_concurrent(oks3, errs3, wall3))
        profile_results.append({"test": "Concurrent SSE 3x", "category": "concurrent",
                                "ok": len(errs3) == 0, "input": sum(r.get("input", 0) for r in oks3) // max(len(oks3), 1),
                                "output": sum(r.get("output", 0) for r in oks3) // max(len(oks3), 1),
                                "elapsed": wall3, "model": "", "content": "", "stop_reason": ""})

        # --- Concurrent SSE 5x ---
        print(f"  [{pcfg['name']:10s}] Concurrent SSE 5x ... ", end="", flush=True)
        conc5, wall5 = run_concurrent_test(pcfg["name"], count=5, auth_value=auth_val)
        oks5 = [r for r in conc5 if r.get("ok")]
        errs5 = [r for r in conc5 if not r.get("ok")]
        print(fmt_concurrent(oks5, errs5, wall5))
        profile_results.append({"test": "Concurrent SSE 5x", "category": "concurrent",
                                "ok": len(errs5) == 0, "input": sum(r.get("input", 0) for r in oks5) // max(len(oks5), 1),
                                "output": sum(r.get("output", 0) for r in oks5) // max(len(oks5), 1),
                                "elapsed": wall5, "model": "", "content": "", "stop_reason": ""})

        # --- Burst 10x ---
        print(f"  [{pcfg['name']:10s}] Burst 10x sequential ... ", end="", flush=True)
        lats, burst_errs = run_burst_test(pcfg["name"], count=10, auth_value=auth_val)
        if lats:
            avg_lat = sum(lats) / len(lats)
            p50 = sorted(lats)[len(lats)//2]
            p99 = sorted(lats)[int(len(lats)*0.99)]
            total_burst = sum(lats)
            print(f"{C_GREEN}{len(lats)} OK{C_RESET}, wall={total_burst:.1f}s, avg={avg_lat:.1f}s, p50={p50:.1f}s, p99={p99:.1f}s, err={burst_errs}")
        else:
            print(f"{C_RED}All 10 failed{C_RESET}")
        profile_results.append({"test": "Burst 10x", "category": "concurrent",
                                "ok": burst_errs == 0, "input": 0, "output": 0,
                                "elapsed": sum(lats) if lats else 0, "model": "", "content": "", "stop_reason": ""})

        all_results[pkey] = profile_results
        all_errors[pkey] = profile_errors

    # ========================================================================
    # COMPARISON TABLES
    # ========================================================================
    total_elapsed = time.time() - total_start

    print(f"\n\n{C_BOLD}{'='*110}{C_RESET}")
    print(f"{C_BOLD} COMPARISON TABLE: Input Tokens Per Test Per Profile{C_RESET}")
    print(f"{C_BOLD}{'='*110}{C_RESET}\n")

    # Build test names excluding concurrent
    test_names = [t["name"] for t in TESTS]
    pw = 10
    col = 11

    header = f"| {'Test':<40s} |"
    for pkey in run_profiles:
        header += f" {pkey:>10s} |"
    print(header)
    sep = f"|{'-'*42}|"
    for _ in PROFILES:
        sep += f"{'-'*12}|"
    print(sep)

    for i, tname in enumerate(test_names):
        row = f"| {tname:<40s} |"
        for pkey in run_profiles:
            r = all_results[pkey][i]
            if r.get("ok"):
                cr = r.get("cache_read", 0)
                if cr > 0:
                    row += f" {'>'+format(cr):>10s} |"
                else:
                    row += f" {r['input']:>10,} |"
            else:
                row += f" {'ERR':>10s} |"
        print(row)

    # ========================================================================
    # CATEGORY SUMMARY
    # ========================================================================
    print(f"\n\n{C_BOLD}{'='*110}{C_RESET}")
    print(f"{C_BOLD} CATEGORY SUMMARY{C_RESET}")
    print(f"{C_BOLD}{'='*110}{C_RESET}\n")

    categories = {}
    for pkey in run_profiles:
        for r in all_results[pkey]:
            cat = r.get("category", "misc")
            if cat not in categories:
                categories[cat] = {}
            if cat not in categories:
                categories[cat] = {}
            if pkey not in categories[cat]:
                categories[cat][pkey] = {"ok": 0, "err": 0, "total_in": 0, "total_out": 0, "total_time": 0}
            if r.get("ok"):
                categories[cat][pkey]["ok"] += 1
                categories[cat][pkey]["total_in"] += r.get("input", 0)
                categories[cat][pkey]["total_out"] += r.get("output", 0)
                categories[cat][pkey]["total_time"] += r.get("elapsed", 0)
            else:
                categories[cat][pkey]["err"] += 1

    header = f"| {'Category':<20s} |"
    for pkey in run_profiles:
        header += f" {pkey + ' pass':>10s} | {pkey + ' in':>10s} | {pkey + ' time':>10s} |"
    print(header)
    sep = f"|{'-'*22}|"
    for _ in PROFILES:
        sep += f"{'-'*12}|{'-'*12}|{'-'*12}|"
    print(sep)

    for cat in ["system", "tools", "conversation", "language", "basic", "concurrent"]:
        row = f"| {cat:<20s} |"
        for pkey in run_profiles:
            d = categories.get(cat, {}).get(pkey, {"ok": 0, "err": 0, "total_in": 0, "total_out": 0, "total_time": 0})
            total = d["ok"] + d["err"]
            pass_rate = f"{d['ok']}/{total}"
            row += f" {pass_rate:>10s} | {d['total_in']:>10,} | {d['total_time']:>9.1f}s |"
        print(row)

    # ========================================================================
    # OVERALL SUMMARY
    # ========================================================================
    print(f"\n\n{C_BOLD}{'='*110}{C_RESET}")
    print(f"{C_BOLD} OVERALL SUMMARY{C_RESET}")
    print(f"{C_BOLD}{'='*110}{C_RESET}\n")

    print(f"| {'Profile':<10s} | {'Provider':<14s} | {'Optimizer':<30s} | {'Pass':>6s} | {'Fail':>6s} | {'Total In':>10s} | {'Total Out':>10s} | {'Total Time':>10s} |")
    print(f"|{'-'*12}|{'-'*16}|{'-'*32}|{'-'*8}|{'-'*8}|{'-'*12}|{'-'*12}|{'-'*12}|")

    for pkey, pcfg in run_profiles.items():
        oks = [r for r in all_results[pkey] if r.get("ok")]
        errs_list = [r for r in all_results[pkey] if not r.get("ok")]
        total_in = sum(r.get("input", 0) for r in oks)
        total_out = sum(r.get("output", 0) for r in oks)
        total_time = sum(r.get("elapsed", 0) for r in all_results[pkey])
        print(f"| {pcfg['name']:<10s} | {pcfg['provider']:<14s} | {pcfg['expect_optimizer']:<30s} | {C_GREEN}{len(oks):>5}{C_RESET} | {C_RED if errs_list else ''}{len(errs_list):>5}{C_RESET} | {total_in:>10,} | {total_out:>10,} | {total_time:>9.1f}s |")

    print(f"\n  Total test time: {total_elapsed:.1f}s")
    print(f"  Tests per profile: {len(TESTS)} + 3 concurrent = {len(TESTS)+3}")
    print(f"  Total API calls: {(len(TESTS)+3) * len(run_profiles)}")

    # ========================================================================
    # ERROR SUMMARY
    # ========================================================================
    has_errors = any(all_errors.values())
    if has_errors:
        print(f"\n\n{C_BOLD}{C_RED} ERROR DETAILS{C_RESET}")
        print(f"{C_BOLD}{C_RED}{'='*110}{C_RESET}\n")
        for pkey in run_profiles:
            if all_errors[pkey]:
                print(f"  {C_RED}{pkey}: {', '.join(all_errors[pkey])}{C_RESET}")

    # ========================================================================
    # GATEWAY LOG HINT
    # ========================================================================
    print(f"\n\n{C_BOLD}{'='*110}{C_RESET}")
    print(f"{C_BOLD} GATEWAY LOG ANALYSIS{C_RESET}")
    print(f"{C_BOLD}{'='*110}{C_RESET}\n")
    print("  Run: docker logs arl-gateway 2>&1 | grep -E 'optimizer_step|zai_provider_skip' | tail -50")
    print("  Check: cc should have ZERO optimizer_step, zai-test should only show zai_provider_skip")


if __name__ == "__main__":
    main()
