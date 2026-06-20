---
description: Structured code review — checks correctness, security, performance, error handling, coding standards
agent: code
---
# Code Review — Paryty Structured Review

Perform a structured review of code changes against Paryty architecture rules, coding standards, and locked decisions.

## Review Process

### 1. Scope Identification
Identify which component(s) the changes affect: agent/ (Rust), agent/src/ebpf/ (BPF C), cluster/ (Go), frontend/ (TypeScript), Python ML services, proto/ (Contracts), cross-component.

### 2. Architecture Compliance
- No layer skipping (ingestion → stream → processing → storage → query)
- No circular dependencies
- Communication follows approved protocols (gRPC, REST+WS)
- Multi-tenancy maintained (tenant_id in all requests)

### 3. Coding Standards Check
Apply the relevant language bible:
- Rust → `.qoder/rules/coding-standards-rust.md`
- Go → `.qoder/rules/coding-standards-go.md`
- TypeScript → `.qoder/rules/coding-standards-typescript.md`
- Python → `.qoder/rules/coding-standards-python.md`
- C → `.qoder/rules/coding-standards-c.md`
- Cross-language → `.qoder/rules/coding-standards.md`

### 4. Security Review
- No credentials in code or logs
- Input validated and sanitized
- Tenant isolation maintained
- TLS/mTLS enforced

### 5. Performance Review
- No N+1 queries
- Pagination for large result sets
- No allocations in hot paths
- Bounded state (channels, windows, caches)

### 6. Testing Review
- Unit tests for new logic
- Edge cases covered
- Integration tests for cross-component changes

### 7. Locked Decisions Compliance
Cross-reference with `.qoder/rules/locked-decisions.md`.

### 8. Observability Compliance
Cross-reference with `.qoder/rules/observability-golden-signals.md`.

### 9. Bug Fix Quality
Cross-reference with `.qoder/rules/bug-fix-discipline.md`:
- Root cause identified and addressed
- Regression test added
- Same anti-pattern searched and fixed across codebase
- Fix is systemic (types > guards > checks)
- Post-mortem note present

## Finding Severity
- **Critical**: Must fix before merge (data loss, security, spec violation, bug fix discipline violation)
- **High**: Should fix before merge (performance regression, missing tests)
- **Medium**: Should fix in follow-up (code quality, documentation)
- **Low**: Nice to have (style, minor optimizations)

## Output Format

```
Code Review Summary

Scope: [files changed]
Component: [agent/cluster/frontend/intelligence/cross-component]
Bible Applied: [coding-standards-xxx.md]

### Critical (must fix)
1. [file:line] — description — suggested fix

### High (should fix)
1. [file:line] — description — suggested fix

### Medium (follow-up)
1. [file:line] — description — suggested fix

### Low (optional)
1. [file:line] — description

### Verdict: [APPROVED / CHANGES REQUESTED]
```
