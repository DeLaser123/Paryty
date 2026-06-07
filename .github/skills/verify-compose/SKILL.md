---
name: verify-compose
description: Validate Docker/Podman Compose files for Paryty — syntax, health checks, resource limits, networking, and service dependencies.
---

# Verify Compose — Docker Compose Verification

## Execution Steps

### Step 1: Validate Syntax
```bash
cd paryty-v1.0 && podman-compose -f deploy/compose/docker-compose.dev.yaml config --quiet 2>&1
```

### Step 2: Check Service Dependencies
```bash
cd paryty-v1.0 && podman-compose -f deploy/compose/docker-compose.dev.yaml config 2>&1
```

### Step 3: Verify Health Checks
Check all critical services have health checks with reasonable intervals.

### Step 4: Verify Resource Limits
Check memory and CPU limits set for all services.

### Step 5: Verify Network Configuration
Check internal network usage and port exposure.

## Checklist
- [ ] All services have restart policy
- [ ] Critical services have health checks
- [ ] Resource limits defined
- [ ] Environment variables use .env file
- [ ] Internal network for service communication
- [ ] Only necessary ports exposed
- [ ] Named volumes for persistent data

### Paryty-Specific Services
- [ ] Redpanda: Kafka API (9092) + Admin (9644)
- [ ] Dragonfly: Redis API (6379)
- [ ] QuestDB: PostgreSQL (9000) + Web console (9009)
- [ ] SeaweedFS: Master (9333) + S3 API (8333)
- [ ] SeaweedFS volume depends on master
- [ ] SeaweedFS filer depends on master + volume
- [ ] Application services depend on infrastructure

## Exit Protocol
- ALL pass: "Compose verification passed"
- Syntax error: Report line and error, STOP
- Missing health check: Report service, STOP
- Port conflict: Report conflicting ports, STOP
