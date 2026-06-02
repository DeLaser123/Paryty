# Paryty Architecture - Detailed Plan

## Overview

Paryty is a distributed observability operating system designed to provide digital twin capabilities for software applications. It features GPU-accelerated topology visualization, real-time data flow animation, predictive forecasting, and simulation drills.

## Three-Part Architecture

### 1. Paryty Agent

The agent is a hyper-light collector deployed on every node in the application cluster. It consists of three collection layers:

**Layer 1: Metal Scraper (Rust)**
- CPU metrics (per-core, per-process)
- Memory metrics (RSS, VSZ, shared, private)
- Disk metrics (IOPS, throughput, latency, queue depth)
- Network metrics (packets, bytes, retransmits, RTT)
- Process tree discovery
- Container detection (cgroups, namespaces)

**Layer 2: eBPF Network Observer (Rust)**
- TCP connection tracking (src/dst IP, port, state)
- DNS resolution mapping (domain to IP)
- HTTP request/response inspection (method, path, status, latency)
- Database query inspection (PostgreSQL, MySQL, Redis protocol)

**Layer 3: Supervisor (Optional, Rust)**
- Health check polling (HTTP, TCP, gRPC)
- Log tailing (stdout, stderr, files)
- Configuration change detection
- Dependency discovery

**Communication Layer (Rust)**
- gRPC bidirectional streaming to Paryty Cluster
- Automatic reconnection with exponential backoff
- Edge buffering (stores data during disconnects)
- Compression (Zstd for metrics, Snappy for traces)
- Flow control with backpressure support

**Go SDK (Language-Agnostic via gRPC)**
- Self-reporting SDK for application services
- Custom metrics (counters, gauges, histograms)
- Business logic traces
- Application health reporting
- OpenTelemetry SDK (standard telemetry)
- gRPC client (Paryty-specific features)
- Language-agnostic: generate SDKs for Go, Python, Java, Node.js, Rust, etc. via .proto files

### 2. Paryty Cluster

The cluster is a Redpanda-like streaming platform for observability data. It consists of four layers:

**Ingestion Layer**
- Stateless, horizontally scalable
- gRPC server for agent connections
- Protocol validation
- Tenant routing (multi-tenant isolation)
- Load balancing

**Stream Engine**
- Redpanda (Kafka-compatible, V1.0) or Apache Kafka (Fortune 500)
- Per-tenant topics with isolation
- Exactly-once delivery semantics
- Configurable retention (1-365 days)
- Built-in tiered storage (hot/warm/cold)
- Built-in schema registry (Protobuf/Avro)

**Topic Architecture:**
- paryty.{tenant}.metrics.raw — Raw metrics from agents
- paryty.{tenant}.metrics.aggregated — Aggregated metrics
- paryty.{tenant}.traces — Distributed traces
- paryty.{tenant}.events — System events
- paryty.{tenant}.topology — Topology changes
- paryty.{tenant}.alerts — Generated alerts

**Processing Layer**
- **Aggregator** — Aggregates metrics per-minute, per-hour, per-day
- **Correlator** — Correlates metrics, traces, and logs; builds dependency graph
- **Enricher** — Adds metadata (service name, version, environment)

**Storage Layer (Tiered)**

Hot Store (Dragonfly):
- Current topology state
- Live metrics (last 5 minutes)
- Active alerts
- Agent connection state
- Latency: <1ms

Warm Store (QuestDB):
- Historical metrics (last 30-90 days)
- Aggregated metrics (per-minute, per-hour)
- Trace spans
- Event logs
- Latency: <50ms

Cold Store (SeaweedFS):
- Long-term retention (1-7 years)
- Compressed traces and logs
- Timeline snapshots (full state every 5 minutes)
- Compliance archives
- Latency: <1s

**Query Layer**
- Stateless, horizontally scalable
- REST API for initial load and CRUD operations
- GraphQL API for complex queries and filtering
- WebSocket API for real-time updates
- SSE API for timeline replay streaming
- Dragonfly-backed query cache

### 3. Paryty Frontend

The frontend is a GPU-accelerated visualization and intelligence dashboard.

**GPU Rendering Engine**
- PixiJS (WebGL 2D renderer) for topology visualization
- D3-force for force-directed graph layout
- 15,000+ nodes at 60fps
- Built-in particle system for data flow animations

