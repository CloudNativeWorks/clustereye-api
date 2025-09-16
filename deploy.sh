#!/bin/bash

# ClusterEye Easy Deployment Script
# This script automatically deploys ClusterEye with all required services

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Script configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$SCRIPT_DIR/.env"
BACKUP_DIR="$SCRIPT_DIR/backups"

# Logging
log() {
    echo -e "${BLUE}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

# Check if Docker and Docker Compose are installed
check_prerequisites() {
    log "Checking prerequisites..."

    if ! command -v docker &> /dev/null; then
        error "Docker is not installed. Please install Docker first."
        echo "Visit: https://docs.docker.com/get-docker/"
        exit 1
    fi

    if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null; then
        error "Docker Compose is not installed. Please install Docker Compose first."
        echo "Visit: https://docs.docker.com/compose/install/"
        exit 1
    fi

    success "Prerequisites check passed"
}

# Generate secure random strings
generate_secrets() {
    log "Generating secure secrets..."

    # Generate JWT secret (64 characters)
    JWT_SECRET=$(openssl rand -base64 48 | tr -d '\n')

    # Generate encryption key (32 bytes, base64 encoded)
    ENCRYPTION_KEY=$(openssl rand -base64 32 | tr -d '\n')

    # Generate database password
    DB_PASSWORD=$(openssl rand -base64 24 | tr -d '\n' | tr '/' '_')

    # Generate InfluxDB token
    INFLUXDB_TOKEN=$(openssl rand -base64 48 | tr -d '\n' | tr '/' '_')

    # Generate InfluxDB admin password
    INFLUXDB_ADMIN_PASSWORD=$(openssl rand -base64 24 | tr -d '\n' | tr '/' '_')

    # Generate Grafana password
    GRAFANA_ADMIN_PASSWORD=$(openssl rand -base64 16 | tr -d '\n' | tr '/' '_')

    success "Secrets generated successfully"
}

# Create environment file with generated secrets
create_env_file() {
    if [[ -f "$ENV_FILE" ]]; then
        warning "Environment file already exists. Creating backup..."
        cp "$ENV_FILE" "$ENV_FILE.backup.$(date +%s)"
    fi

    log "Creating environment configuration..."

    cat > "$ENV_FILE" << EOF
# ClusterEye Production Environment Configuration
# Generated on: $(date)
# IMPORTANT: Keep this file secure and do not share!

# ==============================================
# DATABASE CONFIGURATION
# ==============================================
DB_HOST=postgres
DB_PORT=5432
DB_NAME=clustereye
DB_USER=clustereye_user
DB_PASSWORD=$DB_PASSWORD

# ==============================================
# INFLUXDB CONFIGURATION
# ==============================================
INFLUXDB_URL=http://influxdb:8086
INFLUXDB_TOKEN=$INFLUXDB_TOKEN
INFLUXDB_ORG=clustereye
INFLUXDB_BUCKET=clustereye
INFLUXDB_ADMIN_USER=admin
INFLUXDB_ADMIN_PASSWORD=$INFLUXDB_ADMIN_PASSWORD
INFLUXDB_PORT=8086

# ==============================================
# API SERVER CONFIGURATION
# ==============================================
HTTP_PORT=8080
GRPC_PORT=50051
JWT_SECRET_KEY=$JWT_SECRET
ENCRYPTION_KEY=$ENCRYPTION_KEY

# ==============================================
# LOGGING & MONITORING
# ==============================================
LOG_LEVEL=info
RATE_LIMIT=120

# ==============================================
# SECURITY SETTINGS
# ==============================================
ENABLE_HTTPS=false
SSL_CERT_PATH=/app/ssl/cert.pem
SSL_KEY_PATH=/app/ssl/key.pem

# ==============================================
# NGINX CONFIGURATION
# ==============================================
NGINX_HTTP_PORT=80
NGINX_HTTPS_PORT=443

# ==============================================
# GRAFANA CONFIGURATION
# ==============================================
GRAFANA_PORT=3000
GRAFANA_ADMIN_USER=admin
GRAFANA_ADMIN_PASSWORD=$GRAFANA_ADMIN_PASSWORD

# ==============================================
# BACKUP CONFIGURATION
# ==============================================
BACKUP_ENABLED=true
BACKUP_SCHEDULE="0 2 * * *"
BACKUP_RETENTION_DAYS=7
EOF

    chmod 600 "$ENV_FILE"
    success "Environment file created: $ENV_FILE"
}

# Create necessary directories
create_directories() {
    log "Creating necessary directories..."

    mkdir -p "$BACKUP_DIR"
    mkdir -p "$SCRIPT_DIR/logs"
    mkdir -p "$SCRIPT_DIR/logs/nginx"

    success "Directories created"
}

# Build and start services
deploy_services() {
    log "Building and starting ClusterEye services..."

    cd "$SCRIPT_DIR"

    # Pull latest images
    log "Pulling Docker images..."
    docker-compose pull postgres influxdb nginx grafana 2>/dev/null || docker compose pull postgres influxdb nginx grafana

    # Build API service
    log "Building ClusterEye API..."
    docker-compose build api 2>/dev/null || docker compose build api

    # Start services
    log "Starting services..."
    docker-compose up -d 2>/dev/null || docker compose up -d

    success "Services started successfully"
}

# Wait for services to be ready
wait_for_services() {
    log "Waiting for services to be ready..."

    local max_attempts=60
    local attempt=1

    while [[ $attempt -le $max_attempts ]]; do
        if curl -f -s http://localhost:80/health > /dev/null 2>&1; then
            success "All services are ready!"
            return 0
        fi

        echo -n "."
        sleep 2
        ((attempt++))
    done

    error "Services did not start within expected time"
    return 1
}

# Display service information
show_service_info() {
    echo ""
    echo "=================================="
    echo "🚀 ClusterEye Deployment Complete!"
    echo "=================================="
    echo ""
    echo "📊 Service URLs:"
    echo "  🌐 Web Interface:    http://localhost"
    echo "  📡 API Endpoint:     http://localhost/api"
    echo "  📈 Grafana:          http://localhost:3000"
    echo ""
    echo "🔐 Admin Credentials:"
    echo "  📊 Grafana Admin:    admin / $GRAFANA_ADMIN_PASSWORD"
    echo "  💾 InfluxDB Admin:   admin / $INFLUXDB_ADMIN_PASSWORD"
    echo ""
    echo "🛠️  Management Commands:"
    echo "  📋 View logs:        docker-compose logs -f"
    echo "  🔄 Restart:          docker-compose restart"
    echo "  ⏹️  Stop:             docker-compose down"
    echo "  🗑️  Reset:            docker-compose down -v"
    echo ""
    echo "📁 Important Files:"
    echo "  🔧 Environment:      .env"
    echo "  📝 Logs:             ./logs/"
    echo "  💾 Backups:          ./backups/"
    echo ""
    echo "⚠️  Security Notes:"
    echo "  • Change default passwords before production use"
    echo "  • Configure SSL certificates for HTTPS"
    echo "  • Review and adjust environment variables"
    echo "  • Set up regular backups"
    echo ""
}

# Backup existing data
backup_data() {
    if docker ps -q -f name=clustereye-postgres > /dev/null 2>&1; then
        log "Creating backup of existing data..."

        local backup_file="$BACKUP_DIR/backup_$(date +%Y%m%d_%H%M%S).sql"

        docker exec clustereye-postgres pg_dump -U clustereye_user clustereye > "$backup_file" 2>/dev/null || {
            warning "Could not create database backup"
        }

        if [[ -f "$backup_file" ]]; then
            success "Backup created: $backup_file"
        fi
    fi
}

# Main deployment function
main() {
    echo ""
    echo "🚀 ClusterEye Easy Deployment Script"
    echo "===================================="
    echo ""

    # Check if deployment already exists
    if docker ps -q -f name=clustereye > /dev/null 2>&1; then
        echo "⚠️  Existing ClusterEye deployment detected!"
        read -p "Do you want to continue? This will update the deployment. (y/N): " -n 1 -r
        echo ""
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            echo "Deployment cancelled."
            exit 0
        fi
        backup_data
    fi

    check_prerequisites
    generate_secrets
    create_env_file
    create_directories
    deploy_services

    if wait_for_services; then
        show_service_info
    else
        error "Deployment may have issues. Check logs with: docker-compose logs"
        exit 1
    fi
}

# Run main function
main "$@"