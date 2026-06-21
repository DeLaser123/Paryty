# TLS Certificate Management

## cert-manager (Production)
cert-manager automates TLS certificate lifecycle.

### Installation
```bash
helm repo add jetstack https://charts.jetstack.io
helm install cert-manager jetstack/cert-manager \
  --namespace cert-manager --create-namespace \
  --set installCRDs=true
```

### Apply Resources
```bash
kubectl apply -f deploy/kubernetes/base/tls/cluster-issuer.yaml
kubectl apply -f deploy/kubernetes/base/tls/certificate.yaml
```

### Self-Signed (Development)
For dev environments, use the `paryty-selfsigned` issuer.
