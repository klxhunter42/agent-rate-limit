You are a Principal Distributed Systems Architect and DevSecOps Engineer.

Design and implement a production-grade multi-agent AI system with strong reliability, scalability, and rate-limit protection.

This system must be deployable using docker-compose and must support 100 to 1000 concurrent agents with predictable latency and resource efficiency.

============================
PRIMARY TECHNOLOGY STACK
============================

Cache / Queue:

Dragonfly

Distributed Rate Limit:

https://github.com/uppnrise/distributed-rate-limiter

API Gateway:

Go

Worker Runtime:

Python

Transport:

HTTP / REST

Orchestration:

docker-compose

Observability:

Prometheus
OpenTelemetry

============================
PRIMARY AI PROVIDER
============================

Primary:

Z.ai / GLM models

Examples:

glm-5
glm-4.5
glm-4.6v

Endpoint:

https://api.z.ai/api/anthropic

Compatibility:

Anthropic API compatible

============================
FALLBACK AI PROVIDERS
============================

OpenAI
Anthropic
Google Gemini
OpenRouter

Routing logic:

If provider returns:

429
timeout
5xx

Then:

switch provider automatically

============================
SYSTEM ARCHITECTURE
============================

Client
  |
  v
API Gateway (Go)
  |
  v
Distributed Rate Limiter
  |
  v
Dragonfly Queue / Cache
  |
  v
Python Worker Pool
  |
  v
AI Provider

============================
SERVICES TO IMPLEMENT
============================

1) api-gateway

Language:

Go

Responsibilities:

Accept incoming requests
Validate payload
Apply rate limit
Select API key
Push job to queue
Return request ID

Requirements:

High concurrency
Non-blocking
Stateless
Horizontal scaling

Must implement:

worker pool
context timeout
retry policy
structured logging

============================

2) rate-limiter

Use:

distributed-rate-limiter

Responsibilities:

Global rate control
Burst protection
Token bucket algorithm

Configuration:

capacity:

100

refill_rate:

50 per second

============================

3) dragonfly

Responsibilities:

Queue
Cache
Retry buffer
Session storage

Queue type:

FIFO

Cache TTL:

short:

60 seconds

long:

10 minutes

============================

4) ai-worker

Language:

Python

Responsibilities:

Pull job from queue
Call AI provider
Retry on failure
Store response

Requirements:

Async processing
Timeout handling
Retry logic
Fallback provider

Must support:

provider routing
key rotation
streaming response

============================
KEY MANAGEMENT
============================

Implement:

API key pool

Example:

GLM_KEY_1
GLM_KEY_2
GLM_KEY_3

OPENAI_KEY_1
OPENAI_KEY_2

Selection:

random

Retry rule:

on rate limit:

switch key

============================
RATE LIMIT STRATEGY
============================

Use:

distributed-rate-limiter

Algorithm:

token bucket

Global limit:

100 requests per second

Per-agent limit:

5 requests per second

============================
QUEUE STRATEGY
============================

Use:

Dragonfly

Operations:

LPUSH
BRPOP

Retry policy:

max_retry:

3

Backoff:

exponential

============================
SCALING STRATEGY
============================

Initial:

100 agents

Scale target:

1000 agents

Scaling rules:

workers = agents / 20

Example:

100 agents:

5 workers

1000 agents:

50 workers

All services:

stateless

============================
OBSERVABILITY
============================

Expose:

metrics endpoint

Metrics:

request latency
queue depth
error rate
rate limit hits

Export:

Prometheus

============================
SECURITY
============================

Use:

docker-compose secrets

Never:

hardcode API keys

Enable:

TLS support
request validation
input sanitization

============================
FAILOVER STRATEGY
============================

If provider fails:

retry

If retry fails:

switch provider

If all providers fail:

store error response

============================
PERFORMANCE TARGET
============================

10 agents:

p50 latency:

850 ms

100 agents:

p50 latency:

900 ms

1000 agents:

p50 latency:

1.1 sec

============================
OUTPUT REQUIREMENTS
============================

Generate:

architecture diagram

docker-compose.yml

Go API gateway code

Python worker code

.env example

performance estimation

============================
IMPORTANT CONSTRAINTS
============================

No blocking I/O

Use async processing

Optimize memory usage

Avoid single point of failure

System must run on:

single node

and

multi-node environ