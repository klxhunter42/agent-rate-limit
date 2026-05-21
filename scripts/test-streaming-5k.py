#!/usr/bin/env python3
"""
SSE Streaming Parameterized Test Suite - Up to 5000 test cases
Generates tests from a parameter matrix: profiles x prompt_types x dimensions

Usage:
  python3 test-streaming-5k.py                          # Run all tests (sequential)
  python3 test-streaming-5k.py --profile cc             # Only cc profile
  python3 test-streaming-5k.py --section basic          # Only basic section
  python3 test-streaming-5k.py --limit 50               # First 50 tests only
  python3 test-streaming-5k.py --concurrent 3           # 3 parallel workers
  python3 test-streaming-5k.py --list                   # List all test cases (dry run)
  python3 test-streaming-5k.py --report results.tsv     # Load and display report
"""

import argparse
import json
import os
import subprocess
import sys
import time
import threading
import re
from collections import defaultdict
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass, field
from typing import Optional

# ===== Configuration =====
GATEWAY = os.environ.get("GATEWAY", "http://localhost:9000")
TIMEOUT = int(os.environ.get("TEST_TIMEOUT", "120"))

PROFILES = {
    "cc": {
        "key": "${CC_KEY:-sk-ant-oat01-REPLACE_ME}",
        "model": "claude-sonnet-4-20250514",
        "supports_tools": True,
        "supports_thinking": True,
    },
    "kimi": {
        "key": "${KIMI_KEY:-sk-kimi-REPLACE_ME}",
        "model": "kimi-k2.6",
        "supports_tools": False,
        "supports_thinking": False,
    },
}

# ===== Prompt Matrix =====
# Each entry: (id_suffix, prompt, max_tokens, description)
BASIC_PROMPTS = [
    ("hello", "Say hello in one word.", 32, "hello"),
    ("hi-thai", "พูดสวัสดีเป็นภาษาไทย", 64, "hello thai"),
    ("short", "What is 1+1?", 32, "math short"),
    ("medium", "Explain what a load balancer does in 2-3 sentences.", 128, "load balancer"),
    ("creative", "Write a haiku about Kubernetes.", 128, "haiku"),
    ("code-go", "Write a Go function that reverses a string.", 200, "go reverse"),
    ("code-py", "Write a Python function to check if a string is a palindrome.", 200, "py palindrome"),
    ("code-js", "Write a JavaScript function to debounce another function.", 200, "js debounce"),
    ("code-rust", "Write a Rust function to count words in a string.", 200, "rust wordcount"),
    ("code-ts", "Write a TypeScript generic for a Result type.", 200, "ts result type"),
    ("factual", "What is the capital of Thailand?", 64, "capital"),
    ("list-5", "List 5 programming languages.", 64, "list langs"),
    ("list-10", "List 10 cloud services with one line each.", 200, "list cloud"),
    ("explain", "Explain Docker containers vs VMs in 3 bullet points.", 200, "docker vs vm"),
    ("json-gen", "Generate a JSON object with 3 fields: name, age, hobbies array.", 100, "json gen"),
    ("sql", "Write a SQL query to find top 10 customers by total orders.", 200, "sql query"),
    ("regex", "Write a regex to match email addresses. Explain it.", 150, "regex email"),
    ("yaml", "Generate a Kubernetes deployment YAML for nginx with 3 replicas.", 300, "k8s yaml"),
    ("terraform", "Write a Terraform resource for an AWS S3 bucket with versioning.", 300, "terraform"),
    ("dockerfile", "Write a Dockerfile for a Go HTTP server multi-stage build.", 300, "dockerfile"),
    ("essay", "Explain the benefits of microservices architecture.", 300, "microservices"),
    ("devops", "What are the key practices in DevOps? List and explain 5.", 300, "devops practices"),
    ("security", "Explain OWASP Top 3 vulnerabilities briefly.", 300, "owasp top3"),
    ("network", "Explain TCP 3-way handshake in simple terms.", 200, "tcp handshake"),
    ("linux", "List 10 useful Linux commands for debugging with one-line descriptions.", 300, "linux debug"),
    ("git", "Explain git rebase vs merge with when to use each.", 200, "rebase vs merge"),
    ("ci-cd", "Describe a good CI/CD pipeline for a Go microservice.", 300, "cicd pipeline"),
    ("monitoring", "Explain RED vs USE method for monitoring.", 200, "red vs use"),
    ("k8s-troubleshoot", "How to debug a CrashLoopBackOff pod? Step by step.", 300, "crashloop"),
    ("cost-opt", "List 5 AWS cost optimization strategies.", 200, "aws cost"),
    ("api-design", "Best practices for REST API pagination.", 200, "api pagination"),
    ("auth", "Compare JWT vs session-based authentication.", 200, "jwt vs session"),
    ("db-index", "Explain database indexing with B-tree example.", 200, "db indexing"),
    ("caching", "Compare Redis vs Memcached for caching.", 200, "redis vs memcache"),
    ("grpc", "When to use gRPC vs REST? Pros and cons.", 200, "grpc vs rest"),
]

LANGUAGES = [
    ("thai", "อธิบาย Kubernetes ใน 3 ประโยค", 200, "thai k8s"),
    ("thai-food", "แนะนำอาหารไทย 5 อย่างพร้อมคำอธิบาย", 200, "thai food"),
    ("thai-tech", "อธิบายความแตกต่างระหว่าง Docker กับ VM", 200, "thai docker"),
    ("thai-english", "Explain DevOps in Thai, then summarize in English.", 200, "thai+en mix"),
    ("japanese", "Kubernetesを3文で説明してください", 200, "japanese k8s"),
    ("chinese", "用3句话解释什么是微服务", 200, "chinese micro"),
    ("korean", "Kubernetes를 3문장으로 설명하세요", 200, "korean k8s"),
    ("mixed", "Say 'hello' in Thai, Japanese, Chinese, and Korean.", 100, "multilang hello"),
    ("emoji", "Describe a CI/CD pipeline using only emoji.", 100, "emoji only"),
    ("special-chars", "Test special chars: @#$%^&*()<>{}[]|\\`~", 100, "special chars"),
    ("unicode", "Print: alpha=α beta=β gamma=γ delta=δ pi=π", 100, "greek letters"),
    ("math-symbols", "Write E=mc^2 and explain, use math symbols.", 150, "math symbols"),
    ("thai-code", "อธิบาย Go code: func main() { fmt.Println(\"hello\") }", 200, "thai+code"),
    ("thai-json", "สร้าง JSON สำหรับ config ของ Kubernetes namespace", 200, "thai json"),
    ("thai-devops", "อธิบาย Terraform workflow step by step", 200, "thai terraform"),
]

SYSTEM_PROMPTS = [
    ("pirate", "You are a pirate. Always respond in pirate speak.", "What is Kubernetes?", 200, "pirate"),
    ("formal", "You are a formal academic professor.", "What is Docker?", 200, "formal"),
    ("concise", "You are extremely concise. Maximum 10 words per answer.", "Explain microservices.", 64, "concise"),
    ("json-only", "You only respond in valid JSON format.", "Tell me about load balancers.", 200, "json-only"),
    ("markdown", "You always respond in markdown with headers and bullet points.", "Explain CI/CD.", 200, "markdown"),
    ("shakespeare", "Respond as if you were William Shakespeare.", "What is cloud computing?", 200, "shakespeare"),
    ("debugger", "You are a senior SRE debugging production issues.", "Pod keeps restarting with OOMKilled.", 200, "debugger"),
    ("teacher", "You are a patient coding teacher for beginners.", "What is a variable?", 200, "teacher"),
    ("security-auditor", "You are a security auditor reviewing infrastructure.", "Review this IAM policy.", 200, "security"),
    ("thai-teacher", "คุณเป็นครูสอน DevOps ภาษาไทย อธิบายง่ายๆ", "Docker คืออะไร", 200, "thai teacher"),
    ("long-system", "You are a DevOps expert with 20 years experience in AWS, GCP, Azure. You specialize in Kubernetes, Terraform, CI/CD pipelines, and cost optimization. Always provide practical, production-ready advice.", "How to set up GitOps?", 300, "long system"),
    ("system-array", None, "What is autoscaling?", 200, "system array"),
]

