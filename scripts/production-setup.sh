#!/bin/bash

# Production Domain Setup Script for ClusterEye
# Configures nginx for real domains with SSL certificates

set -e

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
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

error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Script paths
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
NGINX_DIR="$PROJECT_DIR/docker/nginx"
SITES_DIR="$PROJECT_DIR/docker/frontend/sites"

# Check if certbot is available
check_certbot() {
    if ! command -v certbot &> /dev/null; then
        error "Certbot is not installed. Please install it first:"
        echo "  Ubuntu/Debian: sudo apt install certbot python3-certbot-nginx"
        echo "  CentOS/RHEL: sudo yum install certbot python3-certbot-nginx"
        echo "  macOS: brew install certbot"
        exit 1
    fi
}

# Setup domain function
setup_domain() {
    local domain_type="$1"  # api, frontend, customer
    local domain_name="$2"
    local site_name="$3"    # for customer sites

    log "Setting up domain: $domain_name ($domain_type)"

    # Validate domain format
    if [[ ! "$domain_name" =~ ^[a-zA-Z0-9][a-zA-Z0-9-]{1,61}[a-zA-Z0-9]\.[a-zA-Z]{2,}$ ]] && [[ ! "$domain_name" =~ ^[a-zA-Z0-9-]+\.[a-zA-Z0-9][a-zA-Z0-9-]{1,61}[a-zA-Z0-9]\.[a-zA-Z]{2,}$ ]]; then
        error "Invalid domain format: $domain_name"
        return 1
    fi

    case "$domain_type" in
        "api")
            create_api_config "$domain_name"
            ;;
        "frontend")
            create_frontend_config "$domain_name"
            ;;
        "customer")
            create_customer_config "$domain_name" "$site_name"
            ;;
        *)
            error "Unknown domain type: $domain_type"
            return 1
            ;;
    esac

    # Generate SSL certificate
    if [[ "$AUTO_SSL" == "true" ]]; then
        generate_ssl_certificate "$domain_name"
    else
        warning "SSL certificate not generated. Run manually: sudo certbot certonly -d $domain_name"
    fi
}

