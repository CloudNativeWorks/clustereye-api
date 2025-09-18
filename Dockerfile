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

# Build the application with verbose output
RUN echo "Building binary..." && \
    CGO_ENABLED=0 GOOS=linux go build -v -a -installsuffix cgo \
    -ldflags '-w -s' \
    -o clustereye-api ./cmd/api && \
    echo "Binary built successfully:" && \
    ls -la clustereye-api

# Final stage
FROM alpine:3.21

# Install runtime dependencies
RUN apk --no-cache add ca-certificates tzdata curl wget busybox-extras && \
    addgroup -S clustereye && \
    adduser -S clustereye -G clustereye

WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/clustereye-api ./clustereye-api

# Copy configuration files
COPY --from=builder /app/server.yml* ./

# Verify binary was copied and make it executable
RUN echo "Verifying binary after copy:" && \
    ls -la ./clustereye-api && \
    chmod +x ./clustereye-api && \
    chown -R clustereye:clustereye /app && \
    echo "Final binary check:" && \
    ls -la ./clustereye-api

# Switch to non-root user
USER clustereye

# Expose ports
EXPOSE 8080 18000

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Simple startup
CMD ["./clustereye-api"]