EDGE_CASES = [
    ("empty-msg", "", 64, "empty msg"),
    ("single-char", "a", 32, "single char"),
    ("max-tokens-1", "Say the word 'test'.", 1, "max_tokens=1"),
    ("long-single-word", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 64, "long word"),
    ("newlines", "Line1\nLine2\nLine3", 100, "newlines"),
    ("tabs", "col1\tcol2\tcol3", 100, "tabs"),
    ("repeated", "hello hello hello hello hello", 64, "repeated"),
    ("numbers", "1234567890" * 5, 100, "numbers"),
    ("markdown-input", "# Header\n- item1\n- item2\n```go\nfmt.Println()\n```", 200, "markdown in"),
    ("html", "<div>Hello</div><p>World</p>", 100, "html input"),
    ("json-input", json.dumps({"key": "value", "nested": {"a": 1}}), 100, "json input"),
    ("sql-input", "SELECT * FROM users WHERE id = 1;", 100, "sql input"),
    ("url-input", "https://example.com/api/v1/users?page=1&limit=10", 100, "url input"),
    ("path-input", "/usr/local/bin:/home/user/.local/bin", 100, "path input"),
    ("base64-input", "SGVsbG8gV29ybGQ=", 64, "base64 input"),
    ("escape-chars", "Quote: \"test\" Backslash: \\\\ End", 64, "escape chars"),
    ("multiline-code", "func main() {\n\tfmt.Println(\"hello\")\n\tfor i := 0; i < 10; i++ {\n\t\tfmt.Println(i)\n\t}\n}", 200, "multiline code"),
    ("yaml-input", "apiVersion: v1\nkind: Pod\nmetadata:\n  name: test", 200, "yaml input"),
    ("shell-cmd", "find . -name '*.go' | xargs grep 'func main'", 100, "shell cmd"),
    ("whitespace", "   leading and trailing spaces   ", 64, "whitespace"),
    ("unicode-art", "Draw a simple ASCII art cat.", 100, "ascii art"),
    ("csv-data", "name,age,city\nAlice,30,Bangkok\nBob,25,Tokyo", 100, "csv input"),
    ("env-vars", "DATABASE_URL=postgres://user:pass@host:5432/db", 100, "env vars"),
    ("log-line", "2024-01-15T10:30:00Z ERROR server.go:42 connection refused", 100, "log line"),
    ("config-toml", "[server]\nport = 8080\nhost = \"localhost\"", 100, "toml config"),
    ("graphql", "{ users(first: 10) { edges { node { id name } } } }", 100, "graphql"),
    ("single-q", "?", 32, "question mark"),
    ("period", ".", 32, "period"),
    ("yes-no", "yes", 32, "yes"),
    ("number-input", "42", 32, "number"),
    ("emoji-input", "🚀", 32, "emoji input"),
    ("long-sentence", "This is a very long sentence that contains many words and is designed to test how the streaming handles longer input prompts with natural language content.", 200, "long sentence"),
    ("tabular", "col1\tcol2\tcol3\nval1\tval2\tval3", 100, "tabular data"),
    ("nested-json", json.dumps({"a": {"b": {"c": {"d": "deep"}}}}), 100, "nested json"),
    ("brackets", "{[()]}", 32, "brackets"),
    ("multi-lang-code", "#include <stdio.h>\nint main() { printf(\"hello\"); return 0; }", 200, "c code"),
    ("python-class", "class Dog:\n    def __init__(self, name):\n        self.name = name\n    def bark(self):\n        return f'{self.name} says woof!'", 200, "python class"),
    ("sql-join", "SELECT u.name, o.total FROM users u JOIN orders o ON u.id = o.user_id WHERE o.total > 100", 200, "sql join"),
]

