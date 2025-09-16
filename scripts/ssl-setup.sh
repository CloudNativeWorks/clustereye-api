#!/bin/bash

# ClusterEye SSL Certificate Setup Script
# Helps configure SSL certificates for HTTPS

set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
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

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
SSL_DIR="$PROJECT_DIR/docker/ssl"
ENV_FILE="$PROJECT_DIR/.env"

# Create SSL directory
mkdir -p "$SSL_DIR"

show_menu() {
    echo ""
    echo "🔒 ClusterEye SSL Certificate Setup"
    echo "=================================="
    echo ""
    echo "1) Generate self-signed certificate (Development)"
    echo "2) Use existing certificate files"
    echo "3) Setup Let's Encrypt certificate (Production)"
    echo "4) Remove SSL configuration"
    echo "5) Exit"
    echo ""
    read -p "Select an option (1-5): " choice
}

generate_self_signed() {
    log "Generating self-signed SSL certificate..."

    read -p "Enter domain name (default: localhost): " domain
    domain=${domain:-localhost}

    # Generate private key
    openssl genrsa -out "$SSL_DIR/key.pem" 2048

    # Generate certificate
    openssl req -new -x509 -key "$SSL_DIR/key.pem" -out "$SSL_DIR/cert.pem" -days 365 -subj "/CN=$domain"

    # Set permissions
    chmod 600 "$SSL_DIR/key.pem"
    chmod 644 "$SSL_DIR/cert.pem"

    success "Self-signed certificate generated for $domain"

    warning "⚠️  Self-signed certificates are not trusted by browsers"
    warning "Use only for development. For production, use a proper CA certificate."

    enable_https
}

use_existing_cert() {
    log "Setting up existing certificate files..."

    echo ""
    echo "Please place your certificate files in: $SSL_DIR"
    echo "Required files:"
    echo "  - cert.pem (certificate file)"
    echo "  - key.pem (private key file)"
    echo ""

    read -p "Certificate file path: " cert_path
    read -p "Private key file path: " key_path

    if [[ ! -f "$cert_path" ]]; then
        error "Certificate file not found: $cert_path"
        return 1
    fi

    if [[ ! -f "$key_path" ]]; then
        error "Private key file not found: $key_path"
        return 1
    fi

    # Copy files
    cp "$cert_path" "$SSL_DIR/cert.pem"
    cp "$key_path" "$SSL_DIR/key.pem"

    # Set permissions
    chmod 600 "$SSL_DIR/key.pem"
    chmod 644 "$SSL_DIR/cert.pem"

    success "Certificate files copied successfully"

    enable_https
}

setup_letsencrypt() {
    log "Setting up Let's Encrypt certificate..."

    # Check if certbot is installed
    if ! command -v certbot &> /dev/null; then
        error "Certbot is not installed. Please install it first:"
        echo "  Ubuntu/Debian: sudo apt install certbot"
        echo "  CentOS/RHEL: sudo yum install certbot"
        echo "  macOS: brew install certbot"
        return 1
    fi

    read -p "Enter your domain name: " domain
    read -p "Enter your email address: " email

    if [[ -z "$domain" || -z "$email" ]]; then
        error "Domain and email are required"
        return 1
    fi

    warning "⚠️  Make sure your domain points to this server"
    warning "⚠️  Port 80 should be accessible from the internet"

    read -p "Continue? (y/N): " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        return 0
    fi

    # Stop nginx temporarily
    docker-compose -f "$PROJECT_DIR/docker-compose.yml" stop nginx

    # Generate certificate
    sudo certbot certonly --standalone \
        --email "$email" \
        --agree-tos \
        --no-eff-email \
        -d "$domain"

    if [[ $? -eq 0 ]]; then
        # Copy certificates
        sudo cp "/etc/letsencrypt/live/$domain/fullchain.pem" "$SSL_DIR/cert.pem"
        sudo cp "/etc/letsencrypt/live/$domain/privkey.pem" "$SSL_DIR/key.pem"

        # Fix permissions
        sudo chown $USER:$USER "$SSL_DIR"/*.pem
        chmod 600 "$SSL_DIR/key.pem"
        chmod 644 "$SSL_DIR/cert.pem"

        success "Let's Encrypt certificate generated successfully"

        # Setup auto-renewal
        echo "0 12 * * * /usr/bin/certbot renew --quiet && docker-compose -f $PROJECT_DIR/docker-compose.yml restart nginx" | sudo crontab -

        success "Auto-renewal scheduled"

        enable_https
    else
        error "Certificate generation failed"
    fi

    # Restart nginx
    docker-compose -f "$PROJECT_DIR/docker-compose.yml" start nginx
}

enable_https() {
    log "Enabling HTTPS in configuration..."

    # Update .env file
    if [[ -f "$ENV_FILE" ]]; then
        sed -i.bak 's/ENABLE_HTTPS=false/ENABLE_HTTPS=true/' "$ENV_FILE"
        success "HTTPS enabled in environment configuration"
    else
        warning "Environment file not found: $ENV_FILE"
    fi

    # Update nginx configuration
    local nginx_conf="$PROJECT_DIR/docker/nginx/conf.d/clustereye.conf"

    log "Please manually uncomment HTTPS server block in: $nginx_conf"
    log "And comment out the HTTP redirect lines"

    echo ""
    echo "Next steps:"
    echo "1. Review nginx configuration: $nginx_conf"
    echo "2. Restart services: docker-compose restart"
    echo "3. Test HTTPS: https://your-domain"
    echo ""
}

remove_ssl() {
    log "Removing SSL configuration..."

    # Update .env file
    if [[ -f "$ENV_FILE" ]]; then
        sed -i.bak 's/ENABLE_HTTPS=true/ENABLE_HTTPS=false/' "$ENV_FILE"
        success "HTTPS disabled in environment configuration"
    fi

    # Remove certificate files
    read -p "Remove certificate files? (y/N): " -n 1 -r
    echo ""
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        rm -f "$SSL_DIR"/*.pem
        success "Certificate files removed"
    fi

    log "Don't forget to update nginx configuration and restart services"
}

main() {
    while true; do
        show_menu

        case $choice in
            1)
                generate_self_signed
                ;;
            2)
                use_existing_cert
                ;;
            3)
                setup_letsencrypt
                ;;
            4)
                remove_ssl
                ;;
            5)
                echo "Exiting..."
                exit 0
                ;;
            *)
                error "Invalid option. Please select 1-5."
                ;;
        esac

        echo ""
        read -p "Press Enter to continue..."
    done
}

main "$@"