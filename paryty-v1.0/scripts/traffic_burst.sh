#!/bin/bash
echo "=== STARTING 60s LIVE TRAFFIC ==="
start=$(date +%s)
count=0
while [ $(($(date +%s) - start)) -lt 60 ]; do
    count=$((count + 1))
    elapsed=$(($(date +%s) - start))
    echo "[$elapsed s] Request #$count"
    curl -s -o /dev/null -w "  -> %{remote_ip}:%{remote_port} (%{time_total}s)\n" https://example.com 2>/dev/null
    curl -s -o /dev/null -w "  -> %{remote_ip}:%{remote_port} (%{time_total}s)\n" https://httpbin.org/ip 2>/dev/null
    curl -s -o /dev/null -w "  -> %{remote_ip}:%{remote_port} (%{time_total}s)\n" https://www.google.com 2>/dev/null
    curl -s -o /dev/null -w "  -> %{remote_ip}:%{remote_port} (%{time_total}s)\n" https://github.com 2>/dev/null
    curl -s -o /dev/null -w "  -> %{remote_ip}:%{remote_port} (%{time_total}s)\n" https://api.github.com 2>/dev/null
    curl -s -o /dev/null -w "  -> %{remote_ip}:%{remote_port} (%{time_total}s)\n" "https://dns.google/resolve?name=test${count}.com" 2>/dev/null
done
echo "=== DONE: 60s traffic burst complete ==="
