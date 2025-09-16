#!/bin/bash

# ClusterEye Site Ekleme Scripti
# Yeni müşteri siteleri kolayca ekler

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
SITES_DIR="$PROJECT_DIR/docker/frontend/sites"
NGINX_CONF="$PROJECT_DIR/docker/nginx/conf.d/clustereye.conf"

# Create site function
create_site() {
    local site_name="$1"
    local domain="$2"
    local template="$3"

    log "Creating site: $site_name"

    # Create site directory
    local site_dir="$SITES_DIR/$site_name"
    mkdir -p "$site_dir"

    # Create index.html based on template
    case "$template" in
        "dashboard")
            create_dashboard_template "$site_dir" "$site_name"
            ;;
        "landing")
            create_landing_template "$site_dir" "$site_name"
            ;;
        "admin")
            create_admin_template "$site_dir" "$site_name"
            ;;
        *)
            create_basic_template "$site_dir" "$site_name"
            ;;
    esac

    # Add nginx server block
    add_nginx_server_block "$site_name" "$domain"

    success "Site created: $site_name"
    log "Access at: http://$domain"
}

create_dashboard_template() {
    local site_dir="$1"
    local site_name="$2"

    cat > "$site_dir/index.html" << EOF
<!DOCTYPE html>
<html lang="tr">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>$site_name Dashboard</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 0; padding: 20px; background: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; background: white; padding: 20px; border-radius: 8px; }
        .header { background: #2c3e50; color: white; padding: 1rem; border-radius: 4px; margin-bottom: 20px; }
        .metrics { display: grid; grid-template-columns: repeat(auto-fit, minmax(250px, 1fr)); gap: 20px; }
        .metric-card { background: #ecf0f1; padding: 1rem; border-radius: 4px; text-align: center; }
        .metric-value { font-size: 2rem; font-weight: bold; color: #2c3e50; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>$site_name Dashboard</h1>
            <p>Sistem İzleme ve Yönetim Paneli</p>
        </div>

        <div class="metrics">
            <div class="metric-card">
                <h3>Aktif Sunucular</h3>
                <div class="metric-value">12</div>
            </div>
            <div class="metric-card">
                <h3>CPU Kullanımı</h3>
                <div class="metric-value">45%</div>
            </div>
            <div class="metric-card">
                <h3>Memory Kullanımı</h3>
                <div class="metric-value">68%</div>
            </div>
        </div>

        <div style="margin-top: 2rem;">
            <h2>API Erişimi</h2>
            <p><strong>API Endpoint:</strong> <code>/api/v1/</code></p>
            <p><strong>Health Check:</strong> <a href="/api/v1/health" target="_blank">/api/v1/health</a></p>
        </div>
    </div>
</body>
</html>
EOF
}

create_landing_template() {
    local site_dir="$1"
    local site_name="$2"

    cat > "$site_dir/index.html" << EOF
<!DOCTYPE html>
<html lang="tr">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>$site_name</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 0; padding: 0; background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); min-height: 100vh; }
        .hero { text-align: center; padding: 100px 20px; color: white; }
        .container { max-width: 800px; margin: 0 auto; }
        h1 { font-size: 3rem; margin-bottom: 1rem; }
        p { font-size: 1.2rem; margin-bottom: 2rem; }
        .btn { display: inline-block; padding: 12px 30px; background: white; color: #333; text-decoration: none; border-radius: 5px; font-weight: bold; }
    </style>
</head>
<body>
    <div class="hero">
        <div class="container">
            <h1>$site_name</h1>
            <p>ClusterEye ile güçlendirilmiş sistem izleme çözümü</p>
            <a href="/api/v1/health" class="btn">API Durumu</a>
        </div>
    </div>
</body>
</html>
EOF
}

create_admin_template() {
    local site_dir="$1"
    local site_name="$2"

    cat > "$site_dir/index.html" << EOF
<!DOCTYPE html>
<html lang="tr">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>$site_name Admin</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 0; background: #1a1a1a; color: #fff; }
        .header { background: #333; padding: 1rem; }
        .container { padding: 2rem; }
        .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 20px; }
        .card { background: #2a2a2a; padding: 1.5rem; border-radius: 8px; border: 1px solid #444; }
        .btn { display: inline-block; padding: 8px 16px; background: #007bff; color: white; text-decoration: none; border-radius: 4px; }
    </style>
</head>
<body>
    <div class="header">
        <h1>$site_name Admin Panel</h1>
    </div>

    <div class="container">
        <div class="grid">
            <div class="card">
                <h3>Sistem Durumu</h3>
                <p>Tüm servisler aktif</p>
                <a href="/api/v1/health" class="btn">Health Check</a>
            </div>
            <div class="card">
                <h3>Agent Yönetimi</h3>
                <p>Bağlı agent'ları yönetin</p>
                <a href="/api/v1/agents" class="btn">Agent Listesi</a>
            </div>
        </div>
    </div>
</body>
</html>
EOF
}

create_basic_template() {
    local site_dir="$1"
    local site_name="$2"

    cat > "$site_dir/index.html" << EOF
<!DOCTYPE html>
<html lang="tr">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>$site_name</title>
</head>
<body>
    <h1>$site_name</h1>
    <p>ClusterEye destekli site</p>
    <p><a href="/api/v1/health">API Health Check</a></p>
</body>
</html>
EOF
}

add_nginx_server_block() {
    local site_name="$1"
    local domain="$2"

    # Backup nginx config
    cp "$NGINX_CONF" "$NGINX_CONF.backup.$(date +%s)"

    # Add server block
    cat >> "$NGINX_CONF" << EOF

# Site: $site_name
server {
    listen 80;
    server_name $domain;

    location / {
        root /usr/share/nginx/html/sites/$site_name;
        index index.html;
        try_files \$uri \$uri/ /index.html;
    }

    # API access for this site
    location /api/ {
        proxy_set_header X-Site-Name "$site_name";
        include /etc/nginx/conf.d/api_proxy.conf;
    }

    # Static files
    location /static/ {
        alias /usr/share/nginx/html/sites/$site_name/static/;
        expires 1y;
        add_header Cache-Control "public, immutable";
    }
}
EOF

    log "Nginx server block added for $domain"
}

# Interactive site creation
interactive_create() {
    echo ""
    echo "🌐 ClusterEye Site Oluşturucu"
    echo "============================="
    echo ""

    read -p "Site adı (örn: musteri1): " site_name
    read -p "Domain adı (örn: musteri1.localhost): " domain

    echo ""
    echo "Template seçin:"
    echo "1) Dashboard (İzleme paneli)"
    echo "2) Landing (Tanıtım sayfası)"
    echo "3) Admin (Yönetim paneli)"
    echo "4) Basic (Basit sayfa)"
    echo ""
    read -p "Template (1-4): " template_choice

    case "$template_choice" in
        1) template="dashboard" ;;
        2) template="landing" ;;
        3) template="admin" ;;
        4) template="basic" ;;
        *) template="basic" ;;
    esac

    if [[ -z "$site_name" || -z "$domain" ]]; then
        error "Site adı ve domain gerekli"
        exit 1
    fi

    create_site "$site_name" "$domain" "$template"

    echo ""
    echo "✅ Site oluşturuldu!"
    echo ""
    echo "Sonraki adımlar:"
    echo "1. docker-compose restart nginx"
    echo "2. /etc/hosts dosyasına ekleyin: 127.0.0.1 $domain"
    echo "3. Tarayıcıda açın: http://$domain"
    echo ""
}

