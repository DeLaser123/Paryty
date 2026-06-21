# Paryty Operations Runbook

## Overview

This runbook covers day-2 operations for the Paryty observability platform — startup, health checks, common issues, backup, and rollback procedures.

---

## Startup Procedure

### Development (Podman Compose)

#### Step 1: Start Infrastructure

Infrastructure services include Redpanda, Dragonfly, QuestDB, PostgreSQL, and SeaweedFS.

```bash
cd paryty-v1.0

# Start infrastructure only (no Paryty application services)
podman-compose --env-file .env.dev -f deploy/compose/docker-compose.infra.yaml up -d

# Verify infrastructure health
podman ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
```

#### Step 2: Start Application Services

Application services include Ingestion, Query, Pipeline, and Intelligence.

```bash
# Start all application services + infrastructure
podman-compose --env-file .env.dev -f deploy/compose/docker-compose.dev.yaml up -d

# Or start infrastructure first, then application services separately
podman-compose --env-file .env.dev -f deploy/compose/docker-compose.infra.yaml up -d
podman-compose --env-file .env.dev -f deploy/compose/docker-compose.dev.yaml up -d
```

#### Step 3: Start Frontend (Native)

The frontend runs natively via Vite dev server (not in a container):

```bash
cd frontend
npm install
npm run dev
# Frontend available at http://localhost:5173
```

#### Step 4: Verify Health

```bash
# Query service health
curl http://localhost:8080/health

# Ingestion service (gRPC, use grpcurl or check port)
# grpcurl -plaintext localhost:50052 grpc.health.v1.Health/Check

# QuestDB web console
curl http://localhost:9000/status

# Dragonfly
redis-cli -p 6379 ping

# Redpanda
rpk cluster health --api-urls localhost:9644
```

### Production (Kubernetes + Helm)

#### Step 1: Pre-Flight Checks

```bash
# Verify cluster access
kubectl cluster-info

# Verify namespace exists
kubectl get namespace paryty

# Verify secrets exist
kubectl get secrets -n paryty
```

#### Step 2: Deploy with Helm

```bash
# Install (fresh deployment)
helm install paryty deploy/helm/paryty/ \
  -f deploy/helm/paryty/values-prod.yaml \
  --namespace paryty \
  --create-namespace

# Or upgrade existing deployment
helm upgrade paryty deploy/helm/paryty/ \
  -f deploy/helm/paryty/values-prod.yaml \
  --namespace paryty
```

#### Step 3: Verify Rollout

```bash
# Check all deployments
kubectl rollout status deployment/paryty-query -n paryty
kubectl rollout status deployment/paryty-ingestion -n paryty
kubectl rollout status deployment/paryty-pipeline -n paryty

# Check pods
kubectl get pods -n paryty -o wide

# Check services
kubectl get svc -n paryty

# Check logs
kubectl logs -f deployment/paryty-query -n paryty --tail=100
```

#### Step 4: Verify Endpoints

```bash
# Port-forward query service
kubectl port-forward svc/paryty-query 8080:8080 -n paryty

# Health check
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

---

## Health Checks

All Paryty services expose standard health endpoints:

| Endpoint | Purpose | Healthy Response | Unhealthy Response |
|----------|---------|------------------|--------------------|
| `GET /healthz` | Liveness probe — "I am alive" | `200 OK` | `503 Service Unavailable` |
| `GET /readyz` | Readiness probe — "I can serve traffic" | `200 OK` | `503 Service Unavailable` |

### What Readiness Checks

| Component | Readiness Check |
|-----------|-----------------|
| Ingestion | gRPC port open, Redpanda connectivity |
| Query | Dragonfly connectivity, QuestDB connectivity |
| Pipeline | Redpanda connectivity, all stages initialized |
| Intelligence | Model loaded, gRPC port open |
| Frontend | API endpoint reachable |

### Health Check Automation

```bash
# Check all services at once
for port in 8080 50052 50051; do
  echo "Port $port: $(curl -s -o /dev/null -w '%{http_code}' http://localhost:$port/healthz)"
done
```

---

## Common Issues

### Redpanda Broker Down

**Symptom:** Ingestion service logs show `connection refused` or `broker not available`.

**Diagnosis:**
```bash
# Check if container is running
podman ps -a --filter name=paryty-redpanda

