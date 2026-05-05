# =============================================================================
# Production-grade multi-stage Dockerfile for API Gateway (Go)
# Optimized for minimal image size and fast startup
# Supports cross-platform build: change DOCKER_PLATFORM in .env
#   linux/arm64  -> native ARM (Apple Silicon)
#   linux/amd64  -> cross-compile for Intel/AMD servers
# =============================================================================

# --- UI build stage (native platform for speed) ---
FROM --platform=$BUILDPLATFORM oven/bun:1 AS ui-builder
WORKDIR /build/ui
COPY ui/package.json ui/bun.lock ./
RUN bun install --frozen-lockfile
COPY ui/ .
RUN bun run build --outDir /build/api-gateway/static

# --- Go build stage (native platform, cross-compile binary) ---
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG TARGETPLATFORM
RUN apk --no-cache add git ca-certificates

WORKDIR /build

COPY api-gateway/go.mod api-gateway/go.sum ./
RUN go mod download

COPY api-gateway/ .
COPY --from=ui-builder /build/api-gateway/static ./static/
RUN GOARCH=$(echo ${TARGETPLATFORM} | cut -d/ -f2) \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath \
    -ldflags="-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" \
    -o /bin/api-gateway .

# --- Runtime stage (target platform) ---
FROM --platform=$TARGETPLATFORM alpine:3.20

RUN apk --no-cache add ca-certificates curl nodejs && \
 addgroup -S gateway && adduser -S gateway -G gateway

COPY --from=builder /bin/api-gateway /app/api-gateway
COPY api-gateway/sidecar/ /app/sidecar/
RUN chmod +x /app/sidecar/entrypoint.sh

USER gateway:gateway

EXPOSE 8080

ENTRYPOINT ["/app/sidecar/entrypoint.sh"]
