#!/bin/bash
# Optimizer Load Test - exercises each optimizer stage
# Sends varied requests and captures per-technique savings from Prometheus metrics

GATEWAY="http://localhost:8080"
API_KEY="${TEST_API_KEY:?set TEST_API_KEY}"
METRICS_URL="http://localhost:9090/metrics"

echo "=== Optimizer Load Test ==="
echo "Resetting metrics counters..."
redis-cli FLUSHDB 2>/dev/null

# Capture baseline
BASELINE=$(curl -s "$METRICS_URL" 2>/dev/null | grep 'api_gateway_optimizer_chars_saved_total' | head -5)
echo "Baseline metrics: $(echo "$BASELINE" | wc -l) entries"

send_request() {
    local name="$1"
    local body="$2"
    echo ""
    echo "--- $name ---"
    # Send and capture response (will fail at proxy since upstream may not accept, but optimizers run before proxy)
    RESP=$(curl -s -w "\n%{http_code}" -X POST "$GATEWAY/v1/messages" \
        -H "Content-Type: application/json" \
        -H "x-api-key: $API_KEY" \
        -H "anthropic-version: 2023-06-01" \
        -d "$body" 2>&1)
    HTTP_CODE=$(echo "$RESP" | tail -1)
    echo "HTTP: $HTTP_CODE"
}

# ============================================================
# Test 1: System prompt with verbose filler (TextComp + semantic_dedup)
# ============================================================
send_request "TextComp + SemanticDedup (verbose system prompt)" '{
  "model": "glm-4.7-flashx",
  "max_tokens": 100,
  "system": "You are a helpful assistant. It is important to note that you should really try your best to be as helpful as possible. In fact, I would say that it is quite essential that you do your utmost to provide the most helpful responses you can. Furthermore, it should be noted that you are to be very thorough in your analysis. Additionally, please keep in mind that accuracy is paramount. It goes without saying that you should always be accurate. Needless to say, you must be precise and correct in all your responses. In this day and age, it is absolutely critical to maintain the highest standards of quality. At the end of the day, your goal is to help users effectively. Basically, you need to understand what the user is asking and provide relevant information. As a matter of fact, the user is counting on you to deliver valuable insights. For all intents and purposes, you are the expert here. Last but not least, remember to be concise while being comprehensive. Each and every response should be well-structured. When all is said and done, quality matters most.",
  "messages": [{"role": "user", "content": "Hello"}]
}'

# Test 2: Large system prompt with repeated content (semantic_dedup + chunker)
send_request "SemanticDedup + Chunker (repeated system prompt)" '{
  "model": "glm-4.7-flashx",
  "max_tokens": 100,
  "system": "Project: Agent Rate Limiter. Tech stack: Go, Redis, Kubernetes. Architecture: API Gateway with reverse proxy. The project uses Go for the backend, Redis for caching and rate limiting, and Kubernetes for deployment. Architecture consists of an API Gateway that acts as a reverse proxy. Project details: Agent Rate Limiter is built with Go, uses Redis for state, and deploys on Kubernetes. The architecture is based on an API Gateway reverse proxy pattern. Tech stack includes Go programming language, Redis database, and Kubernetes orchestration. Agent Rate Limiter - a Go-based API Gateway with Redis backend, deployed on Kubernetes, implementing reverse proxy architecture for AI model access. The system provides rate limiting, token optimization, and multi-provider support. Key components: proxy handler, rate limiter, optimizer pipeline, metrics collection, and privacy guard. All services communicate via Redis and expose Prometheus metrics.",
  "messages": [{"role": "user", "content": "What is this project?"}]
}'

