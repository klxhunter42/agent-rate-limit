# 07 - Distributed Rate Limiter Service

## Table of Contents

- [1. Architecture Overview](#1-architecture-overview)
- [2. Rate Limiting Algorithms](#2-rate-limiting-algorithms)
- [3. Distributed Coordination](#3-distributed-coordination)
- [4. API Endpoints](#4-api-endpoints)
- [5. Data Store](#5-data-store)
- [6. Configuration](#6-configuration)
- [7. Kubernetes Deployment](#7-kubernetes-deployment)
- [8. Scripts](#8-scripts)
- [9. Examples](#9-examples)
- [10. Text Architecture Diagram](#10-text-architecture-diagram)

---

## 1. Architecture Overview

### 1.1 Why a Separate Java Service

The Distributed Rate Limiter (DRL) is a standalone Java 21 / Spring Boot 3.5.7 service (`dev.bnacar.distributedratelimiter`) that provides production-grade distributed rate limiting with a Redis backend. It exists as a separate service from the Go API gateway for several reasons:

- **Shared rate state**: Multiple gateway instances need a single source of truth for rate counters. Redis provides this via atomic Lua scripts.
- **Independent scaling**: Rate limiting can be scaled horizontally (3-10 replicas) independently from the gateway.
- **Polyglot architecture**: The Go gateway handles request proxying and LLM orchestration; the Java DRL handles complex rate limit math, composite algorithms, adaptive ML, and geographic rules. Each uses the best language for its domain.
- **Operational isolation**: Rate limiter failures (Redis down, algorithm bugs) do not take down the proxy gateway. The DRL degrades gracefully to in-memory fallback.

### 1.2 Communication with Go Gateway

The Go gateway calls the DRL via HTTP REST:

```
Go gateway --POST /api/ratelimit/check--> DRL service --> Redis (Lua scripts)
```

- **Protocol**: REST over HTTP (JSON)
- **Latency target**: sub-millisecond for in-memory, <5ms for Redis-backed
- **Failure mode**: The DRL service fails closed (denies request on Redis failure). The gateway itself is fail-open: if the DRL is unreachable, the gateway proceeds without rate limiting.

### 1.3 Package Structure

```
dev.bnacar.distributedratelimiter
|-- DistributedRateLimiterApplication.java    // Spring Boot entry point
|-- adaptive/                                  // ML-driven adaptive rate limiting
|   |-- AdaptiveRateLimitEngine.java           // Main orchestrator, scheduled evaluation
|   |-- AdaptiveMLModel.java                   // ML prediction model
|   |-- AdaptationDecision.java                // Decision data class
|   |-- AdaptationSignal.java                  // Input signal
|   |-- AnomalyDetector.java                   // Anomaly detection
|   |-- AnomalyScore.java                      // Anomaly score
|   |-- SystemHealth.java                      // System health metrics
|   |-- SystemMetricsCollector.java            // Collects JVM/system metrics
|   |-- TrafficPattern.java                    // Traffic pattern data
|   |-- TrafficPatternAnalyzer.java            // Analyzes request patterns
|   |-- UserBehavior.java                      // User behavior data
|   |-- UserBehaviorModeler.java               // Models per-user request patterns
|-- config/                                    // Spring configuration classes
|   |-- AdminAuthConfiguration.java
|   |-- FilterConfiguration.java
|   |-- GeographicRateLimitingConfiguration.java
|   |-- OpenApiConfig.java
|   |-- SecurityConfiguration.java
|   |-- WebCorsConfiguration.java
|-- controller/                                // REST controllers
|   |-- RateLimitController.java               // POST /api/ratelimit/check
|   |-- RateLimitConfigController.java         // /api/ratelimit/config/*
|   |-- AdminController.java                   // /admin/*
|   |-- ScheduleController.java                // /api/ratelimit/schedule/*
|   |-- AdaptiveRateLimitController.java       // /api/ratelimit/adaptive/*
|   |-- GeographicRateLimitController.java     // /api/ratelimit/geographic/*
|   |-- MetricsController.java                 // GET /metrics
|   |-- BenchmarkController.java              // /api/benchmark/*
|   |-- PerformanceController.java             // /api/performance/*
|-- geo/                                       // Geographic rate limiting
|   |-- GeoLocationService.java                // MaxMind GeoIP2 lookup
|   |-- GeographicRateLimitService.java        // Geo-aware rate checking
|   |-- GeographicConfigurationResolver.java   // Geo rule resolution
|   |-- GeographicAwareConfigurationResolver.java
|   |-- CDNHeaderParser.java                   // CloudFlare/CloudFront/Azure CDN headers
|-- models/                                    // Request/response DTOs
|   |-- RateLimitRequest.java
|   |-- RateLimitResponse.java
|   |-- CompositeRateLimitResponse.java
|   |-- AdaptiveInfo.java / AdaptiveStatus.java
|   |-- ScheduleRequest.java / ScheduleResponse.java
|   |-- EmergencyScheduleRequest.java
|   |-- GeographicRateLimitConfig.java / GeographicRateLimitResponse.java
|   |-- MetricsResponse.java
|   |-- BenchmarkRequest.java / BenchmarkResponse.java
|   |-- PerformanceBaseline.java / PerformanceRegressionResult.java
|   |-- AdminLimitRequest.java / AdminLimitResponse.java
|   |-- AdminKeysResponse.java / AdminKeyStats.java
|   |-- ConfigurationResponse.java / ConfigurationStats.java
|   |-- DefaultConfigRequest.java
|   |-- GeoLocation.java
|   |-- ComplianceZone.java
|-- monitoring/
|   |-- MetricsService.java                    // Per-key metrics, Redis health checks
|   |-- PerformanceRegressionService.java      // Baseline storage & regression detection
|-- observability/
|   |-- CorrelationIdFilter.java               // MDC correlation ID propagation
|-- ratelimit/                                 // Core rate limiting engine
|   |-- RateLimiter.java                       // Interface: tryConsume, getCurrentTokens, etc.
|   |-- RateLimitAlgorithm.java                // Enum: TOKEN_BUCKET, SLIDING_WINDOW, FIXED_WINDOW, LEAKY_BUCKET, COMPOSITE
|   |-- RateLimitConfig.java                   // Per-key config: capacity, refillRate, cleanupIntervalMs, algorithm
|   |-- RateLimiterConfiguration.java          // @ConfigurationProperties(prefix="ratelimiter")
|   |-- ConfigurationResolver.java             // Key -> config resolution with precedence rules
|   |-- RateLimiterService.java                // Main service: in-memory ConcurrentHashMap of buckets
|   |-- DistributedRateLimiterService.java     // @Primary: Redis + in-memory fallback
|   |-- RateLimiterBackend.java                // Strategy interface for backends
|   |-- InMemoryRateLimiterBackend.java        // ConcurrentHashMap-based backend
|   |-- RedisRateLimiterBackend.java           // Redis-backed backend
|   |-- RedisRateLimiterConfiguration.java     // RedisTemplate bean, async executor
|   |-- TokenBucket.java                       // In-memory token bucket
|   |-- SlidingWindow.java                     // In-memory sliding window (ConcurrentLinkedDeque)
|   |-- FixedWindow.java                       // In-memory fixed window (AtomicInteger)
|   |-- LeakyBucket.java                       // In-memory leaky bucket (BlockingQueue + ScheduledExecutor)
|   |-- RedisTokenBucket.java                  // Redis token bucket via Lua script
|   |-- RedisFixedWindow.java                  // Redis fixed window via Lua script
|   |-- RedisLeakyBucket.java                  // Redis leaky bucket via Lua script
|   |-- CompositeRateLimiter.java              // Multi-algorithm composite
|   |-- CompositeRateLimiterService.java       // Composite rate limit checking service
|   |-- CompositeRateLimitConfig.java          // Composite config: limits[], combinationLogic, weights
|   |-- CombinationLogic.java                  // Enum: ALL_MUST_PASS, ANY_CAN_PASS, WEIGHTED_AVERAGE, HIERARCHICAL_AND, PRIORITY_BASED
|   |-- LimitComponent.java                    // Named rate limiter with weight, priority, scope
|-- schedule/
|   |-- RateLimitSchedule.java                 // Schedule entity
|   |-- ScheduleManagerService.java            // Cron-based schedule evaluation
|   |-- ScheduleType.java                      // ONE_TIME, RECURRING, EVENT_DRIVEN
|   |-- TransitionConfig.java                  // Grace period between schedule transitions
|-- security/
|   |-- ApiKeyService.java                     // API key validation
|   |-- IpAddressExtractor.java                // X-Forwarded-For / X-Real-IP extraction
|   |-- IpSecurityService.java                 // IP whitelist/blacklist
|   |-- SecurityFilter.java                    // Security headers filter
```

### 1.4 Key Dependencies

| Dependency                     | Version   | Purpose                                |
|--------------------------------|-----------|----------------------------------------|
| Spring Boot                    | 3.5.7     | Web, Redis, Actuator, Validation       |
| spring-boot-starter-data-redis | (managed) | Redis via Lettuce client               |
| commons-pool2                  | (managed) | Lettuce connection pooling             |
| MaxMind GeoIP2                 | 4.4.0     | Geographic IP lookup                   |
| springdoc-openapi              | 2.7.0     | Swagger UI at `/swagger-ui/index.html` |
| logstash-logback-encoder       | 8.1       | JSON structured logging                |
| micrometer-tracing             | (managed) | Distributed tracing                    |
| Gatling                        | 3.14.7    | Load testing (test scope)              |

---

## 2. Rate Limiting Algorithms

The DRL implements five rate limiting algorithms. Each implements the `RateLimiter` interface:

```java
public interface RateLimiter {
    boolean tryConsume(int tokens);
    int getCurrentTokens();
    int getCapacity();
    int getRefillRate();
    long getLastRefillTime();
}
```

### 2.1 Token Bucket (`TOKEN_BUCKET`)

**Class**: `TokenBucket` (in-memory), `RedisTokenBucket` (Redis)

**Algorithm**:
1. Bucket starts full at `capacity` tokens.
2. Tokens refill at `refillRate` tokens/second based on elapsed time since last refill.
3. `tryConsume(n)` refills first, then deducts if `currentTokens >= n`.
4. Tokens capped at `capacity`.

**Characteristics**:
- Allows burst traffic up to bucket capacity
- Steady refill rate smooths long-term throughput
- Best for: API rate limiting where bursts are acceptable

**In-memory**: `synchronized` methods, `currentTokens` field
**Redis**: Lua script `scripts/token-bucket.lua` - atomic HMGET/HMSET with time-based refill calculation. 24-hour key TTL.

**Redis Lua script logic** (`token-bucket.lua`):
```
KEYS[1] = bucket key
ARGV[1] = capacity, ARGV[2] = refillRate, ARGV[3] = tokensToConsume, ARGV[4] = currentTimeMs
1. HMGET key -> tokens, last_refill
2. Calculate tokensToAdd = floor(elapsed_ms / 1000 * refillRate)
3. currentTokens = min(capacity, currentTokens + tokensToAdd)
4. If currentTokens >= tokensToConsume: deduct, return {1, currentTokens, ...}
5. Else: return {0, currentTokens, ...}
6. HMSET key -> tokens, last_refill, capacity, refill_rate
7. EXPIRE key 86400
```

### 2.2 Sliding Window (`SLIDING_WINDOW`)

**Class**: `SlidingWindow` (in-memory only)

**Algorithm**:
1. Maintains a `ConcurrentLinkedDeque<RequestRecord>` of timestamped requests within a 1-second window.
2. `tryConsume(n)` first cleans up expired records (older than `windowSizeMs`), then checks if `currentCount + n <= capacity`.
3. If allowed, adds a new `RequestRecord` with current timestamp.

**Characteristics**:
- Most precise rate limiting - no boundary reset spikes
- Memory: O(n) for active requests within the window
- Best for: Consistent rate enforcement, sustained burst handling

**Note**: Redis backend currently falls back to token bucket for sliding window (noted as TODO in `RedisRateLimiterBackend`).

### 2.3 Fixed Window (`FIXED_WINDOW`)

**Class**: `FixedWindow` (in-memory), `RedisFixedWindow` (Redis)

**Algorithm**:
1. Time is divided into fixed windows (default 60 seconds).
2. Counter resets at each window boundary: `windowStart = floor(currentTime / windowDuration) * windowDuration`.
3. `tryConsume(n)` increments counter if `currentCount + n <= capacity`.

**Characteristics**:
- Memory efficient - single AtomicInteger per key
- Predictable reset times
- Boundary spike problem: double capacity at window edges
- Best for: Basic rate limiting with simple accounting

**Redis Lua script** (`fixed-window.lua`):
```
KEYS[1] = window key
ARGV[1] = capacity, ARGV[2] = windowDurationMs, ARGV[3] = tokensToConsume, ARGV[4] = currentTimeMs
1. Calculate currentWindowStart = floor(currentTime / windowDuration) * windowDuration
2. If stored windowStart != currentWindowStart: reset count = 0
3. If capacity - count >= tokensToConsume: increment count, return {1, remaining, count, ...}
4. Else: return {0, remaining, count, ...}
5. TTL = windowDuration + 3600 seconds
```

### 2.4 Leaky Bucket (`LEAKY_BUCKET`)

**Class**: `LeakyBucket` (in-memory), `RedisLeakyBucket` (Redis)

**Algorithm**:
1. Requests enter a queue with fixed `queueCapacity`.
2. Queue drains at `leakRatePerSecond` (constant processing rate).
3. `tryConsume(n)` estimates processing time based on queue depth. Rejects if queue full or estimated wait exceeds `maxQueueTimeMs` (default 5s).
4. `enqueueRequest(n)` returns `CompletableFuture<Boolean>` for async operation.

**Characteristics**:
- Constant output rate regardless of input bursts
- Traffic shaping - smooths irregular traffic
- Best for: Downstream system protection, SLA compliance

**In-memory**: `ScheduledExecutorService` processes queue at `1000 / leakRate / 10` ms intervals. Timeout cleanup every 1s.

**Redis Lua script** (`leaky-bucket.lua`):
```
KEYS[1] = bucket key, {key}:meta, {key}:queue
ARGV[1] = capacity, ARGV[2] = leakRate, ARGV[3] = tokensToConsume, ARGV[4] = currentTimeMs, ARGV[5] = maxQueueTimeMs
1. Cleanup expired requests from LPOP front of queue list
2. Drain requests based on elapsed time: tokensToProcess = floor(elapsed_ms / 1000 * leakRate)
3. If queue has capacity: RPUSH new request timestamps, return {1, queueSize, ...}
4. Estimated wait = queueSize / leakRate * 1000 ms
5. TTL 86400 on both metadata key and queue list
```

### 2.5 Composite (`COMPOSITE`)

**Class**: `CompositeRateLimiter`, `CompositeRateLimiterService`

**Algorithm**: Combines multiple rate limiters with configurable combination logic:

| Logic              | Behavior                                                                |
|--------------------|-------------------------------------------------------------------------|
| `ALL_MUST_PASS`    | AND - all components must allow (pre-check then consume all)            |
| `ANY_CAN_PASS`     | OR - at least one component must allow (consume from first that allows) |
| `WEIGHTED_AVERAGE` | Weighted score >= 50% threshold to allow                                |
| `HIERARCHICAL_AND` | Check in scope order: USER -> TENANT -> GLOBAL                          |
| `PRIORITY_BASED`   | Highest priority first, fail-fast on denial                             |

**Components**: Each `LimitComponent` has a name, `RateLimiter` instance, weight (double), priority (int), and scope (String: USER, TENANT, GLOBAL, etc.).

**Response**: Returns `CompositeRateLimitResponse` with per-component results, limiting component name, and combination score breakdown.

---

## 3. Distributed Coordination

### 3.1 Backend Strategy

The `DistributedRateLimiterService` (annotated `@Primary`, conditional on `ratelimiter.redis.enabled=true`) implements a two-tier backend:

```
Request --> DistributedRateLimiterService
              |-- primaryBackend: RedisRateLimiterBackend
              |-- fallbackBackend: InMemoryRateLimiterBackend
```

**Failover logic** (`isAllowed()`):
1. Resolve `RateLimitConfig` for key via `ConfigurationResolver`.
2. Try `primaryBackend.getRateLimiter(key, config).tryConsume(tokens)`.
3. On exception from Redis: switch to `fallbackBackend`, try in-memory.
4. On fallback failure: return `false` (fail closed).

**Health detection**: `RedisRateLimiterBackend.isAvailable()` pings Redis. The `MetricsService` runs a 30-second health check loop.

**State**: `volatile boolean usingFallback` - not thread-safe for concurrent switching but adequate for health-based failover.

### 3.2 Redis Atomicity

All Redis operations use Lua scripts executed atomically via `RedisTemplate.execute(RedisScript, keys, args)`. This ensures:
- Read-modify-write is atomic (no race between HMGET and HMSET).
- No token overconsumption under concurrent access.
- All keys have TTL (24h for token bucket/leaky bucket, window + 1h for fixed window).

### 3.3 Configuration Resolution Hierarchy

`ConfigurationResolver.resolveConfig(key)` checks in order:

1. **Active scheduled overrides** (highest priority) - from `ScheduleManagerService`
2. **Exact key match** - `ratelimiter.keys.<key>` in config
3. **Pattern match** (first match wins) - `ratelimiter.patterns.<pattern>` with `*` wildcard
4. **Default configuration** - `ratelimiter.capacity`, `ratelimiter.refillRate`, etc.

Results are cached in `ConcurrentHashMap<String, RateLimitConfig>` and cleared on config reload.

---

## 4. API Endpoints

### 4.1 Rate Limiting

#### `POST /api/ratelimit/check`

Core rate limiting endpoint. The Go gateway calls this for every request.

**Request** (`RateLimitRequest`):
```json
{
  "key": "user:123",
  "tokens": 1,
  "apiKey": "optional-api-key",
  "algorithm": "TOKEN_BUCKET",
  "compositeConfig": { ... },
  "clientInfo": {
    "sourceIP": "1.2.3.4",
    "countryCode": "US",
    "region": "CA",
    "city": "San Francisco",
    "timezone": "America/Los_Angeles",
    "headers": { "CF-IPCountry": "US" }
  }
}
```

| Field             | Type    | Required   | Description                                                                              |
|-------------------|---------|------------|------------------------------------------------------------------------------------------|
| `key`             | String  | Yes        | Rate limit key (e.g. `user:123`, `api:v1:users`)                                         |
| `tokens`          | Integer | Yes        | Tokens to consume (min 1)                                                                |
| `apiKey`          | String  | If enabled | API key for authentication                                                               |
| `algorithm`       | Enum    | No         | Override algorithm (TOKEN_BUCKET, SLIDING_WINDOW, FIXED_WINDOW, LEAKY_BUCKET, COMPOSITE) |
| `compositeConfig` | Object  | No         | Composite rate limit configuration                                                       |
| `clientInfo`      | Object  | No         | Geographic client info                                                                   |

**Response** (`RateLimitResponse`): HTTP 200 or 429
```json
{
  "key": "user:123",
  "tokensRequested": 1,
  "allowed": true,
  "adaptiveInfo": {
    "originalLimits": { "capacity": 10, "refillRate": 2 },
    "currentLimits": { "capacity": 15, "refillRate": 3 },
    "reasoning": "Increased based on consistent usage pattern",
    "timestamp": "2024-01-15T10:30:00Z",
    "nextEvaluation": "PT5M"
  }
}
```

**Status codes**: 200 (allowed), 429 (rate limited), 401 (invalid API key), 403 (IP blocked)

**Processing flow** in `RateLimitController.checkRateLimit()`:
1. Extract client IP via `IpAddressExtractor`
2. Check IP whitelist/blacklist via `IpSecurityService`
3. Validate API key via `ApiKeyService`
4. If geographic rate limiting enabled: try `GeographicRateLimitService.checkGeographicRateLimit()`
5. If composite request (`algorithm == COMPOSITE || compositeConfig != null`): use `CompositeRateLimiterService`
6. Otherwise: use `RateLimiterService.isAllowed()`
7. Record traffic event for adaptive learning
8. Build adaptive info if enabled

### 4.2 Configuration Management

#### `GET /api/ratelimit/config`
Returns current configuration: defaults, per-key overrides, patterns.

#### `POST /api/ratelimit/config/default`
Update default configuration. Body: `DefaultConfigRequest { capacity?, refillRate?, cleanupIntervalMs? }`

#### `POST /api/ratelimit/config/keys/{key}`
Set per-key config. Body: `KeyConfig { capacity, refillRate, cleanupIntervalMs?, algorithm? }`

#### `POST /api/ratelimit/config/patterns/{pattern}`
Set pattern config. Pattern supports `*` wildcard (e.g. `api:*`).

#### `DELETE /api/ratelimit/config/keys/{key}`
Remove per-key config.

#### `DELETE /api/ratelimit/config/patterns/{pattern}`
Remove pattern config.

#### `POST /api/ratelimit/config/reload`
Clear config cache and all active buckets.

#### `GET /api/ratelimit/config/stats`
Returns `ConfigurationStats { cacheSize, bucketCount, keyConfigCount, patternConfigCount }`.

### 4.3 Admin Operations

All admin endpoints require HTTP Basic Auth. Base path: `/admin`

#### `GET /admin/limits/{key}`
Get rate limit config for a specific key. Returns `AdminLimitResponse`.

#### `PUT /admin/limits/{key}`
Update rate limits for a key. Clears existing bucket and config cache. Body: `AdminLimitRequest { capacity, refillRate, cleanupIntervalMs?, algorithm? }`

#### `DELETE /admin/limits/{key}`
Remove key config and active bucket.

#### `GET /admin/keys`
List all active keys with stats: `AdminKeysResponse { keys: AdminKeyStats[], totalKeys, activeKeys }`

### 4.4 Schedule Management

Base path: `/api/ratelimit/schedule`

| Method | Path                 | Description                                             |
|--------|----------------------|---------------------------------------------------------|
| POST   | `/`                  | Create schedule                                         |
| GET    | `/`                  | List all schedules                                      |
| GET    | `/{name}`            | Get schedule by name                                    |
| PUT    | `/{name}`            | Update schedule                                         |
| DELETE | `/{name}`            | Delete schedule                                         |
| POST   | `/{name}/activate`   | Activate schedule                                       |
| POST   | `/{name}/deactivate` | Deactivate schedule                                     |
| POST   | `/emergency`         | Create emergency schedule (high priority, event-driven) |

**Schedule types** (`ScheduleType`):
- `ONE_TIME`: Active between `startTime` and `endTime`
- `RECURRING`: Active based on cron expression + timezone
- `EVENT_DRIVEN`: Like ONE_TIME but for emergency/manual triggers

### 4.5 Adaptive Rate Limiting

Base path: `/api/ratelimit/adaptive`

| Method | Path              | Description                 |
|--------|-------------------|-----------------------------|
| GET    | `/{key}/status`   | Get adaptive status for key |
| POST   | `/{key}/override` | Set manual override         |
| DELETE | `/{key}/override` | Remove manual override      |
| GET    | `/config`         | Get adaptive configuration  |

### 4.6 Geographic Rate Limiting

Base path: `/api/ratelimit/geographic` (only available when `ratelimiter.geographic.enabled=true`)

| Method | Path              | Description                         |
|--------|-------------------|-------------------------------------|
| GET    | `/rules`          | List all geographic rules           |
| POST   | `/rules`          | Add geographic rule                 |
| DELETE | `/rules/{ruleId}` | Remove geographic rule              |
| GET    | `/detect`         | Detect location for current request |
| GET    | `/stats`          | Cache statistics                    |
| POST   | `/cache/clear`    | Clear geographic caches             |

### 4.7 Metrics & Monitoring

| Method | Path                   | Description                              |
|--------|------------------------|------------------------------------------|
| GET    | `/metrics`             | Per-key metrics, Redis status, totals    |
| GET    | `/actuator/health`     | Application health (liveness, readiness) |
| GET    | `/actuator/metrics`    | Spring Boot metrics                      |
| GET    | `/actuator/prometheus` | Prometheus-compatible metrics            |

**MetricsResponse** structure:
```json
{
  "keyMetrics": {
    "user:123": {
      "allowedRequests": 145,
      "deniedRequests": 5,
      "lastAccessTime": 1705312200000
    }
  },
  "redisConnected": true,
  "totalAllowedRequests": 1450,
  "totalDeniedRequests": 50
}
```

### 4.8 Benchmark

| Method | Path                    | Description                   |
|--------|-------------------------|-------------------------------|
| POST   | `/api/benchmark/run`    | Execute performance benchmark |
| GET    | `/api/benchmark/health` | Benchmark service health      |

**BenchmarkRequest**: `concurrentThreads`, `requestsPerThread`, `durationSeconds`, `keyPrefix`, `tokensPerRequest`, `delayBetweenRequestsMs`

**BenchmarkResponse**: `totalRequests`, `successCount`, `errorCount`, `durationSeconds`, `throughputPerSecond`, `successRate`

### 4.9 Performance

| Method | Path                                          | Description                     |
|--------|-----------------------------------------------|---------------------------------|
| POST   | `/api/performance/baseline`                   | Store performance baseline      |
| POST   | `/api/performance/regression/analyze`         | Analyze regression vs baselines |
| POST   | `/api/performance/baseline/store-and-analyze` | Analyze then store              |
| GET    | `/api/performance/baseline/{testName}`        | Get historical baselines        |
| GET    | `/api/performance/trend/{testName}`           | Get performance trend           |
| GET    | `/api/performance/health`                     | Service health                  |

---

## 5. Data Store

### 5.1 Redis (Primary)

**Connection**: Spring Data Redis with Lettuce client, connection pooling via commons-pool2.

**Connection pool settings**:
```
spring.data.redis.lettuce.pool.max-active=20
spring.data.redis.lettuce.pool.max-idle=10
spring.data.redis.lettuce.pool.min-idle=5
spring.data.redis.lettuce.pool.max-wait=5000ms
```

**Serialization**: `StringRedisSerializer` for keys, `GenericJackson2JsonRedisSerializer` for values.

**Key schema**:
- Token bucket: `rate_limit:{key}` -> Hash `{tokens, last_refill, capacity, refill_rate}`
- Fixed window: `rate_limit:{key}` -> Hash `{count, window_start, capacity, window_duration}`
- Leaky bucket: `rate_limit:{key}:meta` -> Hash `{last_leak_time, queue_size, capacity, leak_rate}` + `rate_limit:{key}:queue` -> List of timestamps

**Key prefix**: configurable, defaults to `rate_limit:`

**TTL**: 24 hours (86400s) for token bucket and leaky bucket, `window_duration + 3600s` for fixed window.

**Lua scripts** (loaded from classpath):
- `scripts/token-bucket.lua`: Atomic token bucket with refill calculation
- `scripts/fixed-window.lua`: Atomic fixed window with boundary detection
- `scripts/leaky-bucket.lua`: Atomic leaky bucket with queue management and timeout cleanup

### 5.2 In-Memory (Fallback)

**Implementation**: `InMemoryRateLimiterBackend` with `ConcurrentHashMap<String, BucketHolder>`

**BucketHolder**: wraps `RateLimiter` instance + `RateLimitConfig` + `lastAccessTime`

**Cleanup**: `ScheduledExecutorService` runs every `cleanupIntervalMs` (default 60s). Removes entries where `currentTime - lastAccessTime > bucketCleanupInterval`.

**Thread safety**:
- `TokenBucket`: `synchronized` methods
- `SlidingWindow`: `synchronized` + `ConcurrentLinkedDeque` + `AtomicInteger`
- `FixedWindow`: `synchronized` + `AtomicInteger`
- `LeakyBucket`: `BlockingQueue` + `ScheduledExecutorService` + `AtomicLong`

---

## 6. Configuration

### 6.1 Application Properties

All configuration is under the `ratelimiter` prefix via `@ConfigurationProperties(prefix = "ratelimiter")`.

#### Core Rate Limiting

| Property                        | Default      | Description                  |
|---------------------------------|--------------|------------------------------|
| `ratelimiter.capacity`          | 10           | Default max tokens           |
| `ratelimiter.refillRate`        | 2            | Default tokens/second refill |
| `ratelimiter.cleanupIntervalMs` | 60000        | Bucket cleanup interval      |
| `ratelimiter.algorithm`         | TOKEN_BUCKET | Default algorithm            |
| `ratelimiter.redis.enabled`     | true         | Enable Redis backend         |

#### Per-Key Configuration

```properties
ratelimiter.keys.premium_user.capacity=50
ratelimiter.keys.premium_user.refillRate=10
ratelimiter.keys.premium_user.algorithm=SLIDING_WINDOW
```

#### Pattern Configuration

```properties
ratelimiter.patterns.api:*.capacity=100
ratelimiter.patterns.api:*.refillRate=50
ratelimiter.patterns.user:*.algorithm=FIXED_WINDOW
```

Pattern matching supports `*` wildcard: `user:*` matches `user:123`, `api:v1:*` matches `api:v1:users`.

#### Security

| Property                                   | Default                 | Description                |
|--------------------------------------------|-------------------------|----------------------------|
| `ratelimiter.security.api-keys.enabled`    | true                    | Enable API key auth        |
| `ratelimiter.security.api-keys.valid-keys` | api-key-1,api-key-2,... | Comma-separated valid keys |
| `ratelimiter.security.ip.whitelist`        | 127.0.0.1,::1           | IP whitelist               |
| `ratelimiter.security.ip.blacklist`        | (empty)                 | IP blacklist               |
| `ratelimiter.security.max-request-size`    | 1MB                     | Max request body           |
| `ratelimiter.security.headers.enabled`     | true                    | Security response headers  |

#### Geographic Rate Limiting

| Property                                       | Default                  | Description              |
|------------------------------------------------|--------------------------|--------------------------|
| `ratelimiter.geographic.enabled`               | true                     | Enable geo rate limiting |
| `ratelimiter.geographic.ip-database-path`      | /data/GeoLite2-City.mmdb | MaxMind DB path          |
| `ratelimiter.geographic.update-interval-hours` | 24                       | DB refresh interval      |
| `ratelimiter.geographic.cache-size`            | 10000                    | Location cache size      |
| `ratelimiter.geographic.cache-ttl-hours`       | 1                        | Cache TTL                |

Geographic rules are configured via properties (indexed):
```properties
ratelimiter.geographic.rules[0].name=eu-gdpr-limits
ratelimiter.geographic.rules[0].region=EU
ratelimiter.geographic.rules[0].compliance-zone=GDPR
ratelimiter.geographic.rules[0].key-pattern=api:*
ratelimiter.geographic.rules[0].capacity=500
ratelimiter.geographic.rules[0].refill-rate=50
ratelimiter.geographic.rules[0].priority=100
```

#### Adaptive Rate Limiting

| Property                                        | Default | Description                |
|-------------------------------------------------|---------|----------------------------|
| `ratelimiter.adaptive.enabled`                  | false   | Enable adaptive ML         |
| `ratelimiter.adaptive.evaluation-interval-ms`   | 300000  | Evaluation cycle (5 min)   |
| `ratelimiter.adaptive.min-confidence-threshold` | 0.7     | Min confidence to apply    |
| `ratelimiter.adaptive.max-adjustment-factor`    | 2.0     | Max capacity multiplier    |
| `ratelimiter.adaptive.min-capacity`             | 10      | Minimum capacity           |
| `ratelimiter.adaptive.max-capacity`             | 100000  | Maximum capacity           |
| `ratelimiter.adaptive.learning-window-days`     | 30      | Learning period            |
| `ratelimiter.adaptive.min-data-points`          | 1000    | Min data before adaptation |

#### Redis Connection

| Property                     | Default   | Description     |
|------------------------------|-----------|-----------------|
| `spring.data.redis.host`     | localhost | Redis host      |
| `spring.data.redis.port`     | 6379      | Redis port      |
| `spring.data.redis.password` | (none)    | Redis password  |
| `spring.data.redis.database` | 0         | Redis database  |
| `spring.data.redis.timeout`  | 2000ms    | Command timeout |

#### Observability

| Property                                   | Default | Description         |
|--------------------------------------------|---------|---------------------|
| `observability.correlation-id.enabled`     | true    | MDC correlation IDs |
| `observability.tracing.enabled`            | true    | Distributed tracing |
| `observability.structured-logging.enabled` | true    | JSON log format     |

### 6.2 Environment Variables

Used primarily in Kubernetes/Docker:

| Env Var                  | Purpose                                    |
|--------------------------|--------------------------------------------|
| `SPRING_DATA_REDIS_HOST` | Redis host override                        |
| `SPRING_DATA_REDIS_PORT` | Redis port override                        |
| `SPRING_PROFILES_ACTIVE` | Spring profile (docker, production)        |
| `JAVA_OPTS`              | JVM options (default: `-Xmx512m -Xms256m`) |
| `REDIS_PASSWORD`         | From K8s secret                            |
| `ADMIN_USERNAME`         | From K8s secret                            |
| `ADMIN_PASSWORD`         | From K8s secret                            |
| `API_KEYS`               | From K8s secret                            |
| `SERVER_SHUTDOWN`        | Set to `graceful` for K8s                  |
| `NVD_API_KEY`            | For OWASP dependency check                 |

---

## 7. Kubernetes Deployment

### 7.1 Base Manifests (`k8s/base/`)

| File              | Resource                               | Description                                             |
|-------------------|----------------------------------------|---------------------------------------------------------|
| `namespace.yaml`  | Namespace                              | `rate-limiter` namespace                                |
| `deployment.yaml` | Deployment + Service + PDB             | 3 replicas, ClusterIP service on port 80 -> 8080        |
| `configmap.yaml`  | ConfigMap                              | `application.properties` + `logback-spring.xml`         |
| `secrets.yaml`    | Secret                                 | Redis password, admin credentials, API keys             |
| `redis.yaml`      | Deployment + Service + PVC + ConfigMap | Redis 7.4 with persistence, redis-exporter sidecar      |
| `ingress.yaml`    | Ingress                                | NGINX ingress with TLS, rate limiting, security headers |
| `rbac.yaml`       | SA + Role + RoleBinding                | Minimal RBAC (get/list pods, configmaps, secrets)       |

### 7.2 Rate Limiter Deployment Details

```yaml
replicas: 3
strategy: RollingUpdate (maxUnavailable: 1, maxSurge: 1)
image: ghcr.io/uppnrise/distributed-rate-limiter:latest
containerPort: 8080
resources:
  requests: { memory: 512Mi, cpu: 200m }
  limits: { memory: 1Gi, cpu: 1000m }
JVM: -Xmx1g -Xms512m -XX:+UseG1GC -XX:MaxGCPauseMillis=100
securityContext:
  runAsNonRoot: true, runAsUser: 1001
  readOnlyRootFilesystem: true, drop ALL capabilities
probes:
  startup: /actuator/health (30s initial, 10s period, 30 failures)
  liveness: /actuator/health/liveness (60s initial, 30s period)
  readiness: /actuator/health/readiness (30s initial, 10s period)
lifecycle:
  preStop: sleep 15 (graceful drain)
terminationGracePeriodSeconds: 30
podAntiAffinity: preferred on different hosts
```

### 7.3 Redis Deployment

```yaml
replicas: 1 (Recreate strategy)
image: redis:7.4-alpine
command: redis-server --appendonly yes --maxmemory 1gb --maxmemory-policy allkeys-lru
resources:
  requests: { memory: 512Mi, cpu: 200m }
  limits: { memory: 1Gi, cpu: 500m }
persistence: PVC 10Gi (fast-ssd storage class)
sidecar: oliver006/redis_exporter:v1.66.0 (port 9121)
```

### 7.4 Environments (`k8s/environments/`)

#### Dev (`k8s/environments/dev/`)
- `kustomization.yaml` - references base with patches
- `namespace-patch.yaml` - dev namespace
- `deployment-patch.yaml` - reduced resources, 1 replica
- `configmap-patch.yaml` - dev config overrides
- `ingress-patch.yaml` - dev domain

#### Prod (`k8s/environments/prod/`)
- `kustomization.yaml` - references base with patches
- `deployment-patch.yaml` - production resource limits
- `hpa.yaml` - HorizontalPodAutoscaler
- `network-policy.yaml` - network segmentation

### 7.5 HPA (Production)

```yaml
apiVersion: autoscaling/v2
minReplicas: 3
maxReplicas: 10
metrics:
  - cpu: 70% average utilization
  - memory: 80% average utilization
  - pods: http_requests_per_second average 1k
behavior:
  scaleUp: 50% or 2 pods per 60s, max policy
  scaleDown: 25% per 60s, min policy, 300s stabilization
```

### 7.6 PodDisruptionBudget

```yaml
minAvailable: 2  # Out of 3 replicas
```

### 7.7 Monitoring (`k8s/monitoring/`)

- `prometheus/config.yaml` - scrape config for DRL and Redis exporter
- `prometheus/rules.yaml` - alert rules for rate limiter health
- `grafana/dashboard.json` - Grafana dashboard for visualization

---

## 8. Scripts

### 8.1 Build Script

**File**: `build-release.sh` (project root)

Builds the project with Maven, creates Docker image.

### 8.2 Deployment Script

**File**: `scripts/deployment/deploy.sh`

Usage:
```bash
./scripts/deployment/deploy.sh <environment> [IMAGE_TAG=tag] [DRY_RUN=true] [WAIT_TIMEOUT=600s]
```

Environments: `dev`, `staging`, `prod`

Steps:
1. Validate environment -> set namespace + kustomize path
2. Check prerequisites (kubectl, kustomize)
3. Validate manifests (kustomize build + kubectl dry-run)
4. Create namespace if needed
5. Update image tag if specified
6. Apply with kustomize
7. Wait for Redis and rate-limiter deployments
8. Verify deployment (pod count, health check)

### 8.3 Redis Backup Script

**File**: `scripts/backup/redis-backup.sh`

Steps:
1. Find Redis pod via label selector
2. Trigger `BGSAVE`
3. Wait for LASTSAVE timestamp change
4. `kubectl cp` dump.rdb from pod
5. Optional S3 upload
6. Cleanup backups older than `RETENTION_DAYS` (default 30)

Configurable via env vars: `NAMESPACE`, `BACKUP_DIR`, `RETENTION_DAYS`, `S3_BUCKET`

### 8.4 Redis Recovery Script

**File**: `scripts/backup/redis-recovery.sh`

Usage:
```bash
./scripts/backup/redis-recovery.sh -f <backup_file> [-n namespace] [-s s3-bucket] [-F]
```

Steps:
1. Download from S3 if needed
2. Verify RDB file
3. Confirm recovery (unless `--force`)
4. Scale Redis to 0, back to 1
5. Copy RDB into pod
6. Restart Redis pod to load backup
7. Verify (ping + DBSIZE + memory info)

### 8.5 Backup CronJob

**File**: `k8s/base/backup-cronjob.yaml`

Kubernetes CronJob that runs the backup script on schedule.

---

## 9. Examples

### 9.1 Basic Rate Limit Check

```bash
curl -X POST http://localhost:8080/api/ratelimit/check \
  -H "Content-Type: application/json" \
  -d '{"key":"user:123","tokens":1}'
```

### 9.2 With API Key

```bash
curl -X POST http://localhost:8080/api/ratelimit/check \
  -H "Content-Type: application/json" \
  -d '{"key":"user:123","tokens":1,"apiKey":"api-key-1"}'
```

### 9.3 Composite Rate Limiting

```bash
curl -X POST http://localhost:8080/api/ratelimit/check \
  -H "Content-Type: application/json" \
  -d '{
    "key": "enterprise:customer:123",
    "tokens": 1,
    "algorithm": "COMPOSITE",
    "compositeConfig": {
      "limits": [
        {"name": "api_calls", "algorithm": "TOKEN_BUCKET", "capacity": 10000, "refillRate": 1000, "scope": "API"},
        {"name": "bandwidth", "algorithm": "LEAKY_BUCKET", "capacity": 100, "refillRate": 50, "scope": "BANDWIDTH"}
      ],
      "combinationLogic": "ALL_MUST_PASS"
    }
  }'
```

### 9.4 Set Per-Key Configuration

```bash
curl -X POST http://localhost:8080/api/ratelimit/config/keys/premium_user \
  -H "Content-Type: application/json" \
  -d '{"capacity":100,"refillRate":20,"algorithm":"SLIDING_WINDOW"}'
```

### 9.5 Create Scheduled Rate Limit

```bash
curl -X POST http://localhost:8080/api/ratelimit/schedule \
  -H "Content-Type: application/json" \
  -d '{
    "name": "peak-hours",
    "keyPattern": "api:*",
    "type": "RECURRING",
    "cronExpression": "0 9-17 * * MON-FRI",
    "timezoneId": "America/New_York",
    "priority": 50,
    "limits": {"capacity": 500, "refillRate": 100}
  }'
```

### 9.6 Emergency Rate Limit

```bash
curl -X POST http://localhost:8080/api/ratelimit/schedule/emergency \
  -H "Content-Type: application/json" \
  -d '{
    "name": "ddos-mitigation",
    "keyPattern": "*",
    "capacity": 10,
    "refillRate": 1,
    "durationValue": "PT1H"
  }'
```

### 9.7 Geographic Rate Limiting

```bash
# Detect location
curl http://localhost:8080/api/ratelimit/geographic/detect?sourceIP=8.8.8.8

# Add geo rule
curl -X POST http://localhost:8080/api/ratelimit/geographic/rules \
  -H "Content-Type: application/json" \
  -d '{"name":"eu-limits","region":"EU","keyPattern":"api:*","capacity":500,"refillRate":50}'
```

### 9.8 Run Benchmark

```bash
curl -X POST http://localhost:8080/api/benchmark/run \
  -H "Content-Type: application/json" \
  -d '{"concurrentThreads":10,"requestsPerThread":100,"durationSeconds":30}'
```

### 9.9 Docker Compose

```bash
cd distributed-rate-limiter
docker-compose up -d

# Check health
curl http://localhost:8080/actuator/health

# Test rate limiting
curl -X POST http://localhost:8080/api/ratelimit/check \
  -H "Content-Type: application/json" \
  -d '{"key":"test","tokens":1}'
```

### 9.10 Client Examples

Client libraries are documented in `docs/examples/`:
- `curl-examples.md` - curl-based examples
- `go-client.md` - Go client integration
- `java-client.md` - Java client integration
- `python-client.md` - Python client integration
- `nodejs-client.md` - Node.js client integration

---

## 10. Text Architecture Diagram

```
                              +-----------------------+
                              |    Go API Gateway     |
                              |  (proxy & orchestrate)|
                              +-----------+-----------+
                                          |
                              HTTP POST /api/ratelimit/check
                                          |
                              +-----------v-----------+
                              |  Distributed Rate     |
                              |  Limiter Service      |
                              |  (Spring Boot 3.5.7)  |
                              |  Java 21              |
                              +---+-------+-------+---+
|       |       |
                     +------------+   +---+---+   +------------+
|                |       |                |
              +------v------+  +------v--+ +--v-------+  +----v------+
| RateLimiter |  | Config  | | Schedule |  | Adaptive  |
| Service     |  | Resolver| | Manager  |  | Engine    |
| (in-memory) |  |         | |          |  | (ML-based)|
              +------+------+  +----+----+ +----+-----+  +-----+----+
|               |           |              |
                     |        +------v------+
|        | Per-Key     |
|        | Patterns    |
|        | Defaults    |
                     |        +-------------+
                     |
              +------v------+
              | Distributed |
              | RateLimiter |
              | Service     |
              | (@Primary)  |
              +--+------+---+
                 |      |
        +--------v--+ ++---------+
| Redis     | | In-Memory|
| Backend   | | Backend  |
| (primary) | | (fallback|
        +-----+-----+ +----------+
              |
    +---------v----------+
    |   Redis 7.4         |
    |   + Lua Scripts     |
    |   + Connection Pool |
    |   + AOF Persistence |
    +---------------------+

    Lua Scripts (atomic operations):
    +---------------------+---------------------+---------------------+
| token-bucket.lua    | fixed-window.lua    | leaky-bucket.lua    |
| HMGET/HMSET         | HMGET/HMSET         | HMGET + LIST ops    |
| Time-based refill   | Window boundary     | Queue + drain rate  |
| TTL 24h             | TTL window+1h       | TTL 24h             |
    +---------------------+---------------------+---------------------+

    Algorithms:
    +-------------+-------------+-------------+-------------+-------------+
| TOKEN_BUCKET| SLIDING_WIN | FIXED_WINDOW| LEAKY_BUCKET| COMPOSITE   |
| Burst allow | Precise     | Simple      | Constant    | Multi-algo  |
| Steady fill | Rolling win | Fixed intv  | Queue drain | AND/OR/Wght |
    +-------------+-------------+-------------+-------------+-------------+

    Security Layer:
    +------------------+------------------+------------------+
| API Key Auth     | IP Whitelist/    | Geo Rate Limit   |
| (ApiKeyService)  | Blacklist        | (MaxMind GeoIP2) |
|                  | (IpSecuritySvc)  | (CDN headers)    |
    +------------------+------------------+------------------+

    Kubernetes:
    +----------------------------------------------------------+
    | Namespace: rate-limiter                                   |
    |                                                           |
    | +--------------------+  +------------------+              |
| | rate-limiter       |  | redis            |              |
| | Deployment (3 rep) |  | Deployment (1)   |              |
| | HPA: 3-10 pods     |  | PVC: 10Gi SSD    |              |
| | PDB: min 2         |  | exporter sidecar |              |
    | +--------+-----------+  +--------+---------+              |
|          |                        |                        |
    | +--------v-----------+  +---------v--------+              |
| | ClusterIP Service  |  | ClusterIP Service|              |
| | port 80 -> 8080    |  | port 6379, 9121  |              |
    | +--------------------+  +------------------+              |
    |                                                           |
    | +--------------------+  +--------------------+            |
| | NGINX Ingress      |  | ConfigMap/Secrets  |            |
| | TLS + rate limit   |  | RBAC               |            |
    | +--------------------+  +--------------------+            |
    +----------------------------------------------------------+

    Monitoring:
    +---------------------------+---------------------------+
| Spring Actuator           | Prometheus + Grafana      |
| /actuator/health          | Redis exporter (9121)     |
| /actuator/metrics         | Application metrics       |
| /actuator/prometheus      | Grafana dashboard JSON    |
| /metrics (custom)         | Alert rules               |
    +---------------------------+---------------------------+
```

---

## Appendix A: CI/CD Pipeline

**File**: `.github/workflows/ci-cd.yml`

The pipeline includes:
- Build with Maven (Java 21)
- Unit tests with JaCoCo coverage (min 50% instruction, 50% branch)
- SpotBugs static analysis
- PMD code quality
- Checkstyle (Google style)
- OWASP dependency check (CVSS > 7 fails build)
- Docker image build and push to GHCR
- Gatling load tests (manual trigger)

## Appendix B: Quality Tools

| Tool       | Purpose                | Config                             |
|------------|------------------------|------------------------------------|
| JaCoCo     | Code coverage          | 50% instruction + branch minimum   |
| SpotBugs   | Static analysis        | `spotbugs-exclude.xml`             |
| PMD        | Code quality           | `quickstart.xml` ruleset           |
| Checkstyle | Code style             | Google checks                      |
| OWASP      | Vulnerability scanning | `owasp-suppressions.xml`, CVSS > 7 |

## Appendix C: Docker

**Multi-stage Dockerfile**:
- Build stage: `eclipse-temurin:21-jdk` -> `./mvnw package -DskipTests`
- Runtime stage: `eclipse-temurin:21-jre` -> non-root user (UID 1001)
- Health check: `curl /actuator/health` every 30s
- JVM defaults: `-Xmx512m -Xms256m`
- Graceful shutdown support

**docker-compose.yml**: Redis 7 Alpine + App container, shared network, health checks, graceful shutdown.
