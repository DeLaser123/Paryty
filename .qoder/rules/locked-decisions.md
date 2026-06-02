# Paryty Locked Architectural Decisions

These decisions are FINAL. Do not re-evaluate or propose alternatives.

## Technology Stack

| Component | Selection | Rejected | Rationale |
|---|---|---|---|
| Agent core | Rust | Go, C++ | Memory safety without GC, zero-cost abstractions |
| Cluster services | Go | Rust, Java | Concurrency model, fast compilation, simple deployment |
| Frontend | TypeScript + React + Vite | Vue, Svelte | Ecosystem maturity, PixiJS compatibility |
| Streaming engine | Redpanda | NATS JetStream, Kafka | Kafka-compatible, no JVM, rootless Podman support |
| Hot store | Dragonfly | Redis | Multi-threaded, Redis-compatible, 3x throughput |
| Warm store | QuestDB | ClickHouse | Time-series optimized, ILP ingestion, SQL interface |
| Cold store | SeaweedFS | MinIO, S3 | S3-compatible, efficient small files, no JVM |
| eBPF framework | libbpf (C) + Rust loader | aya (pure Rust) | CO-RE maturity, verifier compatibility, kernel docs |
| Serialization | Protobuf | JSON, MessagePack | Schema evolution, codegen for Rust+Go+TS |
| Compression | Zstd | Snappy, LZ4 | Best ratio/speed tradeoff, dictionary support |
| GPU rendering | PixiJS | Three.js, raw WebGL | 2D optimized, instanced rendering, battle-tested |
| Frontend state | Zustand | Redux, MobX | Minimal boilerplate, React-native, no providers |
| Web framework | Gin | Echo, Fiber | Middleware ecosystem, performance, community |
| Observability | OpenTelemetry | Prometheus+Grafana | Language-agnostic, Paryty dogfooding principle |
| Container engine | Podman (rootless) | Docker | Rootless by default, daemonless, systemd compat |

## Architecture Decisions

### Processing Pipeline: Single Binary (Phase 3)
- **Decision:** Aggregator, Correlator, Enricher run as in-process stages in one Go binary
- **Not:** Three separate services with separate Redpanda consumer groups
- **Why:** Sequential by nature; single binary eliminates 2/3 network hops; Go concurrency (goroutine pools per stage) handles parallelism; can extract stages later if scaling demands it

### Hot Store Topology Updates: Optimistic Locking (Phase 3)
- **Decision:** WATCH/MULTI/EXEC on Dragonfly for concurrent topology updates
- **Not:** Redlock distributed locks, single-writer pattern
- **Why:** Topology updates are infrequent (new connections are rare); Redpanda partitioning routes same-connection messages to same consumer; near-zero contention in practice; zero overhead when no conflict

### Timeline Replay: Snapshots + Event Log Hybrid (Phase 6)
- **Decision:** Full state snapshots every 5 minutes (SeaweedFS) + Redpanda event replay for inter-snapshot granularity
- **Not:** Pure snapshots, pure event sourcing, or pure deltas
- **Why:** Snapshots provide fast checkpoint loading (~50ms); Redpanda events provide per-second replay granularity; indexed events in QuestDB power TradingView-style timeline markers

### eBPF Implementation: libbpf C + Rust Loader (Phase 2)
- **Decision:** C eBPF programs compiled at build time via build.rs, loaded by Rust userspace with libbpf
- **Not:** aya (pure Rust eBPF)
- **Why:** CO-RE (Compile Once, Run Everywhere) for kernel version compatibility; libbpf is the reference implementation; verifier-friendly C code; vmlinux.h generation; kprobe attachment on tcp_connect, tcp_close, tcp_set_state, udp_sendmsg, tcp_sendmsg, tcp_recvmsg

### Helm Chart: Umbrella with Subcharts (Phase 8)
- **Decision:** Parent chart with subcharts (agent, cluster, frontend, infra)
- **Not:** Monolithic single chart, Kubernetes operator
- **Why:** Components have different scaling/deployment characteristics; independent rollout; selective installation; per-component versioning; operator deferred to v2.0

### Intelligence Layer: Python gRPC Microservices (Phase 6)
- **Decision:** Standalone Python gRPC services for ML (forecasting, anomaly detection)
- **Not:** Embedded Python, REST API
- **Why:** Clean separation from Go cluster; leverages existing gRPC stack; acceptable latency for batch-oriented ML; file-based model persistence for V1.0

### Forecasting: Weighted Ensemble (Phase 6)
- **Decision:** Linear Regression + Prophet + XGBoost, weighted by hourly MAPE
- **Not:** Single model, simple average
- **Why:** Interpretable, adaptive weights, self-correcting, robust to individual model failure

### Simulation Engine: What-if + Chaos Engineering (Phase 6)
- **Decision:** What-if analysis with k6/LitmusChaos integration; interfaces designed for V2.0 full digital twin
- **Not:** Full digital twin in V1.0
- **Why:** Balances predictive value with real-world validation; V2.0 extension points documented

### Self-Monitoring: Dogfooding (Phase 8)
- **Decision:** Paryty Go SDK monitors Paryty itself; lightweight custom health fallback
- **Not:** Prometheus, Grafana, or any competitor tooling
- **Why:** "If we claim to be the ultimate observability platform, we use our own product"

### RBAC: Simple Role Hierarchy (Phase 8)
- **Decision:** Admin / Operator / Viewer roles
- **Not:** Fine-grained attribute-based access control
- **Why:** Covers 90% of use cases; simple to implement and audit

### Certificate Management: cert-manager (Phase 8)
- **Decision:** cert-manager (K8s native) with self-signed fallback for non-K8s deployments
- **Not:** Manual certificate management, Vault
- **Why:** K8s native automation; self-signed fallback covers dev/single-node scenarios

## Operational Principles
- **Dogfooding:** Paryty monitors itself using its own SDK
- **No competitor tools:** No Prometheus, Grafana, Datadog, or similar in the stack
- **User-side simulation:** Simulation engine runs user-side, not cloud-side
- **Graceful degradation:** Agent works on non-Linux (eBPF → /proc/net/tcp fallback → stub)
- **Open source core:** All V1.0 components are open source
