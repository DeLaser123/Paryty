#!/bin/bash
# Paryty Agent Installation Script for Linux
# This script installs the Paryty Agent as a systemd service

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
echo -e "${CYAN}  Paryty Agent Installer (Linux)${NC}"
echo -e "${CYAN}========================================${NC}"
echo ""

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo -e "${RED}[!] This script must be run as root (use sudo)${NC}"
    exit 1
fi

# Parse arguments
API_KEY=""
CLUSTER_ENDPOINT="localhost:50052"

while [[ $# -gt 0 ]]; do
    case $1 in
        --api-key)
            API_KEY="$2"
            shift 2
            ;;
        --endpoint)
            CLUSTER_ENDPOINT="$2"
            shift 2
            ;;
        --help)
            echo "Usage: $0 --api-key <API_KEY> [--endpoint <ENDPOINT>]"
            echo ""
            echo "Options:"
            echo "  --api-key     API key for tenant authentication (required)"
            echo "  --endpoint    Cluster endpoint (default: localhost:50052)"
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

echo -e "${GREEN}[1/7] Creating directories...${NC}"
mkdir -p "$CONFIG_DIR"
mkdir -p "$LOG_DIR"
mkdir -p "$DATA_DIR"

echo -e "${GREEN}[2/7] Installing agent binary...${NC}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BINARY_SOURCE="$SCRIPT_DIR/../../../agent/target/release/paryty-agent"

if [ ! -f "$BINARY_SOURCE" ]; then
    # Try Windows binary location
    BINARY_SOURCE="$SCRIPT_DIR/../../../agent/target/release/paryty-agent.exe"
fi

if [ ! -f "$BINARY_SOURCE" ]; then
    echo -e "${RED}[!] Agent binary not found. Please build the agent first.${NC}"
    echo "  Run: cd agent && cargo build --release"
    exit 1
fi

cp "$BINARY_SOURCE" "$INSTALL_DIR/paryty-agent"
chmod +x "$INSTALL_DIR/paryty-agent"

echo -e "${GREEN}[3/7] Creating configuration file...${NC}"
cat > "$CONFIG_DIR/agent.yaml" << EOF
# Paryty Agent Configuration
# Installed: $(date '+%Y-%m-%d %H:%M:%S')

agent:
  id: "auto"
  cluster_endpoint: "$CLUSTER_ENDPOINT"
  api_key: "$API_KEY"
  self_metrics:
    enabled: true
    port: 9100

layers:
  metal:
    enabled: true
    interval: "10s"
    cpu_per_core: true
    cpu_per_process: true
    memory_rss: true
    disk_io: true
    network_io: true
    process_tree: true
    container_detection: true

  ebpf:
    enabled: false
    tcp_connections: false
    dns_resolution: false
    http_inspection: false
    db_inspection: false
    exclude_ports: [22, 53, 443]
    exclude_ips: ["127.0.0.1", "::1"]
    ring_buffer_size_kb: 256
    poll_interval_ms: 100
    fallback_to_proc: true

  supervisor:
    enabled: false
    health_checks: []
    log_tailing: []

communication:
  protocol: grpc
  tls:
    enabled: false
  compression: zstd
  edge_buffer:
    enabled: true
    max_size_mb: 100
    retention_hours: 24
  flow_control:
    backpressure_enabled: true
    adaptive_sampling: true
    priority_queues:
      - name: "critical"
        topics: ["alerts", "errors"]
        priority: 1
      - name: "normal"
        topics: ["metrics", "traces"]
        priority: 2

logging:
  level: info
  format: json
  output: stdout
EOF

echo -e "${GREEN}[4/7] Creating systemd service...${NC}"
cat > "/etc/systemd/system/$SERVICE_NAME.service" << EOF
[Unit]
Description=Paryty Agent - Host Monitoring Service
Documentation=https://paryty.io/docs/agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
Group=root
ExecStart=$INSTALL_DIR/paryty-agent --config $CONFIG_DIR/agent.yaml
ExecReload=/bin/kill -HUP \$MAINPID
Restart=always
RestartSec=10
StartLimitIntervalSec=60
StartLimitBurst=5

# Environment
Environment=PARYTY_API_KEY=$API_KEY
Environment=PARYTY_CLUSTER_ENDPOINT=$CLUSTER_ENDPOINT

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$LOG_DIR $DATA_DIR
PrivateTmp=true

# Resource limits
LimitNOFILE=65536
LimitNPROC=4096

# Logging
StandardOutput=append:$LOG_DIR/agent-output.log
StandardError=append:$LOG_DIR/agent-error.log

[Install]
WantedBy=multi-user.target
EOF

echo -e "${GREEN}[5/7] Setting up log rotation...${NC}"
cat > "/etc/logrotate.d/$SERVICE_NAME" << EOF
$LOG_DIR/*.log {
    daily
    missingok
    rotate 14
    compress
    delaycompress
    notifempty
    create 0640 root root
    sharedscripts
    postrotate
        systemctl reload $SERVICE_NAME > /dev/null 2>&1 || true
    endscript
}
EOF

echo -e "${GREEN}[6/7] Configuring systemd...${NC}"
systemctl daemon-reload
systemctl enable "$SERVICE_NAME"

echo -e "${GREEN}[7/7] Starting service...${NC}"
systemctl start "$SERVICE_NAME"

# Wait for service to start
sleep 2

# Check service status
if systemctl is-active --quiet "$SERVICE_NAME"; then
    echo ""
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}  Installation Successful!${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
    echo -e "Service Name: ${WHITE}$SERVICE_NAME${NC}"
    echo -e "Status:       ${GREEN}Running${NC}"
    echo -e "Install Dir:  ${WHITE}$INSTALL_DIR${NC}"
    echo -e "Config Dir:   ${WHITE}$CONFIG_DIR${NC}"
    echo -e "Log Dir:      ${WHITE}$LOG_DIR${NC}"
    echo ""
    echo -e "${CYAN}Management Commands:${NC}"
    echo -e "  Start:   ${WHITE}sudo systemctl start $SERVICE_NAME${NC}"
    echo -e "  Stop:    ${WHITE}sudo systemctl stop $SERVICE_NAME${NC}"
    echo -e "  Status:  ${WHITE}sudo systemctl status $SERVICE_NAME${NC}"
    echo -e "  Logs:    ${WHITE}sudo journalctl -u $SERVICE_NAME -f${NC}"
    echo ""
    echo -e "${CYAN}To update API key:${NC}"
    echo -e "  ${WHITE}$INSTALL_DIR/paryty-agent --key <new-api-key>${NC}"
    echo -e "  ${WHITE}sudo systemctl restart $SERVICE_NAME${NC}"
    echo ""
else
    echo -e "${RED}[!] Service failed to start. Check logs:${NC}"
    echo -e "  ${WHITE}sudo journalctl -u $SERVICE_NAME -n 50${NC}"
    exit 1
fi
