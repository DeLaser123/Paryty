---
name: code-review
description: Structured code review for Paryty — checks correctness, security, performance, error handling, and adherence to architecture rules and coding standards.
---

# Code Review — Paryty Structured Review

## Purpose
Perform a structured review of code changes against Paryty architecture rules, coding standards, and locked decisions.

## Review Process

### 1. Scope Identification
- agent/ — Rust review
- agent/src/ebpf/ — BPF C review
- cluster/ — Go review
- frontend/ — TypeScript review
- Python ML services — Python review
- proto/ — Contract review
- Cross-component — Integration review

### 2. Architecture Compliance
- No layer skipping (ingestion to stream to processing to storage to query)
- No circular dependencies
- Communication follows approved protocols (gRPC, REST+WS)
- Multi-tenancy maintained (tenant_id in all requests)

### 3. Coding Standards (Language-Specific Bibles)
Check against the language-specific bible:
- agent/ Rust code — coding-standards-rust.md (74 rules)
- agent/src/ebpf/ BPF C code — coding-standards-c.md (66 rules)
- cluster/ Go code — coding-standards-go.md (70 rules)
- frontend/ TypeScript code — coding-standards-typescript.md (78 rules)
- Python ML code — coding-standards-python.md (64 rules)
- Cross-language — coding-standards.md (shared principles)

Enforce ALL rules from the relevant bible. Focus especially on:
- Error handling follows language-specific patterns
- Logging uses approved library (tracing/slog/console/structlog)
- No forbidden patterns (unwrap, panic, any, println, bare except)
- Functions under 50 lines, files under 500 lines
- Naming conventions followed
- Performance rules (no allocations in hot paths, bounded state)

### 4. Security Review
- No credentials in code or logs
- Input validated and sanitized
- Tenant isolation maintained
- TLS/mTLS enforced for network calls

### 5. Performance Review
- No N+1 queries
- Pagination for large result sets
- No allocations in hot paths (where applicable)
- Bounded state (channels, windows, caches)

### 6. Testing Review
- Unit tests for new logic
- Table-driven tests (Go)
- Edge cases covered (empty, nil, overflow, concurrent)
- Integration tests for cross-component changes

### 7. Locked Decisions Compliance
Cross-reference with .qoder/rules/locked-decisions.md:
- Technology choices match (no aya, no Redis, no ClickHouse)
- Architecture patterns match (single-binary pipeline, optimistic locking)

### 8. Observability Compliance
Cross-reference with .qoder/rules/observability-golden-signals.md:
- Golden signals exposed (latency, traffic, errors, saturation)
- OpenTelemetry SDK used (not language-native metrics)
- Health check endpoints present (/healthz, /readyz)
- Trace context propagated (trace_id in spans)

## Finding Severity
- Critical: Must fix before merge (data loss, security vulnerability, spec violation)
- High: Should fix before merge (performance regression, missing tests)
- Medium: Should fix in follow-up (code quality, documentation)
- Low: Nice to have (style, minor optimizations)

## Output Format

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
