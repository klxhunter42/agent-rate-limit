# AI Worker Service

## 1. Architecture Overview

The AI Worker is an async Python service that consumes inference jobs from a shared Dragonfly (Redis-compatible) queue, dispatches them to upstream LLM providers (Anthropic, OpenAI, Google Gemini, Z.AI/GLM, OpenRouter), and writes results back to Dragonfly cache for the Go API gateway to poll.

**Relationship to Go API Gateway:**

The Go gateway (`api-gateway/queue/dragonfly.go`) acts as the job producer. It serializes `Job` structs as JSON and LPUSHes them onto the `ai_jobs` list. The Python worker BRPOPs from the same list. After processing, the worker SETs a `result:{request_id}` key in Dragonfly with a TTL. The gateway calls `GetResult(requestID)` to poll for completion.

```
 Go API Gateway                     Dragonfly                  AI Worker (Python)
|                               |                             |
|--- LPUSH ai_jobs {job} ----->|                             |
|                               |<---- BRPOP ai_jobs --------|
       |                               |                             |-- dispatch to provider
       |                               |                             |-- await response
|                               |<---- SET result:{id} ------|
|<-- GET result:{id} ----------|                             |
|--- return result to client -->|                             |
```

The gateway and worker share no direct network connection. All communication is through Dragonfly lists (queue) and keys (result cache). This allows independent scaling and deployment of each service.

---

## 2. Main Entry Point (`main.py`)

### Startup Sequence

```
main() -> get_settings() -> build_key_manager() -> Worker.start() -> run_loop() x N
```

1. **Settings loaded** - `WorkerSettings` reads all config from environment variables and `.env` file via pydantic-settings.
2. **Prometheus server started** - `start_http_server(port=9090)` exposes `/metrics` for Prometheus scraping.
3. **Key manager built** - API keys from env vars are grouped by provider and passed to `KeyManager`.
4. **Worker created** - `Worker(settings, key_manager, internal_metrics)` initializes provider cache, RPM limiters, and concurrency semaphores.
5. **Worker.start()** - Connects to Dragonfly with `aioredis.from_url()` and pings to verify connectivity.
6. **Worker coroutines launched** - `N` async tasks (default 10) call `worker.run_loop(i)` concurrently.
7. **Metrics updater started** - Periodic coroutine polls `worker.get_queue_depth()` and updates `QUEUE_DEPTH` gauge every 5 seconds.
8. **Internal metrics HTTP server** - Starts on port 9091 with `/metrics-internal` endpoint returning a JSON snapshot of latency percentiles and error counts.

### Graceful Shutdown

Signal handlers for `SIGINT` and `SIGTERM` set an `asyncio.Event`. The main coroutine awaits this event, then calls `worker.stop()` (sets `_running = False`, closes Redis connection), cancels all worker tasks and the metrics updater, and gathers them with `return_exceptions=True`.

### Structured Logging

Configured via `structlog` with:
- `merge_contextvars` for request-scoped context
- ISO 8601 timestamps
- JSON renderer in production (non-TTY), colorized console in dev
- INFO level filtering (`make_filtering_bound_logger(20)`)

### Class: `PrometheusExporter`

Small adapter that syncs internal `Metrics` counters to Prometheus gauges. Currently only exports `QUEUE_DEPTH`. The heavy lifting is done by direct `pm.*` calls from `worker.py`.

### Function: `build_key_manager(settings)`

Constructs `KeyManager` by mapping each provider's key list property to a dict:
```python
{"glm": ["key1", "key2"], "openai": ["key3"], ...}
```

---

## 3. Worker Loop (`worker.py`)

### Concurrency Model

The worker spawns `N` independent asyncio coroutines (configurable via `WORKER_CONCURRENCY`, default 10). Each coroutine runs an identical `run_loop()`:

```python
async def run_loop(self, worker_id: int):
    while self._running:
        result = await self.redis.brpop(queue_name, timeout=5)
        if result is None:
            continue  # poll timeout
        _, job_json = result
        await self._process_job(job_json, worker_id)
```

`BRPOP` is a blocking Redis pop that returns `None` on timeout. This is the primary idle mechanism -- no busy-waiting.

### Job Processing Pipeline

```
BRPOP -> JSON parse -> concurrency semaphore acquire -> provider fallback chain -> store result
```

