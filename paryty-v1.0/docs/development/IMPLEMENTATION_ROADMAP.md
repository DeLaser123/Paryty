# PARYTY V1.0: THE COMPLETE ROADMAP TO 100K+ LOC

## CURRENT STATE HONEST ASSESSMENT

You have **~29K LOC of scaffolding and basic implementations**. Here's what's actually real:

✅ **Working at basic level:**
- CPU collector (Linux /proc/stat reading, ~200 LOC)
- Basic aggregation logic (avg/min/max/percentile, ~277 LOC)
- PixiJS renderer (basic node/edge drawing, ~188 LOC)
- Proto definitions (~2,000 LOC across 6 files)
- Project structure and build infrastructure

❌ **Stub/placeholder (marked with TODO or empty):**
- TCP tracker: Returns `Ok(Vec::new())` — **zero actual eBPF**
- HTTP inspector: Returns `Ok(None)` — **zero parsing**
- DNS mapper, DB inspector: Same pattern
- Dragonfly client: **Doesn't exist** (only store.go interface)
- QuestDB client: **Doesn't exist**
- SeaweedFS client: **Doesn't exist**
- Correlator: **Not implemented**
- Enricher: **Not implemented**
- Intelligence layer (forecasting, anomaly detection, simulation, timeline): **Doesn't exist**
- Frontend GPU workers, particle systems, force layout: **Basic scaffolding**
- Go SDK: **Minimal wrapper**

**Reality:** You have architecture, build system, and ~15-20% of the actual implementation done.

---

## THE COMPLETE SEQUENTIAL ROADMAP

This is organized in **8 phases**, each building on the previous. **You control all decisions** — I'm telling you exactly what needs to be built, in what order, and why.

---

### PHASE 1: FOUNDATION & DATA PIPELINE (Weeks 1-3)
**Target: +15,000 LOC → Total: ~44K LOC**

**Goal:** Make data flow end-to-end from agent to storage

#### Layer 1: Complete Metal Scrapers (Rust Agent) — ~3,500 LOC

1. **CPU Collector** (already 200 LOC, need +400 LOC)
   - Add /proc/cpuinfo frequency reading
   - Add context switch counting from /proc/stat
   - Add per-process CPU attribution
   - Add CPU topology detection (sockets, cores, threads)

2. **Memory Collector** (exists, need +500 LOC)
   - Parse /proc/meminfo completely (RSS, VSZ, shared, buffers, cached, swap)
   - Add per-process memory mapping from /proc/[pid]/status
   - Add memory pressure detection (OOM killer proximity)
   - Add NUMA node awareness

3. **Disk Collector** (need ~800 LOC)
   - Parse /proc/diskstats (IOPS, throughput, latency, queue depth, IO time)
   - Parse /sys/block/*/queue/* for queue depth, scheduler
   - Add per-partition tracking
   - Add disk type detection (SSD/HDD/NVMe)
   - Add IO latency histogram (p50, p90, p95, p99)

4. **Network Collector** (need ~800 LOC)
   - Parse /proc/net/dev (bytes, packets, errors, drops per interface)
   - Parse /proc/net/snmp (TCP retransmits, UDP errors)
   - Parse /proc/net/tcp, tcp6, udp, udp6 (connection states)
   - Add RTT estimation from /proc/net/tcp
   - Add bandwidth utilization calculation

5. **Process Tree** (need ~600 LOC)
   - Parse /proc/[pid]/stat, /proc/[pid]/status
   - Build process tree (parent-child relationships)
   - Detect process startup/shutdown events
   - Track file descriptors per process

6. **Container Detection** (need ~400 LOC)
   - Parse /proc/[pid]/cgroup for container detection
   - Parse /sys/fs/cgroup/*/ for resource limits
   - Extract container ID, image name, orchestrator labels
   - Map containers to processes

#### Layer 2: Communication Layer Hardening (Rust Agent) — ~3,000 LOC

7. **gRPC Bidirectional Streaming** (need ~1,000 LOC)
   - Implement `SendTelemetry` RPC with tonic
   - Handle server-side config pushes
   - Implement heartbeat mechanism
   - Add connection state machine (connecting, connected, reconnecting)

8. **Edge Buffer** (need ~800 LOC)
   - Implement ring buffer with disk spill (SQLite or flat files)
   - Add batch assembly logic (max size, max age)
   - Implement replay on reconnect
   - Add buffer size limits and eviction policy

9. **Compression Pipeline** (need ~400 LOC)
   - Integrate zstd for metrics compression
   - Integrate snappy for trace compression
   - Add compression level configuration
   - Benchmark and tune compression ratios

10. **Flow Control** (need ~400 LOC)
    - Implement backpressure detection from server
    - Add dynamic batching based on network conditions
    - Implement sampling rate adjustment
    - Add rate limiting to prevent overwhelm

11. **Reconnection Logic** (need ~400 LOC)
    - Exponential backoff with jitter
    - Persistent session recovery
    - Graceful degradation during extended outages
    - Connection health monitoring

#### Layer 3: Ingestion Service (Go Cluster) — ~4,000 LOC

12. **gRPC Server** (need ~1,500 LOC)
    - Implement `IngestionService` from proto
    - Handle agent registration and session management
    - Implement tenant routing from API keys
    - Add protocol validation and sanitization
    - Implement load balancing across ingestion nodes

13. **Authentication & Tenancy** (need ~1,000 LOC)
    - API key validation against Dragonfly cache
    - Tenant isolation enforcement
    - Rate limiting per tenant
    - Audit logging for all ingestions

