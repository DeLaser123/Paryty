#!/bin/bash
# Paryty Agent Status Script for Linux

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
WHITE='\033[1;37m'
GRAY='\033[0;37m'
NC='\033[0m' # No Color

# Configuration
SERVICE_NAME="paryty-agent"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/paryty"
LOG_DIR="/var/log/paryty"

echo -e "${CYAN}========================================${NC}"
echo -e "${CYAN}  Paryty Agent Status (Linux)${NC}"
echo -e "${CYAN}========================================${NC}"
echo ""

# Service Status
echo -e "${WHITE}Service Status:${NC}"
if systemctl list-unit-files | grep -q "$SERVICE_NAME"; then
    STATUS=$(systemctl is-active "$SERVICE_NAME")
    ENABLED=$(systemctl is-enabled "$SERVICE_NAME")
    
    STATUS_COLOR=$GREEN
    if [ "$STATUS" != "active" ]; then
        STATUS_COLOR=$RED
    fi
    
    echo -e "  Name:   ${WHITE}$SERVICE_NAME${NC}"
    echo -e "  Status: ${STATUS_COLOR}$STATUS${NC}"
    echo -e "  Enabled: ${WHITE}$ENABLED${NC}"
    
    # Get PID and resource usage
    PID=$(systemctl show -p MainPID --value "$SERVICE_NAME")
    if [ "$PID" -gt 0 ] && [ -d "/proc/$PID" ]; then
        MEMORY=$(cat "/proc/$PID/status" | grep VmRSS | awk '{print $2}')
        MEMORY_MB=$(echo "scale=2; $MEMORY / 1024" | bc)
        UPTIME=$(ps -p $PID -o etimes= 2>/dev/null | tr -d ' ')
        UPTIME_MIN=$(echo "scale=0; $UPTIME / 60" | bc)
        
        echo -e "  PID:    ${WHITE}$PID${NC}"
        echo -e "  Memory: ${WHITE}${MEMORY_MB} MB${NC}"
        echo -e "  Uptime: ${WHITE}${UPTIME_MIN} minutes${NC}"
    fi
else
    echo -e "  ${RED}[!] Service not installed${NC}"
fi

echo ""

# Configuration
echo -e "${WHITE}Configuration:${NC}"
CONFIG_PATH="$CONFIG_DIR/agent.yaml"
if [ -f "$CONFIG_PATH" ]; then
    echo -e "  Config: ${WHITE}$CONFIG_PATH${NC}"
    
    # Read API key prefix
    API_KEY=$(grep "api_key:" "$CONFIG_PATH" | sed 's/.*api_key: *"\([^"]*\)".*/\1/')
    if [ -n "$API_KEY" ]; then
        PREFIX="${API_KEY:0:16}"
        echo -e "  API Key: ${WHITE}${PREFIX}...${NC}"
    fi
    
    # Read endpoint
    ENDPOINT=$(grep "cluster_endpoint:" "$CONFIG_PATH" | sed 's/.*cluster_endpoint: *"\([^"]*\)".*/\1/')
    if [ -n "$ENDPOINT" ]; then
        echo -e "  Endpoint: ${WHITE}$ENDPOINT${NC}"
    fi
else
    echo -e "  ${RED}[!] Config file not found${NC}"
fi

echo ""

# Environment Variables
echo -e "${WHITE}Environment Variables:${NC}"
ENV_API_KEY=$(grep "PARYTY_API_KEY=" "/etc/systemd/system/$SERVICE_NAME.service" 2>/dev/null | cut -d'=' -f2)
ENV_ENDPOINT=$(grep "PARYTY_CLUSTER_ENDPOINT=" "/etc/systemd/system/$SERVICE_NAME.service" 2>/dev/null | cut -d'=' -f2)

if [ -n "$ENV_API_KEY" ]; then
    PREFIX="${ENV_API_KEY:0:16}"
    echo -e "  PARYTY_API_KEY: ${WHITE}${PREFIX}...${NC}"
else
    echo -e "  PARYTY_API_KEY: ${GRAY}(not set)${NC}"
fi

if [ -n "$ENV_ENDPOINT" ]; then
    echo -e "  PARYTY_CLUSTER_ENDPOINT: ${WHITE}$ENV_ENDPOINT${NC}"
else
    echo -e "  PARYTY_CLUSTER_ENDPOINT: ${GRAY}(not set)${NC}"
fi

echo ""

# Installation
echo -e "${WHITE}Installation:${NC}"
if [ -d "$CONFIG_DIR" ]; then
    echo -e "  Config Dir: ${WHITE}$CONFIG_DIR${NC}"
    
    if [ -f "$INSTALL_DIR/paryty-agent" ]; then
        SIZE=$(du -h "$INSTALL_DIR/paryty-agent" | cut -f1)
        MODIFIED=$(stat -c %y "$INSTALL_DIR/paryty-agent" 2>/dev/null | cut -d'.' -f1)
        echo -e "  Binary: ${WHITE}$SIZE (modified: $MODIFIED)${NC}"
    fi
    
    if [ -d "$LOG_DIR" ]; then
        LOG_COUNT=$(ls -1 "$LOG_DIR" 2>/dev/null | wc -l)
        echo -e "  Logs: ${WHITE}$LOG_COUNT files${NC}"
    fi
else
    echo -e "  ${RED}[!] Installation directory not found${NC}"
fi

echo ""

# Recent Logs
SHOW_LOGS=${1:-true}
LOG_LINES=${2:-20}

if [ "$SHOW_LOGS" = true ] && [ -f "$LOG_DIR/agent-output.log" ]; then
    echo -e "${WHITE}Recent Logs (last $LOG_LINES lines):${NC}"
    echo -e "${GRAY}----------------------------------------${NC}"
    tail -n "$LOG_LINES" "$LOG_DIR/agent-output.log" | while IFS= read -r line; do
        if echo "$line" | grep -q '"level":"ERROR"'; then
            echo -e "${RED}$line${NC}"
        elif echo "$line" | grep -q '"level":"WARN"'; then
            echo -e "${YELLOW}$line${NC}"
        elif echo "$line" | grep -q '"level":"INFO"'; then
            echo -e "${GREEN}$line${NC}"
        else
            echo -e "${GRAY}$line${NC}"
        fi
    done
    echo -e "${GRAY}----------------------------------------${NC}"
fi

echo ""
echo -e "${CYAN}========================================${NC}"
