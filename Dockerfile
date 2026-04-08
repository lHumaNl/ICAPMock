# =============================================================================
# ICAP Mock Server - Production Dockerfile
# =============================================================================
# Multi-stage build for minimal image size and security
# 
# Build: docker build -t icap-mock:latest .
# Run:   docker run -p 1344:1344 -p 8080:8080 -p 9090:9090 icap-mock:latest
# =============================================================================

# -----------------------------------------------------------------------------
# Stage 1: Builder - Compile the Go binary
# -----------------------------------------------------------------------------
FROM golang:1.26-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set environment variables for reproducible builds
ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    GODEBUG=netdns=go

# Create working directory
WORKDIR /build

# Copy go mod files first for better layer caching
COPY go.mod go.sum ./

# Download dependencies with verification
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build arguments for version injection
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_DATE

# Build the binary with optimizations and version info
RUN go build \
    -ldflags="-s -w \
    -X main.version=${VERSION} \
    -X main.gitCommit=${GIT_COMMIT} \
    -X main.buildDate=${BUILD_DATE}" \
    -trimpath \
    -o /build/icap-mock \
    ./cmd/icap-mock

# Verify the binary works
RUN /build/icap-mock --version

# -----------------------------------------------------------------------------
# Stage 2: Runtime - Minimal Alpine image
# -----------------------------------------------------------------------------
FROM alpine:3.19 AS runtime

# Install runtime dependencies
RUN apk --no-cache add \
    ca-certificates \
    tzdata \
    wget \
    && rm -rf /var/cache/apk/*

# Create non-root user and group for security
RUN addgroup -g 1000 -S icapgroup && \
    adduser -u 1000 -S icapuser -G icapgroup -h /app -s /bin/false

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder --chown=1000:1000 /build/icap-mock /app/icap-mock

# Copy configuration files
COPY --chown=1000:1000 configs/ /app/configs/

# Create data directory for request storage
RUN mkdir -p /app/data/requests && \
    chown -R 1000:1000 /app/data

# Build arguments for labels
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_DATE

# OCI Image Labels (https://github.com/opencontainers/image-spec)
LABEL org.opencontainers.image.title="ICAP Mock Server" \
      org.opencontainers.image.description="Production-ready ICAP mock server for testing and development" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${GIT_COMMIT}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.vendor="ICAP Mock" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.source="https://github.com/icap-mock/icap-mock" \
      org.opencontainers.image.documentation="https://github.com/icap-mock/icap-mock#readme"

# Additional labels for container management
LABEL version="${VERSION}" \
      maintainer="ICAP Mock Team" \
      description="ICAP Mock Server - Testing and Development Tool"

# Switch to non-root user
USER 1000

# Expose ports:
# - 1344: ICAP protocol port
# - 8080: Health check endpoints (/health, /ready)
# - 9090: Prometheus metrics endpoint (/metrics)
EXPOSE 1344 8080 9090

# Health check using wget to the /health endpoint
# Checks every 30s, timeout after 3s, start after 5s, retry 3 times
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO/dev/null --tries=1 http://localhost:8080/health || exit 1

# Set environment variables
ENV GODEBUG=netdns=go

# Set the entrypoint and default command
ENTRYPOINT ["./icap-mock"]
CMD ["--config", "configs/example.yaml"]