#### Step 1: JSON Parsing and Field Extraction

Job fields extracted:
| Field         | Default                     | Description                      |
|---------------|-----------------------------|----------------------------------|
| `request_id`  | `"unknown"`                 | Correlation ID for result lookup |
| `agent_id`    | `"unknown"`                 | Agent that submitted the job     |
| `provider`    | `settings.default_provider` | Target provider name             |
| `model`       | `settings.default_model`    | Target model name                |
| `messages`    | `[]`                        | Chat messages array              |
| `max_tokens`  | `1024`                      | Max completion tokens            |
| `temperature` | `0.7`                       | Sampling temperature             |
| `retry_count` | `0`                         | Current retry attempt number     |

#### Step 2: Model Fallback via Concurrency Semaphores

`_try_acquire_model(requested_model)` performs a non-blocking check on the requested model's semaphore. If at capacity, it walks through `MODEL_FALLBACK_ORDER`:

```python
MODEL_FALLBACK_ORDER = [
    "glm-5.1", "glm-5-turbo", "glm-5",
    "glm-4.7", "glm-4.6",
]
```

If all fallback models are also at capacity, the worker falls back to a blocking `async with model_sem` on the originally requested model. A global semaphore (`upstream_global_limit`) wraps the model semaphore if configured.

#### Step 3: Provider Fallback Chain

`_execute_job()` iterates through providers in `PROVIDER_FALLBACK_ORDER`:

```python
PROVIDER_FALLBACK_ORDER = ["glm", "openai", "anthropic", "gemini", "openrouter"]
```

The requested provider is tried first, then the fallback order (skipping providers without configured keys). For each provider:
1. `key_manager.get_key()` -- get a random non-cooldown key
2. If all keys cooling down -- wait for shortest cooldown to expire
3. `_rpm_limiter.acquire()` -- sliding window RPM enforcement
4. `provider.complete()` -- actual LLM API call
5. On success -- record metrics, store result, return
6. On rate limit error (429, overloaded) -- cooldown the key, try next provider
7. On other error -- log, try next provider

#### Step 4: Result Storage

Results are stored as JSON in Dragonfly with key `result:{request_id}` and TTL from `settings.result_ttl` (default 600 seconds).

### Error Handling and Retries

- **Rate limit detection** (`_is_rate_limit_error`): checks for "429", "rate_limit", "1305" (Z.AI overloaded), "overloaded" in error string, and `status_code` in (429, 502, 503, 504).
- **Key cooldown**: On rate limit, the offending key is put in cooldown for up to 60 seconds via `key_manager.cooldown_key()`.
- **Retry logic**: If all providers fail and `retry_count < max_retries` (default 3), the job is re-queued with exponential backoff: `base_backoff * 2^retry_count + jitter`, capped at 60 seconds.
- **Final failure**: After exhausting retries, an error result is stored: `{"status": "error", "error": "...", "retry_count": N}`.

### Provider Caching

`_get_provider(provider_name, api_key)` caches provider instances keyed by `provider:sha256(api_key)[:16]`. This allows connection reuse when the same key is selected again.

---

## 4. Key Manager (`key_manager.py`)

### Class: `KeyManager`

Thread-safe API key pool with random selection and cooldown rotation.

#### Constructor

```python
KeyManager(keys_by_provider: dict[str, list[str]])
```

Initializes per-provider key pools and empty cooldown dicts.

#### Methods

| Method | Signature | Description |
|---|---|---|
| `get_key` | `async (provider: str) -> str \| None` | Returns a random key not in cooldown. Returns `None` if all keys cooling down. |
| `cooldown_key` | `async (provider: str, key: str, duration: float = 60) -> str \| None` | Puts key on cooldown (capped at 60s). Returns next available key or `None`. |
| `rotate_key` | `async (provider: str, failed_key: str) -> str \| None` | Alias for `cooldown_key`. |
| `get_available_providers` | `() -> list[str]` | Providers with at least one key registered. |
| `has_keys` | `(provider: str) -> bool` | Whether provider has any keys (regardless of cooldown). |
| `has_available_keys` | `(provider: str) -> bool` | Whether provider has any keys NOT in cooldown. |
| `key_counts` | `() -> dict[str, dict[str, int]]` | Stats per provider: `{total, available, cooling_down}`. |
| `shortest_cooldown` | `async (provider: str) -> float` | Seconds until next cooldown expires (min 0.5s buffer). |

