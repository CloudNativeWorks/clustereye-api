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
RUN apk --no-cache add ca-certificates tzdata && \
    addgroup -S clustereye && \
    adduser -S clustereye -G clustereye

WORKDIR /app

# Copy the binary from builder stage
COPY --from=builder /app/clustereye-api .

# Copy configuration files if they exist
COPY --from=builder /app/server.yml.example ./server.yml.example 2>/dev/null || true

# Change ownership
RUN chown -R clustereye:clustereye /app

# Switch to non-root user
USER clustereye

# Expose ports
EXPOSE 8080 18000

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["./clustereye-api"]