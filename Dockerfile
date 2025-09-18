# Build stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk --no-cache add ca-certificates git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
    -ldflags '-w -s' \
    -o clustereye-api ./cmd/api

# Final stage
FROM alpine:3.21

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata curl wget busybox-extras && \
    addgroup -S clustereye && \
    adduser -S clustereye -G clustereye

WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/clustereye-api .

# Copy configuration files
COPY --from=builder /app/server.yml* ./

# Ensure binary is executable and change ownership
RUN chmod +x /app/clustereye-api && \
    chown -R clustereye:clustereye /app

# Switch to non-root user
USER clustereye

# Expose ports
EXPOSE 8080 18000

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Add debugging environment variable
ENV DEBUG_MODE=false

# Create a simple startup script
RUN echo '#!/bin/sh' > /app/startup.sh && \
    echo 'set -e' >> /app/startup.sh && \
    echo 'echo "=== ClusterEye API Debug Info ==="' >> /app/startup.sh && \
    echo 'echo "Working directory: $(pwd)"' >> /app/startup.sh && \
    echo 'echo "Files:"' >> /app/startup.sh && \
    echo 'ls -la' >> /app/startup.sh && \
    echo 'echo "Binary executable:"' >> /app/startup.sh && \
    echo 'ls -la ./clustereye-api' >> /app/startup.sh && \
    echo 'echo "Environment variables:"' >> /app/startup.sh && \
    echo 'env | grep -E "(DB_|LOG_|INFLUX)" || true' >> /app/startup.sh && \
    echo 'echo "=== Starting Application ==="' >> /app/startup.sh && \
    echo 'exec ./clustereye-api "$@"' >> /app/startup.sh && \
    chmod +x /app/startup.sh

ENTRYPOINT ["/app/startup.sh"]