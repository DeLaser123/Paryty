---
name: verify-kubernetes
description: Validate Kubernetes manifests and Helm charts for Paryty — syntax, security context, resource quotas, RBAC, and network policies.
---

# Verify Kubernetes — Manifest & Helm Verification

## Execution Steps

### Step 1: Validate Base Manifests
```bash
kubectl apply -k paryty-v1.0/deploy/kubernetes/base/ --dry-run=client 2>&1
```

### Step 2: Validate Overlays
```bash
kubectl apply -k paryty-v1.0/deploy/kubernetes/overlays/dev/ --dry-run=client 2>&1
kubectl apply -k paryty-v1.0/deploy/kubernetes/overlays/staging/ --dry-run=client 2>&1
kubectl apply -k paryty-v1.0/deploy/kubernetes/overlays/prod/ --dry-run=client 2>&1
```

### Step 3: Validate Helm Charts (if present)
```bash
helm lint paryty-v1.0/charts/paryty/ 2>&1
helm template paryty-v1.0/charts/paryty/ 2>&1 | kubectl apply --dry-run=client -f - 2>&1
```

## Security Checklist
- [ ] Non-root security context
- [ ] Read-only root filesystem
- [ ] No privileged containers
- [ ] Capabilities dropped (ALL)
- [ ] No host network or host PID

## Resource Checklist
- [ ] Resource requests defined
- [ ] Resource limits defined
- [ ] Requests <= Limits
- [ ] Namespace resource quota

## RBAC Checklist
- [ ] Service accounts defined
- [ ] Minimal role permissions
- [ ] No cluster-admin bindings
- [ ] Role bindings are namespace-scoped

## Paryty-Specific
- [ ] Cluster: gRPC health check on /healthz and /readyz
- [ ] Agent: DaemonSet for node-level deployment
- [ ] Frontend: HTTP health check
- [ ] Network policies restrict inter-service traffic

## Exit Protocol
- ALL pass: "Kubernetes verification passed"
- Invalid manifest: Report YAML error, STOP
- Security issue: Report container and issue, STOP
- RBAC issue: Report binding and permission, STOP
