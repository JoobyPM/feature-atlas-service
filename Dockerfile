# syntax=docker/dockerfile:1

# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /feature-atlasd ./cmd/feature-atlasd

# Runtime stage
FROM alpine:3.19

WORKDIR /app

# Install CA certificates for TLS and wget for health checks
RUN apk add --no-cache ca-certificates

# Create non-root user
RUN adduser -D -u 1000 appuser

# Copy binary from builder
COPY --from=builder /feature-atlasd /app/feature-atlasd

# Certificates will be mounted at runtime
VOLUME /app/certs

# Switch to non-root user
USER appuser

# Expose ports: HTTPS (mTLS) and HTTP (health)
EXPOSE 8443 8080

# Health check uses the dedicated health port (no mTLS required)
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget --spider http://localhost:8080/healthz 2>/dev/null || exit 1

# Default command
ENTRYPOINT ["/app/feature-atlasd"]
CMD ["-listen", ":8443", "-health-port", ":8080", "-tls-cert", "/app/certs/server.crt", "-tls-key", "/app/certs/server.key", "-client-ca", "/app/certs/ca.crt", "-admin-cert", "/app/certs/admin.crt"]