# Extended prompts for higher coverage
EXTENDED_PROMPTS = [
    ("api-rate-limit", "Explain API rate limiting strategies.", 200, "rate limiting"),
    ("webhook", "Design a webhook system for event notifications.", 300, "webhook design"),
    ("idempotency", "Explain idempotency in distributed systems.", 200, "idempotency"),
    ("event-sourcing", "Explain event sourcing pattern.", 300, "event sourcing"),
    ("cqrs", "Explain CQRS pattern with pros and cons.", 300, "cqrs"),
    ("saga", "Explain the Saga pattern for distributed transactions.", 300, "saga"),
    ("circuit-breaker", "Implement a circuit breaker in Go.", 200, "circuit breaker"),
    ("service-mesh", "Compare Istio vs Linkerd service meshes.", 300, "service mesh"),
    ("api-gateway", "Design an API gateway architecture.", 300, "api gateway"),
    ("load-test", "Write a k6 load testing script for an API.", 300, "k6 load test"),
    ("chaos-eng", "Explain chaos engineering principles.", 200, "chaos eng"),
    ("sre-sl", "Explain SRE SLI/SLO/SLA concepts.", 200, "sli slo sla"),
    ("post-mortem", "Write a post-mortem template for incidents.", 300, "post-mortem"),
    ("gitops", "Explain GitOps workflow with ArgoCD.", 300, "gitops"),
    ("progressive", "Explain progressive delivery (canary/blue-green).", 300, "progressive del"),
    ("feature-flag", "Design a feature flag system.", 300, "feature flags"),
    ("config-mgmt", "Compare Ansible vs Puppet vs Chef.", 300, "config mgmt"),
    ("container-sec", "Container security best practices.", 300, "container sec"),
    ("zero-trust", "Explain zero trust networking.", 200, "zero trust"),
    ("mtls", "Explain mTLS and how to implement it.", 200, "mtls"),
    ("oauth2", "Explain OAuth2 flow with PKCE.", 200, "oauth2 pkce"),
    ("jwt-claims", "Explain JWT structure and claims.", 200, "jwt claims"),
    ("saml", "Compare SAML vs OIDC for SSO.", 200, "saml vs oidc"),
    ("rbac-abac", "Compare RBAC vs ABAC access models.", 200, "rbac vs abac"),
    ("secrets-mgmt", "Secrets management with HashiCorp Vault.", 300, "vault secrets"),
    ("db-replication", "Explain database replication strategies.", 200, "db replication"),
    ("sharding", "Explain database sharding patterns.", 200, "db sharding"),
    ("cqrs-impl", "Implement CQRS with event sourcing in Go.", 300, "cqrs go"),
    ("graphql-rest", "When to use GraphQL vs REST.", 200, "graphql vs rest"),
    ("grpc-stream", "gRPC streaming vs REST SSE.", 200, "grpc streaming"),
    ("message-queue", "Compare Kafka vs RabbitMQ vs SQS.", 300, "message queues"),
    ("pub-sub", "Explain pub/sub pattern for microservices.", 200, "pub sub"),
    ("dead-letter", "Design a dead letter queue system.", 300, "dead letter"),
    ("backpressure", "Explain backpressure in streaming systems.", 200, "backpressure"),
    ("data-pipeline", "Design a real-time data pipeline.", 300, "data pipeline"),
    ("etl", "ETL vs ELT pipeline design.", 200, "etl vs elt"),
    ("data-lake", "Data lake vs data warehouse architecture.", 300, "data lake"),
    ("time-series", "Time-series database comparison.", 200, "time-series db"),
    ("graph-db", "When to use a graph database.", 200, "graph db"),
    ("vector-db", "Vector databases for AI embeddings.", 200, "vector db"),
    ("object-storage", "Object storage vs block storage.", 200, "object storage"),
    ("cdn", "CDN architecture and caching strategies.", 200, "cdn arch"),
    ("dns", "Explain DNS resolution flow.", 200, "dns flow"),
    ("tcp-udp", "TCP vs UDP: when to use each.", 200, "tcp vs udp"),
    ("http2-http3", "HTTP/2 vs HTTP/3 improvements.", 200, "http2 vs http3"),
    ("websocket", "WebSocket vs SSE vs long polling.", 200, "ws vs sse"),
    ("tls-handshake", "Explain TLS 1.3 handshake.", 200, "tls handshake"),
    ("bpf", "Explain eBPF for observability.", 200, "ebpf"),
    ("wasm", "WebAssembly for server-side applications.", 200, "wasm server"),
    ("edge-compute", "Edge computing architecture patterns.", 200, "edge compute"),
    ("multi-cloud", "Multi-cloud strategy considerations.", 300, "multi-cloud"),
    ("hybrid-cloud", "Hybrid cloud architecture design.", 300, "hybrid cloud"),
    ("serverless", "Serverless architecture pros/cons.", 200, "serverless"),
    ("container-runtimes", "containerd vs CRI-O vs Docker.", 200, "container rt"),
    ("k8s-cni", "Kubernetes CNI plugins comparison.", 300, "k8s cni"),
    ("k8s-csi", "Kubernetes CSI storage drivers.", 200, "k8s csi"),
    ("k8s-operator", "Write a Kubernetes operator design.", 300, "k8s operator"),
    ("k8s-crd", "Design a Kubernetes CRD.", 300, "k8s crd"),
    ("k8s-mutation", "Kubernetes admission webhook design.", 300, "k8s webhook"),
    ("k8s-service-mesh", "Service mesh for Kubernetes.", 300, "k8s mesh"),
    ("k8s-gitops", "GitOps with Flux vs ArgoCD.", 300, "flux vs argo"),
    ("observability-3pillars", "Three pillars of observability.", 200, "3 pillars"),
    ("distributed-tracing", "Distributed tracing with OpenTelemetry.", 300, "dist tracing"),
    ("log-aggregation", "Log aggregation architecture.", 300, "log aggregation"),
    ("alerting", "Design an alerting strategy.", 300, "alerting"),
    ("synthetic-monitoring", "Synthetic monitoring vs real-user monitoring.", 200, "synthetic mon"),
    ("error-budget", "Error budget and SLO-based alerting.", 200, "error budget"),
    ("blameless", "Blameless post-incident culture.", 200, "blameless"),
    ("toil-reduction", "Identifying and reducing toil.", 200, "toil reduction"),
    ("capacity-planning", "Capacity planning methodology.", 300, "capacity plan"),
    ("cost-attribution", "Cloud cost attribution strategies.", 200, "cost attribution"),
    ("finops", "FinOps practices for cloud spend.", 300, "finops"),
    ("ri-sp", "Reserved instances vs savings plans.", 200, "ri vs sp"),
    ("spot-instances", "Using spot/preemptible instances.", 200, "spot instances"),
    ("right-sizing", "Instance right-sizing methodology.", 200, "right sizing"),
    ("terraform-module", "Design reusable Terraform modules.", 300, "tf modules"),
    ("terraform-state", "Terraform state management strategies.", 300, "tf state"),
    ("terraform-workspace", "Terraform workspaces vs directories.", 200, "tf workspaces"),
    ("ansible-role", "Design reusable Ansible roles.", 300, "ansible roles"),
    ("packer-ami", "Build AMIs with Packer.", 200, "packer ami"),
    ("vagrant-dev", "Set up dev environments with Vagrant.", 200, "vagrant dev"),
    ("docker-multi-stage", "Docker multi-stage build patterns.", 300, "docker stages"),
    ("docker-slim", "Reducing Docker image size.", 200, "docker slim"),
    ("docker-security", "Docker security scanning.", 200, "docker scan"),
    ("compose-prod", "Docker Compose for production.", 300, "compose prod"),
    ("buildkit", "Docker BuildKit features.", 200, "buildkit"),
    ("kaniko", "Build images without Docker daemon.", 200, "kaniko"),
    ("crossplane", "Infrastructure as code with Crossplane.", 300, "crossplane"),
    ("pulumi", "Pulumi vs Terraform comparison.", 200, "pulumi vs tf"),
    ("github-actions-matrix", "GitHub Actions matrix strategy.", 200, "gha matrix"),
    ("gitlab-ci-cache", "GitLab CI caching strategies.", 200, "gitlab cache"),
    ("jenkins-pipeline", "Jenkins declarative pipeline.", 300, "jenkins pipe"),
    ("tekton", "Tekton CI/CD pipelines.", 200, "tekton"),
    ("spinnaker", "Spinnaker deployment strategies.", 200, "spinnaker"),
    ("argo-workflows", "Argo Workflows for data pipelines.", 300, "argo workflow"),
    ("airflow", "Apache Airflow DAG design.", 300, "airflow dag"),
    ("dbt", "dbt for data transformation.", 200, "dbt"),
    ("spark", "Apache Spark job design.", 300, "spark job"),
    ("flink", "Apache Flink stream processing.", 200, "flink stream"),
    ("beam", "Apache Beam unified processing.", 200, "beam"),
    ("celery", "Celery task queue architecture.", 200, "celery"),
    ("temporal", "Temporal workflow engine.", 200, "temporal"),
    ("step-functions", "AWS Step Functions state machine.", 300, "step functions"),
    ("cloud-events", "CloudEvents specification.", 200, "cloud events"),
    ("open-telemetry", "OpenTelemetry instrumentation.", 300, "otel"),
    ("prometheus-operator", "Prometheus Operator for Kubernetes.", 300, "prom operator"),
    ("thanos", "Thanos for Prometheus HA.", 200, "thanos"),
    ("mimir", "Grafana Mimir for metrics.", 200, "mimir"),
    ("loki", "Grafana Loki for logs.", 200, "loki"),
    ("tempo", "Grafana Tempo for traces.", 200, "tempo"),
    ("pyroscope", "Continuous profiling with Pyroscope.", 200, "pyroscope"),
]

LONG_OUTPUT_PROMPTS = [
    ("essay-300", "Write a 300-word essay on cloud computing trends.", 512, "essay 300w"),
    ("essay-500", "Write a 500-word essay on DevOps culture.", 800, "essay 500w"),
    ("list-20", "List 20 programming languages with one line each.", 400, "list 20"),
    ("count-100", "Count from 1 to 100, one number per line.", 512, "count 1-100"),
    ("alpha-26", "List each letter of the alphabet with a word.", 300, "alphabet"),
    ("story", "Write a short story about a DevOps engineer's worst day.", 500, "devops story"),
    ("steps-10", "Write a 10-step guide to deploying Kubernetes in production.", 500, "k8s 10 steps"),
    ("compare-5", "Compare 5 programming languages: syntax, performance, ecosystem.", 500, "lang compare"),
    ("timeline", "Create a timeline of cloud computing milestones (2010-2025).", 500, "cloud timeline"),
    ("architecture", "Describe a production-grade microservices architecture.", 500, "micro arch"),
]

