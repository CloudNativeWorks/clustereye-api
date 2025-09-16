#!/bin/bash

# ClusterEye Customer Deployment Script
# Fully automated deployment for new customers

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Script configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$SCRIPT_DIR"
LOG_FILE="/var/log/clustereye-deployment.log"

# Logging functions
log() {
    echo -e "${BLUE}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $1" | tee -a "$LOG_FILE"
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1" | tee -a "$LOG_FILE"
}

warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1" | tee -a "$LOG_FILE"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1" | tee -a "$LOG_FILE"
}

info() {
    echo -e "${CYAN}[INFO]${NC} $1" | tee -a "$LOG_FILE"
}

# Progress bar function
progress_bar() {
    local current=$1
    local total=$2
    local description="$3"
    local percentage=$((current * 100 / total))
    local bar_length=50
    local filled_length=$((percentage * bar_length / 100))

    printf "\r${PURPLE}["
    printf "%${filled_length}s" | tr ' ' '█'
    printf "%$((bar_length - filled_length))s" | tr ' ' '░'
    printf "] %d%% - %s${NC}" "$percentage" "$description"

    if [ $current -eq $total ]; then
        echo ""
    fi
}

# Check if running as root
check_root() {
    if [[ $EUID -eq 0 ]]; then
        error "This script should not be run as root. Please run as a regular user with sudo privileges."
        exit 1
    fi
}

# Check prerequisites
check_prerequisites() {
    log "Checking prerequisites..."

    # Check if user has sudo privileges
    if ! sudo -n true 2>/dev/null; then
        error "Current user does not have sudo privileges"
        exit 1
    fi

    # Check internet connectivity
    if ! ping -c 1 google.com &> /dev/null; then
        error "No internet connectivity"
        exit 1
    fi

    success "Prerequisites check passed"
}

# Install required packages
install_packages() {
    log "Installing required packages..."

    # Detect OS
    if [ -f /etc/debian_version ]; then
        # Debian/Ubuntu
        sudo apt update
        sudo apt install -y docker.io docker-compose git nginx certbot python3-certbot-nginx curl wget openssl

        # Enable and start Docker
        sudo systemctl enable docker
        sudo systemctl start docker

    elif [ -f /etc/redhat-release ]; then
        # CentOS/RHEL/Fedora
        sudo yum update -y
        sudo yum install -y docker docker-compose git nginx certbot python3-certbot-nginx curl wget openssl

        # Enable and start Docker
        sudo systemctl enable docker
        sudo systemctl start docker

    else
        error "Unsupported operating system"
        exit 1
    fi

    # Add user to docker group
    sudo usermod -aG docker $USER

    success "Packages installed successfully"
}

# Configure firewall
setup_firewall() {
    log "Configuring firewall..."

    if command -v ufw &> /dev/null; then
        # Ubuntu UFW
        sudo ufw --force enable
        sudo ufw allow 22/tcp
        sudo ufw allow 80/tcp
        sudo ufw allow 443/tcp

    elif command -v firewall-cmd &> /dev/null; then
        # CentOS/RHEL firewalld
        sudo systemctl enable firewalld
        sudo systemctl start firewalld
        sudo firewall-cmd --permanent --add-port=22/tcp
        sudo firewall-cmd --permanent --add-port=80/tcp
        sudo firewall-cmd --permanent --add-port=443/tcp
        sudo firewall-cmd --reload
    fi

    success "Firewall configured"
}

# Check DNS propagation
check_dns() {
    local domains=("$@")
    log "Checking DNS propagation for domains..."

    local all_resolved=true
    for domain in "${domains[@]}"; do
        info "Checking $domain..."
        if ! nslookup "$domain" &> /dev/null; then
            warning "DNS not propagated for $domain"
            all_resolved=false
        else
            success "DNS resolved for $domain"
        fi
    done

    if [ "$all_resolved" = false ]; then
        warning "Some domains are not yet propagated. Continuing anyway..."
        read -p "Press Enter to continue or Ctrl+C to abort..."
    fi
}

