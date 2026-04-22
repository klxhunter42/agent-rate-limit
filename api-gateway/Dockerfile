# =============================================================================
# Production-grade multi-stage Dockerfile for API Gateway (Go)
# Optimized for minimal image size and fast startup
# =============================================================================

# --- Build stage ---
FROM golang:1.26-alpine AS builder

RUN apk --no-cache add git ca-certificates

WORKDIR /build

# Dependency caching layer
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Build with optimizations
COPY . .
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" \
    -o /bin/api-gateway .

# --- Runtime stage ---
FROM alpine:3.20

RUN apk --no-cache add ca-certificates curl && \
    addgroup -S gateway && adduser -S gateway -G gateway

COPY --from=builder /bin/api-gateway /app/api-gateway

USER gateway:gateway

EXPOSE 8080

ENTRYPOINT ["/app/api-gateway"]