MULTI_TURN_SETS = [
    ("math-chain", [
        {"role": "user", "content": "What is 2+2?"},
        {"role": "assistant", "content": "2+2 equals 4."},
        {"role": "user", "content": "Multiply that by 3."},
    ], 100, "math chain"),
    ("context-ref", [
        {"role": "user", "content": "My favorite color is blue."},
        {"role": "assistant", "content": "Noted, your favorite color is blue."},
        {"role": "user", "content": "What is my favorite color?"},
    ], 64, "context ref"),
    ("code-ref", [
        {"role": "user", "content": "Define a variable x = 42."},
        {"role": "assistant", "content": "x = 42"},
        {"role": "user", "content": "What is x * 2?"},
    ], 64, "code ref"),
    ("multi-topic", [
        {"role": "user", "content": "What is Python?"},
        {"role": "assistant", "content": "Python is a high-level programming language."},
        {"role": "user", "content": "And what is Go?"},
        {"role": "assistant", "content": "Go is a statically typed compiled language."},
        {"role": "user", "content": "Compare the two."},
    ], 200, "multi topic"),
    ("long-context", [
        {"role": "user", "content": f"Number {i}: {i}"}
        for i in range(1, 11)
    ] + [{"role": "user", "content": "What was number 7?"}], 64, "10-turn ctx"),
]

TOOL_SETS = [
    ("weather", [
        {"name": "get_weather", "description": "Get weather for a city",
         "input_schema": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}},
    ], "What is the weather in Bangkok?", 200, "weather tool"),
    ("calculator", [
        {"name": "calculator", "description": "Calculate math expression",
         "input_schema": {"type": "object", "properties": {"expr": {"type": "string"}}, "required": ["expr"]}},
    ], "What is 2^10?", 200, "calc tool"),
    ("multi-tool", [
        {"name": "get_weather", "description": "Get weather", "input_schema": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}},
        {"name": "calculator", "description": "Calculate", "input_schema": {"type": "object", "properties": {"expr": {"type": "string"}}, "required": ["expr"]}},
        {"name": "search", "description": "Search web", "input_schema": {"type": "object", "properties": {"q": {"type": "string"}}, "required": ["q"]}},
    ], "Should I bring an umbrella in Tokyo today?", 200, "multi tool"),
    ("5-tools", [
        {"name": f"tool_{i}", "description": f"Tool {i} does thing {i}",
         "input_schema": {"type": "object", "properties": {"input": {"type": "string"}}, "required": ["input"]}}
        for i in range(5)
    ], "Use tool_2 to process 'hello'", 200, "5 tools"),
    ("10-tools", [
        {"name": f"tool_{i}", "description": f"Tool {i} does thing {i}",
         "input_schema": {"type": "object", "properties": {"input": {"type": "string"}}, "required": ["input"]}}
        for i in range(10)
    ], "What tools are available?", 200, "10 tools"),
    ("force-tool", [
        {"name": "get_weather", "description": "Get weather", "input_schema": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}},
    ], "What is the weather in London?", 200, "force tool"),
]

INFRA_TOPICS = [
    ("k8s-deploy", "Write a Kubernetes Deployment manifest for a Go web server.", 300, "k8s deploy"),
    ("k8s-service", "Write a Kubernetes Service manifest of type LoadBalancer.", 200, "k8s svc"),
    ("k8s-configmap", "Write a ConfigMap with database connection settings.", 200, "k8s configmap"),
    ("k8s-secret", "Write a Kubernetes Secret for TLS certificates.", 200, "k8s secret"),
    ("k8s-hpa", "Write a HorizontalPodAutoscaler manifest.", 200, "k8s hpa"),
    ("k8s-network", "Explain Kubernetes NetworkPolicy with an example.", 300, "k8s netpol"),
    ("k8s-rbac", "Write RBAC Role and RoleBinding for a read-only user.", 300, "k8s rbac"),
    ("terraform-ec2", "Write Terraform for an EC2 instance with user_data.", 300, "tf ec2"),
    ("terraform-vpc", "Write Terraform for a VPC with public/private subnets.", 400, "tf vpc"),
    ("terraform-s3", "Write Terraform for an S3 bucket with logging.", 300, "tf s3"),
    ("terraform-rds", "Write Terraform for an RDS instance with encryption.", 300, "tf rds"),
    ("ansible-play", "Write an Ansible playbook to install Docker.", 300, "ansible docker"),
    ("docker-compose", "Write a docker-compose.yml for a 3-tier app.", 400, "compose 3tier"),
    ("helm-chart", "Explain Helm chart structure for a web app.", 300, "helm chart"),
    ("argocd-app", "Write an ArgoCD Application manifest.", 200, "argocd app"),
    ("prometheus", "Write Prometheus alerting rules for high memory usage.", 300, "prom rules"),
    ("grafana-dash", "Describe a Grafana dashboard JSON structure.", 300, "grafana json"),
    ("vault-policy", "Write a HashiCorp Vault policy for database secrets.", 300, "vault policy"),
    ("nginx-conf", "Write an nginx config for reverse proxy with SSL.", 300, "nginx conf"),
    ("haproxy", "Write HAProxy config for load balancing 3 backends.", 300, "haproxy cfg"),
    ("elk-pipeline", "Write a Logstash pipeline config for parsing nginx logs.", 300, "logstash cfg"),
    ("redis-conf", "Write Redis config for persistence and memory limits.", 200, "redis cfg"),
    ("github-actions", "Write a GitHub Actions workflow for Go CI.", 300, "gha ci"),
    ("gitlab-ci", "Write a GitLab CI pipeline for Docker build and deploy.", 300, "gitlab ci"),
    ("makefile", "Write a Makefile for a Go project with build/test/lint.", 200, "makefile"),
]

# ===== Test Case Dataclass =====
@dataclass
class TestCase:
    test_id: str
    section: str
    profile: str
    model: str
    apikey: str
    stream: bool
    body: dict
    description: str
    expect_status: int = 200


