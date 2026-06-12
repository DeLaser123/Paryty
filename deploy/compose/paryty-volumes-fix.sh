#!/bin/bash
# paryty-volumes-fix.sh
# 
# Problem: podman stores volumes at /mnt/d/podman/containers/volumes (D: via V9FS)
#          V9FS doesn't support mmap — database containers (QuestDB, Postgres) crash with SIGSEGV
#
# Solution: Bind-mount volumes/ to the VM's ext4 filesystem
#           Images and container layers stay on D: (no mmap needed)
#           Volumes (databases) go to ext4 (mmap-safe)
#
# This script runs at every podman machine boot via /etc/wsl.conf [boot] command.

EXT4_VOLUMES="/var/lib/containers/storage/volumes"
D_VOLUMES="/mnt/d/podman/containers/volumes"

# Create both directories
mkdir -p "$EXT4_VOLUMES"
mkdir -p "$D_VOLUMES"

# Only bind-mount if not already mounted
if ! mountpoint -q "$D_VOLUMES" 2>/dev/null; then
    mount --bind "$EXT4_VOLUMES" "$D_VOLUMES"
    echo "[paryty-volumes-fix] Bind-mounted volumes to ext4"
else
    echo "[paryty-volumes-fix] Volumes already bind-mounted"
fi
