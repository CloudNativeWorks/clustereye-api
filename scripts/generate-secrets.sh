#!/bin/bash

# Generate secure secrets for ClusterEye deployment

set -e

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() {
    echo -e "${BLUE}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $1"
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log "Generating secure secrets for ClusterEye..."

# Generate secrets
DB_PASSWORD=$(openssl rand -base64 24 | tr -d '\n' | tr '/' '_')
INFLUXDB_TOKEN=$(openssl rand -base64 48 | tr -d '\n' | tr '/' '_')
INFLUXDB_ADMIN_PASSWORD=$(openssl rand -base64 24 | tr -d '\n' | tr '/' '_')
JWT_SECRET_KEY=$(openssl rand -base64 48 | tr -d '\n')
ENCRYPTION_KEY=$(openssl rand -base64 32 | tr -d '\n')
GRAFANA_ADMIN_PASSWORD=$(openssl rand -base64 16 | tr -d '\n' | tr '/' '_')
REDIS_PASSWORD=$(openssl rand -base64 16 | tr -d '\n' | tr '/' '_')

echo ""
echo "🔐 Generated Secrets (copy these to your .env file):"
echo "=================================================="
echo ""
echo "DB_PASSWORD=$DB_PASSWORD"
echo "INFLUXDB_TOKEN=$INFLUXDB_TOKEN"
echo "INFLUXDB_ADMIN_PASSWORD=$INFLUXDB_ADMIN_PASSWORD"
echo "JWT_SECRET_KEY=$JWT_SECRET_KEY"
echo "ENCRYPTION_KEY=$ENCRYPTION_KEY"
echo "GRAFANA_ADMIN_PASSWORD=$GRAFANA_ADMIN_PASSWORD"
echo "REDIS_PASSWORD=$REDIS_PASSWORD"
echo ""
warning "⚠️  Keep these secrets secure and do not share them!"
echo ""

# Save to temporary file
TEMP_FILE="/tmp/clustereye-secrets-$(date +%s).txt"
cat > "$TEMP_FILE" << EOF
# ClusterEye Generated Secrets - $(date)
# KEEP THIS FILE SECURE!

DB_PASSWORD=$DB_PASSWORD
INFLUXDB_TOKEN=$INFLUXDB_TOKEN
INFLUXDB_ADMIN_PASSWORD=$INFLUXDB_ADMIN_PASSWORD
JWT_SECRET_KEY=$JWT_SECRET_KEY
ENCRYPTION_KEY=$ENCRYPTION_KEY
GRAFANA_ADMIN_PASSWORD=$GRAFANA_ADMIN_PASSWORD
REDIS_PASSWORD=$REDIS_PASSWORD
EOF

success "Secrets saved to: $TEMP_FILE"
warning "Remove this file after copying secrets to .env!"

echo ""
echo "Next steps:"
echo "1. Copy secrets to your .env file"
echo "2. rm $TEMP_FILE"
echo "3. Continue with deployment"