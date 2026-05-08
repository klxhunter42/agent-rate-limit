#!/usr/bin/env python3
"""Comprehensive optimizer load test - all 17 stages"""
import json, subprocess, time, urllib.request, urllib.error

GW = "http://localhost:8080"
API_KEY = os.environ.get("TEST_API_KEY", "")
METRICS = "http://localhost:8080/metrics"

def send(name, body):
    data = json.dumps(body).encode()
    req = urllib.request.Request(
        f"{GW}/v1/messages",
        data=data,
        headers={
            "Content-Type": "application/json",
            "x-api-key": API_KEY,
            "anthropic-version": "2023-06-01",
        },
        method="POST",
    )
    try:
        resp = urllib.request.urlopen(req, timeout=15)
        code = resp.getcode()
    except urllib.error.HTTPError as e:
        code = e.code
    except Exception as e:
        code = -1
    print(f"  {name}: HTTP {code}")

def reset_metrics():
    subprocess.run(["redis-cli", "FLUSHDB"], capture_output=True)
    print("Metrics reset via Redis FLUSHDB")

def dump_metrics():
    print("\n=== Per-Technique Optimization Results ===")
    try:
        resp = urllib.request.urlopen(METRICS, timeout=5)
        text = resp.read().decode()
        for line in text.split("\n"):
            if "api_gateway_optimizer_chars_saved_total" in line:
                print(f"  {line}")
        print()
        for line in text.split("\n"):
            if "api_gateway_tokens_saved_total" in line:
                print(f"  {line}")
        print()
        for line in text.split("\n"):
            if "api_gateway_cost_savings_total" in line:
                print(f"  {line}")
        print()
        for line in text.split("\n"):
            if "api_gateway_optimizer_duration_seconds" in line and "_sum" in line:
                print(f"  {line}")
    except Exception as e:
        print(f"  Error: {e}")

# ============================================================
reset_metrics()

# Test 1: TextComp + SemanticDedup (verbose system prompt)
send("TextComp+SemDedup (verbose sys)", {
    "model": "glm-4.7-flashx",
    "max_tokens": 100,
    "system": "You are a helpful assistant. It is important to note that you should really try your best to be as helpful as possible. In fact, I would say that it is quite essential that you do your utmost to provide the most helpful responses you can. Furthermore, it should be noted that you are to be very thorough in your analysis. Additionally, please keep in mind that accuracy is paramount. It goes without saying that you should always be accurate. Needless to say, you must be precise and correct in all your responses. In this day and age, it is absolutely critical to maintain the highest standards of quality. At the end of the day, your goal is to help users effectively. Basically, you need to understand what the user is asking and provide relevant information. As a matter of fact, the user is counting on you to deliver valuable insights. For all intents and purposes, you are the expert here. Last but not least, remember to be concise while being comprehensive. Each and every response should be well-structured. When all is said and done, quality matters most. It is worth mentioning that consistency is key. Not to mention, reliability cannot be overstated. In the final analysis, your performance speaks for itself.",
    "messages": [{"role": "user", "content": "Hello"}],
})

# Test 2: SemanticDedup + Chunker (repeated system prompt)
send("SemDedup+Chunker (repeated sys)", {
    "model": "glm-4.7-flashx",
    "max_tokens": 100,
    "system": "Project: Agent Rate Limiter. Tech stack: Go, Redis, Kubernetes. Architecture: API Gateway with reverse proxy. The project uses Go for the backend, Redis for caching and rate limiting, and Kubernetes for deployment. Architecture consists of an API Gateway that acts as a reverse proxy. Project details: Agent Rate Limiter is built with Go, uses Redis for state, and deploys on Kubernetes. The architecture is based on an API Gateway reverse proxy pattern. Tech stack includes Go programming language, Redis database, and Kubernetes orchestration. Agent Rate Limiter - a Go-based API Gateway with Redis backend, deployed on Kubernetes, implementing reverse proxy architecture for AI model access. The system provides rate limiting, token optimization, and multi-provider support. Key components: proxy handler, rate limiter, optimizer pipeline, metrics collection, and privacy guard. All services communicate via Redis and expose Prometheus metrics.",
    "messages": [{"role": "user", "content": "What is this project?"}],
})

