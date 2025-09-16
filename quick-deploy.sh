#!/bin/bash

# ClusterEye Quick Deploy - One Command Setup
# Ultra-fast deployment for new customers

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
NC='\033[0m'

# Quick input
echo -e "${PURPLE}"
echo "  ██████╗██╗     ██╗   ██╗███████╗████████╗███████╗██████╗ ███████╗██╗   ██╗███████╗"
echo " ██╔════╝██║     ██║   ██║██╔════╝╚══██╔══╝██╔════╝██╔══██╗██╔════╝╚██╗ ██╔╝██╔════╝"
echo " ██║     ██║     ██║   ██║███████╗   ██║   █████╗  ██████╔╝█████╗   ╚████╔╝ █████╗  "
echo " ██║     ██║     ██║   ██║╚════██║   ██║   ██╔══╝  ██╔══██╗██╔══╝    ╚██╔╝  ██╔══╝  "
echo " ╚██████╗███████╗╚██████╔╝███████║   ██║   ███████╗██║  ██║███████╗   ██║   ███████╗"
echo "  ╚═════╝╚══════╝ ╚═════╝ ╚══════╝   ╚═╝   ╚══════╝╚═╝  ╚═╝╚══════╝   ╚═╝   ╚══════╝"
echo -e "${NC}"
echo ""
echo -e "${BLUE}🚀 One-Command Customer Deployment${NC}"
echo "=================================="
echo ""

# Quick examples
echo -e "${YELLOW}📝 Usage Examples:${NC}"
echo ""
echo "For A101:"
echo "  $0 A101 api-a101.clustereye.com a101.clustereye.com admin@a101.com.tr"
echo ""
echo "For Migros:"
echo "  $0 Migros api-migros.clustereye.com migros.clustereye.com admin@migros.com.tr"
echo ""
echo "For CarrefourSA:"
echo "  $0 CarrefourSA api-carrefour.clustereye.com carrefour.clustereye.com admin@carrefoursa.com"
echo ""

if [[ $# -eq 4 ]]; then
    # Command line mode - direct deployment
    echo -e "${GREEN}✅ Starting automated deployment...${NC}"
    ./deploy-customer.sh "$1" "$2" "$3" "$4"
elif [[ $# -eq 1 && "$1" == "a101" ]]; then
    # A101 quick deploy
    echo -e "${GREEN}✅ A101 Quick Deploy Mode${NC}"
    ./deploy-customer.sh "A101" "api-a101.clustereye.com" "a101.clustereye.com" "admin@clustereye.com"
else
    # Interactive mode
    read -p "🏢 Customer Name: " customer
    read -p "🌐 API Domain: " api_domain
    read -p "📱 Frontend Domain: " frontend_domain
    read -p "📧 Email: " email

    if [[ -n "$customer" && -n "$api_domain" && -n "$frontend_domain" && -n "$email" ]]; then
        echo -e "${GREEN}✅ Starting deployment for $customer...${NC}"
        ./deploy-customer.sh "$customer" "$api_domain" "$frontend_domain" "$email"
    else
        echo -e "${RED}❌ All fields required${NC}"
        exit 1
    fi
fi