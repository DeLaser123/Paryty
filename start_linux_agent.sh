#!/bin/bash
pkill -f paryty-agent 2>/dev/null
sleep 1
nohup /mnt/d/__Projects/Paryty/paryty-v1.0/agent/target/release/paryty-agent-linux-x86_64 \
  -c /mnt/d/__Projects/Paryty/paryty-v1.0/configs/agent/agent-gai-tech-linux.yaml \
  > /tmp/agent-linux2.log 2>&1 &
sleep 4
cat /tmp/agent-linux2.log
