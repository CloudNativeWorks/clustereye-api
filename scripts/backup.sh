#!/bin/bash

# ClusterEye Backup Script
# Creates backups of PostgreSQL and InfluxDB data

set -e

# Configuration
BACKUP_DIR="$(dirname "$0")/../backups"
DATE=$(date +%Y%m%d_%H%M%S)
RETENTION_DAYS=${BACKUP_RETENTION_DAYS:-7}

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

log() {
    echo -e "${GREEN}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $1"
}

warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Create backup directory
mkdir -p "$BACKUP_DIR"

# PostgreSQL Backup
backup_postgres() {
    log "Creating PostgreSQL backup..."

    local backup_file="$BACKUP_DIR/postgres_backup_$DATE.sql"

    if docker exec clustereye-postgres pg_dump -U clustereye_user clustereye > "$backup_file"; then
        log "PostgreSQL backup completed: $backup_file"
        gzip "$backup_file"
        log "Compressed backup: $backup_file.gz"
    else
        error "PostgreSQL backup failed"
        return 1
    fi
}

# InfluxDB Backup
backup_influxdb() {
    log "Creating InfluxDB backup..."

    local backup_dir="$BACKUP_DIR/influxdb_backup_$DATE"
    mkdir -p "$backup_dir"

    # Get the token from environment or docker
    local token=$(docker exec clustereye-influxdb printenv DOCKER_INFLUXDB_INIT_ADMIN_TOKEN 2>/dev/null || echo "")

    if [[ -z "$token" ]]; then
        warning "InfluxDB token not found, skipping InfluxDB backup"
        return 0
    fi

    if docker exec clustereye-influxdb influx backup \
        --host http://localhost:8086 \
        --token "$token" \
        --org clustereye \
        --bucket clustereye \
        /tmp/backup 2>/dev/null; then

        # Copy backup from container
        docker cp clustereye-influxdb:/tmp/backup "$backup_dir/"

        # Create tar archive
        cd "$BACKUP_DIR"
        tar -czf "influxdb_backup_$DATE.tar.gz" "influxdb_backup_$DATE"
        rm -rf "influxdb_backup_$DATE"

        log "InfluxDB backup completed: $backup_dir.tar.gz"
    else
        warning "InfluxDB backup failed or no data to backup"
    fi
}

# Cleanup old backups
cleanup_old_backups() {
    log "Cleaning up old backups (retention: $RETENTION_DAYS days)..."

    find "$BACKUP_DIR" -name "*.sql.gz" -mtime +$RETENTION_DAYS -delete 2>/dev/null || true
    find "$BACKUP_DIR" -name "*.tar.gz" -mtime +$RETENTION_DAYS -delete 2>/dev/null || true

    log "Cleanup completed"
}

# Main backup function
main() {
    log "Starting ClusterEye backup process..."

    # Check if services are running
    if ! docker ps -q -f name=clustereye-postgres > /dev/null; then
        error "PostgreSQL container is not running"
        exit 1
    fi

    backup_postgres
    backup_influxdb
    cleanup_old_backups

    log "Backup process completed successfully"

    # Show backup summary
    echo ""
    echo "Backup Summary:"
    echo "==============="
    ls -lh "$BACKUP_DIR"/*_$DATE* 2>/dev/null || true
}

# Run backup
main "$@"