#### Cooldown Mechanism

Cooldowns are stored as `monotonic_time + duration` floats in a per-provider dict. `_available_keys()` filters out keys where `cooldown_until > now`. This avoids a separate timer/cleanup task -- expired cooldowns are simply ignored on the next `get_key()` call.

#### Constants

- `MAX_COOLDOWN = 60` -- maximum cooldown duration in seconds

---

## 5. Provider Implementations

### Base Class (`providers/base.py`)

#### Dataclass: `ProviderResponse`

```python
@dataclass
class ProviderResponse:
    content: str
    model: str
    provider: str
    usage: dict[str, int]  # {prompt_tokens, completion_tokens, total_tokens}
    finish_reason: str = "stop"
```

Standardized response envelope for all providers.

#### Abstract Class: `BaseProvider`

```python
class BaseProvider(ABC):
    @abstractmethod
    async def complete(
        self,
        messages: list[dict[str, Any]],
        model: str,
        max_tokens: int = 1024,
        temperature: float = 0.7,
        **kwargs,
    ) -> ProviderResponse: ...

    @abstractmethod
    def get_name(self) -> str: ...
```

### Provider Class Hierarchy

```
BaseProvider (ABC)
  |-- GLMProvider         (anthropic.AsyncAnthropic, custom base_url)
  |-- AnthropicProvider   (anthropic.AsyncAnthropic)
  |-- OpenAIProvider      (openai.AsyncOpenAI)
  |-- GeminiProvider      (google.generativeai.GenerativeModel)
  |-- OpenRouterProvider  (openai.AsyncOpenAI, custom base_url)
```

### GLMProvider (`providers/glm.py`)

- **SDK**: `anthropic.AsyncAnthropic` with `base_url` pointing to Z.AI endpoint
- **Endpoint**: Configurable via `GLM_ENDPOINT`, default `https://api.z.ai/api/anthropic`
- **Model mapping**: `GLM_MODELS` dict maps friendly names (`glm-5`, `glm-4.5`, `glm-4.6v`) to API identifiers
- **System message handling**: Extracts `role=system` messages and passes as `system` parameter (Anthropic API convention)
- **Response parsing**: Iterates `response.content` blocks, concatenating `block.text` attributes
- **Usage mapping**: `input_tokens` -> `prompt_tokens`, `output_tokens` -> `completion_tokens`
- **Finish reason**: `response.stop_reason` with fallback to `"stop"`

### AnthropicProvider (`providers/anthropic_provider.py`)

- **SDK**: `anthropic.AsyncAnthropic`
- **Default model**: `claude-sonnet-4-20250514`
- **System message handling**: Same as GLM -- extracts system role and passes as `system` param
- **Response parsing**: Same block iteration pattern as GLM
- **Usage mapping**: Identical to GLM (input/output token naming)
- **Finish reason**: `response.stop_reason` with fallback to `"stop"`

### OpenAIProvider (`providers/openai_provider.py`)

- **SDK**: `openai.AsyncOpenAI`
- **Default model**: `gpt-4o`
- **System message handling**: System messages passed inline in `messages` array (OpenAI convention)
- **Response parsing**: `response.choices[0].message.content`
- **Error handling**: Raises `RuntimeError` if `response.choices` is empty
- **Usage mapping**: Direct passthrough of `response.usage.prompt_tokens`, `.completion_tokens`, `.total_tokens`
- **Finish reason**: `response.choices[0].finish_reason`

### GeminiProvider (`providers/gemini.py`)