# Test 3: tool_result with shell output (ToolComp shell ls format)
send_request "ToolComp ShellLs (tool_result with file listing)" '{
  "model": "glm-4.7-flashx",
  "max_tokens": 100,
  "system": "You are helpful.",
  "messages": [
    {"role": "user", "content": "List the files"},
    {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_1", "name": "Bash", "input": {"command": "ls -la"}}]},
    {"role": "user", "content": [
      {"type": "tool_result", "tool_use_id": "tu_1", "content": "total 128\ndrwxr-xr-x  25 hunter  staff    800 May  6 04:30 .\ndrwxr-xr-x  15 hunter  staff    480 May  6 04:20 ..\n-rw-r--r--   1 hunter  staff   6148 May  5 10:00 .DS_Store\ndrwxr-xr-x  13 hunter  staff    416 May  5 12:00 .git\ndrwxr-xr-x   4 hunter  staff    128 May  5 10:00 .github\n-rw-r--r--   1 hunter  staff    250 May  5 10:00 .golangci.yml\n-rw-r--r--   1 hunter  staff    120 May  5 10:00 .env.example\ndrwxr-xr-x  10 hunter  staff    320 May  5 10:00 api-gateway\ndrwxr-xr-x   8 hunter  staff    256 May  5 10:00 cmd\ndrwxr-xr-x   3 hunter  staff     96 May  5 10:00 config\ndrwxr-xr-x   5 hunter  staff    160 May  5 10:00 docs\ndrwxr-xr-x   4 hunter  staff    128 May  5 10:00 helm\n-rw-r--r--   1 hunter  staff   1100 May  5 10:00 go.mod\n-rw-r--r--   1 hunter  staff    800 May  5 10:00 go.sum\n-rw-r--r--   1 hunter  staff    300 May  5 10:00 Makefile\n-rw-r--r--   1 hunter  staff    200 May  5 10:00 Dockerfile\ndrwxr-xr-x   6 hunter  staff    192 May  5 10:00 internal\ndrwxr-xr-x   3 hunter  staff     96 May  5 10:00 pkg\n-rw-r--r--   1 hunter  staff    500 May  5 10:00 README.md\n-rw-r--r--   1 hunter  staff    400 May  5 10:00 docker-compose.yml\n-rw-r--r--   1 hunter  staff    300 May  5 10:00 .dockerignore"}
    ]}
  ]
}'

# Test 4: tool_result with JSON (ToolComp JSON compact)
send_request "ToolComp JSON (tool_result with JSON response)" '{
  "model": "glm-4.7-flashx",
  "max_tokens": 100,
  "system": "You are helpful.",
  "messages": [
    {"role": "user", "content": "Get the config"},
    {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_2", "name": "Read", "input": {"file_path": "config.json"}}]},
    {"role": "user", "content": [
      {"type": "tool_result", "tool_use_id": "tu_2", "content": "{\n  \"server\": {\n    \"host\": \"0.0.0.0\",\n    \"port\": 8080,\n    \"timeout\": 30000,\n    \"readTimeout\": 10000,\n    \"writeTimeout\": 10000\n  },\n  \"redis\": {\n    \"host\": \"localhost\",\n    \"port\": 6379,\n    \"db\": 0,\n    \"poolSize\": 100,\n    \"minIdleConns\": 10,\n    \"maxRetries\": 3,\n    \"dialTimeout\": 5000\n  },\n  \"upstream\": {\n    \"url\": \"https://api.anthropic.com\",\n    \"timeout\": 120000,\n    \"maxRetries\": 2,\n    \"retryDelay\": 1000\n  },\n  \"rateLimit\": {\n    \"global\": 1000,\n    \"perAgent\": 50,\n    \"window\": 60,\n    \"strategy\": \"sliding_window\"\n  },\n  \"optimizer\": {\n    \"enabled\": true,\n    \"level\": \"aggressive\",\n    \"techniques\": [\"dedup\", \"compress\", \"summarize\", \"filter\"]\n  },\n  \"privacy\": {\n    \"enabled\": true,\n    \"maskEmails\": true,\n    \"maskPhones\": true,\n    \"maskCreditCards\": true\n  }\n}"}
    ]}
  ]
}'