create_api_config() {
    local domain="$1"
    local config_file="$NGINX_DIR/conf.d/${domain}.conf"

    log "Creating API configuration for $domain"

    cat > "$config_file" << EOF
# API Configuration for $domain
# Generated on: $(date)

# HTTP to HTTPS redirect
server {
    if (\$host = $domain) {
        return 301 https://\$host\$request_uri;
    } # managed by Certbot

    listen 80;
    server_name $domain;
    return 301 https://\$server_name\$request_uri;
}

# HTTPS API Server
server {
    listen 443 ssl http2;
    server_name $domain;

    # SSL Configuration (will be updated by Certbot)
    ssl_certificate /etc/letsencrypt/live/$domain/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$domain/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers ECDHE-RSA-AES256-GCM-SHA512:DHE-RSA-AES256-GCM-SHA512:ECDHE-RSA-AES256-GCM-SHA384:DHE-RSA-AES256-GCM-SHA384;
    ssl_prefer_server_ciphers off;
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 10m;

    # HSTS
    add_header Strict-Transport-Security "max-age=63072000" always;

    # Rate limiting
    limit_req zone=api burst=20 nodelay;

    # Health check
    location /health {
        access_log off;
        proxy_pass http://api:8080;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }

    # API endpoints
    location / {
        proxy_pass http://api:8080;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;

        # WebSocket support
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";

        # Timeouts
        proxy_connect_timeout 30s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;

        # Logging
        error_log /var/log/nginx/${domain}_error.log;
        access_log /var/log/nginx/${domain}_access.log;
    }

    # Security headers
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-Frame-Options "DENY" always;
    add_header X-XSS-Protection "1; mode=block" always;
}
EOF

    success "API configuration created: $config_file"
}

create_frontend_config() {
    local domain="$1"
    local config_file="$NGINX_DIR/conf.d/${domain}.conf"

    log "Creating frontend configuration for $domain"

    cat > "$config_file" << EOF
# Frontend Configuration for $domain
# Generated on: $(date)

# HTTP to HTTPS redirect
server {
    listen 80;
    server_name $domain;
    return 301 https://\$server_name\$request_uri;
}

# HTTPS Frontend Server
server {
    listen 443 ssl http2;
    server_name $domain;

    # SSL Configuration
    ssl_certificate /etc/letsencrypt/live/$domain/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$domain/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers ECDHE-RSA-AES256-GCM-SHA512:DHE-RSA-AES256-GCM-SHA512:ECDHE-RSA-AES256-GCM-SHA384:DHE-RSA-AES256-GCM-SHA384;
    ssl_prefer_server_ciphers off;
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 10m;

    # HSTS
    add_header Strict-Transport-Security "max-age=63072000" always;

    # Document root
    root /usr/share/nginx/html;
    index index.html;

    # MIME types
    include /etc/nginx/mime.types;
    default_type application/octet-stream;

    # JavaScript modules
    location ~ \.js\$ {
        add_header Content-Type "application/javascript" always;
        add_header Cache-Control "no-cache";
    }

    location ~ \.mjs\$ {
        add_header Content-Type "application/javascript" always;
        add_header Cache-Control "no-cache";
    }

    # SPA routing
    location / {
        try_files \$uri \$uri/ /index.html;
        add_header Cache-Control "no-cache";

        # Security headers
        add_header X-Content-Type-Options "nosniff" always;
        add_header X-Frame-Options "SAMEORIGIN" always;
        add_header X-XSS-Protection "1; mode=block" always;
        add_header Referrer-Policy "strict-origin-when-cross-origin" always;
    }

    # Static assets
    location /assets {
        expires 1y;
        add_header Cache-Control "public, no-transform, immutable";

        location ~* \.js\$ {
            add_header Content-Type "application/javascript" always;
            try_files \$uri =404;
        }

        location ~* \.(css|png|jpg|jpeg|gif|ico|svg|woff|woff2|ttf|eot)\$ {
            try_files \$uri =404;
        }
    }

    # Alternative static folder
    location /static {
        alias /usr/share/nginx/html/static;
        expires 1y;
        add_header Cache-Control "public, no-transform, immutable";
    }

    # Gzip compression
    gzip on;
    gzip_vary on;
    gzip_min_length 1024;
    gzip_proxied any;
    gzip_comp_level 6;
    gzip_types
        text/plain
        text/css
        text/xml
        text/javascript
        application/json
        application/javascript
        application/xml+rss
        application/atom+xml
        image/svg+xml;
}
EOF

    success "Frontend configuration created: $config_file"
}

create_customer_config() {
    local domain="$1"
    local site_name="$2"
    local config_file="$NGINX_DIR/conf.d/${domain}.conf"

    # Create site directory if it doesn't exist
    mkdir -p "$SITES_DIR/$site_name"

    # Create basic index.html if it doesn't exist
    if [[ ! -f "$SITES_DIR/$site_name/index.html" ]]; then
        cat > "$SITES_DIR/$site_name/index.html" << EOF
<!DOCTYPE html>
<html lang="tr">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>$site_name Dashboard</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 0; padding: 20px; background: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; background: white; padding: 20px; border-radius: 8px; }
        .header { background: #2c3e50; color: white; padding: 1rem; border-radius: 4px; margin-bottom: 20px; text-align: center; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>$site_name</h1>
            <p>ClusterEye Powered Dashboard</p>
        </div>
        <p>Domain: $domain</p>
        <p>Site: $site_name</p>
        <p><a href="/api/health">API Health Check</a></p>
    </div>
</body>
</html>
EOF
    fi

    log "Creating customer configuration for $domain ($site_name)"

    cat > "$config_file" << EOF
# Customer Configuration for $domain ($site_name)
# Generated on: $(date)

# HTTP to HTTPS redirect
server {
    listen 80;
    server_name $domain;
    return 301 https://\$server_name\$request_uri;
}

# HTTPS Customer Site
server {
    listen 443 ssl http2;
    server_name $domain;

    # SSL Configuration
    ssl_certificate /etc/letsencrypt/live/$domain/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$domain/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers ECDHE-RSA-AES256-GCM-SHA512:DHE-RSA-AES256-GCM-SHA512:ECDHE-RSA-AES256-GCM-SHA384:DHE-RSA-AES256-GCM-SHA384;
    ssl_prefer_server_ciphers off;
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 10m;

    # HSTS
    add_header Strict-Transport-Security "max-age=63072000" always;

    # Document root
    root /usr/share/nginx/html/sites/$site_name;
    index index.html;

    # MIME types
    include /etc/nginx/mime.types;
    default_type application/octet-stream;

    # SPA routing
    location / {
        try_files \$uri \$uri/ /index.html;
        add_header Cache-Control "no-cache";
    }

    # Static assets
    location /assets {
        expires 1y;
        add_header Cache-Control "public, no-transform, immutable";
    }

    location /static {
        alias /usr/share/nginx/html/sites/$site_name/static;
        expires 1y;
        add_header Cache-Control "public, no-transform, immutable";
    }

    # Customer-specific API access
    location /api {
        proxy_pass http://api:8080;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        proxy_set_header X-Client-ID "$site_name";
        proxy_set_header X-Client-Domain "$domain";
    }

    # Gzip compression
    gzip on;
    gzip_vary on;
    gzip_proxied any;
    gzip_comp_level 6;
    gzip_types text/plain text/css text/xml application/json application/javascript application/xml+rss application/atom+xml image/svg+xml;

    # Security headers
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-XSS-Protection "1; mode=block" always;
}
EOF

    success "Customer configuration created: $config_file"
    success "Customer site directory: $SITES_DIR/$site_name"
}

generate_ssl_certificate() {
    local domain="$1"

    log "Generating SSL certificate for $domain"

    warning "Make sure your domain points to this server before proceeding!"
    read -p "Continue with SSL certificate generation? (y/N): " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        warning "SSL certificate generation skipped for $domain"
        return 0
    fi

    # Stop nginx temporarily for standalone mode
    docker-compose -f "$PROJECT_DIR/docker-compose.yml" stop nginx 2>/dev/null || true

    # Generate certificate
    if sudo certbot certonly --standalone --email "$SSL_EMAIL" --agree-tos --no-eff-email -d "$domain"; then
        success "SSL certificate generated for $domain"
    else
        error "SSL certificate generation failed for $domain"
    fi

    # Start nginx again
    docker-compose -f "$PROJECT_DIR/docker-compose.yml" start nginx 2>/dev/null || true
}

# Interactive setup
interactive_setup() {
    echo ""
    echo "🌐 Production Domain Setup"
    echo "========================="
    echo ""

    echo "Domain türü seçin:"
    echo "1) API Domain (ör: api.yourdomain.com)"
    echo "2) Frontend Domain (ör: app.yourdomain.com)"
    echo "3) Customer Site (ör: client1.yourdomain.com)"
    echo ""
    read -p "Seçim (1-3): " domain_type_choice

    case "$domain_type_choice" in
        1) domain_type="api" ;;
        2) domain_type="frontend" ;;
        3) domain_type="customer" ;;
        *)
            error "Geçersiz seçim"
            exit 1
            ;;
    esac

    read -p "Domain adı: " domain_name

    if [[ "$domain_type" == "customer" ]]; then
        read -p "Site adı (klasör adı): " site_name
    fi

    read -p "Email adresiniz (SSL için): " SSL_EMAIL
    read -p "Otomatik SSL sertifikası oluşturulsun mu? (y/N): " -n 1 -r
    echo ""
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        AUTO_SSL="true"
    else
        AUTO_SSL="false"
    fi

    if [[ -z "$domain_name" || -z "$SSL_EMAIL" ]]; then
        error "Domain adı ve email gerekli"
        exit 1
    fi

    setup_domain "$domain_type" "$domain_name" "$site_name"

    echo ""
    echo "✅ Domain kurulumu tamamlandı!"
    echo ""
    echo "Sonraki adımlar:"
    echo "1. DNS kayıtlarınızı kontrol edin"
    echo "2. docker-compose restart nginx"
    echo "3. Siteyi test edin: https://$domain_name"
    echo ""
    if [[ "$AUTO_SSL" == "false" ]]; then
        echo "SSL sertifikası için manuel olarak çalıştırın:"
        echo "sudo certbot certonly -d $domain_name"
    fi
    echo ""
}

