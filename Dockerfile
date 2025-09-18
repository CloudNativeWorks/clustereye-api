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

# Create a more robust startup script
RUN cat > /app/startup.sh << 'EOF' && \
#!/bin/sh
set -e

echo "=== ClusterEye API Starting ==="
echo "Working directory: $(pwd)"
echo "User: $(whoami)"
echo "Date: $(date)"

echo "=== Configuration Files ==="
ls -la *.yml 2>/dev/null || echo "No yml files found"

echo "=== Environment Variables ==="
env | grep -E "(DB_|LOG_|INFLUX)" || echo "No relevant env vars found"

echo "=== Binary Check ==="
ls -la ./clustereye-api || echo "Binary not found"
file ./clustereye-api || echo "Cannot check binary type"

echo "=== Network Check ==="
if command -v nslookup >/dev/null 2>&1; then
    nslookup clustereye-stack-postgresql || echo "DNS lookup failed"
fi

echo "=== Starting Application ==="
exec ./clustereye-api "$@"
EOF

RUN chmod +x /app/startup.sh

ENTRYPOINT ["/app/startup.sh"]