# Test 5: tool_result with log output (ToolComp log dedup)
send_request "ToolComp Log (tool_result with repeated log lines)" '{
  "model": "glm-4.7-flashx",
  "max_tokens": 100,
  "system": "You are helpful.",
  "messages": [
    {"role": "user", "content": "Check the logs"},
    {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_3", "name": "Bash", "input": {"command": "tail -50 app.log"}}]},
    {"role": "user", "content": [
      {"type": "tool_result", "tool_use_id": "tu_3", "content": "2026-05-06 04:00:01 INFO Server started on :8080\n2026-05-06 04:00:02 INFO Connected to Redis at localhost:6379\n2026-05-06 04:00:02 INFO Connected to Redis at localhost:6379\n2026-05-06 04:00:02 INFO Connected to Redis at localhost:6379\n2026-05-06 04:00:03 INFO Metrics endpoint started on :9090\n2026-05-06 04:00:03 INFO Metrics endpoint started on :9090\n2026-05-06 04:00:05 INFO Health check passed\n2026-05-06 04:00:10 INFO Health check passed\n2026-05-06 04:00:15 INFO Health check passed\n2026-05-06 04:00:20 INFO Health check passed\n2026-05-06 04:00:25 INFO Health check passed\n2026-05-06 04:00:30 INFO Health check passed\n2026-05-06 04:00:35 INFO Health check passed\n2026-05-06 04:00:40 INFO Health check passed\n2026-05-06 04:00:45 INFO Health check passed\n2026-05-06 04:00:50 INFO Health check passed\n2026-05-06 04:00:55 INFO Health check passed\n2026-05-06 04:01:00 INFO Health check passed\n2026-05-06 04:01:05 WARN Rate limit approaching for agent-123\n2026-05-06 04:01:10 INFO Health check passed\n2026-05-06 04:01:15 INFO Health check passed\n2026-05-06 04:01:20 INFO Request processed in 45ms\n2026-05-06 04:01:25 INFO Request processed in 32ms\n2026-05-06 04:01:30 INFO Request processed in 38ms\n2026-05-06 04:01:35 INFO Request processed in 41ms\n2026-05-06 04:01:40 INFO Request processed in 29ms\n2026-05-06 04:01:45 INFO Request processed in 35ms\n2026-05-06 04:01:50 INFO Cache hit for session abc\n2026-05-06 04:01:55 INFO Cache hit for session def\n2026-05-06 04:02:00 ERROR Connection timeout to upstream\n2026-05-06 04:02:05 INFO Retry successful\n2026-05-06 04:02:10 INFO Health check passed\n2026-05-06 04:02:15 INFO Health check passed\n2026-05-06 04:02:20 INFO Health check passed\n2026-05-06 04:02:25 INFO Health check passed\n2026-05-06 04:02:30 INFO Health check passed\n2026-05-06 04:02:35 INFO Health check passed\n2026-05-06 04:02:40 INFO Health check passed\n2026-05-06 04:02:45 INFO Health check passed\n2026-05-06 04:02:50 INFO Health check passed\n2026-05-06 04:02:55 INFO Server shutdown initiated"}
    ]}
  ]
}'

# Test 6: tool_result with diff output (ToolComp diff compression)
send_request "ToolComp Diff (tool_result with unified diff)" '{
  "model": "glm-4.7-flashx",
  "max_tokens": 100,
  "system": "You are helpful.",
  "messages": [
    {"role": "user", "content": "Show the diff"},
    {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_4", "name": "Bash", "input": {"command": "git diff"}}]},
    {"role": "user", "content": [
      {"type": "tool_result", "tool_use_id": "tu_4", "content": "diff --git a/handler.go b/handler.go\nindex abc1234..def5678 100644\n--- a/handler.go\n+++ b/handler.go\n@@ -10,8 +10,6 @@ import (\n-    \"old/import1\"\n-    \"old/import2\"\n )\n@@ -25,7 +23,8 @@ func main() {\n-    port := 8080\n-    log.Printf(\"Starting on %d\", port)\n+    port := os.Getenv(\"PORT\")\n+    if port == \"\" {\n+        port = \"8080\"\n+    }\n+    log.Printf(\"Starting on %s\", port)\n }\ndiff --git a/config.go b/config.go\nindex 111..222 100644\n--- a/config.go\n+++ b/config.go\n@@ -5,3 +5,4 @@ type Config struct {\n+    Verbose bool\n }\n\nNo newline at end of file"}
    ]}
  ]
}'