# Check logs
podman logs paryty-redpanda --tail=50

# Check disk space (Redpanda stops writing if disk is full)
podman exec paryty-redpanda df -h /var/lib/redpanda/data

# Check Redpanda health
podman exec paryty-redpanda rpk cluster health
```

**Common Causes & Fixes:**
| Cause | Fix |
|-------|-----|
| Container crashed | `podman restart paryty-redpanda` |
| Disk full | Clean old topics: `rpk topic delete paryty.* --retention.ms=0` |
| Memory limit hit | Increase memory in compose file: `--memory 2G` |
| Port conflict | Check `netstat -tlnp | grep 9092` and resolve |

---

### QuestDB OOM (Out of Memory)

**Symptom:** QuestDB container restarts repeatedly. `podman stats` shows memory at limit.

**Diagnosis:**
```bash
# Check container stats
podman stats paryty-questdb --no-stream

# Check container logs for OOM
podman logs paryty-questdb --tail=100 | grep -i "out of memory"

# Check QuestDB health
curl http://localhost:9000/status
```

**Fix:**
```bash
# Increase memory limit in compose file
# In docker-compose.dev.yaml, add to questdb service:
#   mem_limit: 4g
# Or use deploy.resources.limits.memory in Kubernetes

# Restart after config change
podman-compose --env-file .env.dev -f deploy/compose/docker-compose.dev.yaml up -d questdb
```

---

### Dragonfly Connection Refused

**Symptom:** Query service fails to start. Logs show `connection refused` on port 6379.

**Diagnosis:**
```bash
# Check if Dragonfly is running
podman ps -a --filter name=paryty-dragonfly

# Test connectivity
redis-cli -p 6379 ping

# Check Dragonfly logs
podman logs paryty-dragonfly --tail=50
```

**Fix:**
```bash
# Restart Dragonfly
podman restart paryty-dragonfly

# If port conflict, check what's using 6379
netstat -tlnp | grep 6379

# If persistent, check memory limits
podman stats paryty-dragonfly --no-stream
```

---

### SeaweedFS Unavailable

**Symptom:** Timeline snapshots fail. Logs show S3 connection errors.

**Diagnosis:**
```bash
# Check all SeaweedFS components
podman ps -a --filter name=paryty-seaweedfs

# Check master health
curl http://localhost:9333/cluster/status

# Check S3 gateway
curl http://localhost:8333
```

**Fix:**
```bash
# Restart in order: master → volume → filer → s3
podman restart paryty-seaweedfs-master
sleep 5
podman restart paryty-seaweedfs-volume
sleep 3
podman restart paryty-seaweedfs-filer
sleep 3
podman restart paryty-seaweedfs-s3
```

---

### Agent Cannot Connect to Cluster

**Symptom:** Agent logs show `connection refused` or `connection timeout` to ingestion endpoint.

**Diagnosis:**
```bash
# Verify ingestion service is running
podman ps --filter name=paryty-ingestion

# Test gRPC connectivity
# grpcurl -plaintext localhost:50052 grpc.health.v1.Health/Check

# Check network connectivity from agent host
telnet <cluster-ip> 50052

# Check agent logs
tail -f agent-out.log
tail -f agent-err.log
```

**Fix:**
1. Verify `PARYTY_CLUSTER_ENDPOINT` in agent config matches ingestion service address
2. Verify firewall rules allow traffic on port 50052
3. Verify ingestion service is healthy before agent starts

---

### Frontend Cannot Reach API

**Symptom:** Frontend shows "connection error" or empty topology.

**Diagnosis:**
```bash
# Check if query service is running
curl http://localhost:8080/health

# Check Vite proxy config in frontend/vite.config.ts
# Verify VITE_API_URL and VITE_WS_URL in .env.dev

# Check browser console for CORS errors
```

**Fix:**
1. Verify `VITE_API_URL=http://127.0.0.1:8080` in `.env.dev`
2. Verify `PARYTY_CORS_ORIGINS` includes `http://localhost:5173` in query service env
3. Restart query service after config change

---

## Backup Procedure

### PostgreSQL (Control Plane)