# Test 3: ToolComp shell ls
send("ToolComp ShellLs", {
    "model": "glm-4.7-flashx",
    "max_tokens": 100,
    "system": "You are helpful.",
    "messages": [
        {"role": "user", "content": "List the files"},
        {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_1", "name": "Bash", "input": {"command": "ls -la"}}]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "tu_1", "content": "total 128\ndrwxr-xr-x 25 hunter staff 800 May 6 04:30 .\ndrwxr-xr-x 15 hunter staff 480 May 5 04:20 ..\n-rw-r--r-- 1 hunter staff 6148 May 5 10:00 .DS_Store\ndrwxr-xr-x 13 hunter staff 416 May 5 12:00 .git\ndrwxr-xr-x 4 hunter staff 128 May 5 10:00 .github\n-rw-r--r-- 1 hunter staff 250 May 5 10:00 .golangci.yml\n-rw-r--r-- 1 hunter staff 120 May 5 10:00 .env.example\ndrwxr-xr-x 10 hunter staff 320 May 5 10:00 api-gateway\ndrwxr-xr-x 8 hunter staff 256 May 5 10:00 cmd\ndrwxr-xr-x 3 hunter staff 96 May 5 10:00 config\ndrwxr-xr-x 5 hunter staff 160 May 5 10:00 docs\ndrwxr-xr-x 4 hunter staff 128 May 5 10:00 helm\n-rw-r--r-- 1 hunter staff 1100 May 5 10:00 go.mod\n-rw-r--r-- 1 hunter staff 800 May 5 10:00 go.sum\n-rw-r--r-- 1 hunter staff 300 May 5 10:00 Makefile\n-rw-r--r-- 1 hunter staff 200 May 5 10:00 Dockerfile\ndrwxr-xr-x 6 hunter staff 192 May 5 10:00 internal\ndrwxr-xr-x 3 hunter staff 96 May 5 10:00 pkg\n-rw-r--r-- 1 hunter staff 500 May 5 10:00 README.md\n-rw-r--r-- 1 hunter staff 400 May 5 10:00 docker-compose.yml\n-rw-r--r-- 1 hunter staff 300 May 5 10:00 .dockerignore"}
        ]},
    ],
})

# Test 4: ToolComp JSON
send("ToolComp JSON", {
    "model": "glm-4.7-flashx",
    "max_tokens": 100,
    "system": "You are helpful.",
    "messages": [
        {"role": "user", "content": "Get the config"},
        {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_2", "name": "Read", "input": {"file_path": "config.json"}}]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "tu_2", "content": '{\n  "server": {\n    "host": "0.0.0.0",\n    "port": 8080,\n    "timeout": 30000,\n    "readTimeout": 10000,\n    "writeTimeout": 10000\n  },\n  "redis": {\n    "host": "localhost",\n    "port": 6379,\n    "db": 0,\n    "poolSize": 100,\n    "minIdleConns": 10,\n    "maxRetries": 3,\n    "dialTimeout": 5000\n  },\n  "upstream": {\n    "url": "https://api.anthropic.com",\n    "timeout": 120000,\n    "maxRetries": 2,\n    "retryDelay": 1000\n  },\n  "rateLimit": {\n    "global": 1000,\n    "perAgent": 50,\n    "window": 60,\n    "strategy": "sliding_window"\n  },\n  "optimizer": {\n    "enabled": true,\n    "level": "aggressive",\n    "techniques": ["dedup", "compress", "summarize", "filter"]\n  },\n  "privacy": {\n    "enabled": true,\n    "maskEmails": true,\n    "maskPhones": true,\n    "maskCreditCards": true\n  }\n}'}
        ]},
    ],
})

# Test 5: ToolComp log dedup
log_lines = []
for i in range(50):
    log_lines.append(f"2026-05-06 04:0{i%60:02d}:{(i*5)%60:02d} INFO Health check passed")
log_lines.append("2026-05-06 04:01:05 WARN Rate limit approaching for agent-123")
log_lines.append("2026-05-06 04:02:00 ERROR Connection timeout to upstream")
log_content = "\n".join(log_lines)
# Add repeated lines
log_content += "\n" + "\n".join(["2026-05-06 04:00:02 INFO Connected to Redis at localhost:6379"] * 10)

send("ToolComp LogDedup", {
    "model": "glm-4.7-flashx",
    "max_tokens": 100,
    "system": "You are helpful.",
    "messages": [
        {"role": "user", "content": "Check the logs"},
        {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_3", "name": "Bash", "input": {"command": "tail -50 app.log"}}]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "tu_3", "content": log_content}
        ]},
    ],
})

# Test 6: ToolComp diff
send("ToolComp Diff", {
    "model": "glm-4.7-flashx",
    "max_tokens": 100,
    "system": "You are helpful.",
    "messages": [
        {"role": "user", "content": "Show the diff"},
        {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_4", "name": "Bash", "input": {"command": "git diff"}}]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "tu_4", "content": "diff --git a/handler.go b/handler.go\nindex abc1234..def5678 100644\n--- a/handler.go\n+++ b/handler.go\n@@ -10,8 +10,6 @@ import (\n- \"old/import1\"\n- \"old/import2\"\n )\n@@ -25,7 +23,8 @@ func main() {\n- port := 8080\n- log.Printf(\"Starting on %d\", port)\n+ port := os.Getenv(\"PORT\")\n+ if port == \"\" {\n+ port = \"8080\"\n+ }\n+ log.Printf(\"Starting on %s\", port)\n }\ndiff --git a/config.go b/config.go\nindex 111..222 100644\n--- a/config.go\n+++ b/config.go\n@@ -5,3 +5,4 @@ type Config struct {\n+ Verbose bool\n }\n"}
        ]},
    ],
})