- **SDK**: `google.generativeai` (synchronous SDK used with `await`)
- **Default model**: `gemini-2.0-flash`
- **Per-request configuration**: Calls `genai.configure(api_key=...)` before each request to isolate keys across cached instances (workaround for global state in google-generativeai SDK)
- **Message conversion**: `role=system` -> `system_instruction`, `role=assistant` -> `role=model`, `role=user` -> `role=user`. Content wrapped in `parts` array.
- **Response parsing**: `response.text`
- **Usage mapping**: `response.usage_metadata.prompt_token_count`, `.candidates_token_count`, `.total_token_count`
- **Finish reason**: Always `"stop"` (Gemini SDK doesn't expose finish reason in the same way)

### OpenRouterProvider (`providers/openrouter.py`)

- **SDK**: `openai.AsyncOpenAI` with `base_url="https://openrouter.ai/api/v1"`
- **Default model**: `openai/gpt-4o`
- **Behavior**: Identical to OpenAIProvider -- same message format, same response parsing, same usage mapping
- **Difference**: Routes requests through OpenRouter's model routing layer

### Provider Factory

```python
def _create_provider(
    provider_name: str,
    settings: WorkerSettings,
    key_manager: KeyManager,
    api_key: str | None = None,
) -> BaseProvider
```

Match-case factory that instantiates the correct provider class. GLM receives `endpoint` from settings; all others receive `api_key` and `timeout`.

---

## 6. Configuration (`config.py`)

### Class: `WorkerSettings`

All settings loaded from environment variables (or `.env` file) via pydantic-settings.

#### Redis / Dragonfly

| Env Var            | Default                  | Description                                                |
|--------------------|--------------------------|------------------------------------------------------------|
| `REDIS_URL`        | `redis://dragonfly:6379` | Dragonfly connection URL                                   |
| `QUEUE_NAME`       | `ai_jobs`                | Redis list name for job queue                              |
| `RETRY_QUEUE_NAME` | `ai_jobs_retry`          | Retry queue name (reserved, not currently used separately) |
| `RESULT_TTL`       | `600`                    | Result cache TTL in seconds                                |
| `SHORT_CACHE_TTL`  | `60`                     | Short cache TTL                                            |

#### Worker Behavior

| Env Var              | Default | Description                          |
|----------------------|---------|--------------------------------------|
| `WORKER_CONCURRENCY` | `10`    | Number of parallel worker coroutines |
| `MAX_RETRIES`        | `3`     | Max retry attempts per job           |
| `BASE_BACKOFF`       | `1.0`   | Base backoff multiplier in seconds   |
| `POLL_TIMEOUT`       | `5`     | BRPOP timeout in seconds             |

#### Observability

| Env Var         | Default               | Description                      |
|-----------------|-----------------------|----------------------------------|
| `OTEL_ENDPOINT` | `otel-collector:4317` | OpenTelemetry collector endpoint |
| `METRICS_PORT`  | `9090`                | Prometheus `/metrics` port       |

#### Provider API Keys

All key env vars accept comma-separated values for multi-key pools:

| Env Var               | Default | Description            |
|-----------------------|---------|------------------------|
| `GLM_API_KEYS`        | `""`    | Z.AI/GLM API keys      |
| `OPENAI_API_KEYS`     | `""`    | OpenAI API keys        |
| `ANTHROPIC_API_KEYS`  | `""`    | Anthropic API keys     |
| `GEMINI_API_KEYS`     | `""`    | Google Gemini API keys |
| `OPENROUTER_API_KEYS` | `""`    | OpenRouter API keys    |

#### Provider-Specific

| Env Var             | Default                          | Description       |
|---------------------|----------------------------------|-------------------|
| `GLM_ENDPOINT`      | `https://api.z.ai/api/anthropic` | Z.AI API endpoint |
| `GLM_DEFAULT_MODEL` | `glm-5`                          | Default GLM model |

#### Upstream Concurrency Limits

| Env Var                  | Default | Description                                               |
|--------------------------|---------|-----------------------------------------------------------|
| `UPSTREAM_MODEL_LIMITS`  | `""`    | Per-model concurrency limits, format: `model1:N,model2:M` |
| `UPSTREAM_DEFAULT_LIMIT` | `1`     | Default concurrency limit for unlisted models             |
| `UPSTREAM_GLOBAL_LIMIT`  | `0`     | Global concurrency cap across all models (0 = unlimited)  |

#### Provider Defaults

| Env Var            | Default | Description                               |
|--------------------|---------|-------------------------------------------|
| `DEFAULT_PROVIDER` | `glm`   | Default provider when job doesn't specify |
| `DEFAULT_MODEL`    | `glm-5` | Default model when job doesn't specify    |
| `REQUEST_TIMEOUT`  | `120`   | Provider API call timeout in seconds      |

#### Per-Provider RPM Limits

| Env Var               | Default | Description                                                |
|-----------------------|---------|------------------------------------------------------------|
| `PROVIDER_RPM_LIMITS` | `""`    | Per-provider requests-per-minute, format: `provider:N,...` |

### Computed Properties

- `glm_key_list`, `openai_key_list`, `anthropic_key_list`, `gemini_key_list`, `openrouter_key_list` -- parsed key lists
- `model_limits` -- parsed `dict[str, int]` of model concurrency limits
- `rpm_limits` -- parsed `dict[str, int]` of provider RPM limits
- `available_providers` -- list of providers with at least one key configured

### Module Constants

```python
PROVIDER_FALLBACK_ORDER = ["glm", "openai", "anthropic", "gemini", "openrouter"]

MODEL_FALLBACK_ORDER = [
    "glm-5.1", "glm-5-turbo", "glm-5",
    "glm-4.7", "glm-4.6",
]
```

---

## 7. Prometheus Metrics (`prom_metrics.py`)

All metrics use the `ai_worker_` prefix. Shared between `main.py` and `worker.py` via module-level imports.

### Counters

| Metric                            | Type    | Labels              | Description                              |
|-----------------------------------|---------|---------------------|------------------------------------------|
| `ai_worker_jobs_processed_total`  | Counter | `provider`          | Total jobs successfully completed        |
| `ai_worker_jobs_failed_total`     | Counter | (none)              | Total jobs that failed after all retries |
| `ai_worker_jobs_retried_total`    | Counter | (none)              | Total jobs re-enqueued for retry         |
| `ai_worker_provider_errors_total` | Counter | `provider`          | Total provider call errors               |
| `ai_worker_rate_limit_hits_total` | Counter | `provider`          | Total rate limit (429/overloaded) errors |
| `ai_worker_token_input_total`     | Counter | `provider`, `model` | Cumulative input tokens consumed         |
| `ai_worker_token_output_total`    | Counter | `provider`, `model` | Cumulative output tokens generated       |

### Histograms

| Metric                               | Type      | Labels     | Buckets (seconds)                                      | Description                           |
|--------------------------------------|-----------|------------|--------------------------------------------------------|---------------------------------------|
| `ai_worker_provider_latency_seconds` | Histogram | `provider` | 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0, 120.0 | Provider request latency distribution |

### Gauges

| Metric                  | Type  | Labels | Description                         |
|-------------------------|-------|--------|-------------------------------------|
| `ai_worker_queue_depth` | Gauge | (none) | Current number of jobs in the queue |
| `ai_worker_active`      | Gauge | (none) | Number of active worker coroutines  |

### Internal Metrics Endpoint (port 9091)

`GET /metrics-internal` returns a JSON snapshot with latency percentiles:

```json
{
  "jobs_processed": 1234,
  "jobs_failed": 5,
  "jobs_retried": 12,
  "queue_depth": 3,
  "provider_latency": {
    "glm": {"p50": 0.8, "p95": 2.1, "p99": 5.3, "avg": 1.2, "count": 1000}
  },
  "provider_errors": {"glm": 2},
  "rate_limit_hits": {"glm": 5}
}
```

Latency samples are kept as a sliding window of the last 1000 per provider.

---

## 8. Architecture Diagram

```
                              +-----------------+
                              |  Go API Gateway |
                              |  (api-gateway)  |
                              +--------+--------+
                                       |
                          LPUSH ai_jobs  |  GET result:{id}
                                       |
                              +--------v--------+
                              |   Dragonfly /    |
                              |   Redis (queue + |
                              |   result cache)  |
                              +--------+--------+
                                       |
                         BRPOP ai_jobs  |  SET result:{id}
                                       |
                        +--------------v--------------+
                        |       AI Worker (Python)     |
                        |        main.py               |
                        |                              |
                        |  +---------+  +----------+  |
|  |Prometheus|  | Internal |  |
|  | :9090    |  | Metrics  |  |
|  | /metrics |  | :9091    |  |
                        |  +---------+  +----------+  |
                        |                              |
                        |  Worker.run_loop() x N       |
                        |  +------------------------+  |
|  | Concurrency Semaphores  |  |
|  |  per-model + global     |  |
                        |  +------------------------+  |
                        |  +------------------------+  |
|  | ProviderRateLimiter     |  |
|  |  sliding window RPM     |  |
                        |  +------------------------+  |
                        |  +------------------------+  |
|  | KeyManager             |  |
|  |  key pools + cooldowns  |  |
                        |  +------------------------+  |
                        |                              |
                        |  Provider Fallback Chain:    |
                        |  glm -> openai -> anthropic  |
                        |  -> gemini -> openrouter     |
                        +------+----------+----------+-+
|          |          |
                    +----------+  +-------+  +-------+----------+
|             |          |                    |
              +-----v----+  +----v-----+ +--v------+  +--------v---+
| Z.AI/GLM |  | OpenAI   | |Anthropic|  | Gemini     |
| (Anthro   |  | (OpenAI  | |(Anthro   |  | (Google    |
|  SDK,     |  |  SDK)    | | SDK)     |  |  genai)    |
              |  custom   |  +----------+ +----------+  +------------+
              |  base_url)|                            +------------+
              +----------+                            | OpenRouter  |
                                                      | (OpenAI SDK,|
                                                      |  custom URL)|
                                                      +------------+
```

### Data Flow

```
1. Client -> Go Gateway: POST /v1/messages (or agent endpoint)
2. Go Gateway -> Dragonfly: LPUSH ai_jobs {"request_id", "provider", "model", "messages", ...}
3. AI Worker -> Dragonfly: BRPOP ai_jobs (blocks until job available)
4. AI Worker: Parse job -> acquire concurrency slot -> acquire RPM slot -> get API key
5. AI Worker -> Upstream Provider: HTTP request (Anthropic/OpenAI/Gemini/Z.AI/OpenRouter)
6. Upstream Provider -> AI Worker: LLM response (content + usage)
7. AI Worker -> Dragonfly: SET result:{request_id} {"status":"completed", "content":..., "usage":...}
8. Go Gateway -> Dragonfly: GET result:{request_id} (polling)
9. Go Gateway -> Client: Return response
```

### Retry Flow

```
Job fails (provider error)
  |
  +-- Is it rate limit (429/overloaded)?
  |     YES -> cooldown_key(60s) -> try next provider in fallback chain
  |     NO  -> try next provider in fallback chain
  |
  +-- All providers exhausted?
        YES -> retry_count < max_retries?
              YES -> sleep(backoff * 2^retry + jitter) -> LPUSH back to queue
              NO  -> SET result:{id} {"status":"error", "error":"..."}
```

---

## 9. Docker Build (`Dockerfile`)

Multi-stage build:
- **Builder stage**: `python:3.12-slim` with gcc/libffi-dev for C extension compilation. Installs deps to `/install` prefix.
- **Runtime stage**: `python:3.12-slim` with curl (for healthcheck). Copies `/install` to `/usr/local`. Creates non-root `worker` user.
- **Exposed ports**: 9090 (Prometheus), 9091 (internal metrics)
- **Healthcheck**: `curl -f http://localhost:9091/metrics-internal` every 15s
- **Entrypoint**: `python -u -O main.py` (unbuffered stdout, optimized bytecode)

---

## 10. Dependencies (`requirements.txt`)

| Package                                          | Purpose                                                    |
|--------------------------------------------------|------------------------------------------------------------|
| `redis[hiredis]>=5.0.0`                          | Async Redis client (Dragonfly connection)                  |
| `httpx>=0.27.0`                                  | HTTP client (used by provider SDKs)                        |
| `anthropic>=0.40.0`                              | Anthropic SDK (used by AnthropicProvider and GLMProvider)  |
| `openai>=1.30.0`                                 | OpenAI SDK (used by OpenAIProvider and OpenRouterProvider) |
| `google-generativeai>=0.7.0`                     | Google Gemini SDK                                          |
| `opentelemetry-api>=1.25.0`                      | OpenTelemetry API (tracing)                                |
| `opentelemetry-sdk>=1.25.0`                      | OpenTelemetry SDK                                          |
| `opentelemetry-exporter-otlp-proto-grpc>=1.25.0` | OTLP gRPC exporter                                         |
| `prometheus-client>=0.20.0`                      | Prometheus metrics                                         |
| `pydantic>=2.7.0`                                | Data validation                                            |
| `pydantic-settings>=2.3.0`                       | Env-based config loading                                   |
| `structlog>=24.2.0`                              | Structured logging                                         |