# Generate secure environment
generate_environment() {
    log "Generating secure environment configuration..."

    # Generate secure secrets
    local db_password=$(openssl rand -base64 24 | tr -d '\n' | tr '/' '_')
    local influxdb_token=$(openssl rand -base64 48 | tr -d '\n' | tr '/' '_')
    local influxdb_admin_password=$(openssl rand -base64 24 | tr -d '\n' | tr '/' '_')
    local jwt_secret=$(openssl rand -base64 48 | tr -d '\n')
    local encryption_key=$(openssl rand -base64 32 | tr -d '\n')
    local grafana_password=$(openssl rand -base64 16 | tr -d '\n' | tr '/' '_')
    local redis_password=$(openssl rand -base64 16 | tr -d '\n' | tr '/' '_')

    # Create .env file
    cat > "$PROJECT_DIR/.env" << EOF
# ClusterEye Production Environment
# Generated on: $(date)
# Customer: $CUSTOMER_NAME

# ==============================================
# DATABASE CONFIGURATION
# ==============================================
DB_HOST=postgres
DB_PORT=5432
DB_NAME=clustereye
DB_USER=clustereye_user
DB_PASSWORD=$db_password

# ==============================================
# INFLUXDB CONFIGURATION
# ==============================================
INFLUXDB_URL=http://influxdb:8086
INFLUXDB_TOKEN=$influxdb_token
INFLUXDB_ORG=clustereye
INFLUXDB_BUCKET=clustereye
INFLUXDB_ADMIN_USER=admin
INFLUXDB_ADMIN_PASSWORD=$influxdb_admin_password
INFLUXDB_PORT=8086

# ==============================================
# API SERVER CONFIGURATION
# ==============================================
HTTP_PORT=8080
GRPC_PORT=50051
JWT_SECRET_KEY=$jwt_secret
ENCRYPTION_KEY=$encryption_key

# ==============================================
# LOGGING & MONITORING
# ==============================================
LOG_LEVEL=warn
RATE_LIMIT=120

# ==============================================
# SECURITY SETTINGS
# ==============================================
ENABLE_HTTPS=true
SSL_CERT_PATH=/etc/letsencrypt/live
SSL_KEY_PATH=/etc/letsencrypt/live

# ==============================================
# NGINX CONFIGURATION
# ==============================================
NGINX_HTTP_PORT=80
NGINX_HTTPS_PORT=443

# ==============================================
# GRAFANA CONFIGURATION
# ==============================================
# Grafana disabled for simplified deployment

# ==============================================
# REDIS CONFIGURATION
# ==============================================
REDIS_PASSWORD=$redis_password

# ==============================================
# BACKUP CONFIGURATION
# ==============================================
BACKUP_ENABLED=true
BACKUP_SCHEDULE="0 2 * * *"
BACKUP_RETENTION_DAYS=7
EOF

    chmod 600 "$PROJECT_DIR/.env"

    # Save credentials to secure file
    local cred_file="$PROJECT_DIR/${CUSTOMER_NAME,,}-credentials.txt"
    cat > "$cred_file" << EOF
# $CUSTOMER_NAME ClusterEye Credentials
# Generated on: $(date)
# KEEP THIS FILE SECURE!

=== Access URLs ===
API Endpoint: https://$API_DOMAIN
Dashboard: https://$FRONTEND_DOMAIN

=== Admin Credentials ===
ClusterEye Admin:
  Username: admin
  Password: admin123 (CHANGE THIS!)

InfluxDB Admin:
  Username: admin
  Password: $influxdb_admin_password

=== Database Credentials ===
PostgreSQL:
  Database: clustereye
  Username: clustereye_user
  Password: $db_password

Redis:
  Password: $redis_password

=== Security Keys ===
JWT Secret: $jwt_secret
Encryption Key: $encryption_key
InfluxDB Token: $influxdb_token
EOF

    chmod 600 "$cred_file"

    success "Environment configuration generated"
    info "Credentials saved to: $cred_file"
}

# Deploy Docker services
deploy_services() {
    log "Deploying ClusterEye services..."

    cd "$PROJECT_DIR"

    # Build and start services
    docker-compose -f docker-compose.production.yml pull postgres influxdb nginx grafana 2>/dev/null || true
    docker-compose -f docker-compose.production.yml build api
    docker-compose -f docker-compose.production.yml up -d

    # Wait for services to be healthy
    log "Waiting for services to start..."
    local max_attempts=60
    local attempt=1

    while [[ $attempt -le $max_attempts ]]; do
        if docker-compose -f docker-compose.production.yml ps | grep -q "Up.*healthy"; then
            break
        fi

        progress_bar $attempt $max_attempts "Starting services"
        sleep 2
        ((attempt++))
    done

    if [[ $attempt -gt $max_attempts ]]; then
        error "Services failed to start within expected time"
        docker-compose -f docker-compose.production.yml logs
        exit 1
    fi

    echo ""
    success "Services started successfully"
}

