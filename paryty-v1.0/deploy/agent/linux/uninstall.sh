#!/bin/bash
# Paryty Agent Uninstallation Script for Linux

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
LOG_DIR="/var/log/paryty"
DATA_DIR="/var/lib/paryty"

echo -e "${CYAN}========================================${NC}"
echo -e "${CYAN}  Paryty Agent Uninstaller (Linux)${NC}"
echo -e "${CYAN}========================================${NC}"
echo ""

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}[!] This script must be run as root (use sudo)${NC}"
    exit 1
fi

# Parse arguments
REMOVE_FILES=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --remove-files)
            REMOVE_FILES=true
            shift
            ;;
        --help)
            echo "Usage: $0 [--remove-files]"
            echo ""
            echo "Options:"
            echo "  --remove-files  Remove configuration and log files"
            echo "  --help          Show this help message"
            exit 0
            ;;
        *)
            echo -e "${RED}[!] Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

# Check if service exists
if ! systemctl list-unit-files | grep -q "$SERVICE_NAME"; then
    echo -e "${YELLOW}[!] Service '$SERVICE_NAME' not found.${NC}"
    if [ "$REMOVE_FILES" = true ]; then
        echo -e "${YELLOW}    Removing files...${NC}"
        rm -rf "$CONFIG_DIR" "$LOG_DIR" "$DATA_DIR"
    fi
    exit 0
fi

echo -e "${GREEN}[1/4] Stopping service...${NC}"
if systemctl is-active --quiet "$SERVICE_NAME"; then
    systemctl stop "$SERVICE_NAME"
    sleep 2
fi

echo -e "${GREEN}[2/4] Disabling service...${NC}"
systemctl disable "$SERVICE_NAME" 2>/dev/null || true

echo -e "${GREEN}[3/4] Removing service file...${NC}"
rm -f "/etc/systemd/system/$SERVICE_NAME.service"
rm -f "/etc/logrotate.d/$SERVICE_NAME"
systemctl daemon-reload

echo -e "${GREEN}[4/4] Cleaning up...${NC}"
if [ "$REMOVE_FILES" = true ]; then
    echo -e "${YELLOW}  Removing installation files...${NC}"
    rm -f "$INSTALL_DIR/paryty-agent"
    rm -rf "$CONFIG_DIR"
    rm -rf "$LOG_DIR"
    rm -rf "$DATA_DIR"
    echo -e "${GREEN}  Removed all files${NC}"
else
    echo -e "${YELLOW}  Installation files preserved:${NC}"
    echo -e "    Binary: $INSTALL_DIR/paryty-agent"
    echo -e "    Config: $CONFIG_DIR"
    echo -e "    Logs:   $LOG_DIR"
    echo -e "  Use --remove-files to delete them.${NC}"
fi

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}  Uninstallation Complete!${NC}"
echo -e "${GREEN}========================================${NC}"