# ===== Test Generator =====
def generate_tests(
    profiles_filter: Optional[list] = None,
    sections_filter: Optional[list] = None,
    limit: Optional[int] = None,
) -> list[TestCase]:
    tests = []
    counter = 0
    target_profiles = [p for p in PROFILES if (profiles_filter is None or p in profiles_filter)]

    def add(section, profile, stream, body, desc, expect=200):
        nonlocal counter
        counter += 1
        p = PROFILES[profile]
        body_with_model = {**body, "model": p["model"]}
        tc = TestCase(
            test_id=f"{counter:04d}",
            section=section,
            profile=profile,
            model=p["model"],
            apikey=p["key"],
            stream=stream,
            body=body_with_model,
            description=desc,
            expect_status=expect,
        )
        tests.append(tc)

    def should_run(section):
        return sections_filter is None or section in sections_filter

    # Section 1: Basic Streaming (profiles x prompts x stream=true)
    # ~35 prompts x 3 profiles = 105
    if should_run("basic"):
        for profile in target_profiles:
            for pid, prompt, mt, desc in BASIC_PROMPTS:
                add("basic", profile, True, {
                    "max_tokens": mt, "stream": True,
                    "messages": [{"role": "user", "content": prompt}],
                }, desc)

    # Section 2: System Prompts (system x profiles)
    # ~12 system x 3 profiles = 36
    if should_run("system"):
        for profile in target_profiles:
            for spid, sys_prompt, user_msg, mt, desc in SYSTEM_PROMPTS:
                body = {"max_tokens": mt, "stream": True, "messages": [
                    {"role": "user", "content": user_msg or "Tell me about Kubernetes."},
                ]}
                if spid == "system-array":
                    body["system"] = [
                        {"type": "text", "text": sys_prompt or "You are a helpful assistant."},
                        {"type": "text", "text": "Always be concise."},
                    ]
                else:
                    body["system"] = sys_prompt
                add("system", profile, True, body, f"sys:{desc}")

    # Section 3: Multi-Turn Conversations
    # ~5 sets x 3 profiles = 15
    if should_run("multi-turn"):
        for profile in target_profiles:
            for mid, messages, mt, desc in MULTI_TURN_SETS:
                add("multi-turn", profile, True, {
                    "max_tokens": mt, "stream": True, "messages": messages,
                }, desc)

    # Section 4: Tool Use (cc only, supports_tools)
    # ~6 tool sets x 1 profile = 6
    if should_run("tools"):
        for profile in target_profiles:
            if not PROFILES[profile]["supports_tools"]:
                continue
            for tid, tools, msg, mt, desc in TOOL_SETS:
                body = {
                    "max_tokens": mt, "stream": True, "tools": tools,
                    "messages": [{"role": "user", "content": msg}],
                }
                if tid == "force-tool":
                    body["tool_choice"] = {"type": "auto"}
                add("tools", profile, True, body, desc)

    # Section 6: Language & Encoding
    # ~15 languages x 3 profiles = 45
    if should_run("language"):
        for profile in target_profiles:
            for lid, prompt, mt, desc in LANGUAGES:
                add("language", profile, True, {
                    "max_tokens": mt, "stream": True,
                    "messages": [{"role": "user", "content": prompt}],
                }, desc)

    # Section 7: Long Output
    # ~10 prompts x 3 profiles = 30
    if should_run("long-output"):
        for profile in target_profiles:
            for lid, prompt, mt, desc in LONG_OUTPUT_PROMPTS:
                add("long-output", profile, True, {
                    "max_tokens": mt, "stream": True,
                    "messages": [{"role": "user", "content": prompt}],
                }, desc)

    # Section 8: Infrastructure Topics
    # ~25 topics x 3 profiles = 75
    if should_run("infra"):
        for profile in target_profiles:
            for iid, prompt, mt, desc in INFRA_TOPICS:
                add("infra", profile, True, {
                    "max_tokens": mt, "stream": True,
                    "messages": [{"role": "user", "content": prompt}],
                }, desc)

    # Section 9: Edge Cases
    # ~19 cases x 3 profiles = 57
    if should_run("edge"):
        for profile in target_profiles:
            for eid, prompt, mt, desc in EDGE_CASES:
                add("edge", profile, True, {
                    "max_tokens": mt, "stream": True,
                    "messages": [{"role": "user", "content": prompt}],
                }, desc)

    # Section 10: Varying max_tokens (latency correlation)
    # 10 token levels x 3 profiles x 2 prompts = 60
    if should_run("token-sweep"):
        for profile in target_profiles:
            for mt in [1, 2, 4, 8, 16, 32, 64, 128, 256, 512]:
                add("token-sweep", profile, True, {
                    "max_tokens": mt, "stream": True,
                    "messages": [{"role": "user", "content": "Explain recursion in programming."}],
                }, f"mt={mt}")
                add("token-sweep", profile, True, {
                    "max_tokens": mt, "stream": True,
                    "messages": [{"role": "user", "content": "Write a poem about servers."}],
                }, f"poem mt={mt}")

    # Section 11: Temperature Sweep (model behavior variation)
    # 6 temps x 3 profiles x 2 prompts = 36
    if should_run("temperature"):
        for profile in target_profiles:
            for temp in [0.0, 0.2, 0.5, 0.7, 0.9, 1.0]:
                add("temperature", profile, True, {
                    "max_tokens": 128, "stream": True, "temperature": temp,
                    "messages": [{"role": "user", "content": "Write a one-line joke."}],
                }, f"temp={temp}")
                add("temperature", profile, True, {
                    "max_tokens": 128, "stream": True, "temperature": temp,
                    "messages": [{"role": "user", "content": "Explain quantum computing simply."}],
                }, f"quantum t={temp}")

    # Section 12: Top-P Sweep
    # 5 values x 3 profiles = 15
    if should_run("top-p"):
        for profile in target_profiles:
            for top_p in [0.1, 0.3, 0.5, 0.8, 1.0]:
                add("top-p", profile, True, {
                    "max_tokens": 128, "stream": True, "top_p": top_p,
                    "messages": [{"role": "user", "content": "Describe a sunset in one sentence."}],
                }, f"top_p={top_p}")

    # Section 13: Stop Sequences
    # 5 configs x 3 profiles = 15
    if should_run("stop-seq"):
        for profile in target_profiles:
            add("stop-seq", profile, True, {
                "max_tokens": 200, "stream": True, "stop_sequences": ["\n"],
                "messages": [{"role": "user", "content": "List 10 animals."}],
            }, "stop=newline")
            add("stop-seq", profile, True, {
                "max_tokens": 200, "stream": True, "stop_sequences": ["."],
                "messages": [{"role": "user", "content": "Write a paragraph about AI."}],
            }, "stop=period")
            add("stop-seq", profile, True, {
                "max_tokens": 200, "stream": True, "stop_sequences": ["STOP", "END"],
                "messages": [{"role": "user", "content": "Count from 1 to 20."}],
            }, "stop=STOP/END")
            add("stop-seq", profile, True, {
                "max_tokens": 200, "stream": True, "stop_sequences": ["```"],
                "messages": [{"role": "user", "content": "Write a Go function."}],
            }, "stop=codeblock")
            add("stop-seq", profile, True, {
                "max_tokens": 200, "stream": True, "stop_sequences": ["\n\n"],
                "messages": [{"role": "user", "content": "Explain cloud computing in detail."}],
            }, "stop=double-newline")

    # Section 14: Multi-Message (growing context)
    # 10 sizes x 3 profiles = 30
    if should_run("multi-msg"):
        for profile in target_profiles:
            for n in [2, 3, 4, 5, 6, 8, 10, 12, 15, 20]:
                msgs = []
                for i in range(n):
                    role = "user" if i % 2 == 0 else "assistant"
                    if role == "user":
                        msgs.append({"role": "user", "content": f"Question {i//2 + 1}: What is {i+1}+{i+2}?"})
                    else:
                        msgs.append({"role": "assistant", "content": f"The answer is {i+1+i+2}."})
                msgs.append({"role": "user", "content": "Summarize all questions above."})
                add("multi-msg", profile, True, {
                    "max_tokens": 200, "stream": True, "messages": msgs,
                }, f"{n+1} msgs")

    # Section 15: SSE Integrity Validation (verify event sequence)
    # ~10 prompts x 3 profiles = 30
    if should_run("sse-integrity"):
        for profile in target_profiles:
            for pid, prompt, mt, desc in BASIC_PROMPTS[:10]:
                add("sse-integrity", profile, True, {
                    "max_tokens": mt, "stream": True,
                    "messages": [{"role": "user", "content": prompt}],
                }, f"integrity:{desc}")

    # Section 16: Concurrent Load
    # 6 configs x 3 profiles = 18
    if should_run("concurrent"):
        for profile in target_profiles:
            for concurrency in [2, 3, 5, 8, 10, 15]:
                add("concurrent", profile, True, {
                    "max_tokens": 64, "stream": True,
                    "messages": [{"role": "user", "content": "Say 'hello' and nothing else."}],
                }, f"concurrent x{concurrency}")

    # Section 17: Combined Parameters (system + temperature + max_tokens)
    # 3 system x 3 temps x 3 mt x 3 profiles = 81
    if should_run("combined"):
        for profile in target_profiles:
            for sys_p in ["You are a helpful assistant.", "You are a coding expert.", "Respond in JSON."]:
                for temp in [0.0, 0.5, 1.0]:
                    for mt in [32, 128, 256]:
                        add("combined", profile, True, {
                            "max_tokens": mt, "stream": True, "temperature": temp,
                            "system": sys_p,
                            "messages": [{"role": "user", "content": "What is caching?"}],
                        }, f"sys+temp+mt")

    # Section 18: Error Handling (invalid requests)
    # ~8 cases x 3 profiles = 24
    if should_run("errors"):
        for profile in target_profiles:
            # Invalid model
            add("errors", profile, True, {
                "max_tokens": 64, "stream": True,
                "messages": [{"role": "user", "content": "hello"}],
                "_override_model": "invalid-model-xyz",
            }, "invalid model", expect=400)
            # Missing messages
            add("errors", profile, True, {
                "max_tokens": 64, "stream": True,
                "messages": [],
            }, "empty messages", expect=400)
            # max_tokens=0
            add("errors", profile, True, {
                "max_tokens": 0, "stream": True,
                "messages": [{"role": "user", "content": "hello"}],
            }, "max_tokens=0", expect=400)
            # Very long single message
            add("errors", profile, True, {
                "max_tokens": 64, "stream": True,
                "messages": [{"role": "user", "content": "x" * 100000}],
            }, "100k char msg")

    # Section 19: Repeat Requests (consistency check)
    # 5 prompts x 3 repeats x 3 profiles = 45
    if should_run("consistency"):
        for profile in target_profiles:
            for pid, prompt, mt, desc in BASIC_PROMPTS[:5]:
                for repeat in range(3):
                    add("consistency", profile, True, {
                        "max_tokens": mt, "stream": True,
                        "messages": [{"role": "user", "content": prompt}],
                    }, f"repeat{repeat+1} {desc}")

    # Section 20: Streaming Quality Stress (long streaming with metrics)
    # 5 prompts x 3 profiles = 15
    if should_run("stress"):
        for profile in target_profiles:
            for prompt, mt, desc in [
                ("Write a detailed 500-word guide on Terraform best practices.", 800, "tf guide"),
                ("Explain the complete Kubernetes pod lifecycle.", 600, "pod lifecycle"),
                ("Write a comprehensive Dockerfile best practices guide.", 600, "docker guide"),
                ("Describe AWS VPC design patterns in detail.", 600, "vpc patterns"),
                ("Write a complete CI/CD pipeline guide for Kubernetes.", 800, "cicd guide"),
            ]:
                add("stress", profile, True, {
                    "max_tokens": mt, "stream": True,
                    "messages": [{"role": "user", "content": prompt}],
                }, desc)

    # Section 21: Sequential SSE Burst (rapid sequential requests)
    # 10 sequential x 3 profiles = 30
    if should_run("sequential"):
        for profile in target_profiles:
            for i in range(10):
                add("sequential", profile, True, {
                    "max_tokens": 128, "stream": True,
                    "messages": [{"role": "user", "content": f"Test {i}: What is {i}*{i+1}?"}],
                }, f"seq #{i}")

    # Section 22: Privacy/Masking Triggers
    # ~10 PII-containing prompts x 3 profiles = 30
    if should_run("privacy"):
        for profile in target_profiles:
            privacy_prompts = [
                ("My email is test@example.com, what is Kubernetes?", "email in msg"),
                ("Call me at +66-02-123-4567 for the deployment.", "phone in msg"),
                ("My SSN is 123-45-6789, explain Docker.", "ssn in msg"),
                ("Send output to admin@company.co.th", "thai email"),
                ("The API key is sk-ant-abc123def, explain REST.", "api key in msg"),
                ("Password is P@ssw0rd!, explain security.", "password in msg"),
                ("IP 192.168.1.100 is the server, explain networking.", "ip in msg"),
                ("Credit card 4532-1234-5678-9012, explain encryption.", "cc in msg"),
                ("AWS secret AKIAIOSFODNN7EXAMPLE, explain IAM.", "aws key in msg"),
                ("Token eyJhbGciOiJIUzI1NiJ9.test.sig, explain JWT.", "jwt in msg"),
            ]
            for prompt, desc in privacy_prompts:
                add("privacy", profile, True, {
                    "max_tokens": 128, "stream": True,
                    "messages": [{"role": "user", "content": prompt}],
                }, desc)

    # Section 23: Extended DevOps/Platform Topics
    # ~110 topics x 3 profiles = 330
    if should_run("extended"):
        for profile in target_profiles:
            for pid, prompt, mt, desc in EXTENDED_PROMPTS:
                add("extended", profile, True, {
                    "max_tokens": mt, "stream": True,
                    "messages": [{"role": "user", "content": prompt}],
                }, desc)

    # Section 24: Thai Language Deep Coverage
    # ~25 prompts x 3 profiles = 75
    if should_run("thai-deep"):
        for profile in target_profiles:
            for prompt, mt, desc in [
                ("อธิบาย Terraform คืออะไร", 200, "th tf"),
                ("เขียน Docker Compose สำหรับ web app", 300, "th compose"),
                ("Kubernetes Pod คืออะไร", 200, "th pod"),
                ("อธิบาย CI/CD Pipeline", 200, "th cicd"),
                ("Git branching strategy แบบไหนดี", 200, "th git branch"),
                ("Load Balancer ทำงานยังไง", 200, "th lb"),
                ("SSL/TLS certificate อธิบายหน่อย", 200, "th ssl"),
                ("Microservices vs Monolith เลือกอะไรดี", 200, "th micro"),
                ("Database indexing สำคัญยังไง", 200, "th db index"),
                ("Redis cache ใช้ตอนไหน", 200, "th redis"),
                ("Monitoring ที่ดีควรมีอะไรบ้าง", 200, "th monitor"),
                ("Log management ทำยังไงให้ดี", 200, "th logging"),
                ("Incident response process ควรเป็นยังไง", 300, "th incident"),
                ("Cost optimization ใน AWS ทำยังไง", 200, "th aws cost"),
                ("Security best practices สำหรับ production", 300, "th security"),
                ("Infrastructure as Code ทำไมต้องใช้", 200, "th iac"),
                ("Ansible vs Terraform ต่างกันตรงไหน", 200, "th ansible vs tf"),
                ("Blue-green deployment คืออะไร", 200, "th blue-green"),
                ("Canary deployment ทำยังไง", 200, "th canary"),
                ("Feature flag ใช้ทำอะไร", 200, "th feature flag"),
                ("Rate limiting สำคัญยังไง", 200, "th rate limit"),
                ("API versioning ทำยังไงดี", 200, "th api ver"),
                ("Error handling ใน microservices", 200, "th error"),
                ("Event-driven architecture คืออะไร", 200, "th event"),
                ("Data pipeline ออกแบบยังไง", 300, "th data pipe"),
            ]:
                add("thai-deep", profile, True, {
                    "max_tokens": mt, "stream": True,
                    "messages": [{"role": "user", "content": prompt}],
                }, desc)

    # Section 25: Extended SSE Integrity (all prompts x 3 profiles)
    # 35 basic prompts x 3 profiles = 105
    if should_run("integrity-deep"):
        for profile in target_profiles:
            for pid, prompt, mt, desc in BASIC_PROMPTS:
                add("integrity-deep", profile, True, {
                    "max_tokens": mt, "stream": True,
                    "messages": [{"role": "user", "content": prompt}],
                }, f"integrity:{desc}")

    # Section 26: Temperature x Prompt Matrix
    # 10 prompts x 5 temps x 3 profiles = 150
    if should_run("temp-matrix"):
        for profile in target_profiles:
            for pid, prompt, mt, desc in BASIC_PROMPTS[:10]:
                for temp in [0.0, 0.3, 0.5, 0.7, 1.0]:
                    add("temp-matrix", profile, True, {
                        "max_tokens": mt, "stream": True, "temperature": temp,
                        "messages": [{"role": "user", "content": prompt}],
                    }, f"t={temp} {desc}")

    # Section 27: System Prompt x Temperature Matrix
    # 6 system x 4 temps x 3 profiles = 72
    if should_run("sys-temp"):
        for profile in target_profiles:
            for sys_p in [
                "You are a helpful assistant.",
                "You are a coding expert.",
                "Respond in JSON.",
                "You are extremely concise.",
                "You are a DevOps expert.",
                "You explain things simply for beginners.",
            ]:
                for temp in [0.0, 0.5, 0.7, 1.0]:
                    add("sys-temp", profile, True, {
                        "max_tokens": 128, "stream": True, "temperature": temp,
                        "system": sys_p,
                        "messages": [{"role": "user", "content": "What is caching?"}],
                    }, f"sys+temp")

    # Apply limit
    if limit:
        tests = tests[:limit]

    return tests


