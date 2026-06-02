#!/bin/bash
# vm-setup.sh - M7.2-M7.4: Linux VM Setup for Paryty Agent
#
# Run this script inside the Ubuntu 22.04+ VM to install all required tools.
#
# Usage:
#   chmod +x vm-setup.sh
#   sudo ./vm-setup.sh
#
# This script installs:
#   - Rust toolchain (rustup)
#   - Go toolchain
#   - protoc (Protocol Buffers compiler)
#   - Podman
#   - Build essentials (gcc, make, pkg-config, etc.)
#   - eBPF tools (bpftrace, bpftool)
#   - Paryty agent build dependencies

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() { echo -e "${GREEN}[+]${NC} $1"; }
warn() { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[-]${NC} $1"; exit 1; }

# Check if running as root
if [[ $EUID -ne 0 ]]; then
   error "This script must be run as root (use sudo)"
fi

echo -e "\n=== Paryty v1.0 Agent VM Setup ==="
echo "Timestamp: $(date -Iseconds)"
echo ""

# --- Step 1: System Update ---
log "Updating system packages..."
apt-get update -qq
apt-get upgrade -y -qq

# --- Step 2: Install Build Essentials ---
log "Installing build essentials..."
apt-get install -y -qq \
    build-essential \
    pkg-config \
    libssl-dev \
    curl \
    wget \
    git \
    unzip \
    jq \
    htop \
    iotop \
    net-tools \
    dnsutils \
    ca-certificates \
    gnupg \
    lsb-release

# --- Step 3: Check eBPF Support ---
log "Checking eBPF kernel support..."
KERNEL_VERSION=$(uname -r | cut -d. -f1,2)
KERNEL_MAJOR=$(echo "$KERNEL_VERSION" | cut -d. -f1)
KERNEL_MINOR=$(echo "$KERNEL_VERSION" | cut -d. -f2)

if [[ $KERNEL_MAJOR -lt 5 ]] || [[ $KERNEL_MAJOR -eq 5 && $KERNEL_MINOR -lt 10 ]]; then
    warn "Kernel $KERNEL_VERSION detected. eBPF features require 5.10+. Consider upgrading."
    warn "Run: sudo apt install --install-recommends linux-generic-hwe-22.04"
else
    log "Kernel $KERNEL_VERSION supports eBPF ✓"
fi

# Check BPF filesystem
if [[ -d /sys/fs/bpf ]]; then
    log "BPF filesystem mounted ✓"
else
    warn "BPF filesystem not found at /sys/fs/bpf"
    warn "Mount with: sudo mount -t bpf bpf /sys/fs/bpf"
fi

# --- Step 4: Install eBPF Tools ---
log "Installing eBPF tools..."
apt-get install -y -qq bpftrace linux-tools-$(uname -r) linux-tools-generic 2>/dev/null || {
    warn "Could not install kernel-specific tools, trying generic..."
    apt-get install -y -qq bpftrace 2>/dev/null || warn "bpftrace installation failed"
}

# Verify eBPF
if command -v bpftrace &>/dev/null; then
    log "bpftrace installed ✓"
    # Test eBPF (requires root)
    if bpftrace -e 'BEGIN { printf("eBPF works!\n"); exit(); }' 2>/dev/null; then
        log "eBPF functional test passed ✓"
    else
        warn "eBPF test failed - may need kernel upgrade or different VM config"
    fi
else
    warn "bpftrace not available"
fi

# --- Step 5: Install Rust ---
log "Installing Rust toolchain..."
if command -v rustc &>/dev/null; then
    log "Rust already installed: $(rustc --version)"
else
    # Install rustup for the default user (not root)
    DEFAULT_USER=${SUDO_USER:-$USER}
    su - "$DEFAULT_USER" -c 'curl --proto "=https" --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y'
    log "Rust installed for user: $DEFAULT_USER"
fi

# --- Step 6: Install Go ---
log "Installing Go..."
GO_VERSION="1.25.0"
if command -v go &>/dev/null; then
    log "Go already installed: $(go version)"
else
    GO_ARCH=$(dpkg --print-architecture)
    case "$GO_ARCH" in
        amd64) GO_ARCH="amd64" ;;
        arm64) GO_ARCH="arm64" ;;
        *) error "Unsupported architecture: $GO_ARCH" ;;
    esac
    
    wget -q "https://go.dev/dl/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz" -O /tmp/go.tar.gz
    rm -rf /usr/local/go
    tar -C /usr/local -xzf /tmp/go.tar.gz
    rm /tmp/go.tar.gz
    
    # Add to PATH
    echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile.d/go.sh
    echo 'export PATH=$PATH:$HOME/go/bin' >> /etc/profile.d/go.sh
    chmod +x /etc/profile.d/go.sh
    
    log "Go $GO_VERSION installed ✓"
