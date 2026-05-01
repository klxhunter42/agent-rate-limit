# =============================================================================
# Production-grade multi-stage Dockerfile for API Gateway (Go)
# Optimized for minimal image size and fast startup
# =============================================================================

# --- Build stage ---
FROM golang:1.25-alpine AS builder

RUN apk --no-cache add git ca-certificates

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)" \
    -o /bin/api-gateway .

# --- Runtime stage ---
FROM alpine:3.20

RUN apk --no-cache add ca-certificates curl nodejs && \
    addgroup -S gateway && adduser -S gateway -G gateway

COPY --from=builder /bin/api-gateway /app/api-gateway
COPY sidecar/ /app/sidecar/
RUN chmod +x /app/sidecar/entrypoint.sh

USER gateway:gateway

EXPOSE 8080

ENTRYPOINT ["/app/sidecar/entrypoint.sh"]