# List existing configurations
list_configs() {
    echo ""
    echo "📋 Mevcut Domain Konfigürasyonları:"
    echo "=================================="

    for config in "$NGINX_DIR/conf.d"/*.conf; do
        if [[ -f "$config" && ! "$config" =~ "template" ]]; then
            config_name=$(basename "$config" .conf)
            echo "  🌐 $config_name"

            # Extract domain from config
            domain=$(grep -o "server_name [^;]*" "$config" | head -1 | cut -d' ' -f2)
            if [[ "$domain" != "_" ]]; then
                echo "     Domain: $domain"
            fi
        fi
    done
}

# Main function
main() {
    mkdir -p "$NGINX_DIR/conf.d"
    mkdir -p "$SITES_DIR"

    if [[ $# -eq 0 ]]; then
        interactive_setup
    else
        case "$1" in
            "setup")
                if [[ $# -ge 3 ]]; then
                    SSL_EMAIL="${4:-admin@localhost}"
                    AUTO_SSL="${5:-false}"
                    setup_domain "$2" "$3" "$4"
                else
                    echo "Usage: $0 setup <type> <domain> [site_name] [email] [auto_ssl]"
                    echo "  type: api, frontend, customer"
                fi
                ;;
            "list")
                list_configs
                ;;
            *)
                echo "Usage: $0 [setup|list]"
                echo "  setup <type> <domain> [site_name] - Setup domain"
                echo "  list                               - List configurations"
                ;;
        esac
    fi
}

main "$@"