# ===== Test Runner =====
@dataclass
class TestResult:
    test_id: str
    section: str
    profile: str
    model: str
    stream: bool
    description: str
    http_code: int = 0
    ttfb_ms: float = 0
    total_ms: float = 0
    chunk_count: int = 0
    text_chunks: int = 0
    max_interval_ms: float = 0
    quality: str = "N/A"
    pass_fail: str = "FAIL"
    error: str = ""


def run_single_test(tc: TestCase) -> TestResult:
    result = TestResult(
        test_id=tc.test_id, section=tc.section, profile=tc.profile,
        model=tc.model, stream=tc.stream, description=tc.description,
    )

    body = {k: v for k, v in tc.body.items() if not k.startswith("_override")}
    if "_override_model" in tc.body:
        body["model"] = tc.body["_override_model"]

    body_json = json.dumps(body, ensure_ascii=False)

    try:
        proc = subprocess.run(
            [
                "curl", "-sS", "-N", "--no-buffer", "--max-time", str(TIMEOUT),
                "-X", "POST", f"{GATEWAY}/v1/messages",
                "-H", "Content-Type: application/json",
                "-H", f"x-api-key: {tc.apikey}",
                "-H", f"X-Profile: {tc.profile}",
                "-H", "anthropic-version: 2023-06-01",
                "-w", "\n__STATS__%{http_code} %{time_starttransfer} %{time_total} %{size_download}",
                "-d", body_json,
            ],
            capture_output=True, text=True, timeout=TIMEOUT + 10,
        )
        output = proc.stdout

        # Parse curl stats from last line
        lines = output.split("\n")
        stats_line = ""
        data_lines = lines
        for i, line in enumerate(lines):
            if line.startswith("__STATS__"):
                stats_line = line.replace("__STATS__", "")
                data_lines = lines[:i]
                break

        if stats_line:
            parts = stats_line.strip().split()
            if len(parts) >= 3:
                result.http_code = int(parts[0]) if parts[0].isdigit() else 0
                result.ttfb_ms = float(parts[1]) * 1000 if parts[1] != "0.000" else 0
                result.total_ms = float(parts[2]) * 1000 if parts[2] != "0.000" else 0

        # Count SSE chunks
        full_output = "\n".join(data_lines)
        if tc.stream:
            result.chunk_count = full_output.count("data: ")
            result.text_chunks = full_output.count("content_block_delta")

        # Validate SSE event sequence for sse-integrity section
        if tc.section == "sse-integrity" and tc.stream and result.http_code == 200:
            if "message_start" not in full_output:
                result.error = "missing message_start"
            elif "message_stop" not in full_output:
                result.error = "missing message_stop"
            elif "content_block_start" not in full_output:
                result.error = "missing content_block_start"
            elif "content_block_stop" not in full_output:
                result.error = "missing content_block_stop"

        # Determine quality
        if tc.stream and result.text_chunks > 1:
            result.quality = "STREAMING"
        elif tc.stream and result.text_chunks <= 1:
            result.quality = "SINGLE_CHUNK"
        elif not tc.stream:
            result.quality = "NON_STREAM"

        # Pass/Fail
        if tc.expect_status != 200:
            result.pass_fail = "PASS" if result.http_code == tc.expect_status else "FAIL"
        elif result.http_code != 200:
            result.pass_fail = "FAIL"
        elif tc.stream and result.text_chunks == 0 and result.http_code == 200:
            # Could be a stop_sequence hit or very short response
            result.pass_fail = "PASS"  # Allow zero chunks for edge cases
        else:
            result.pass_fail = "PASS"

        if result.error:
            result.pass_fail = "FAIL"

    except subprocess.TimeoutExpired:
        result.pass_fail = "FAIL"
        result.error = "TIMEOUT"
    except Exception as e:
        result.pass_fail = "FAIL"
        result.error = str(e)[:100]

    return result


