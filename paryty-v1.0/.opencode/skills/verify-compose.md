# Verify Compose — Docker Compose Verification

## Purpose
Validate Docker Compose files for syntax, service dependencies, health checks, and configuration correctness.

## Execution Steps

### Step 1: Validate Syntax
Run: docker compose -f deploy/compose/docker-compose.dev.yaml config --quiet 2>&1
Expected: No syntax errors.

### Step 2: Check Service Dependencies
Run: docker compose -f deploy/compose/docker-compose.dev.yaml config 2>&1
Expected: All dependencies are valid service names.

### Step 3: Verify Health Checks
Check: All critical services have health checks
Check: Health check intervals are reasonable
Check: Health check commands are valid

### Step 4: Verify Resource Limits
Check: Memory limits set for all services
Check: CPU limits set where appropriate
Check: No service can consume unbounded resources

### Step 5: Verify Network Configuration
Check: Services use internal network for inter-service communication
Check: Only necessary ports exposed to host
Check: No port conflicts

### Step 6: Verify Volume Mounts
Check: Named volumes for persistent data
Check: Bind mounts use relative paths
Check: No sensitive data in bind mounts

## Best Practices Checklist

### Service Configuration
- [ ] All services have restart policy
- [ ] Critical services have health checks
- [ ] Resource limits defined
- [ ] Environment variables use .env file

### Networking
- [ ] Internal network for service communication
- [ ] Only necessary ports exposed
- [ ] No port conflicts
- [ ] DNS resolution works between services

### Volumes
- [ ] Named volumes for persistent data
- [ ] Volume permissions correct
- [ ] No sensitive data in volumes

### Paryty-Specific Services
- [ ] Redpanda: Kafka API port (9092) exposed
- [ ] Redpanda: Admin port (9644) exposed
- [ ] Dragonfly: Redis API port (6379) exposed
- [ ] QuestDB: PostgreSQL port (9000) exposed
- [ ] QuestDB: Web console port (9009) exposed
- [ ] SeaweedFS: Master port (9333) exposed
- [ ] SeaweedFS: S3 API port (8333) exposed

### Service Dependencies
- [ ] SeaweedFS volume depends on master
- [ ] SeaweedFS filer depends on master + volume
- [ ] SeaweedFS S3 depends on filer
- [ ] Application services depend on infrastructure

## Exit Protocol
- ALL checks pass: Report "Compose verification passed"
- Syntax error: Report line and error, STOP
- Missing health check: Report service name, STOP
- Port conflict: Report conflicting ports, STOP
- Dependency issue: Report dependency chain, STOP

## Notes
- Run after any compose file change
- Use `docker compose config` to validate
- Use `docker compose up --dry-run` to test (if available)
