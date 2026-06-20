#!/bin/bash
# Start Paryty Linux agent (gai-tech tenant)
set -e

BIN_SRC="/mnt/d/__Projects/Paryty/paryty-v1.0/agent/target/release/paryty-agent-linux-x86_64"
BIN_DST="/tmp/paryty-agent-gai-tech"
CONFIG="/mnt/d/__Projects/Paryty/paryty-v1.0/configs/agent/agent-gai-tech-linux.yaml"
LOGFILE="/mnt/d/__Projects/Paryty/paryty-v1.0/agent-linux-gaitech.log"

echo "Copying agent binary..."
cp "$BIN_SRC" "$BIN_DST"
chmod +x "$BIN_DST"

echo "Starting agent..."
RUST_LOG=debug exec "$BIN_DST" -c "$CONFIG" > "$LOGFILE" 2>&1