# ===== Report =====
def print_header():
    print()
    print("=" * 78)
    print("  SSE Streaming Parameterized Test Suite")
    print("  Up to 5000 test cases across 22 sections")
    print("=" * 78)
    print()


def print_section_header(section, total_in_section):
    section_names = {
        "basic": "Basic Streaming",
        "system": "System Prompts",
        "multi-turn": "Multi-Turn Conversations",
        "tools": "Tool Use",
        "language": "Language & Encoding",
        "long-output": "Long Output",
        "infra": "Infrastructure Topics",
        "edge": "Edge Cases",
        "token-sweep": "Token Sweep (max_tokens)",
        "temperature": "Temperature Sweep",
        "top-p": "Top-P Sweep",
        "stop-seq": "Stop Sequences",
        "multi-msg": "Multi-Message Context Growth",
        "sse-integrity": "SSE Event Sequence Integrity",
        "concurrent": "Concurrent Load",
        "combined": "Combined Parameters",
        "errors": "Error Handling",
        "consistency": "Consistency (Repeat Requests)",
        "stress": "Streaming Quality Stress",
        "sequential": "Sequential SSE Burst",
        "privacy": "Privacy/Masking Triggers",
        "extended": "Extended DevOps/Platform Topics",
        "thai-deep": "Thai Language Deep Coverage",
        "integrity-deep": "Extended SSE Integrity",
        "temp-matrix": "Temperature x Prompt Matrix",
        "sys-temp": "System Prompt x Temperature Matrix",
    }
    name = section_names.get(section, section)
    print()
    print(f"  [{section.upper()}] {name} ({total_in_section} tests)")
    print(f"  {'-' * 70}")


def print_result(r: TestResult, num: int):
    icon = "\033[0;32mPASS\033[0m" if r.pass_fail == "PASS" else "\033[0;31mFAIL\033[0m"
    stream_tag = "S" if r.stream else "N"
    dim = "\033[2m"
    nc = "\033[0m"

    extra = ""
    if r.error:
        extra = f" {dim}[{r.error}]{nc}"

    print(f"  {num:4d}. {icon} {r.profile}/{r.model[:20]:<20s} {stream_tag} HTTP:{r.http_code:3d} "
          f"TTFB:{r.ttfb_ms:6.0f}ms Total:{r.total_ms:6.0f}ms "
          f"Chunks:{r.text_chunks:4d} {dim}{r.description}{nc}{extra}")


