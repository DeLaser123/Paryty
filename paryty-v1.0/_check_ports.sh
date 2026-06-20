#!/bin/bash
for port in 50052 8082 3000; do
  timeout 1 bash -c "echo x > /dev/tcp/localhost/$port" 2>/dev/null && echo "localhost:$port OK" || echo "localhost:$port FAIL"
done