14. **Redpanda Producer** (need ~1,000 LOC)
    - Integrate franz-go or sarama Kafka client
    - Implement topic routing: `paryty.{tenant}.metrics.raw`
    - Add exactly-once semantics (idempotent producer)
    - Implement batching and compression for Redpanda

15. **Health & Observability** (need ~500 LOC)
    - Health check endpoints (readiness, liveness)
    - Prometheus metrics for ingestion (requests/sec, latency, errors)
    - Structured logging with correlation IDs
    - Distributed tracing (OpenTelemetry)

#### Layer 4: Hot Store Integration (Go Cluster) — ~2,500 LOC

16. **Dragonfly Client** (need ~1,500 LOC)
    - Implement Redis-compatible client (go-redis)
    - Topology state storage (hash per service)
    - Live metrics cache (sorted sets with TTL)
    - Active alerts storage (sets with metadata)
    - Agent connection tracking (hash map)
    - Implement atomic updates and transactions

17. **Hot Store Query Layer** (need ~1,000 LOC)
    - Get latest metrics for dashboard
    - Get current topology state
    - Get active alerts
    - Implement query caching with invalidation

#### Layer 5: Warm Store Integration (Go Cluster) — ~2,000 LOC

18. **QuestDB Client** (need ~1,500 LOC)
    - PostgreSQL wire protocol integration (lib/pq)
    - Schema creation for metrics, traces, events
    - Batch INSERT optimization (COPY protocol)
    - Time-range query implementation
    - Retention policy enforcement (DELETE old partitions)

19. **Warm Store Query Layer** (need ~500 LOC)
    - Historical metrics query (time range, aggregation)
    - Trace span query (by trace ID, service, duration)
    - Event log query (filtering, pagination)

---

### PHASE 2: EBPF NETWORK OBSERVER (Weeks 4-6)
**Target: +12,000 LOC → Total: ~56K LOC**

**Goal:** Zero-instrumentation network visibility

#### Layer 6: eBPF Infrastructure (Rust Agent) — ~2,000 LOC

20. **eBPF Loader** (need ~800 LOC)
    - Integrate `aya` crate for eBPF loading
    - Implement eBPF program compilation (XDP, TC, kprobe, tracepoint)
    - Map management (ring buffers, hash maps, arrays)
    - Kernel version detection and compatibility checks
    - Graceful fallback for non-Linux/old kernels

21. **eBPF Map Readers** (need ~700 LOC)
    - Ring buffer consumer for high-throughput events
    - Perf buffer consumer (fallback)
    - Map polling with configurable intervals
    - Event serialization to protobuf

22. **eBPF Security & Lifecycle** (need ~500 LOC)
    - Capability detection (CAP_BPF, CAP_PERFMON)
    - Program attachment/detachment on demand
    - Resource limits (map sizes, program count)
    - Error handling for eBPF verifier rejections

#### Layer 7: TCP Connection Tracker (eBPF) — ~3,000 LOC

23. **eBPF Program (C/Rust)** (need ~1,000 LOC)
    - kprobe on `tcp_connect`, `tcp_close`, `tcp_set_state`
    - Extract sock struct: src/dst IP, port, state, pid
    - Ring buffer output for connection events
    - Hash map for connection state tracking

24. **User-Space Parser** (need ~1,000 LOC)
    - Parse ring buffer events into `TcpConnectionEvent`
    - Maintain connection state machine (ESTABLISHED, TIME_WAIT, etc.)
    - Calculate connection duration, bytes transferred
    - Correlate connections to process PIDs

25. **Connection Graph Builder** (need ~1,000 LOC)
    - Build dependency graph from connections
    - Identify service boundaries (port-based detection)
    - Detect connection anomalies (port scans, connection storms)
    - Export topology change events to cluster

#### Layer 8: DNS Mapper (eBPF) — ~2,000 LOC

26. **eBPF Program** (need ~800 LOC)
    - kprobe on `udp_sendmsg` (port 53 detection)
    - Parse DNS query/response from packet payload
    - Map domain names to resolved IPs
    - Ring buffer output for DNS events

27. **User-Space Mapper** (need ~700 LOC)
    - Maintain DNS cache (domain → IP mapping with TTL)
    - Correlate DNS lookups to process PIDs
    - Detect DNS anomalies (high query rate, NXDOMAIN spikes)
    - Export DNS mapping to cluster for enrichment

28. **DNS-IP Correlation** (need ~500 LOC)
    - Link TCP connections to DNS lookups (IP resolution)
    - Build service discovery map (domain → service name)
    - Detect DNS-based service mesh patterns

#### Layer 9: HTTP Inspector (eBPF) — ~3,500 LOC

29. **eBPF Program** (need ~1,200 LOC)
    - kprobe on `tcp_recvmsg`, `tcp_sendmsg` (ports 80, 443, 8080, etc.)
    - Parse HTTP/1.1 request line: method, path, version
    - Parse HTTP/1.1 response: status code, content-length
    - Handle HTTP/2 frame parsing (more complex)
    - Ring buffer output for HTTP events

30. **User-Space Parser** (need ~1,200 LOC)
    - Reassemble HTTP streams from TCP segments
    - Parse HTTP headers (Host, Content-Type, User-Agent)
    - Calculate request/response latency
    - Track request sizes, response sizes
    - Detect HTTP errors (4xx, 5xx spikes)

31. **HTTP Correlation** (need ~600 LOC)
    - Correlate HTTP requests to traces (if SDK present)
    - Build service dependency from HTTP calls
    - Detect API endpoint patterns
    - Export HTTP metrics to cluster

