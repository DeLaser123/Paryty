# ADR-008: Python gRPC Microservice for ML

## Status: Accepted

## Date: 2025-01-15

## Context

Paryty's Intelligence Layer provides forecasting, anomaly detection, drift detection, and simulation. Requirements:
- ML libraries (Prophet, XGBoost, scikit-learn, TensorFlow) are Python-native
- Must integrate with Go cluster via gRPC
- Must not embed Python in Go binary (deployment complexity)
- Must support model versioning and retraining

## Decision

**Standalone Python gRPC microservice** for all ML capabilities. Not embedded Python, not REST API.

## Rationale

| Criterion | Python gRPC Service | Embedded Python (Go→Python FFI) | REST API |
|-----------|---------------------|--------------------------------|----------|
| Deployment | Separate container | Complex (Python runtime in Go) | Separate container |
| gRPC support | grpc.aio (async) | N/A | HTTP overhead |
| ML library access | Native (pip install) | Complex (FFI bindings) | Native |
| Model persistence | File-based (joblib) | File-based | File-based |
| Scaling | Independent (HPA) | Tied to Go process | Independent |
| Latency | Network hop (~1-5ms) | In-process (~0.1ms) | HTTP overhead (~2-10ms) |
| Language purity | Python (ML-native) | Mixed (Go + Python) | Python (ML-native) |

**Key factors:**
1. **Clean separation** — ML team works in Python, cluster team works in Go. No FFI complexity.
2. **Native ML libraries** — Prophet, XGBoost, scikit-learn install via pip. No bindings or wrappers.
3. **Independent scaling** — ML inference scales separately from data processing. CPU-heavy ML doesn't affect I/O-heavy cluster.
4. **gRPC integration** — Reuses Paryty's existing gRPC stack. Proto definitions shared across languages.

## Consequences

- **Network hop** — ~1-5ms latency for gRPC call. Acceptable for batch-oriented ML (forecasts, anomaly detection).
- **Separate deployment** — One more container to manage. Mitigated by Helm subchart.
- **Model persistence** — File-based (`joblib.dump`/`joblib.load`) for V1.0. Can upgrade to model registry later.

## Alternatives Considered

1. **Embedded Python** — Rejected: Deployment complexity (Python runtime inside Go binary), version conflicts, debugging difficulty.
2. **REST API** — Rejected: HTTP overhead, no streaming support, less type-safe than gRPC + Protobuf.
3. **Go ML libraries (gonum, gorgonia)** — Rejected: Immature ecosystem, no Prophet/XGBoost equivalents, reinventing the wheel.
