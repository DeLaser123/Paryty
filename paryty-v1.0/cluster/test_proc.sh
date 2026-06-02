#!/bin/bash
for pid in $(ls -d /proc/[0-9]* 2>/dev/null | sed 's|/proc/||'); do
  echo -n "PID=$pid: "
  for f in stat status cmdline cgroup; do
    echo -n "$f="
    timeout 1 cat /proc/$pid/$f > /dev/null 2>&1 && echo -n "OK " || echo -n "TMO "
  done
  echo -n "fd="
  timeout 1 ls /proc/$pid/fd > /dev/null 2>&1 && echo "OK" || echo "TMO"
done
