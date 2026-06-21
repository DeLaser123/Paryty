# ADR-004: Dragonfly over Redis

## Status: Accepted

## Date: 2025-01-15

## Context

Paryty needs a hot store for real-time data (last 5 minutes): current topology state, live metrics, active alerts, agent connection state. Requirements:
- Sub-millisecond latency
- High throughput (10K+ agents updating concurrently)
- Redis-compatible API (ecosystem access)
- Simple operations (SMB self-hosting)

## Decision

**Dragonfly** over Redis Cluster.

## Rationale

| Criterion | Dragonfly | Redis Cluster | KeyDB |
|-----------|-----------|---------------|-------|
| Threading model | Multi-threaded | Single-threaded | Multi-threaded |
| Throughput | 3x Redis | Baseline | ~2x Redis |
| Memory efficiency | 50% less than Redis | Baseline | Similar to Redis |
| API compatibility | Redis-compatible | Native | Redis-compatible |
| Clustering | Single binary | Complex (6+ nodes) | Active replica |
| License | BSL 1.1 → Apache 2.0 | SSPL (restrictive) | BSD |
| JSON support | Built-in | Module required | Module required |
| Search | Built-in | Module required | Not available |

**Key factors:**
1. **Multi-threaded** — Dragonfly uses all available CPU cores. Redis is single-threaded (I/O threads help, but core processing is still single).
2. **Memory efficiency** — 50% less memory for equivalent data. Critical for SMB budgets.
3. **Redis API compatible** — go-redis client works unchanged. Zero code changes.
4. **Simplest operations** — Single binary, no cluster setup. Perfect for self-hosting.

## Consequences

- **BSL 1.1 license** — Converts to Apache 2.0 after 4 years. Acceptable for V1.0.
- **Smaller community** — Mitigated by Redis API compatibility (all Redis knowledge applies).
- **Hot data is ephemeral** — No backup needed. Data repopulates from agents on restart.

## Alternatives Considered

1. **Redis Cluster** — Rejected: SSPL license (restrictive for SaaS), single-threaded, complex clustering.
2. **KeyDB** — Rejected: Less active development, missing JSON and search features.
3. **Memcached** — Rejected: No persistence, limited data structures, no clustering.
