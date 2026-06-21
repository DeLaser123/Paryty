# ADR-007: OpenTelemetry over Prometheus for Observability

## Status: Accepted

## Date: 2025-01-15

## Context

Paryty monitors itself using its own SDK (dogfooding principle). The observability standard must be:
- Language-agnostic (Rust, Go, TypeScript, Python)
- Capable of metrics, traces, and logs
- Compatible with Paryty's own ingestion pipeline
- Not tied to any competitor tooling (no Prometheus, Grafana, Datadog)

## Decision

**OpenTelemetry** as the observability standard. Not Prometheus, not vendor-specific SDKs.

## Rationale

| Criterion | OpenTelemetry | Prometheus | Datadog SDK |
|-----------|---------------|------------|-------------|
| Signals | Metrics + Traces + Logs | Metrics only | All three |
| Language support | All (Rust, Go, TS, Python) | All (client libraries) | All |
| Trace context propagation | Built-in (W3C) | Not available | Proprietary |
| Vendor neutrality | Yes | Yes (but metrics-only) | No (vendor lock-in) |
| Paryty dogfooding | Yes (OTLP → Paryty ingestion) | No (pull model) | No (competitor) |
| Standardization | CNCF graduated | CNCF graduated | Proprietary |

**Key factors:**
1. **Dogfooding** — Paryty monitors itself using its own SDK. OpenTelemetry's OTLP protocol feeds directly into Paryty's ingestion pipeline.
2. **Language-agnostic** — Single standard across Rust agent, Go cluster, TypeScript frontend, Python intelligence layer.
3. **Three signals** — Metrics, traces, and logs in one standard. Prometheus only does metrics.
4. **Trace context propagation** — `trace_id` flows from Agent → Ingestion → Pipeline → Storage → Query → Frontend.

## Consequences

- **No Prometheus/Grafana** — Paryty uses its own visualization. This is a feature, not a limitation.
- **OTLP export** — All components export via OTLP/gRPC to Paryty's own collector (dogfooding).
- **Golden signals** — Every component exposes latency, traffic, errors, saturation via OpenTelemetry.

## Alternatives Considered

1. **Prometheus** — Rejected: Metrics-only (no traces/logs), pull model doesn't fit Paryty's push architecture, competitor tooling.
2. **Datadog SDK** — Rejected: Vendor lock-in, proprietary, violates dogfooding principle.
3. **Language-native metrics** (Go expvar, Rust metrics crate) — Rejected: No standardization, no trace context propagation.
