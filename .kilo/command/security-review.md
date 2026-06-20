---
description: Run security audit against OWASP Top 10, CWE Top 25 across all components
agent: security-reviewer
---
# Security Review — Paryty Security Audit

Perform a systematic security audit across all Paryty components.

## Audit Steps

### 1. Authentication & Authorization
- [ ] Agent authenticates via API key or mTLS
- [ ] Frontend authenticates via JWT with expiration
- [ ] RBAC enforced: Admin / Operator / Viewer
- [ ] No privilege escalation paths

### 2. Transport Security
- [ ] TLS for all network communication
- [ ] mTLS for cluster-to-cluster and agent-to-cluster
- [ ] No plaintext credentials in transit
- [ ] Certificate validation enabled

### 3. Data Protection
- [ ] No credentials in code, logs, or error messages
- [ ] Tenant isolation at storage level
- [ ] No cross-tenant data leakage
- [ ] Audit logging for all mutations

### 4. Input Validation
- [ ] All user input validated and sanitized
- [ ] No SQL injection
- [ ] No XSS via topology labels
- [ ] Protobuf messages validated (size limits, required fields)

### 5. Container & Runtime Security
- [ ] Non-root security context
- [ ] Read-only root filesystem where possible
- [ ] No privileged containers
- [ ] Capabilities dropped (ALL)

### 6. eBPF Security
- [ ] No arbitrary kernel memory access
- [ ] No payload content in BPF events (metadata only)
- [ ] BPF map sizes bounded

### 7. Dependency Security
- [ ] No known CVEs (cargo audit, govulncheck, npm audit)
- [ ] Supply chain verification

## Finding Format

```
[Severity] Title
  File: path:line
  Issue: description
  Fix: recommended remediation
  CWE: reference
```

Severity: Critical / High / Medium / Low / Info

## Red Flags (Immediate Escalation)
Hardcoded secrets, SQL injection, XSS, mTLS not enforced, tenant isolation bypass, privilege escalation, container running as root.
