#!/bin/bash
# Manual infrastructure deployment for Paryty
# Temporary workaround for podman-compose and aardvark-dns issues on Windows
# Uses default SLIRP networking instead of custom networks

set -e

echo "=== Cleaning up existing containers ==="
podman rm -f paryty-redpanda paryty-dragonfly paryty-questdb paryty-postgres 2>/dev/null || true
podman rm -f paryty-seaweedfs-master paryty-seaweedfs-volume paryty-seaweedfs-filer paryty-seaweedfs-s3 2>/dev/null || true
sleep 1

echo ""
echo "=== Starting Redpanda ==="
podman run -d \
  --name paryty-redpanda \
  --no-healthcheck \
  -p 9092:9092 \
  -p 9644:9644 \
  -p 8082:8082 \
  -p 8081:8081 \
  docker.io/redpandadata/redpanda:latest \
  redpanda start \
  --smp 1 \
  --memory 1G \
  --overprovisioned \
  --node-id 0 \
  --check=false \
  --kafka-addr PLAINTEXT://0.0.0.0:9092 \
  --advertise-kafka-addr PLAINTEXT://localhost:9092 \
  --pandaproxy-addr PLAINTEXT://0.0.0.0:8082 \
  --advertise-pandaproxy-addr PLAINTEXT://localhost:8082 \
  --schema-registry-addr PLAINTEXT://0.0.0.0:8081 \
  --rpc-addr 0.0.0.0:33145 \
  --advertise-rpc-addr localhost:33145

echo ""
echo "=== Starting Dragonfly ==="
podman run -d \
  --name paryty-dragonfly \
  --no-healthcheck \
  -p 6379:6379 \
  docker.dragonflydb.io/dragonflydb/dragonfly:latest

echo ""
echo "=== Starting Questdb ==="
podman run -d \
  --name paryty-questdb \
  --no-healthcheck \
  -p 9000:9000 \
  -p 9009:9009 \
  -p 8812:8812 \
  docker.io/questdb/questdb:latest

echo ""
echo "=== Starting Postgres ==="
podman run -d \
  --name paryty-postgres \
  --no-healthcheck \
  -p 5432:5432 \
  -e POSTGRES_DB=paryty \
  -e POSTGRES_USER=paryty \
  -e POSTGRES_PASSWORD=paryty \
  docker.io/library/postgres:16-alpine

echo ""
echo "=== Starting Seaweedfs Master ==="
podman run -d \
  --name paryty-seaweedfs-master \
  --no-healthcheck \
  --network=host \
  docker.io/chrislusf/seaweedfs:latest \
  master -ip=localhost -port=9333 -volumePreallocate=false

echo ""
echo "=== Starting Seaweedfs Volume ==="
podman run -d \
  --name paryty-seaweedfs-volume \
  --no-healthcheck \
  --network=host \
  docker.io/chrislusf/seaweedfs:latest \
  volume -mserver=localhost:9333 -port=8080 -publicUrl=localhost:8080

echo ""
echo "=== Waiting for SeaweedFS Volume to start ==="
sleep 3

echo ""
echo "=== Starting Seaweedfs Filer ==="
podman run -d \
  --name paryty-seaweedfs-filer \
  --no-healthcheck \
  --network=host \
  docker.io/chrislusf/seaweedfs:latest \
  filer -master=localhost:9333 -port=8888

echo ""
echo "=== Waiting for SeaweedFS Filer to start ==="
sleep 3

echo ""
echo "=== Starting Seaweedfs S3 ==="
podman run -d \
  --name paryty-seaweedfs-s3 \
  --no-healthcheck \
  --network=host \
  docker.io/chrislusf/seaweedfs:latest \
  s3 -filer=localhost:8888 -port=8333

echo ""
echo "=== Waiting for all services to initialize ==="
sleep 5

echo ""
echo "=== Infrastructure Deployment Complete ==="
echo "Checking container status..."
podman ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