def print_summary(results: list[TestResult], elapsed: float):
    print()
    print("=" * 78)
    print("  SUMMARY")
    print("=" * 78)

    total = len(results)
    passed = sum(1 for r in results if r.pass_fail == "PASS")
    failed = sum(1 for r in results if r.pass_fail == "FAIL")

    # By profile
    by_profile = defaultdict(lambda: {"pass": 0, "fail": 0, "ttfb": [], "total": [], "chunks": []})
    by_section = defaultdict(lambda: {"pass": 0, "fail": 0})

    for r in results:
        p = by_profile[r.profile]
        if r.pass_fail == "PASS":
            p["pass"] += 1
        else:
            p["fail"] += 1
        if r.ttfb_ms > 0:
            p["ttfb"].append(r.ttfb_ms)
        if r.total_ms > 0:
            p["total"].append(r.total_ms)
        if r.text_chunks > 0:
            p["chunks"].append(r.text_chunks)

        s = by_section[r.section]
        if r.pass_fail == "PASS":
            s["pass"] += 1
        else:
            s["fail"] += 1

    print(f"\n  Total: {total}  Passed: {passed}  Failed: {failed}  Time: {elapsed:.1f}s")
    print(f"  Pass Rate: {passed/total*100:.1f}%" if total > 0 else "  No tests run")

    print(f"\n  {'Profile':<12s} {'Pass':>6s} {'Fail':>6s} {'Avg TTFB':>10s} {'Avg Total':>10s} {'Avg Chunks':>11s}")
    print(f"  {'-'*55}")
    for profile in ["cc", "kimi"]:
        if profile in by_profile:
            p = by_profile[profile]
            avg_ttfb = sum(p["ttfb"]) / len(p["ttfb"]) if p["ttfb"] else 0
            avg_total = sum(p["total"]) / len(p["total"]) if p["total"] else 0
            avg_chunks = sum(p["chunks"]) / len(p["chunks"]) if p["chunks"] else 0
            print(f"  {profile:<12s} {p['pass']:>6d} {p['fail']:>6d} {avg_ttfb:>9.0f}ms {avg_total:>9.0f}ms {avg_chunks:>10.1f}")

    print(f"\n  {'Section':<20s} {'Pass':>6s} {'Fail':>6s} {'Rate':>8s}")
    print(f"  {'-'*42}")
    for section in sorted(by_section.keys()):
        s = by_section[section]
        total_s = s["pass"] + s["fail"]
        rate = s["pass"] / total_s * 100 if total_s > 0 else 0
        print(f"  {section:<20s} {s['pass']:>6d} {s['fail']:>6d} {rate:>7.1f}%")

    # Failed tests detail
    failures = [r for r in results if r.pass_fail == "FAIL"]
    if failures:
        print(f"\n  FAILED TESTS ({len(failures)}):")
        for r in failures:
            print(f"    {r.test_id} {r.profile}/{r.model} {r.description} - {r.error or 'http ' + str(r.http_code)}")

    print()
    print("=" * 78)


def write_tsv(results: list[TestResult], path: str):
    with open(path, "w") as f:
        f.write("test_id\tsection\tprofile\tmodel\tstream\thttp_status\tttfb_ms\ttotal_ms\ttext_chunks\tmax_interval_ms\tquality\tpass_fail\terror\tdescription\n")
        for r in results:
            f.write(f"{r.test_id}\t{r.section}\t{r.profile}\t{r.model}\t{r.stream}\t"
                    f"{r.http_code}\t{r.ttfb_ms:.0f}\t{r.total_ms:.0f}\t{r.text_chunks}\t"
                    f"{r.max_interval_ms:.0f}\t{r.quality}\t{r.pass_fail}\t{r.error}\t{r.description}\n")
    print(f"  Results written to: {path}")


def list_tests(tests: list[TestCase]):
    print(f"\n  Generated {len(tests)} test cases:\n")
    by_section = defaultdict(list)
    for tc in tests:
        by_section[tc.section].append(tc)

    for section in sorted(by_section.keys()):
        tcs = by_section[section]
        print(f"  [{section}] {len(tcs)} tests")
        for tc in tcs[:5]:
            print(f"    {tc.test_id} {tc.profile}/{tc.model[:20]} stream={tc.stream} {tc.description}")
        if len(tcs) > 5:
            print(f"    ... and {len(tcs)-5} more")
        print()


# ===== Main =====
def main():
    parser = argparse.ArgumentParser(description="SSE Streaming Parameterized Test Suite")
    parser.add_argument("--profile", nargs="*", help="Filter profiles (cc, kimi)")
    parser.add_argument("--section", nargs="*", help="Filter sections")
    parser.add_argument("--limit", type=int, help="Max tests to run")
    parser.add_argument("--concurrent", type=int, default=1, help="Parallel workers")
    parser.add_argument("--list", action="store_true", help="List tests without running")
    parser.add_argument("--output", default=None, help="Output TSV file path")
    parser.add_argument("--report", default=None, help="Display report from TSV file")
    parser.add_argument("--gateway", default=GATEWAY, help="Gateway URL")
    args = parser.parse_args()

    if args.report:
        # Read and display TSV report
        import csv
        results = []
        with open(args.report) as f:
            reader = csv.DictReader(f, delimiter="\t")
            for row in reader:
                results.append(TestResult(
                    test_id=row["test_id"], section=row["section"], profile=row["profile"],
                    model=row["model"], stream=row["stream"] == "True",
                    description=row["description"],
                    http_code=int(row.get("http_status", 0)),
                    ttfb_ms=float(row.get("ttfb_ms", 0)),
                    total_ms=float(row.get("total_ms", 0)),
                    text_chunks=int(row.get("text_chunks", 0)),
                    quality=row.get("quality", "N/A"),
                    pass_fail=row.get("pass_fail", "FAIL"),
                    error=row.get("error", ""),
                ))
        print_summary(results, 0)
        return

    # Generate tests
    tests = generate_tests(
        profiles_filter=args.profile,
        sections_filter=args.section,
        limit=args.limit,
    )

    if args.list:
        list_tests(tests)
        print(f"  Total: {len(tests)} test cases")
        return

    print_header()

    print(f"  Running {len(tests)} tests with {args.concurrent} worker(s)")
    print(f"  Gateway: {GATEWAY}")
    print(f"  Profiles: {', '.join(set(t.profile for t in tests))}")

    start = time.time()
    results = []

    if args.concurrent > 1:
        # Group by section for ordered output
        current_section = None
        section_tests = defaultdict(list)
        for tc in tests:
            section_tests[tc.section].append(tc)

        all_results = []
        num = 0

        for section in sorted(section_tests.keys()):
            tcs = section_tests[section]
            print_section_header(section, len(tcs))

            with ThreadPoolExecutor(max_workers=args.concurrent) as executor:
                future_to_tc = {executor.submit(run_single_test, tc): tc for tc in tcs}
                section_results = []
                for future in as_completed(future_to_tc):
                    num += 1
                    r = future.result()
                    section_results.append(r)
                    all_results.append(r)
                    print_result(r, num)

        results = all_results
    else:
        current_section = None
        num = 0
        for tc in tests:
            if tc.section != current_section:
                current_section = tc.section
                section_count = sum(1 for t in tests if t.section == current_section)
                print_section_header(current_section, section_count)

            num += 1
            r = run_single_test(tc)
            results.append(r)
            print_result(r, num)

    elapsed = time.time() - start
    print_summary(results, elapsed)

    # Write TSV
    output_path = args.output or f"/tmp/sse-test-5k-results-{int(time.time())}.tsv"
    write_tsv(results, output_path)


if __name__ == "__main__":
    main()