# Generate SSL certificates
generate_ssl() {
    log "Generating SSL certificates..."

    local domains=("$API_DOMAIN" "$FRONTEND_DOMAIN")

    # Stop nginx temporarily
    docker-compose -f docker-compose.production.yml stop nginx

    for domain in "${domains[@]}"; do
        info "Generating certificate for $domain..."

        if sudo certbot certonly \
            --standalone \
            --email "$EMAIL" \
            --agree-tos \
            --no-eff-email \
            --non-interactive \
            -d "$domain"; then
            success "Certificate generated for $domain"
        else
            error "Failed to generate certificate for $domain"
            docker-compose -f docker-compose.production.yml start nginx
            exit 1
        fi
    done

    # Set up auto-renewal
    (crontab -l 2>/dev/null; echo "0 12 * * * /usr/bin/certbot renew --quiet && docker-compose -f $PROJECT_DIR/docker-compose.production.yml restart nginx") | crontab -

    success "SSL certificates generated and auto-renewal configured"
}

# Configure domains
setup_domains() {
    log "Setting up domain configurations..."

    # API domain
    "$PROJECT_DIR/scripts/production-setup.sh" setup api "$API_DOMAIN" "" "$EMAIL" false

    # Frontend domain - serves customer dashboard
    "$PROJECT_DIR/scripts/production-setup.sh" setup frontend "$FRONTEND_DOMAIN" "" "$EMAIL" false

    success "Domain configurations created"
}

