# CD Pipeline — GitHub Actions Continuous Deployment

## Purpose
Validate and run the CD pipeline for Paryty deployment. Covers container image building, registry push, environment promotion, and rollback procedures.

## Pipeline Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    CD Pipeline                           │
├──────────┬──────────┬──────────┬──────────┬─────────────┤
│  Build   │  Push    │  Deploy  │  Verify  │  Rollback   │
│  Images  │  Registry│  Target  │  Health  │  Strategy   │
├──────────┼──────────┼──────────┼──────────┼─────────────┤
│ Cluster  │ GHCR     │ Dev      │ Healthz  │ Automatic   │
│ Agent    │          │ Staging  │ Readyz   │ Manual      │
│ Frontend │          │ Prod     │ Metrics  │ Canary      │
└──────────┴──────────┴──────────┴──────────┴─────────────┘
```

## Environment Promotion Flow

```
main branch ──► Dev (automatic)
                    │
                    ▼
              Staging (manual approval)
                    │
                    ▼
              Production (manual approval + canary)
```

## Execution Steps

### Step 1: Validate Container Images
Run: docker build -f deploy/docker/Dockerfile.cluster -t paryty-cluster:test . 2>&1
Expected: Image builds successfully.

### Step 2: Scan Container Images
Run: trivy image --severity HIGH,CRITICAL paryty-cluster:test 2>&1
Expected: No critical/high vulnerabilities.

### Step 3: Push to Registry
Run: docker push ghcr.io/paryty/paryty-cluster:latest 2>&1
Expected: Image pushed successfully.

### Step 4: Deploy to Dev
Run: kubectl apply -k deploy/kubernetes/overlays/dev/ 2>&1
Expected: All pods running in dev namespace.

### Step 5: Verify Deployment Health
Run: kubectl get pods -n paryty-dev 2>&1
Run: kubectl logs -n paryty-dev -l app=paryty-cluster --tail=50 2>&1
Expected: All pods healthy, no error logs.

### Step 6: Promote to Staging
Run: kubectl apply -k deploy/kubernetes/overlays/staging/ 2>&1
Expected: All pods running in staging namespace.

### Step 7: Promote to Production (Canary)
Run: kubectl apply -k deploy/kubernetes/overlays/prod/ 2>&1
Expected: Canary deployment starts, health checks pass.

## Rollback Procedures

### Automatic Rollback
- Health check fails after deployment
- Error rate exceeds threshold (> 1%)
- Latency exceeds threshold (> 2x baseline)

### Manual Rollback
```bash
# Rollback to previous version
kubectl rollout undo deployment/paryty-cluster -n paryty-prod

# Rollback to specific revision
kubectl rollout undo deployment/paryty-cluster -n paryty-prod --to-revision=2

# Check rollout history
kubectl rollout history deployment/paryty-cluster -n paryty-prod
```

### Canary Rollback
```bash
# Scale down canary
kubectl scale deployment/paryty-cluster-canary -n paryty-prod --replicas=0

# Scale up stable
kubectl scale deployment/paryty-cluster -n paryty-prod --replicas=3
```

## Exit Protocol
- ALL steps pass: Report "CD pipeline passed"
- Image build fails: Report Dockerfile error, STOP
- Image scan fails: Report vulnerabilities, STOP
- Push fails: Report registry error, STOP
- Deploy fails: Report Kubernetes error, STOP
- Health check fails: Trigger automatic rollback, STOP

## Notes
- CD runs on main branch pushes and manual triggers
- Dev deployment is automatic
- Staging requires manual approval
- Production requires manual approval + canary verification
- Rollback is automatic on health check failure
