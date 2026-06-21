# Paryty Istio Service Mesh Configuration

## Overview

Paryty uses Istio ambient mesh for service-to-service communication security (mTLS), traffic routing, and resilience (retries, circuit breaking).

All inter-service communication within the `paryty` namespace is encrypted with mutual TLS via Istio's automatic certificate management. No application code needs to handle TLS certificates — Istio injects sidecars that transparently encrypt all pod-to-pod traffic.

## Prerequisites

- Kubernetes 1.25+
- `istioctl` 1.20+ installed locally
- Helm 3+ (for Istio installation)
- cert-manager already installed (see `../tls/`)

## Installation

### 1. Install Istio with the ambient profile

```bash
# Install Istio with ambient mesh profile
istioctl install --set profile=ambient -y

# Verify installation
istioctl verify-install
kubectl get pods -n istio-system
```

The ambient profile is recommended for Paryty because:
- No sidecar injection required (lighter resource footprint)
- mTLS is handled at the node level by ztunnel
- Layer 7 policies (HTTP routing, retries) use waypoint proxies only where needed

### 2. Enable sidecar injection for the paryty namespace

If you prefer the classic sidecar model:

```bash
# Label the namespace for sidecar injection
kubectl label namespace paryty istio-injection=enabled --overwrite

# Verify the label
kubectl get namespace paryty --show-labels
```

### 3. Apply Paryty Istio configuration

```bash
# Apply all Istio resources
kubectl apply -f deploy/kubernetes/base/istio/

# Verify resources were created
kubectl get peerauthentication -n paryty
kubectl get destinationrule -n paryty
kubectl get virtualservice -n paryty
kubectl get gateway -n istio-system paryty-gateway
```

### 4. Restart pods to pick up sidecars (if using sidecar model)

```bash
# Restart all deployments to inject sidecars
kubectl rollout restart deployment -n paryty
```

## Configuration Files

| File | Purpose |
|------|---------|
| `peer-authentication.yaml` | Enforces STRICT mTLS across the paryty namespace |
| `destination-rules.yaml` | Configures ISTIO_MUTUAL TLS + connection pools per service |
| `virtual-services.yaml` | Routes external traffic from ingress gateway to services |
| `README.md` | This file |

## mTLS Architecture

```
Agent (Rust, non-mesh) ────── gRPC (TLS) ──────► Ingestion (Go, mesh)
                                                       │
                                                    mTLS (auto)
                                                       │
                                                       ▼
                                                Pipeline (Go, mesh)
                                                       │
                                                    mTLS (auto)
                                                       │
                                                       ▼
                                                 Query (Go, mesh)
                                                       │
                                     ┌─────────────────┼─────────────────┐
                                     │                                   │
                                 mTLS (auto)                        mTLS (auto)
                                     │                                   │
                                     ▼                                   ▼
                           Frontend (nginx, mesh)            Intelligence (Python, mesh)
```

**Note:** The Paryty Agent (Rust) connects via gRPC with application-level TLS — it does not participate in the Istio mesh. The Ingestion service is the mesh boundary.

## Health Check Exclusion

Health check ports (8080, 9090) are configured as PERMISSIVE in PeerAuthentication to allow Kubernetes liveness/readiness probes and agent SDK metric scraping to function without mTLS.

## Troubleshooting

### Check mTLS enforcement

```bash
# Verify STRICT mode is applied
kubectl get peerauthentication -n paryty paryty-mtls-strict -o yaml

# Check if any pods are missing sidecars
istioctl proxy-status
```

### Debug connection issues

```bash
# Check destination rule configuration
istioctl analyze -n paryty

# View Envoy configuration for a specific pod
istioctl proxy-config routes deploy/paryty-ingestion -n paryty

# Check TLS certificates
istioctl proxy-config secret deploy/paryty-ingestion -n paryty
```

### Common issues

| Symptom | Likely Cause | Fix |
|---------|-------------|-----|
| Pods crash looping | Sidecar not ready before app | Add `holdApplicationUntilProxyStarts: true` to pod annotation |
| 503 errors between services | DestinationRule host mismatch | Verify host matches `{service}.{namespace}.svc.cluster.local` |
| Health probes failing | Probe path blocked by mTLS | Verify port-level PERMISSIVE on 8080 |
| External traffic not reaching services | Gateway not configured | Check `kubectl get gateway -A` |

## Integration with cert-manager

The ingress Gateway at `istio-system/paryty-gateway` references the cert-manager secret `paryty-tls-certs` (created by `../tls/certificate.yaml`). This provides Let's Encrypt TLS termination at the ingress boundary. Internal mesh traffic uses Istio's own auto-provisioned certificates — no cert-manager involvement at the service-to-service level.

## Resource Recommendations

| Component | CPU Request | CPU Limit | Memory Request | Memory Limit |
|-----------|------------|-----------|----------------|--------------|
| istiod | 500m | 2000m | 512Mi | 2Gi |
| ztunnel (per node) | 100m | 500m | 128Mi | 512Mi |
| ingressgateway | 250m | 1000m | 256Mi | 1Gi |
