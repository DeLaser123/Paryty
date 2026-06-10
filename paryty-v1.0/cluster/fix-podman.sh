#!/bin/bash
echo "=== Podman machine diagnostics ==="
echo "SS port 51553 listeners:"
ss -tlnp 2>/dev/null | grep 51553 || echo "  NOT LISTENING"
echo ""
echo "SSH service status:"
systemctl is-active sshd 2>/dev/null || echo "  systemctl not available"
echo ""
echo "Starting SSH..."
nohup /usr/sbin/sshd -D -p 51553 &>/dev/null &
sleep 2
echo ""
echo "SSH port after restart:"
ss -tlnp 2>/dev/null | grep 51553 || echo "  STILL NOT LISTENING"
