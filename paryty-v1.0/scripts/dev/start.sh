#!/bin/bash
# Paryty Development Start Script

set -e

echo "Starting Paryty development environment..."

# Check if Podman is running
if ! podman info > /dev/null 2>&1; then
    echo "Error: Podman is not running. Please start Podman and try again."
    exit 1
fi

# Start infrastructure services
echo "Starting infrastructure services..."
podman-compose -f deploy/compose/docker-compose.dev.yaml up -d

# Wait for services to be healthy
echo "Waiting for services to be ready..."
sleep 10

# Check service health
echo "Checking service health..."

# Check Redpanda
if curl -s http://localhost:9644/v1/status/ready > /dev/null 2>&1; then
    echo "✓ Redpanda is ready"
else
    echo "✗ Redpanda is not ready"
fi

# Check Dragonfly
if redis-cli -h localhost -p 6379 ping > /dev/null 2>&1; then
    echo "✓ Dragonfly is ready"
else
    echo "✗ Dragonfly is not ready"
fi

# Check QuestDB
if curl -s http://localhost:9009/status > /dev/null 2>&1; then
    echo "✓ QuestDB is ready"
else
    echo "✗ QuestDB is not ready"
fi

# Check PostgreSQL
if pg_isready -h localhost -p 5432 > /dev/null 2>&1; then
    echo "✓ PostgreSQL is ready"
else
    echo "✗ PostgreSQL is not ready"
fi

# Check SeaweedFS
if curl -s http://localhost:9333/cluster/status > /dev/null 2>&1; then
    echo "✓ SeaweedFS is ready"
else
    echo "✗ SeaweedFS is not ready"
fi

echo ""
echo "Infrastructure services started successfully!"
echo ""
echo "Services:"
echo "  - Redpanda:    http://localhost:9092 (Kafka API), http://localhost:9644 (Admin)"
echo "  - Dragonfly:   http://localhost:6379 (Redis API)"
echo "  - QuestDB:     http://localhost:9009 (Web Console), http://localhost:8812 (InfluxDB)"
echo "  - PostgreSQL:  http://localhost:5432"
echo "  - SeaweedFS:   http://localhost:9333 (Master), http://localhost:8333 (S3 API)"
echo ""
echo "To start the Paryty services, run:"
echo "  ./scripts/dev/start-services.sh"