32. **HTTPS Limitation Handling** (need ~500 LOC)
    - Document TLS limitation (can't inspect encrypted traffic)
    - Implement SNI extraction from TLS ClientHello
    - Provide service mesh integration notes (mTLS support)

#### Layer 10: Database Protocol Inspector (eBPF) — ~2,500 LOC

33. **PostgreSQL Inspector** (need ~900 LOC)
    - eBPF program: kprobe on port 5432
    - Parse PostgreSQL wire protocol (StartupMessage, Query, RowDescription)
    - Extract SQL query text (limited to first N bytes)
    - Track query latency, response rows
    - Detect slow queries, N+1 patterns

34. **MySQL Inspector** (need ~800 LOC)
    - eBPF program: kprobe on port 3306
    - Parse MySQL wire protocol (COM_QUERY, OK, Resultset)
    - Extract SQL query text
    - Track query latency, affected rows

35. **Redis Inspector** (need ~800 LOC)
    - eBPF program: kprobe on port 6379
    - Parse Redis protocol (RESP: bulk strings, arrays)
    - Extract command names (GET, SET, HGETALL, etc.)
    - Track command latency, key patterns

---

### PHASE 3: PROCESSING PIPELINE (Weeks 7-9)
**Target: +10,000 LOC → Total: ~66K LOC**

**Goal:** Transform raw data into actionable intelligence

#### Layer 11: Aggregator Service (Go) — ~4,000 LOC

36. **Windowed Aggregation** (need ~2,000 LOC)
    - Implement tumbling windows (1m, 5m, 1h, 1d)
    - Support per-agent, per-service, per-tenant aggregation
    - Calculate: avg, min, max, p50, p90, p95, p99, sum, count, stddev
    - Handle late-arriving data (grace periods)
    - Emit aggregated metrics to `paryty.{tenant}.metrics.aggregated`

37. **Downsampling** (need ~1,000 LOC)
    - Implement retention-based downsampling
    - 1m resolution → keep 7 days
    - 5m resolution → keep 30 days
    - 1h resolution → keep 90 days
    - 1d resolution → keep 1 year

38. **Top-N Tracking** (need ~500 LOC)
    - Track top-N consumers (CPU, memory, disk IO, network)
    - Maintain leaderboards per time window
    - Detect sudden rank changes (anomalies)

39. **Aggregator Health & Tuning** (need ~500 LOC)
    - Self-monitoring (processing lag, memory usage)
    - Dynamic window adjustment based on load
    - Checkpointing for crash recovery

#### Layer 12: Correlator Service (Go) — ~4,000 LOC

40. **Trace-Metric Correlation** (need ~1,500 LOC)
    - Join trace spans with host metrics at same timestamp
    - Detect resource contention (high CPU during slow traces)
    - Build trace-to-infrastructure mapping

41. **Log-Trace Correlation** (need ~1,000 LOC)
    - Parse trace_id, span_id from log entries
    - Link logs to trace spans
    - Build incident timeline (logs + traces + metrics)

42. **Dependency Graph Builder** (need ~1,000 LOC)
    - Build service dependency graph from eBPF data
    - Update topology on connection changes
    - Calculate service criticality (upstream/downstream impact)
    - Export to `paryty.{tenant}.topology` topic

43. **Event Correlation** (need ~500 LOC)
    - Detect cascading failures (error propagation patterns)
    - Correlate deployment events with metric changes
    - Build incident blast radius analysis

#### Layer 13: Enricher Service (Go) — ~2,000 LOC

44. **Metadata Injection** (need ~1,000 LOC)
    - Add service name, version, environment from agent labels
    - Add cloud metadata (AWS region, GCP zone, Azure region)
    - Add Kubernetes metadata (namespace, pod, deployment, node)
    - Add geographic metadata (datacenter, edge location)

45. **Service Mapping** (need ~500 LOC)
    - Map ports to service names (IANA registry + custom mapping)
    - Map IPs to hostnames (DNS reverse lookup)
    - Map processes to services (command-line patterns)

46. **Tag Propagation** (need ~500 LOC)
    - Propagate labels across related metrics/traces/logs
    - Implement tag inheritance (pod → deployment → namespace → cluster)
    - Support custom tag rules (regex-based extraction)

---

### PHASE 4: STORAGE LAYER COMPLETION (Weeks 10-12)
**Target: +10,000 LOC → Total: ~76K LOC**

**Goal:** Production-ready 3-tier storage with data lifecycle

#### Layer 14: Hot Store Productionization (Go) — ~3,000 LOC

47. **Topology State Management** (need ~1,000 LOC)
    - Implement atomic topology updates (WATCH/MULTI/EXEC)
    - Support optimistic concurrency control
    - Build topology diff calculator (for timeline engine)

48. **Live Metrics Optimization** (need ~1,000 LOC)
    - Implement sorted sets with timestamp scores
    - Support range queries (last N minutes)
    - Implement automatic TTL-based eviction
    - Add compression for metric payloads

49. **Alert State Machine** (need ~500 LOC)
    - Track alert lifecycle (firing, resolved, acknowledged)
    - Implement alert deduplication
    - Support alert grouping by service/tenant

50. **Connection Tracking** (need ~500 LOC)
    - Track active agent connections (last heartbeat, session ID)
    - Detect disconnected agents
    - Implement connection-based load balancing

#### Layer 15: Warm Store Productionization (Go) — ~4,000 LOC

51. **Schema Design & Migration** (need ~1,000 LOC)
    - Design QuestDB schema (metrics, traces, events, alerts)
    - Implement automatic partitioning (daily/hourly)
    - Build schema migration system
    - Optimize column types for time-series

52. **High-Throughput Ingestion** (need ~1,500 LOC)
    - Implement QuestDB REST API ingestion (bulk insert)
    - Optimize batch sizes (10K-100K rows per batch)
    - Implement retry logic with backpressure
    - Add ingestion metrics (rows/sec, latency, errors)

53. **Query Optimization** (need ~1,000 LOC)
    - Implement time-range queries with downsampling
    - Support GROUP BY time, service, metric_name
    - Add query result caching (Dragonfly-backed)
    - Implement query timeout and cancellation

54. **Retention Management** (need ~500 LOC)
    - Implement automated partition dropping
    - Support per-tenant retention policies
    - Add archival trigger for cold store migration

#### Layer 16: Cold Store Implementation (Go) — ~3,000 LOC

55. **SeaweedFS Integration** (need ~1,500 LOC)
    - Implement S3-compatible client (aws-sdk-go)
    - Design object key schema: `{tenant}/{type}/{date}/{id}.zst`
    - Implement compression before upload (zstd)
    - Add upload batching and parallel transfers

56. **Timeline Snapshots** (need ~1,000 LOC)
    - Implement full state snapshot every 5 minutes
    - Serialize topology + metrics + alerts to JSON
    - Compress and upload to SeaweedFS
    - Implement 7-day retention with auto-cleanup

57. **Cold Store Retrieval** (need ~500 LOC)
    - Implement range queries (get snapshots between timestamps)
    - Support timeline replay data fetching
    - Add download caching for repeated access

---

### PHASE 5: FRONTEND VISUALIZATION (Weeks 13-16)
**Target: +12,000 LOC → Total: ~88K LOC**

**Goal:** GPU-accelerated, real-time observability dashboard

#### Layer 17: GPU Rendering Engine (TypeScript) — ~5,000 LOC

58. **PixiJS Topology Renderer** (current 188 LOC, need +2,000 LOC)
    - Implement instanced rendering for 15K+ nodes
    - Add zoom levels (service → container → host → datacenter)
    - Implement node clustering (group nearby nodes)
    - Add multi-level drill-down navigation

59. **Force-Directed Layout** (need ~1,500 LOC)
    - Integrate d3-force for automatic node positioning
    - Implement layout constraints (keep connected nodes close)
    - Add layout animation (smooth transitions)
    - Support manual node pinning/dragging

60. **Particle System** (need ~1,000 LOC)
    - Implement particle flow along edges (data flow visualization)
    - Color particles by throughput/latency
    - Add particle density for traffic intensity
    - Optimize with GPU compute (Web Workers)

61. **Visual Effects** (need ~500 LOC)
    - Glow effects for high CPU/memory (node color intensity)
    - Edge animation for throughput (width, color)
    - Pulse animation for alerts/errors
    - Health status color coding (green/yellow/red)

#### Layer 18: Real-Time Data Pipeline (TypeScript) — ~3,000 LOC

62. **WebSocket Manager** (need ~1,000 LOC)
    - Implement WebSocket connection with auto-reconnect
    - Subscribe to real-time metric updates
    - Handle backpressure (drop old updates if behind)
    - Implement message deduplication

63. **SSE Timeline Replay** (need ~800 LOC)
    - Implement Server-Sent Events for timeline streaming
    - Support replay speed control (0.25x - 16x)
    - Stream topology snapshots + metric deltas
    - Handle pause/resume/seek

64. **Web Workers** (need ~1,200 LOC)
    - Data parser worker (deserialize protobuf/JSON)
    - Metric processor worker (calculate aggregates, percentiles)
    - Topology layout worker (run d3-force off main thread)
    - SharedArrayBuffer ring buffer for zero-copy communication

#### Layer 19: State Management & Stores (TypeScript) — ~2,000 LOC

65. **Zustand Stores** (need ~2,000 LOC)
    - Topology store (nodes, edges, layout state)
    - Metrics store (time-series data, aggregation state)
    - Alert store (active alerts, alert history)
    - User preferences store (zoom level, theme, time range)
    - Timeline store (replay state, snapshots, speed)

#### Layer 20: UI Components (TypeScript) — ~2,000 LOC

66. **Dashboard Components** (need ~800 LOC)
    - Metric cards (current value, trend, sparkline)
    - Time range selector (last 5m, 1h, 24h, 7d, custom)
    - Service selector (filter by service, host, container)
    - Alert panel (active alerts, alert history)

67. **Topology View** (need ~600 LOC)
    - Full-screen topology visualization
    - Node detail panel (metrics, logs, traces)
    - Edge detail panel (throughput, latency, error rate)
    - Search & filter (find node by name, label)

68. **Timeline Replay UI** (need ~600 LOC)
    - Timeline scrubber (seek to any point)
    - Speed controls (0.25x, 0.5x, 1x, 2x, 4x, 8x, 16x)
    - Diff viewer (compare two points in time)
    - Export button (JSON, PDF, HTML)

---

### PHASE 6: INTELLIGENCE LAYER (Weeks 17-21)
**Target: +15,000 LOC → Total: ~103K LOC**

**Goal:** Predictive analytics and simulation capabilities

#### Layer 21: Forecasting Engine (Python) — ~4,000 LOC

69. **Linear Regression Layer** (need ~1,000 LOC)
    - Real-time baseline forecasting (<1ms)
    - Implement rolling window regression
    - Support multiple metrics (CPU, memory, disk)
    - Calculate confidence intervals (95%, 99%)

70. **Prophet Integration** (need ~1,200 LOC)
    - Seasonal pattern detection (daily, weekly cycles)
    - Holiday effect modeling
    - Changepoint detection
    - Forecast 7 days ahead

71. **XGBoost Integration** (need ~1,000 LOC)
    - Multi-variable pattern recognition
    - Feature engineering (lag features, rolling stats)
    - Model training pipeline (cross-validation, hyperparameter tuning)
    - Forecast complex interactions (CPU + memory + disk)

72. **Ensemble Logic** (need ~500 LOC)
    - Combine forecasts from all 3 layers
    - Weight by model confidence and recent accuracy
    - Handle model disagreements
    - Generate capacity planning alerts

73. **gRPC Service Wrapper** (need ~300 LOC)
    - Implement forecasting gRPC service
    - Handle batch forecasting requests
    - Cache forecasts (reuse until next collection)

#### Layer 22: Anomaly Detection (Python) — ~5,000 LOC

74. **Statistical Methods** (need ~1,000 LOC)
    - Z-Score detection (point anomalies)
    - IQR detection (outlier identification)
    - EWMA (Exponential Weighted Moving Average) for trend anomalies
    - All run in <1ms per metric

75. **Isolation Forest** (need ~1,200 LOC)
    - Train Isolation Forest on historical metrics
    - Detect complex point anomalies
    - Handle high-dimensional data (multiple metrics together)
    - Run in <10ms per batch

76. **Autoencoder (TensorFlow)** (need ~2,000 LOC)
    - Train autoencoder on normal metric patterns
    - Detect contextual anomalies (memory leaks, gradual degradation)
    - Detect collective anomalies (cascading failures)
    - Run in <50ms per batch
    - Implement model retraining pipeline

77. **Ensemble Decision** (need ~500 LOC)
    - Combine all 3 layers with voting mechanism
    - Calculate confidence scores
    - Reduce false positives
    - Generate alert with anomaly explanation

78. **gRPC Service Wrapper** (need ~300 LOC)
    - Implement anomaly detection gRPC service
    - Handle real-time metric streams
    - Return anomaly scores and explanations

#### Layer 23: Simulation Engine (Go) — ~4,000 LOC

79. **Digital Twin "What-If"** (need ~1,500 LOC)
    - Model infrastructure changes (add/remove nodes, scale services)
    - Simulate metric changes based on historical patterns
    - Calculate impact on performance and cost
    - Visualize simulation results in frontend

80. **External Integrations** (need ~1,000 LOC)
    - k6 load testing integration (trigger tests, parse results)
    - LitmusChaos integration (inject failures, observe impact)
    - tc network simulation (add latency, packet loss)
    - Orchestrate multi-tool simulations

81. **Bot Engine & Pattern Analyzer** (need ~1,000 LOC)
    - Detect bot vs human request patterns
    - Identify rapid fire, smart bots, distributed bots
    - Analyze timing anomalies, volume anomalies
    - Generate threat assessment reports

82. **Degradation Simulator** (need ~500 LOC)
    - Simulate graceful degradation under load
    - Model rate limiting, request queuing effects
    - Predict breaking points
    - Recommend capacity adjustments

#### Layer 24: Timeline Engine (Go) — ~2,000 LOC

83. **Snapshot Manager** (need ~800 LOC)
    - Manage 5-minute interval snapshots
    - Store in SeaweedFS with 7-day retention
    - Implement snapshot diffing (what changed between snapshots)
    - Support snapshot tagging (mark incident points)

84. **Replay Engine** (need ~700 LOC)
    - Replay system state at any point in time
    - Stream to frontend via SSE
    - Support speed control (0.25x - 16x)
    - Handle pause/resume/seek

85. **Diff Calculator** (need ~300 LOC)
    - Compare topology between two points
    - Compare metrics between two points
    - Compare alerts between two points
    - Generate human-readable diff report

86. **Export Manager** (need ~200 LOC)
    - Export snapshots as JSON
    - Export incident reports as PDF
    - Export interactive HTML timelines

---

### PHASE 7: SDK & DEVELOPER EXPERIENCE (Weeks 22-23)
**Target: +5,000 LOC → Total: ~108K LOC**

**Goal:** Easy instrumentation for application developers

#### Layer 25: Go SDK — ~3,000 LOC

87. **OpenTelemetry Wrapper** (need ~1,000 LOC)
    - Wrap OpenTelemetry SDK for metrics, traces, logs
    - Implement Paryty-specific span attributes
    - Support context propagation (W3C Trace Context)
    - Auto-instrumentation for HTTP, gRPC, SQL

88. **Custom Metrics API** (need ~800 LOC)
    - Counter, Gauge, Histogram implementations
    - Business metric support (orders/sec, revenue, conversions)
    - Metric naming conventions and validation

89. **Health Reporting** (need ~500 LOC)
    - Application health endpoint integration
    - Dependency health checking (database, cache, external APIs)
    - Readiness/liveness probe support

90. **SDK Configuration** (need ~400 LOC)
    - Environment variable configuration
    - YAML configuration file support
    - Runtime configuration updates
    - Sampling rate control

91. **Examples & Documentation** (need ~300 LOC)
    - HTTP service example
    - gRPC service example
    - Worker/job example
    - README with quickstart guide

#### Layer 26: Multi-Language SDKs (Proto Generation) — ~2,000 LOC

92. **Python SDK** (need ~700 LOC)
    - Generate from .proto files
    - Add Pythonic wrapper (simpler API)
    - Include in PyPI package

93. **Java SDK** (need ~700 LOC)
    - Generate from .proto files
    - Add Java wrapper (builder pattern, fluent API)
    - Include in Maven package

94. **Node.js SDK** (need ~600 LOC)
    - Generate from .proto files
    - Add TypeScript types
    - Include in npm package

---

### PHASE 8: PRODUCTION HARDENING (Weeks 24-26)
**Target: +7,000 LOC → Total: ~115K LOC**

**Goal:** Enterprise-ready, production-grade system

#### Layer 27: Security & Multi-Tenancy — ~2,000 LOC

95. **TLS Everywhere** (need ~500 LOC)
    - mTLS for agent-cluster communication
    - TLS for all internal service communication
    - Certificate rotation automation

96. **RBAC & Authorization** (need ~800 LOC)
    - Role-based access control (admin, viewer, operator)
    - Tenant isolation enforcement
    - API key management (create, rotate, revoke)

97. **Audit Logging** (need ~400 LOC)
    - Log all administrative actions
    - Log data access patterns
    - Support compliance (SOC2, HIPAA, GDPR)

98. **Secrets Management** (need ~300 LOC)
    - Integrate with Vault or Kubernetes Secrets
    - Rotate API keys automatically
    - Encrypt sensitive configuration

#### Layer 28: Observability (Dogfooding) — ~2,000 LOC

99. **Paryty Self-Monitoring** (need ~1,000 LOC)
    - Instrument all Paryty services with Paryty SDK
    - Monitor ingestion latency, processing lag, storage health
    - Alert on Paryty system degradation

100. **Performance Dashboards** (need ~600 LOC)
     - Build dashboards for Paryty performance
     - Track SLOs (ingestion latency <100ms, query latency <500ms)
     - Capacity monitoring (Redpanda disk, QuestDB storage)

101. **Chaos Testing** (need ~400 LOC)
     - Test agent reconnection under failure
     - Test data loss scenarios (Redpanda node down)
     - Test storage failover (Dragonfly node down)

#### Layer 29: Deployment & CI/CD — ~1,500 LOC

102. **Kubernetes Manifests** (need ~800 LOC)
     - Complete K8s manifests for all services
     - HPA (Horizontal Pod Autoscaler) configurations
     - PodDisruptionBudgets for high availability
     - Resource requests/limits tuning

103. **Helm Charts** (need ~500 LOC)
     - Parameterized Helm charts
     - Multi-environment support (dev, staging, prod)
     - Values files for different scales (SMB, enterprise)

104. **CI/CD Pipeline** (need ~200 LOC)
     - GitHub Actions for build, test, deploy
     - Automated testing (unit, integration, e2e)
     - Container image scanning

#### Layer 30: Testing & Validation — ~1,500 LOC

105. **Integration Tests** (need ~800 LOC)
     - Agent → Cluster → Storage end-to-end tests
     - Query layer integration tests
     - Intelligence layer model validation tests

106. **Load Tests** (need ~500 LOC)
     - Simulate 10K agents sending metrics
     - Test query performance under load
     - Validate storage throughput

107. **Scenario Tests** (need ~200 LOC)
     - Synthetic workload generation
     - Failure scenario testing
     - Timeline replay validation

---

## TOTAL: 115,000+ LOC

This gets you to an **enterprise-grade V1.0** that delivers on all Paryty promises.

---

## EXECUTION PRINCIPLES FOR AI AGENT BUILD

Since this is **AI agent-only implementation**, here's what you need to enforce:

### 1. Sequential Phase Execution
- **Do NOT let agents skip phases.** Phase 1 must be complete before Phase 2 starts.
- Each phase has **acceptance criteria** (see below).
- **You approve** before moving to next phase.

### 2. Agent Team Structure Per Phase

For each phase, deploy specialized agents:
- **Rust Agent Specialist** (agent implementation)
- **Go Cluster Specialist** (backend services)
- **Python ML Specialist** (intelligence layer)
- **Frontend Specialist** (React/PixiJS)
- **eBPF Specialist** (kernel programming)
- **Infrastructure Specialist** (K8s, CI/CD, deployment)

### 3. Acceptance Criteria Per Phase

**Phase 1 Acceptance:**
- ✅ Metrics flow from agent CPU collector → ingestion → Redpanda → QuestDB
- ✅ Query returns metrics from QuestDB in <500ms
- ✅ Agent reconnects after network interruption with buffer replay

**Phase 2 Acceptance:**
- ✅ eBPF TCP tracker detects new connections in real-time
- ✅ HTTP inspector extracts method, path, status, latency
- ✅ DNS mapper resolves domain → IP correlations
- ✅ Dependency graph built from eBPF data

**Phase 3 Acceptance:**
- ✅ Aggregator emits 1m, 5m, 1h aggregated metrics
- ✅ Correlator links traces to metrics to logs
- ✅ Enricher adds service name, environment, k8s metadata

**Phase 4 Acceptance:**
- ✅ Hot store serves live metrics in <1ms
- ✅ Warm store handles 100K inserts/sec, queries in <50ms
- ✅ Cold store archives snapshots, retrievable for timeline replay

**Phase 5 Acceptance:**
- ✅ Frontend renders 15K nodes at 60fps
- ✅ Real-time metrics update via WebSocket
- ✅ Timeline replay works with speed controls

**Phase 6 Acceptance:**
- ✅ Forecasting predicts CPU usage 7 days ahead with <10% error
- ✅ Anomaly detection identifies injected anomalies with >90% precision
- ✅ Simulation engine runs "what-if" scenarios

**Phase 7 Acceptance:**
- ✅ Go SDK instruments HTTP service with <5 lines of code
- ✅ Python/Java/Node.js SDKs generated and published

**Phase 8 Acceptance:**
- ✅ All services communicate over mTLS
- ✅ RBAC enforced (admin can't access other tenant data)
- ✅ Paryty monitors itself with Paryty
- ✅ Helm chart deploys full stack to Kubernetes

---

## WHAT YOU CONTROL (YOUR DECISIONS)

1. **Phase prioritization** — Can reorder phases based on your go-to-market strategy
2. **Feature inclusion** — Can cut features from V1.0 (e.g., skip simulation engine)
3. **Tech choices** — Can override any technology decision
4. **Quality bar** — Define what "done" means for each layer
5. **Scope boundaries** — Decide what's V1.0 vs V1.1 vs V2.0
6. **Agent assignments** — Choose which agents work on which layers
7. **Review checkpoints** — Set review frequency (after each layer? each phase?)

---

## REALISTIC TIMELINE

With AI agents working in parallel within phases:
- **Phases 1-2:** 6 weeks (foundation + eBPF)
- **Phases 3-4:** 6 weeks (processing + storage)
- **Phases 5-6:** 8 weeks (frontend + intelligence)
- **Phases 7-8:** 4 weeks (SDK + hardening)

**Total: 24 weeks (~6 months)** to enterprise-grade V1.0

---

## BOTTOM LINE

The previous agent told you it was "complete" because they likely:
1. Built scaffolding and assumed you'd fill in the details
2. Didn't understand the depth required for production eBPF, storage integrations, intelligence layer
3. Counted proto files and build configs as "implementation"

**Reality:** You have ~29K LOC of architecture and basic implementations. You need **~86K more LOC** of actual implementation to reach 115K LOC enterprise-grade V1.0.

**This roadmap gives you the exact 107 implementation items** across 30 layers and 8 phases. Every item is specific, measurable, and buildable by AI agents under your direction.

---

## ARCHITECTURAL DECISIONS REGISTER (ALL LOCKED)

Every decision below has been finalized. **No agent may deviate from these decisions** without explicit user approval. Each decision links to its hardened spec for full context, rationale, and pseudo-code.

---

### Phase 1: Foundation & Data Pipeline
**Spec:** `docs/phase-1-hardened-spec.md` (2,312 lines)

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | Configuration Format | **YAML + env overrides** | YAML for files, env vars for container overrides. Simple, widely supported. |
| 2 | Edge Buffer Persistence | **SQLite for disk spillover** | Lightweight, zero-config, ACID. Survives agent restarts. |
| 3 | Redpanda Client | **franz-go** | High-performance, native Kafka protocol. Supports exactly-once semantics. |
| 4 | QuestDB Ingestion | **ILP (InfluxDB Line Protocol)** | Low-latency, high-throughput. QuestDB's native fast path. |
| 5 | Tenant Isolation | **Full tenant isolation** | Separate Redpanda topics per tenant. No cross-tenant data leakage. |
| 6 | Compression | **Zstd for all** | Best compression ratio + speed. Remove Snappy, unify on Zstd. |
| 7 | Health Checks | **Shallow liveness + deep readiness** | Liveness: process alive. Readiness: all dependencies connected. |

---

### Phase 2: eBPF Network Observer
**Spec:** `docs/phase-2-hardened-spec.md` (1,948 lines)

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | eBPF Framework | **libbpf (C eBPF + Rust loader)** | NOT aya. libbpf is the kernel standard, better verifier compatibility, CO-RE support. |
| 2 | Attachment Strategy | **Kprobes on kernel functions** | Most compatible across kernel versions. Fallback to tracepoints if needed. |
| 3 | HTTP Inspection | **SNI extraction + plaintext HTTP** | Full HTTP parsing deferred. SNI from TLS ClientHello gives service names without decryption. |
| 4 | Database Inspection | **Deferred to Phase 4** | Phase 2 focuses on network topology only. DB inspection requires additional eBPF programs. |
| 5 | Non-Linux Fallback | **Graceful degradation to /proc** | Agent works on macOS/Windows with /proc-based collection. eBPF features auto-detected. |

---

### Phase 3: Processing Pipeline
**Spec:** `docs/phase-3-hardened-spec.md` (2,185 lines)

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | Aggregation Window Type | **Tumbling windows** | Fixed, non-overlapping time buckets (1m, 5m, 1h, 1d). Simpler, lower memory, deterministic boundaries. |
| 2 | Processing Pipeline Architecture | **Pipeline service (single binary)** | Aggregate → Correlate → Enrich in one binary. Lower latency, simpler deployment, single consumer group. |
| 3 | DB Protocol Inspection | **Defer to Phase 4** | Keep Phase 3 focused on Go processing logic. DB inspection requires eBPF changes. |
| 4 | Enrichment Metadata Source | **Agent labels** | Simple metadata from agent registration. No Kubernetes API dependency. |

---

### Phase 4: Storage Layer Completion
**Spec:** `docs/phase-4-hardened-spec.md` (2,673 lines)

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | Hot Store Concurrent Updates | **WATCH/MULTI/EXEC (optimistic locking)** | Zero overhead when no conflict, automatic retry on contention. |
| 2 | QuestDB Ingestion Method | **Hybrid ILP + REST** | ILP for real-time (low latency), REST for batch/backfill (high throughput). |
| 3 | Timeline Snapshot Architecture | **Full snapshots + Event log** | Full snapshots every 5min as checkpoints. Redpanda event log for inter-snapshot replay. |
| 4 | Cold Store Query Cache | **Dragonfly LRU Cache** | Cache recently accessed snapshots (~100 snapshots, ~1GB). Covers 90% of queries. |
| 5 | eBPF DB Protocol Inspection | **Full query extraction + TLS fallback** | Parse PostgreSQL/MySQL/Redis protocols. Fall back to connection-only for TLS. |

---

### Phase 5: Frontend Visualization
**Spec:** `docs/phase-5-hardened-spec.md` (1,883 lines)

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | Rendering Strategy | **Instanced rendering (PIXI.ParticleContainer)** | 15K+ nodes at 60fps. Single draw call per container. |
| 2 | Layout Algorithm | **Hierarchical + Clustering** | Group by service/type/region/layer. d3-force within clusters. Best of both worlds. |
| 3 | Particle System | **PixiJS Particle Container** | GPU-accelerated particles for edge data flow visualization. |
| 4 | Theme & Typography | **Monochromatic B&W + Geist Sans/Mono** | Geist Sans (Headings, Titles & Labels) + Geist Mono (UI Text, code/data). **STOP GATE**: Agents must request user's UI/UX rulebook before implementing styling. |

---

### Phase 6: Intelligence Layer
**Spec:** `docs/phase-6-hardened-spec.md` (3,165 lines)

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | Python Service Architecture | **Standalone gRPC Microservice** | Independent Python service. Clean separation, independent scaling, no Go-Python FFI. |
| 2 | ML Model Management | **File-Based Persistence (pickle/joblib)** | Simple, no external dependencies. Models saved to disk, loaded on startup. |
| 3 | Forecasting Ensemble Strategy | **Weighted Average** | MAPE-based self-correcting weights. Simple, effective, transparent. |
| 4 | Simulation Engine | **What-If + Chaos Engineering (V2.0 → Full Digital Twin)** | Interface abstractions designed for V2.0 migration to live mirroring. k6 + LitmusChaos integration. |

---

### Phase 7: SDK & Developer Experience
**Spec:** `docs/phase-7-hardened-spec.md` (1,482 lines)

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | OpenTelemetry Integration | **Wrap OTel SDK** | Paryty API wrapping OTel internally. Gets auto-instrumentation ecosystem. |
| 2 | Multi-Language SDK Approach | **Proto + Idiomatic Wrapper** | Proto stubs + language-native wrappers (Pythonic, Java builder, TypeScript types). |
| 3 | SDK Distribution | **Package Managers** | Go modules, PyPI, Maven Central, npm. Standard distribution channels. |
| 4 | Auto-Instrumentation Strategy | **Middleware-Based** | HTTP middleware, gRPC interceptors, SQL driver wrapper. Non-invasive, standard pattern. |

---

### Phase 8: Production Hardening
**Spec:** `docs/phase-8-hardened-spec.md` (2,325 lines)

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | Certificate Management | **cert-manager + non-K8s fallback** | K8s-native cert issuance/rotation. Self-signed fallback for VMs/bare metal. |
| 2 | RBAC Model | **Simple Role Hierarchy (Admin/Operator/Viewer)** | Three fixed roles. Covers 90% of use cases. Simple to implement and audit. |
| 3 | Self-Monitoring | **Paryty SDK + custom fallback (NO Prometheus)** | Full dogfooding. Custom lightweight health checker for critical infra. No competitor tools. |
| 4 | Helm Chart Scope | **Umbrella Chart with Subcharts** | Parent chart with 6 subcharts (agent, cluster, frontend, security, monitoring, storage). |

---

### Cross-Phase Design Principles

These principles apply across ALL phases and override any agent suggestion:

1. **No competitor monitoring tools** — Paryty monitors itself. No Prometheus, Grafana, or Datadog. "If we claim to be the ultimate observability platform, we use our own product."
2. **Monochromatic UI** — Black and white theme. Geist Sans for headings/titles/labels, Geist Mono for UI text/code/data. Agents must request the user's UI/UX rulebook before implementing any styling.
3. **V2.0 migration paths** — Simulation engine interfaces designed for V2.0 full digital twin migration.
4. **Zstd for all compression** — No Snappy, no gzip. Unified on Zstd.
5. **ILP for QuestDB** — InfluxDB Line Protocol as the primary ingestion path, REST for batch.
6. **franz-go for Redpanda** — Native Kafka protocol client.
7. **SQLite for edge buffer** — Lightweight, zero-config disk spillover.
8. **cert-manager for TLS** — With self-signed fallback for non-K8s.
9. **Middleware-based auto-instrumentation** — HTTP middleware, gRPC interceptors, SQL wrappers.
10. **Package manager distribution** — Go modules, PyPI, Maven Central, npm.

---

### Hardened Spec Index

| Spec | Lines | LOC Target | Status |
|------|-------|------------|--------|
| `docs/phase-1-hardened-spec.md` | 2,312 | ~15,000 | LOCKED |
| `docs/phase-2-hardened-spec.md` | 1,948 | ~10,000 | LOCKED |
| `docs/phase-3-hardened-spec.md` | 2,185 | ~8,000 | LOCKED |
| `docs/phase-4-hardened-spec.md` | 2,673 | ~9,000 | LOCKED |
| `docs/phase-5-hardened-spec.md` | 1,883 | ~10,400 | LOCKED |
| `docs/phase-6-hardened-spec.md` | 3,165 | ~14,200 | LOCKED |
| `docs/phase-7-hardened-spec.md` | 1,482 | ~4,200 | LOCKED |
| `docs/phase-8-hardened-spec.md` | 2,325 | ~7,000 | LOCKED |
| **Total** | **17,973** | **~77,800** | **ALL LOCKED** |
