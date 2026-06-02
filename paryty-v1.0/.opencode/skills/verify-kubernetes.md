# Verify Kubernetes — Kubernetes Manifest Verification

## Purpose
Validate Kubernetes manifests for correctness, security, and best practices. Run after any manifest change.

## Execution Steps

### Step 1: Validate Manifests
Run: kubectl apply -k deploy/kubernetes/base/ --dry-run=client 2>&1
Expected: All manifests are valid.

### Step 2: Validate Overlays
Run: kubectl apply -k deploy/kubernetes/overlays/dev/ --dry-run=client 2>&1
Run: kubectl apply -k deploy/kubernetes/overlays/staging/ --dry-run=client 2>&1
Run: kubectl apply -k deploy/kubernetes/overlays/prod/ --dry-run=client 2>&1
Expected: All overlays are valid.

### Step 3: Security Context Check
Check: All containers run as non-root
Check: Read-only root filesystem where possible
Check: No privileged containers
Check: Capabilities dropped

### Step 4: Resource Quotas Check
Check: All containers have resource requests
Check: All containers have resource limits
Check: Requests <= Limits
Check: Namespace has resource quota

### Step 5: Network Policy Check
Check: Default deny ingress policy exists
Check: Explicit allow rules for required traffic
Check: No pods exposed without service

### Step 6: RBAC Check
Check: Service accounts have minimal permissions
Check: No cluster-admin bindings
Check: Role bindings are namespace-scoped

## Best Practices Checklist

### Security
- [ ] Non-root security context
- [ ] Read-only root filesystem
- [ ] No privileged containers
- [ ] Capabilities dropped (ALL)
- [ ] No host network
- [ ] No host PID

### Resources
- [ ] Resource requests defined
- [ ] Resource limits defined
- [ ] Requests <= Limits
- [ ] Namespace resource quota

### Networking
- [ ] Services defined for all pods
- [ ] Network policies restrict traffic
- [ ] Ingress/controller configured

### RBAC
- [ ] Service accounts defined
- [ ] Minimal role permissions
- [ ] No cluster-admin bindings

### Health Checks
- [ ] Liveness probes defined
- [ ] Readiness probes defined
- [ ] Startup probes for slow-starting services

### Paryty-Specific
- [ ] Cluster: gRPC health check
- [ ] Cluster: /healthz endpoint
- [ ] Cluster: /readyz endpoint
- [ ] Frontend: HTTP health check
- [ ] Agent: DaemonSet for node-level deployment

## Kustomize Structure
```
deploy/kubernetes/
├── base/
│   ├── kustomization.yaml
│   ├── namespace.yaml
│   ├── cluster-deployment.yaml
│   ├── cluster-service.yaml
│   ├── frontend-deployment.yaml
│   ├── frontend-service.yaml
│   └── network-policy.yaml
└── overlays/
    ├── dev/
    │   ├── kustomization.yaml
    │   └── patches/
    ├── staging/
    │   ├── kustomization.yaml
    │   └── patches/
    └── prod/
        ├── kustomization.yaml
        └── patches/
```

## Exit Protocol
- ALL checks pass: Report "Kubernetes verification passed"
- Invalid manifest: Report YAML error, STOP
- Security issue: Report pod/container and issue, STOP
- Missing resources: Report container name, STOP
- RBAC issue: Report binding and permission, STOP

## Notes
- Run after any manifest change
- Use `kubectl --dry-run=client` for validation
- Use `kustomize build` to render overlays
- Security scanning can use kube-score or kubesec