# Remove site function
remove_site() {
    local site_name="$1"

    log "Removing site: $site_name"

    # Remove site directory
    if [[ -d "$SITES_DIR/$site_name" ]]; then
        rm -rf "$SITES_DIR/$site_name"
        success "Site files removed"
    fi

    # Remove nginx config (manual step required)
    warning "Nginx konfigürasyonundan server block'u manuel olarak silin: $NGINX_CONF"
}

# List sites
list_sites() {
    echo ""
    echo "📋 Mevcut Siteler:"
    echo "=================="

    if [[ -d "$SITES_DIR" ]]; then
        for site in "$SITES_DIR"/*; do
            if [[ -d "$site" ]]; then
                site_name=$(basename "$site")
                echo "  📁 $site_name"
            fi
        done
    else
        echo "Henüz site oluşturulmamış"
    fi
}

# Main menu
main() {
    mkdir -p "$SITES_DIR"

    if [[ $# -eq 0 ]]; then
        interactive_create
    else
        case "$1" in
            "create")
                if [[ $# -ge 4 ]]; then
                    create_site "$2" "$3" "$4"
                else
                    interactive_create
                fi
                ;;
            "remove")
                remove_site "$2"
                ;;
            "list")
                list_sites
                ;;
            *)
                echo "Usage: $0 [create|remove|list]"
                echo "  create <name> <domain> <template> - Create new site"
                echo "  remove <name>                     - Remove site"
                echo "  list                              - List sites"
                ;;
        esac
    fi
}

main "$@"