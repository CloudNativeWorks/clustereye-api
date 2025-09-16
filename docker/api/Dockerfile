# Multi-stage build for ClusterEye API
FROM golang:1.23-alpine AS builder

# Install required packages
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o clustereye-api ./cmd/api

# Final stage
FROM alpine:3.19

# Install ca-certificates for SSL/TLS
RUN apk --no-cache add ca-certificates wget curl

# Create app user
RUN addgroup -g 1001 -S app && adduser -u 1001 -S app -G app

# Create necessary directories
RUN mkdir -p /app/logs /app/ssl && chown -R app:app /app

WORKDIR /app

# Copy binary from builder stage
COPY --from=builder --chown=app:app /app/clustereye-api .

# Copy configuration files
COPY --chown=app:app server.yml.example server.yml

# Set permissions
RUN chmod +x clustereye-api

# Switch to app user
USER app

# Expose ports
EXPOSE 8080 50051

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://localhost:8080/health || exit 1

# Run the application
CMD ["./clustereye-api"]