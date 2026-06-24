# =============================================================================
# Production-grade multi-stage Dockerfile for API Gateway (Go)
# Optimized for minimal image size and fast startup
# =============================================================================

# --- UI build stage ---
FROM oven/bun:1 AS ui-builder
WORKDIR /build/ui
COPY ui/package.json ui/bun.lock ./
RUN NODE_TLS_REJECT_UNAUTHORIZED=0 bun install --frozen-lockfile
COPY ui/ .
RUN bun run build --outDir /build/api-gateway/static

# --- Go build stage ---
FROM golang:1.25-alpine AS builder

RUN sed -i 's|https://dl-cdn.alpinelinux.org|http://dl-cdn.alpinelinux.org|g' /etc/apk/repositories && \
    apk --no-cache add git ca-certificates vips-dev gcc musl musl-dev

WORKDIR /build

COPY api-gateway/ .
COPY --from=ui-builder /build/api-gateway/static ./static/
RUN CGO_ENABLED=1 go build \
 -mod=vendor \
 -trimpath \
 -ldflags="-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" \
 -o /bin/api-gateway .

# --- Runtime stage ---
FROM alpine:3.20

RUN sed -i 's|https://dl-cdn.alpinelinux.org|http://dl-cdn.alpinelinux.org|g' /etc/apk/repositories && \
    apk --no-cache add ca-certificates curl nodejs vips && \
 addgroup -S gateway && adduser -S gateway -G gateway

COPY --from=builder /bin/api-gateway /app/api-gateway
COPY api-gateway/sidecar/ /app/sidecar/
RUN chmod +x /app/sidecar/entrypoint.sh

USER gateway:gateway

EXPOSE 8080

ENTRYPOINT ["/app/sidecar/entrypoint.sh"]