# Test 7: Long message with whitespace (message_text whitespace optimization)
send_request "WhitespaceOpt + DedupSentences (multi-space message)" '{
  "model": "glm-4.7-flashx",
  "max_tokens": 100,
  "system": "You are helpful.",
  "messages": [
    {"role": "user", "content": "I need    help    with     my    code.    \n\n\n   The function   is   not   working   properly.   \n\n\n\n   It   keeps   returning   nil   instead   of   the   expected   value.   \n\n\n\n   The function is not working properly.   It keeps returning nil instead of the expected value.   I have tried everything I can think of but nothing works.   Please help me debug this issue.   The function is not working properly."}
  ]
}'

# Test 8: Text blocks in content array (message_block_text optimization)
send_request "BlockTextOpt (content array with text blocks)" '{
  "model": "glm-4.7-flashx",
  "max_tokens": 100,
  "system": "You are helpful.",
  "messages": [
    {"role": "user", "content": [
      {"type": "text", "text": "This   is   a   text   block   with   lots   of   extra   whitespace   that   should   be   optimized   by   the   whitespace   optimizer   to   reduce   token   count   significantly."},
      {"type": "text", "text": "Another   text   block   here   with   similar   whitespace   issues   that   should   also   be   compressed   and   deduplicated   where   possible   to   save   tokens."}
    ]}
  ]
}'

# Test 9: Mixed content with tables (ToolComp table format)
send_request "ToolComp Table (tool_result with table output)" '{
  "model": "glm-4.7-flashx",
  "max_tokens": 100,
  "system": "You are helpful.",
  "messages": [
    {"role": "user", "content": "Show the status"},
    {"role": "assistant", "content": [{"type": "tool_use", "id": "tu_5", "name": "Bash", "input": {"command": "kubectl get pods"}}]},
    {"role": "user", "content": [
      {"type": "tool_result", "tool_use_id": "tu_5", "content": "NAME                          READY   STATUS    RESTARTS   AGE\napi-gateway-6d8f9b7c4-x2k9m   1/1     Running   0          2d\napi-gateway-6d8f9b7c4-p4j2n   1/1     Running   0          2d\napi-gateway-6d8f9b7c4-r8w3q   1/1     Running   0          2d\nredis-master-0                1/1     Running   0          5d\nredis-slave-0                 1/1     Running   0          5d\nredis-slave-1                 1/1     Running   0          5d\nprometheus-7b5c6d4e8f-k9l2p   1/1     Running   0          3d\ngrafana-84f7d6c9b5-m3n4o      1/1     Running   0          3d\n----------------------------------------------------------\nTotal: 8 pods, 8 Running, 0 Pending, 0 Failed"}
    ]}
  ]
}'

echo ""
echo "=== Waiting for metrics to settle ==="
sleep 2

echo ""
echo "=== Per-Technique Optimization Results ==="
echo ""

# Get metrics
curl -s "$METRICS_URL" 2>/dev/null | grep 'api_gateway_optimizer_chars_saved_total' | while read line; do
    echo "$line"
done

echo ""
echo "=== Token Savings ==="
curl -s "$METRICS_URL" 2>/dev/null | grep 'api_gateway_tokens_saved_total' | while read line; do
    echo "$line"
done

echo ""
echo "=== Cost Savings ==="
curl -s "$METRICS_URL" 2>/dev/null | grep 'api_gateway_cost_savings_total' | while read line; do
    echo "$line"
done

echo ""
echo "=== Duration per Technique ==="
curl -s "$METRICS_URL" 2>/dev/null | grep 'api_gateway_optimizer_duration_seconds' | grep '_sum' | while read line; do
    echo "$line"
done

echo ""
echo "=== Done ==="
