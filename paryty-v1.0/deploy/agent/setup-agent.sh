#!/usr/bin/env bash
# Paryty Agent — VM Node Setup Script
#
# Run this inside the target Linux VM to install and start the agent.
#
# Prerequisites:
#   - paryty-agent-linux-x86_64 binary transferred to /tmp/paryty-agent
#   - agent-vm.yaml config transferred to /tmp/agent.yaml
#
# Usage:
#   sudo bash setup-agent.sh <cluster-host-ip>
#
# Example:
#   sudo bash setup-agent.sh 192.168.1.100

set -euo pipefail

CLUSTER_IP="${1:-192.168.1.100}"
INSTALL_DIR="/opt/paryty"
CONFIG_DIR="/etc/paryty"
SERVICE_NAME="paryty-agent"

echo "=== Paryty Agent Setup ==="
echo "Cluster endpoint: ${CLUSTER_IP}:50051"

# Create directories
mkdir -p "${INSTALL_DIR}/bin" "${CONFIG_DIR}"

# Install binary
if [ -f /tmp/paryty-agent ]; then
    mv /tmp/paryty-agent "${INSTALL_DIR}/bin/paryty-agent"
    chmod +x "${INSTALL_DIR}/bin/paryty-agent"
    echo "Binary installed to ${INSTALL_DIR}/bin/paryty-agent"
else
    echo "ERROR: /tmp/paryty-agent not found. Transfer the binary first."
    exit 1
fi

# Install config with correct cluster IP
if [ -f /tmp/agent.yaml ]; then
    sed "s/CLUSTER_HOST_IP/${CLUSTER_IP}/g" /tmp/agent.yaml > "${CONFIG_DIR}/agent.yaml"
    echo "Config installed to ${CONFIG_DIR}/agent.yaml"
else
    echo "ERROR: /tmp/agent.yaml not found. Transfer the config first."
    exit 1
fi

# Create systemd service
cat > "/etc/systemd/system/${SERVICE_NAME}.service" << EOF
[Unit]
Description=Paryty Observability Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/bin/paryty-agent -c ${CONFIG_DIR}/agent.yaml
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=paryty-agent

# Security hardening
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
ReadOnlyPaths=/proc /sys
ReadWritePaths=/tmp

# Resource limits
MemoryMax=128M
CPUQuota=25%

[Install]
WantedBy=multi-user.target
EOF

# Enable and start
systemctl daemon-reload
systemctl enable "${SERVICE_NAME}"
systemctl start "${SERVICE_NAME}"

echo ""
echo "=== Installation Complete ==="
echo "Binary:  ${INSTALL_DIR}/bin/paryty-agent"
echo "Config:  ${CONFIG_DIR}/agent.yaml"
echo "Service: ${SERVICE_NAME}"
echo ""
echo "Commands:"
echo "  systemctl status ${SERVICE_NAME}    # Check status"
echo "  journalctl -u ${SERVICE_NAME} -f    # Follow logs"
echo "  systemctl restart ${SERVICE_NAME}   # Restart"
echo "  systemctl stop ${SERVICE_NAME}      # Stop"
