#!/bin/bash
# Paryty Agent API Key Manager for Linux

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Configuration
SERVICE_NAME="paryty-agent"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/paryty"

echo -e "${CYAN}========================================${NC}"
echo -e "${CYAN}  Paryty Agent API Key Manager (Linux)${NC}"
echo -e "${CYAN}========================================${NC}"
echo ""

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}[!] This script must be run as root (use sudo)${NC}"
    exit 1
fi

# Parse arguments
API_KEY=""
RESTART=true

while [[ $# -gt 0 ]]; do
    case $1 in
        --api-key)
            API_KEY="$2"
            shift 2
            ;;
        --no-restart)
            RESTART=false
            shift
            ;;
        --help)
            echo "Usage: $0 --api-key <API_KEY> [--no-restart]"
            echo ""
            echo "Options:"
            echo "  --api-key     New API key to set (required)"
            echo "  --no-restart  Don't restart service after update"
            echo "  --help        Show this help message"
            exit 0
            ;;
        *)
            echo -e "${RED}[!] Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

# Validate API key
if [ -z "$API_KEY" ]; then
    echo -e "${RED}[!] API key is required. Use --api-key <API_KEY>${NC}"
    exit 1
fi

if [[ ! "$API_KEY" =~ ^pk_live_ ]]; then
    echo -e "${RED}[!] Invalid API key format. Key must start with 'pk_live_'${NC}"
    exit 1
fi

CONFIG_PATH="$CONFIG_DIR/agent.yaml"
BINARY_PATH="$INSTALL_DIR/paryty-agent"

# Check installation
if [ ! -f "$BINARY_PATH" ]; then
    echo -e "${RED}[!] Agent binary not found at: $BINARY_PATH${NC}"
    exit 1
fi

if [ ! -f "$CONFIG_PATH" ]; then
    echo -e "${RED}[!] Config file not found at: $CONFIG_PATH${NC}"
    exit 1
fi

echo -e "${GREEN}[1/3] Updating API key in config file...${NC}"
"$BINARY_PATH" --key "$API_KEY" --config "$CONFIG_PATH"

echo -e "${GREEN}[2/3] Updating service environment variable...${NC}"
# Update the systemd service file with new environment
sed -i "s|Environment=PARYTY_API_KEY=.*|Environment=PARYTY_API_KEY=$API_KEY|g" "/etc/systemd/system/$SERVICE_NAME.service"

if [ "$RESTART" = true ]; then
    echo -e "${GREEN}[3/3] Restarting service...${NC}"
    systemctl daemon-reload
    
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        systemctl restart "$SERVICE_NAME"
        sleep 2
        
        if systemctl is-active --quiet "$SERVICE_NAME"; then
            echo ""
            echo -e "${GREEN}========================================${NC}"
            echo -e "${GREEN}  API Key Updated Successfully!${NC}"
            echo -e "${GREEN}========================================${NC}"
            echo ""
            echo -e "Service restarted and running with new key.${NC}"
        else
            echo -e "${YELLOW}[!] Service failed to restart. Check logs.${NC}"
        fi
    else
        echo -e "${YELLOW}[!] Service not running. Key updated for next start.${NC}"
    fi
else
    echo -e "${YELLOW}[3/3] Skipping restart (use --no-restart to skip)${NC}"
    echo ""
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}  API Key Updated!${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
    echo -e "Restart the service to apply the new key:${NC}"
    echo -e "  ${WHITE}sudo systemctl restart $SERVICE_NAME${NC}"
fi

echo ""
echo -e "Key Prefix: ${WHITE}${API_KEY:0:16}...${NC}"
echo -e "Config:     ${WHITE}$CONFIG_PATH${NC}"
echo ""
