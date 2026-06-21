# ADR-003: Redpanda over Apache Kafka

## Status: Accepted

## Date: 2025-01-15

## Context

Paryty needs a durable, ordered streaming platform for observability data. The stream engine is the central nervous system — all data flows through it. Requirements:
- Kafka-compatible API (ecosystem access)
- Low operational complexity (SMB self-hosting)
- Built-in tiered storage (hot/warm/cold for 7-day timeline)
- No ZooKeeper dependency

## Decision

**Redpanda** for V1.0. Apache Kafka for Fortune 500 deployments.

## Rationale

| Criterion | Redpanda | Apache Kafka | NATS JetStream |
|-----------|----------|--------------|----------------|
| API compatibility | Kafka-compatible | Native | Own API |
| ZooKeeper dependency | None | Required (KRaft improving) | None |
| Operational complexity | Low (single binary) | High (ZK + brokers) | Low |
| Latency | 10x lower than Kafka | Baseline | Very low |
| Tiered storage | Built-in | Available (3.0+) | Limited |
| Resource usage | Lower | Higher | Very low |
| Maturity | Growing | Battle-tested | Growing |
| License | BSL 1.1 | Apache 2.0 | Apache 2.0 |

**Key factors:**
1. **No ZooKeeper** — Eliminates the #1 operational pain point of Kafka.
2. **Kafka-compatible API** — All Kafka client libraries (Sarama, franz-go) work unchanged. Zero code changes.
3. **Built-in tiered storage** — Critical for 7-day timeline replay feature. Redpanda handles hot/warm/cold automatically.
4. **SMB self-hosting** — Redpanda's single-binary deployment is dramatically simpler than Kafka for small teams.

## Consequences

- **BSL 1.1 license** — Converts to Apache 2.0 after 4 years. Acceptable for V1.0.
- **Smaller ecosystem** — Mitigated by Kafka API compatibility.
- **Fortune 500 path** — Kafka is available as an alternative for enterprises that require it.

## Alternatives Considered

1. **Apache Kafka** — Used for Fortune 500 deployments. Too complex for SMB self-hosting.
2. **NATS JetStream** — Rejected: Not Kafka-compatible, smaller ecosystem, less mature for large-scale streaming.
3. **Apache Pulsar** — Rejected: More complex architecture, smaller community.
