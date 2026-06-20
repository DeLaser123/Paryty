#!/bin/bash
for ip in localhost 192.168.16.1 172.30.80.1; do
  timeout 1 bash -c "echo > /dev/tcp/$ip/50052" 2>/dev/null && echo "$ip:50052 OK" || echo "$ip:50052 FAIL"
done
echo "---"
ip route show default
echo "---"
cat /etc/resolv.conf | grep nameserver