fi

# --- Step 7: Install protoc ---
log "Installing protoc..."
PROTOC_VERSION="25.1"
if command -v protoc &>/dev/null; then
    log "protoc already installed: $(protoc --version)"
else
    PROTOC_ARCH=$(dpkg --print-architecture)
    case "$PROTOC_ARCH" in
        amd64) PROTOC_ARCH="x86_64" ;;
        arm64) PROTOC_ARCH="aarch_64" ;;
    esac
    
    wget -q "https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-linux-${PROTOC_ARCH}.zip" -O /tmp/protoc.zip
    unzip -q /tmp/protoc.zip -d /usr/local
    rm /tmp/protoc.zip
    chmod +x /usr/local/bin/protoc
    
    log "protoc $(protoc --version) installed ✓"
fi

# --- Step 8: Install Podman ---
log "Installing Podman..."
if command -v podman &>/dev/null; then
    log "Podman already installed: $(podman --version)"
else
    # Add Podman repository
    source /etc/os-release
    mkdir -p /etc/apt/keyrings
    curl -fsSL "https://download.opensuse.org/repositories/devel:/kubic:/libcontainers:/unstable/xUbuntu_${VERSION_ID}/Release.key" | \
        gpg --dearmor -o /etc/apt/keyrings/devel-kubic-libcontainers-unstable.gpg 2>/dev/null || true
    
    echo "deb [signed-by=/etc/apt/keyrings/devel-kubic-libcontainers-unstable.gpg] https://download.opensuse.org/repositories/devel:/kubic:/libcontainers:/unstable/xUbuntu_${VERSION_ID}/ /" | \
        tee /etc/apt/sources.list.d/devel-kubic-libcontainers-unstable.list > /dev/null
    
    apt-get update -qq
    apt-get install -y -qq podman podman-compose 2>/dev/null || {
        warn "Podman from repo failed, trying apt..."
        apt-get install -y -qq podman 2>/dev/null || warn "Podman installation failed"
    }
    
    log "Podman $(podman --version 2>/dev/null || echo 'installed') ✓"
fi

# --- Step 9: Install Go tools ---
log "Installing Go tools..."
DEFAULT_USER=${SUDO_USER:-$USER}
su - "$DEFAULT_USER" -c '
    export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin
    go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
' 2>/dev/null || warn "Some Go tools failed to install"

# --- Step 10: Configure system for agent ---
log "Configuring system for Paryty agent..."

# Increase file descriptor limits
cat >> /etc/security/limits.conf <<EOF
# Paryty agent requirements
* soft nofile 65536
* hard nofile 65536
EOF

# Enable BPF filesystem on boot
if ! grep -q "bpf" /etc/fstab; then
    echo "bpf /sys/fs/bpf bpf defaults 0 0" >> /etc/fstab
fi

# --- Summary ---
echo ""
echo -e "\n=== Setup Complete ==="
echo "Installed:"
echo "  - Rust:     $(rustc --version 2>/dev/null || echo 'install failed')"
echo "  - Go:       $(go version 2>/dev/null || echo 'install failed')"
echo "  - protoc:   $(protoc --version 2>/dev/null || echo 'install failed')"
echo "  - Podman:   $(podman --version 2>/dev/null || echo 'install failed')"
echo "  - bpftrace: $(bpftrace --version 2>/dev/null || echo 'not available')"
echo "  - Kernel:   $(uname -r)"
echo ""
echo "Next steps:"
echo "  1. Log out and back in (for PATH changes)"
echo "  2. Clone the Paryty repository:"
echo "     git clone <repo-url> ~/paryty-v1.0"
echo "  3. Build the agent:"
echo "     cd ~/paryty-v1.0/agent && cargo build --release"
echo "  4. Configure and run:"
echo "     export PARYTY_CLUSTER_ADDR=<host-ip>:8080"
echo "     ./target/release/paryty-agent"
echo ""