```bash
# Full backup
pg_dump -Fc -f paryty-backup-$(date +%Y%m%d).dump paryty

# With custom format (compressed, parallel restore)
pg_dump -Fc -Z 9 -f paryty-backup-$(date +%Y%m%d).dump paryty

# Verify backup
pg_restore --list paryty-backup-$(date +%Y%m%d).dump | head -20

# Restore
pg_restore -d paryty paryty-backup-$(date +%Y%m%d).dump
```

### QuestDB (Warm Store)

QuestDB does not support native backup commands. Use file-system snapshots:

```bash
# Stop QuestDB writes (optional, for consistency)
# Or use volume snapshots if on Kubernetes

# Snapshot the data volume
podman volume inspect questdb_data
# Copy data directory
cp -r /var/lib/questdb /backup/questdb-$(date +%Y%m%d)
```

### SeaweedFS (Cold Store)

```bash
# Volume snapshot via SeaweedFS admin API
curl http://localhost:9333/vol/vacuum

# Or snapshot the S3 bucket
aws --endpoint-url http://localhost:8333 s3 sync s3://paryty /backup/seaweedfs-$(date +%Y%m%d)
```

### Dragonfly (Hot Store)

Dragonfly is ephemeral (hot data, last 5 minutes). No backup needed — data repopulates from agents on restart.

---

## Rollback Procedure

### Helm Rollback (Kubernetes)

```bash
# View revision history
helm history paryty -n paryty

# Rollback to specific revision
helm rollback paryty <revision-number> -n paryty

# Verify rollback
kubectl rollout status deployment/paryty-query -n paryty
kubectl get pods -n paryty
```

### Database Migration Rollback

```bash
# Using golang-migrate
migrate -path cluster/migrations -database "$DATABASE_URL" down 1

# Verify current version
migrate -path cluster/migrations -database "$DATABASE_URL" version
```

### Compose Rollback (Development)

```bash
# Pull previous image tag
podman pull paryty/query:<previous-tag>

# Update compose file or override image
podman-compose --env-file .env.dev -f deploy/compose/docker-compose.dev.yaml up -d
```

---

## Scaling Guide

### Horizontal Scaling (Kubernetes)

```bash
# Scale query service
kubectl scale deployment/paryty-query --replicas=3 -n paryty

# Scale ingestion service
kubectl scale deployment/paryty-ingestion --replicas=5 -n paryty

# Scale pipeline service
kubectl scale deployment/paryty-pipeline --replicas=3 -n paryty

# Auto-scaling (if HPA configured)
kubectl get hpa -n paryty
```

### Resource Adjustments

```bash
# Check current resource usage
kubectl top pods -n paryty

# Adjust resource limits in values-prod.yaml, then:
helm upgrade paryty deploy/helm/paryty/ -f deploy/helm/paryty/values-prod.yaml -n paryty
```

---

## Log Aggregation

### Viewing Logs (Kubernetes)

```bash
# All pods in namespace
kubectl logs -f -l app=paryty -n paryty --tail=100

# Specific service
kubectl logs -f deployment/paryty-query -n paryty --tail=200

# Previous container (if crashed)
kubectl logs deployment/paryty-query -n paryty --previous
```

### Viewing Logs (Compose)

```bash
# All services
podman-compose --env-file .env.dev -f deploy/compose/docker-compose.dev.yaml logs -f

# Specific service
podman logs -f paryty-query --tail=100
podman logs -f paryty-ingestion --tail=100
podman logs -f paryty-pipeline --tail=100
```

---

## Incident Response Quick Reference

| Symptom | First Check | Escalation |
|---------|-------------|------------|
| No data in frontend | `curl localhost:8080/health` → check query service | Check Redpanda → Ingestion → Pipeline |
| High latency | `kubectl top pods` → check resource saturation | Scale up or check QuestDB query load |
| Agent disconnects | Check agent logs → check ingestion health | Check network → TLS certificates |
| Alerts not firing | Check pipeline logs → check alert rules | Check Dragonfly → alert evaluation loop |
| Timeline gaps | Check SeaweedFS health → check snapshot schedule | Check cold store connectivity |
| Memory pressure | `kubectl top pods` → identify offender | Scale horizontally or increase limits |
