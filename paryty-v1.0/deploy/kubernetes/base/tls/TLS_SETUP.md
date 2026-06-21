# Paryty TLS & mTLS Enforcement Guide

## Production TLS Requirements

All Paryty services MUST use TLS in production. The following enforcement mechanisms are in place:

### Query Service (HTTP)
- Set `PARYTY_REQUIRE_TLS=true` (default in non-dev mode)
- Provide `--tls-cert` and `--tls-key` CLI flags, or `PARYTY_TLS_CERT_FILE` and `PARYTY_TLS_KEY_FILE` env vars
- TLS 1.3 minimum enforced
- Startup fails fatally if TLS certs are missing in production mode

### Ingestion Service (gRPC)
- Set `PARYTY_TLS_CERT_FILE` and `PARYTY_TLS_KEY_FILE` env vars for server TLS
- Set `PARYTY_TLS_CLIENT_CA_FILE` to enable mTLS (requires client certs)
- `RequireMTLS()` returns true when both server cert and client CA are present
- Plaintext connections are rejected when mTLS is enabled

### Agent (gRPC Client)
- Set `PARYTY_TLS_ENABLED=true` in agent config
- Agent validates server certificate hostname
- Agent presents client certificate when `PARYTY_TLS_CLIENT_CERT_FILE` and `PARYTY_TLS_CLIENT_KEY_FILE` are configured

## cert-manager Integration

For Kubernetes deployments, cert-manager automates TLS certificate lifecycle:

```yaml
# cert-manager ClusterIssuer (deploy/kubernetes/base/tls/cluster-issuer.yaml)
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: paryty-letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: ops@paryty.io
    privateKeySecretRef:
      name: paryty-letsencrypt-account-key
    solvers:
    - http01:
        ingress:
          class: nginx
```

For self-signed certs in non-production environments:

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: paryty-selfsigned
spec:
  selfSigned: {}
```

## Certificate Resources

Each service gets a Certificate resource that cert-manager manages:

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: paryty-query-tls
  namespace: paryty
spec:
  secretName: paryty-query-tls
  issuerRef:
    name: paryty-letsencrypt-prod
    kind: ClusterIssuer
  dnsNames:
  - api.paryty.io
  - paryty-query.paryty.svc.cluster.local
```

## Quick Start — Generate Self-Signed Certs for Dev

```bash
# Generate CA
openssl req -x509 -newkey rsa:4096 -keyout ca-key.pem -out ca-cert.pem -days 365 -nodes \
  -subj "/CN=Paryty Dev CA"

# Generate server cert signed by CA
openssl req -newkey rsa:2048 -keyout server-key.pem -out server.csr -nodes \
  -subj "/CN=localhost"
openssl x509 -req -in server.csr -CA ca-cert.pem -CAkey ca-key.pem -CAcreateserial \
  -out server-cert.pem -days 365

# Start query service with TLS
PARYTY_REQUIRE_TLS=true \
PARYTY_TLS_CERT_FILE=server-cert.pem \
PARYTY_TLS_KEY_FILE=server-key.pem \
./query-service

# Start ingestion with mTLS
PARYTY_TLS_CERT_FILE=server-cert.pem \
PARYTY_TLS_KEY_FILE=server-key.pem \
PARYTY_TLS_CLIENT_CA_FILE=ca-cert.pem \
./ingestion-service
```