**Visualization Features**
- Force-directed topology layout
- Particle animations for data flow
- Glow effects for CPU/memory load
- Edge animation for throughput
- Color coding for health status
- Multi-level zoom (service, container, host, datacenter)

**Intelligence Layer**

The Intelligence Layer provides predictive, diagnostic, and simulation capabilities. It consists of four engines, each with a hybrid architecture optimized for Paryty's use cases.

Forecasting Engine (Hybrid Ensemble):
- Layer 1: Linear Regression for real-time baselines (<1ms)
- Layer 2: Prophet for seasonal patterns (daily, weekly cycles) (<100ms)
- Layer 3: XGBoost for complex multi-variable patterns (<10ms)
- 7-day CPU/memory/disk forecasting with confidence intervals
- Capacity planning alerts
- Implementation: Python service via gRPC
- Libraries: Prophet (MIT), XGBoost (Apache 2.0), scikit-learn (BSD)

Anomaly Detection (Hybrid Ensemble):
- Layer 1: Statistical methods — Z-Score, IQR, EWMA for point anomalies (<1ms)
- Layer 2: Isolation Forest for complex point anomalies and high-dimensional data (<10ms)
- Layer 3: Autoencoders for contextual anomalies (memory leaks) and collective anomalies (cascading failures) (<50ms)
- Ensemble decision combining all three layers
- Confidence scores and alert generation
- Implementation: Python service via gRPC
- Libraries: scikit-learn (BSD), TensorFlow (Apache 2.0)

Simulation Engine (Hybrid Integration — All Open Source):

System-Side Simulation:
- Digital Twin: "What if" scenario modeling for capacity planning (custom implementation)
- LitmusChaos: Chaos engineering — failure injection, shutdown attacks, network attacks (Apache 2.0)
- k6: Load testing — stress testing, peak load, soak testing (AGPL 3.0)
- tc: Network simulation — latency injection, packet loss, bandwidth limiting (Linux kernel)

User-Side Simulation (Paryty Watif):
- Locust: User behavior simulation — bot vs human patterns, request workflows (MIT)
- Custom Bot Engine: Bot pattern simulation — rapid fire, smart bots, distributed bots, slow drip bots
- Pattern Analyzer: Request pattern analysis — timing anomalies, volume anomalies, source anomalies, behavior anomalies
- Threat Detector: Threat detection — false alarms, suspicious behavior, confirmed bots, attacks
- Degradation Simulator: Graceful degradation — rate limiting, request queuing, response throttling, content degradation
- Implementation: Go service with integrations

Timeline Engine (Custom Implementation):
- Snapshot Manager: Full state snapshots every 5 minutes, stored in SeaweedFS, 7-day retention
- Replay Engine: Replay system state at any point in time, integrated with topology visualization
- Diff Calculator: Compare state between two points — topology changes, metric changes, alert changes
- Export Manager: Export snapshots for incident postmortems (JSON, PDF, HTML)
- Speed Controller: Replay speed controls (0.25x, 0.5x, 1x, 2x, 4x, 8x, 16x)
- Data Sources: Redpanda (events), QuestDB (metrics), SeaweedFS (snapshots)
- Implementation: Go service with tiered storage integration

## Deployment Models

### Self-Hosted
Deploy the entire Paryty stack on your own infrastructure. Full control over data and configuration.

### Managed SaaS
Use Paryty's hosted service. Agents connect to Paryty's managed cluster. Zero infrastructure management.

### Hybrid
Deploy the cluster on your infrastructure, but use Paryty's cloud-hosted intelligence layer for forecasting and anomaly detection.

## Data Flow

`
Agent (Node) → gRPC → Ingestion Layer → Stream Engine → Processing Layer → Storage Layer → Query Layer → Frontend
`

## Scalability

### V1.0 (SMB)
- 2-10 ingestion nodes
- 3 stream engine nodes
- 2-5 processing nodes
- 3 storage nodes (Dragonfly + QuestDB)
- 2 query nodes
- Supports 10K-100K agents

### Fortune 500
- 10-100 ingestion nodes
- 10-50 stream engine nodes
- 10-50 processing nodes
- 10-100 storage nodes
- 10-50 query nodes
- Supports 1M+ agents

## Security

- TLS for all communications
- API key authentication
- Tenant isolation
- Role-based access control (RBAC)
- Audit logging
- SOC2, HIPAA, GDPR compliance (Enterprise tier)
