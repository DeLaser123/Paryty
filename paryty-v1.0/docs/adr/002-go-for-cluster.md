# ADR-002: Go for Cluster Services

## Status: Accepted

## Date: 2025-01-15

## Context

The Paryty cluster runs multiple services (ingestion, pipeline, query) that must:
- Handle high-concurrency gRPC and HTTP connections
- Be horizontally scalable
- Have fast compilation for rapid development
- Integrate with Redpanda, Dragonfly, QuestDB, SeaweedFS

## Decision

**Go** for all cluster services. Not Rust, not Java.

## Rationale

| Criterion | Go | Rust | Java/Kotlin |
|-----------|----|----|-------------|
| Concurrency model | Goroutines (simple, efficient) | Tokio (powerful, complex) | Virtual threads (new) |
| Compilation speed | ~5 seconds | ~60 seconds | ~30 seconds |
| Binary size | ~15 MB | ~10 MB | JVM required |
| Memory usage | ~50 MB base | ~20 MB base | ~200 MB (JVM) |
| gRPC support | Excellent (official) | Excellent (tonic) | Excellent (official) |
| Talent pool | Very large | Growing | Very large |
| Deployment | Single binary | Single binary | JVM + JAR |

**Key factors:**
1. **Concurrency model** — Goroutines are the simplest concurrent programming model. `errgroup` provides structured concurrency with cancellation.
2. **Fast compilation** — 5-second build times enable rapid iteration. Critical for a team building complex pipeline logic.
3. **Ecosystem** — go-redis, pgx, Sarama (Kafka/Redpanda), all mature and well-maintained.
4. **Simplicity** — Go's simplicity is a feature for team scalability. New engineers contribute in days, not weeks.

## Consequences

- **GC pauses** — Acceptable for cluster services (not latency-critical like agent). Mitigated by GOGC tuning.
- **Verbose error handling** — Mitigated by consistent error patterns (`fmt.Errorf("context: %w", err)`).
- **No generics until 1.18+** — Now resolved with Go 1.18+ generics.
- **Fast development velocity** — Team can ship features quickly.

## Alternatives Considered

1. **Rust** — Rejected for cluster: slower development, overkill for I/O-bound services. Kept for agent where performance-per-watt matters.
2. **Java/Kotlin** — Rejected: JVM overhead, slower startup, higher memory usage for container deployments.
3. **Node.js/TypeScript** — Rejected: Single-threaded, higher memory for equivalent throughput.