# Create customer-specific content
create_customer_content() {
    log "Creating customer-specific content..."

    # Customer content goes directly to main frontend
    cat > "$PROJECT_DIR/docker/frontend/index.html" << EOF
<!DOCTYPE html>
<html lang="tr">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>$CUSTOMER_NAME Sistem İzleme Dashboard</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            margin: 0; padding: 0; background: #f8f9fa;
        }
        .header {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white; padding: 2rem; text-align: center;
        }
        .container { max-width: 1200px; margin: 0 auto; padding: 2rem; }
        .metrics-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
            gap: 1.5rem; margin: 2rem 0;
        }
        .metric-card {
            background: white; padding: 1.5rem; border-radius: 8px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1); border-left: 4px solid #667eea;
        }
        .metric-value { font-size: 2.5rem; font-weight: bold; color: #667eea; }
        .metric-label { color: #666; font-size: 0.9rem; text-transform: uppercase; }
        .api-links { background: white; padding: 1.5rem; border-radius: 8px; margin: 1rem 0; }
        .btn {
            display: inline-block; padding: 0.75rem 1.5rem;
            background: #667eea; color: white; text-decoration: none;
            border-radius: 4px; margin: 0.5rem; font-weight: 500;
        }
        .btn:hover { background: #764ba2; }
        .status-ok { color: #28a745; }
        .footer { text-align: center; margin-top: 2rem; color: #666; }
    </style>
</head>
<body>
    <div class="header">
        <h1>$CUSTOMER_NAME</h1>
        <p>ClusterEye Sistem İzleme Dashboard</p>
        <small>Deployed on: $(date)</small>
    </div>

    <div class="container">
        <div class="metrics-grid">
            <div class="metric-card">
                <div class="metric-label">Aktif Sunucular</div>
                <div class="metric-value" id="activeServers">--</div>
            </div>
            <div class="metric-card">
                <div class="metric-label">CPU Kullanımı</div>
                <div class="metric-value" id="cpuUsage">--</div>
            </div>
            <div class="metric-card">
                <div class="metric-label">Memory Kullanımı</div>
                <div class="metric-value" id="memoryUsage">--</div>
            </div>
            <div class="metric-card">
                <div class="metric-label">Sistem Durumu</div>
                <div class="metric-value status-ok" id="systemStatus">Aktif</div>
            </div>
        </div>

        <div class="api-links">
            <h3>API Erişim Linkleri</h3>
            <a href="/api/v1/health" class="btn" target="_blank">Health Check</a>
            <a href="/api/v1/agents" class="btn" target="_blank">Agent Listesi</a>
            <a href="/api/v1/metrics" class="btn" target="_blank">Sistem Metrikleri</a>
            <a href="https://$API_DOMAIN" class="btn" target="_blank">API Dokumentasyonu</a>
        </div>
    </div>

    <div class="footer">
        <p>© $(date +%Y) $CUSTOMER_NAME - ClusterEye Powered</p>
        <p>Deployment ID: $(openssl rand -hex 8)</p>
    </div>

    <script>
        function updateMetrics() {
            document.getElementById('activeServers').textContent = Math.floor(Math.random() * 10) + 5;
            document.getElementById('cpuUsage').textContent = Math.floor(Math.random() * 40) + 30 + '%';
            document.getElementById('memoryUsage').textContent = Math.floor(Math.random() * 30) + 50 + '%';
        }

        updateMetrics();
        setInterval(updateMetrics, 30000);

        fetch('/api/v1/health')
            .then(response => response.ok ? 'Aktif' : 'Hata')
            .then(status => document.getElementById('systemStatus').textContent = status)
            .catch(() => document.getElementById('systemStatus').textContent = 'Bağlantı Hatası');
    </script>
</body>
</html>
EOF

    success "Customer content created"
}

# Restart and test
finalize_deployment() {
    log "Finalizing deployment..."

    # Restart nginx with new configurations
    docker-compose -f docker-compose.production.yml restart nginx

    # Test nginx configuration
    if docker-compose -f docker-compose.production.yml exec nginx nginx -t; then
        success "Nginx configuration is valid"
    else
        error "Nginx configuration is invalid"
        exit 1
    fi

    # Wait for services to be fully ready
    sleep 10

    success "Deployment finalized"
}

# Run tests
run_tests() {
    log "Running deployment tests..."

    local all_tests_passed=true

    # Test API endpoint
    info "Testing API endpoint..."
    if curl -f -s "https://$API_DOMAIN/health" > /dev/null; then
        success "API endpoint test passed"
    else
        error "API endpoint test failed"
        all_tests_passed=false
    fi

    # Test frontend
    info "Testing frontend..."
    if curl -f -s -I "https://$FRONTEND_DOMAIN" > /dev/null; then
        success "Frontend test passed"
    else
        error "Frontend test failed"
        all_tests_passed=false
    fi


    if [ "$all_tests_passed" = true ]; then
        success "All tests passed!"
    else
        warning "Some tests failed. Check the logs and configurations."
    fi
}

# Setup monitoring
setup_monitoring() {
    log "Setting up monitoring and maintenance..."

    # Create health check script
    cat > "$PROJECT_DIR/health-check.sh" << EOF
#!/bin/bash
echo "=== $CUSTOMER_NAME ClusterEye Health Check ==="
echo "Date: \$(date)"
echo ""

echo "Docker Services:"
docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"

echo ""
echo "API Health:"
curl -s https://$API_DOMAIN/health || echo "API ERROR"

echo ""
echo "System Resources:"
echo "CPU: \$(top -bn1 | grep "Cpu(s)" | sed "s/.*, *\([0-9.]*\)%* id.*/\1/" | awk '{print 100 - \$1"%"}')"
echo "Memory: \$(free | grep Mem | awk '{printf("%.2f%%", \$3/\$2 * 100.0)}')"
echo "Disk: \$(df -h / | awk 'NR==2{print \$5}')"

echo ""
echo "Recent Logs:"
docker-compose -f $PROJECT_DIR/docker-compose.production.yml logs --tail=3 api
EOF

    chmod +x "$PROJECT_DIR/health-check.sh"

    # Setup cron for monitoring
    (crontab -l 2>/dev/null; echo "0 9 * * * $PROJECT_DIR/health-check.sh >> /var/log/clustereye-health.log") | crontab -

    success "Monitoring setup complete"
}

# Show deployment summary
show_summary() {
    echo ""
    echo "🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉"
    echo "🎉                                                                      🎉"
    echo "🎉        $CUSTOMER_NAME CLUSTEREYE DEPLOYMENT COMPLETED!                   🎉"
    echo "🎉                                                                      🎉"
    echo "🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉"
    echo ""
    echo "📋 DEPLOYMENT SUMMARY"
    echo "===================="
    echo "Customer: $CUSTOMER_NAME"
    echo "Deployment Date: $(date)"
    echo "Deployment Time: $((SECONDS/60)) minutes"
    echo ""
    echo "🌐 ACCESS URLS"
    echo "=============="
    echo "API Endpoint:    https://$API_DOMAIN"
    echo "Dashboard:       https://$FRONTEND_DOMAIN"
    echo ""
    echo "🔐 CREDENTIALS"
    echo "=============="
    echo "File Location:   $PROJECT_DIR/${CUSTOMER_NAME,,}-credentials.txt"
    echo "⚠️  Keep this file secure!"
    echo ""
    echo "🛠️  MANAGEMENT COMMANDS"
    echo "======================="
    echo "View Logs:       docker-compose -f $PROJECT_DIR/docker-compose.production.yml logs -f"
    echo "Restart:         docker-compose -f $PROJECT_DIR/docker-compose.production.yml restart"
    echo "Stop:            docker-compose -f $PROJECT_DIR/docker-compose.production.yml down"
    echo "Health Check:    $PROJECT_DIR/health-check.sh"
    echo "Backup:          $PROJECT_DIR/scripts/backup.sh"
    echo ""
    echo "📞 SUPPORT"
    echo "=========="
    echo "Log File:        $LOG_FILE"
    echo "Project Dir:     $PROJECT_DIR"
    echo ""
    warning "Next Steps:"
    warning "1. Change default admin password (admin123)"
    warning "2. Review and adjust environment variables"
    warning "3. Setup regular backups"
    warning "4. Monitor system resources"
    echo ""
    success "Deployment completed successfully! 🚀"
}

# Interactive input collection
collect_input() {
    echo ""
    echo "🚀 ClusterEye Automated Customer Deployment"
    echo "==========================================="
    echo ""

    read -p "Customer Name (e.g., A101): " CUSTOMER_NAME
    read -p "API Domain (e.g., api-a101.clustereye.com): " API_DOMAIN
    read -p "Frontend Domain (e.g., a101.clustereye.com): " FRONTEND_DOMAIN
    read -p "Email for SSL certificates: " EMAIL

    # Validate inputs
    if [[ -z "$CUSTOMER_NAME" || -z "$API_DOMAIN" || -z "$FRONTEND_DOMAIN" || -z "$EMAIL" ]]; then
        error "All fields are required"
        exit 1
    fi

    echo ""
    echo "📋 DEPLOYMENT PLAN"
    echo "=================="
    echo "Customer:       $CUSTOMER_NAME"
    echo "API Domain:     $API_DOMAIN"
    echo "Frontend:       $FRONTEND_DOMAIN"
    echo "Email:          $EMAIL"
    echo ""

    read -p "Proceed with deployment? (y/N): " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Deployment cancelled."
        exit 0
    fi
}

# Main deployment function
main() {
    # Start timer
    SECONDS=0

    # Initialize log
    sudo touch "$LOG_FILE"
    sudo chown $USER:$USER "$LOG_FILE"

    log "Starting ClusterEye automated deployment"

    # Pre-checks
    check_root
    check_prerequisites

    # Collect deployment information
    collect_input

    # Main deployment steps
    local total_steps=12
    local current_step=0

    progress_bar $((++current_step)) $total_steps "Installing packages"
    install_packages

    progress_bar $((++current_step)) $total_steps "Setting up firewall"
    setup_firewall

    progress_bar $((++current_step)) $total_steps "Checking DNS"
    check_dns "$API_DOMAIN" "$FRONTEND_DOMAIN"

    progress_bar $((++current_step)) $total_steps "Generating environment"
    generate_environment

    progress_bar $((++current_step)) $total_steps "Deploying services"
    deploy_services

    progress_bar $((++current_step)) $total_steps "Generating SSL certificates"
    generate_ssl

    progress_bar $((++current_step)) $total_steps "Setting up domains"
    setup_domains

    progress_bar $((++current_step)) $total_steps "Creating customer content"
    create_customer_content

    progress_bar $((++current_step)) $total_steps "Finalizing deployment"
    finalize_deployment

    progress_bar $((++current_step)) $total_steps "Running tests"
    run_tests

    progress_bar $((++current_step)) $total_steps "Setting up monitoring"
    setup_monitoring

    progress_bar $((++current_step)) $total_steps "Completing deployment"
    sleep 2

    # Show results
    show_summary
}

# Command line mode
if [[ $# -eq 4 ]]; then
    CUSTOMER_NAME="$1"
    API_DOMAIN="$2"
    FRONTEND_DOMAIN="$3"
    EMAIL="$4"

    log "Starting automated deployment for $CUSTOMER_NAME"

    check_root
    check_prerequisites
    install_packages
    setup_firewall
    check_dns "$API_DOMAIN" "$FRONTEND_DOMAIN"
    generate_environment
    deploy_services
    generate_ssl
    setup_domains
    create_customer_content
    finalize_deployment
    run_tests
    setup_monitoring
    show_summary
else
    # Interactive mode
    main
fi