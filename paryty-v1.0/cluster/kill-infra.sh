#!/bin/bash
set -e
echo "Killing native WSL infra processes (sudo)..."

for port in 6379 9092 8812 9009 9000 8080 8333 9333 8888 8081 8082 9644; do
    pids=$(sudo ss -tlnp 2>/dev/null | grep ":$port " | sed -n 's/.*pid=\([0-9]*\).*/\1/p')
    for pid in $pids; do
        if [ -n "$pid" ] && [ "$pid" != "" ]; then
            proc_name=$(ps -p $pid -o comm= 2>/dev/null || echo "unknown")
            echo "Killing PID $pid ($proc_name) on port $port"
            sudo kill -9 $pid 2>/dev/null || echo "  -> failed"
        fi
    done
done

sleep 1
echo "Remaining listeners:"
sudo ss -tlnp 2>/dev/null | grep -E ':(6379|9092|8812|9009|9000|8080|8333|9333|8888|8081|8082|9644) ' || echo "(none)"
