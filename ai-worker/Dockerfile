# =============================================================================
# Production-grade multi-stage Dockerfile for AI Worker (Python)
# Optimized for minimal image size and fast startup
# =============================================================================

# --- Builder stage ---
FROM python:3.12-slim AS builder

RUN apt-get update && \
    apt-get install -y --no-install-recommends gcc libffi-dev && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /build

COPY requirements.txt .

# Install to a separate prefix for clean copy
RUN pip install --no-cache-dir --prefix=/install -r requirements.txt

# --- Runtime stage ---
FROM python:3.12-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends curl && \
    rm -rf /var/lib/apt/lists/* && \
    groupadd -r worker && useradd -r -g worker worker

WORKDIR /app

# Copy installed packages from builder
COPY --from=builder /install /usr/local

# Copy application code
COPY --chown=worker:worker . .

USER worker

EXPOSE 9090 9091

HEALTHCHECK --interval=15s --timeout=5s --retries=3 --start-period=10s \
    CMD curl -f http://localhost:9091/metrics-internal || exit 1

CMD ["python", "-u", "-O", "main.py"]
