#!/bin/bash
# Deploy Paryty Cluster Services (Manual podman run approach)
# This script builds and deploys all Paryty services using podman run commands
# WORKAROUND: Uses default networking (no custom networks) due to WSL aardvark-dns bug

set -e

echo "=== Building Paryty Cluster Services ==="

# Build cluster services
echo "Building ingestion service..."
podman build --build-arg SERVICE=ingestion -t paryty-ingestion -f deploy/docker/Dockerfile.cluster cluster/

echo "Building query service..."
podman build --build-arg SERVICE=query -t paryty-query -f deploy/docker/Dockerfile.cluster cluster/

echo "Building aggregator service..."
podman build --build-arg SERVICE=aggregator -t paryty-aggregator -f deploy/docker/Dockerfile.cluster cluster/

echo "Building correlator service..."
podman build --build-arg SERVICE=correlator -t paryty-correlator -f deploy/docker/Dockerfile.cluster cluster/

echo "Building enricher service..."
podman build --build-arg SERVICE=enricher -t paryty-enricher -f deploy/docker/Dockerfile.cluster cluster/

echo "Building frontend..."
podman build -t paryty-frontend -f deploy/docker/Dockerfile.frontend frontend/

echo "=== All images built successfully ==="

echo "=== Starting Paryty Cluster Services ==="

# Ingestion service (ports 28080, 28081 - mapped to 8080, 8081 inside container)
# NOTE: Using 28080/28081 to avoid conflicts with infra services using host networking
echo "Starting ingestion service..."
podman run -d \
  --name paryty-ingestion \
  --no-healthcheck \
  -p 28080:8080 \
  -p 28081:8081 \
  -e PORT=8080 \
  -e REDPANDA_URL=host.containers.internal:9092 \
  -e DRAGONFLY_URL=redis://host.containers.internal:6379 \
  -e QUESTDB_URL=postgres://paryty:paryty@host.containers.internal:8812/paryty \
  -e SEAWEEDFS_URL=host.containers.internal:8333 \
  paryty-ingestion

sleep 2

# Query service (port 28082 -> 8080 inside container)
# NOTE: Using 28082 to avoid conflicts with infra services using host networking
echo "Starting query service..."
podman run -d \
  --name paryty-query \
  --no-healthcheck \
  -p 28082:8080 \
  -e PORT=8080 \
  -e REDPANDA_URL=host.containers.internal:9092 \
  -e DRAGONFLY_URL=redis://host.containers.internal:6379 \
  -e QUESTDB_URL=postgres://paryty:paryty@host.containers.internal:8812/paryty \
  -e SEAWEEDFS_URL=host.containers.internal:8333 \
  paryty-query

sleep 2

# Aggregator service (no exposed ports - internal only)
echo "Starting aggregator service..."
podman run -d \
  --name paryty-aggregator \
  --no-healthcheck \
  -e REDPANDA_URL=host.containers.internal:9092 \
  -e AGGREGATION_WINDOW=1m \
  paryty-aggregator

sleep 2

# Correlator service (no exposed ports - internal only)
echo "Starting correlator service..."
podman run -d \
  --name paryty-correlator \
  --no-healthcheck \
  -e REDPANDA_URL=host.containers.internal:9092 \
  paryty-correlator

sleep 2

# Enricher service (no exposed ports - internal only)
echo "Starting enricher service..."
podman run -d \
  --name paryty-enricher \
  --no-healthcheck \
  -e REDPANDA_URL=host.containers.internal:9092 \
  -e DRAGONFLY_URL=redis://host.containers.internal:6379 \
  -e QUESTDB_URL=postgres://paryty:paryty@host.containers.internal:8812/paryty \
  -e SEAWEEDFS_URL=host.containers.internal:8333 \
  paryty-enricher

sleep 2

# Frontend (port 3000)
echo "Starting frontend..."
podman run -d \
  --name paryty-frontend \
  --no-healthcheck \
  -p 3000:3000 \
  paryty-frontend

sleep 3

echo "=== Paryty Cluster Services Deployed ==="
echo ""
echo "Services running:"
echo "  - Ingestion:  http://localhost:28080 (gRPC), http://localhost:28081 (health)"
echo "  - Query API:  http://localhost:28082"
echo "  - Frontend:   http://localhost:3000"
echo "  - Aggregator: (internal)"
echo "  - Correlator: (internal)"
echo "  - Enricher:   (internal)"
echo ""
echo "Check status: wsl -d podman-machine-default podman ps"