# Test 7: Whitespace + sentence dedup in message text
send("WhitespaceOpt+SentDedup", {
    "model": "glm-4.7-flashx",
    "max_tokens": 100,
    "system": "You are helpful.",
    "messages": [
        {"role": "user", "content": "I need help with my code\n\n\n The function is not working properly\n\n\n\n It keeps returning nil instead of the expected values\n\n\n\n The function is not working properly\nIt keeps returning nil instead of the expected value\nI have tried everything I can think of but nothing works\nPlease help me debug this issue\nThe function is not working properly."}
    ],
})

# Test 8: Block text optimization
send("BlockTextOpt", {
    "model": "glm-4.7-flashx",
    "max_tokens": 100,
    "system": "You are helpful.",
    "messages": [
        {"role": "user", "content": [
            {"type": "text", "text": "This is a text block with lots of extra whitespace that should be optimized by the whitespace optimizer to reduce token count significantly."},
            {"type": "text", "text": "Another text block here with similar whitespace issues that should also be compressed and deduplicated where possible to save tokens."}
        ]},
    ],
})

# Test 9: ToolComp table
send("ToolComp Table", {
    "model": "glm-4.7-flashx",
    "max_tokens": 100,
    "system": "You are helpful.",
    "messages": [
        {"role": "user", "content": "Show the status"},
        {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_5", "name": "Bash", "input": {"command": "kubectl get pods"}}]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "tu_5", "content": "NAME READY STATUS RESTARTS AGE\napi-gateway-6d8f9b7c4-x2k9m 1/1 Running 0 2d\napi-gateway-6d8f9b7c4-p4j2n 1/1 Running 0 2d\napi-gateway-6d8f9b7c4-r8w3q 1/1 Running 0 2d\nredis-master-0 1/1 Running 0 5d\nredis-slave-0 1/1 Running 0 5d\nredis-slave-1 1/1 Running 0 5d\nprometheus-7b5c6d4e8f-k9l2p 1/1 Running 0 3d\ngrafana-84f7d6c9b5-m3n4o 1/1 Running 0 3d\n----------------------------------------------------------\nTotal: 8 pods, 8 Running, 0 Pending, 0 Failed"}
        ]},
    ],
})

# Test 10: ToolFilter (>15 tools)
tools = [
    {"name": n, "description": d}
    for n, d in [
        ("Read", "Read files from the filesystem"),
        ("Edit", "Edit existing files"),
        ("Write", "Write new files"),
        ("Bash", "Execute shell commands"),
        ("Glob", "Find files matching patterns"),
        ("Grep", "Search for patterns in files"),
        ("WebFetch", "Fetch content from URLs"),
        ("WebSearch", "Search the web"),
        ("NotebookEdit", "Edit Jupyter notebook cells"),
        ("TodoWrite", "Manage todo list"),
        ("Agent", "Spawn sub-agents"),
        ("Plan", "Create implementation plans"),
        ("EnterPlanMode", "Enter plan mode"),
        ("ExitPlanMode", "Exit plan mode"),
        ("AskUserQuestion", "Ask user questions"),
        ("CronCreate", "Schedule cron tasks"),
        ("CronDelete", "Delete cron tasks"),
        ("ScheduleWakeup", "Schedule wake-up"),
        ("EnterWorktree", "Enter git worktree"),
        ("ExitWorktree", "Exit git worktree"),
        ("mcp__docker_exec", "Execute in Docker container"),
        ("mcp__docker_logs", "Get Docker container logs"),
        ("mcp__docker_ps", "List Docker containers"),
        ("mcp__docker_build", "Build Docker image"),
        ("mcp__docker_prune", "Prune Docker resources"),
    ]
]

send("ToolFilter (25 tools, user asks about files)", {
    "model": "glm-4.7-flashx",
    "max_tokens": 100,
    "system": "You are helpful.",
    "messages": [{"role": "user", "content": "Please read the configuration file and show me the database settings"}],
    "tools": tools,
})

# Test 11: RED BUDGET - Summarizer (system prompt > 8K context window = 8192 tokens)
big_system = "You are a helpful assistant with expertise in software engineering, system design, and DevOps practices. " * 300  # ~30K chars
send("RED BUDGET: Summarizer", {
    "model": "glm-4.7-flashx",
    "max_tokens": 100,
    "system": big_system,
    "messages": [{"role": "user", "content": "Help me debug my code"}],
})

# Test 12: RED BUDGET + ToolComp + ToolFilter combined
send("RED: Summarizer+ToolComp+ToolFilter", {
    "model": "glm-4.7-flashx",
    "max_tokens": 100,
    "system": big_system,
    "messages": [
        {"role": "user", "content": "Check the server logs"},
        {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_6", "name": "Bash", "input": {"command": "tail -100 /var/log/app.log"}}]},
        {"role": "user", "content": [
            {"type": "tool_result", "tool_use_id": "tu_6", "content": log_content + "\n" + log_content}
        ]},
    ],
    "tools": tools,
})

print("\n=== Waiting for metrics to settle ===")
time.sleep(2)

dump_metrics()
print("\n=== Done ===")
