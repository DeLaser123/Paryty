# Database Connection Pooling Strategy

## Overview

Paryty uses four data stores, each with distinct connection patterns. This document defines the connection pooling strategy for each.

---

## PostgreSQL (Control Plane)

**Driver:** `pgx/v5` (pgxpool)

**Purpose:** User accounts, tenant configuration, RBAC, audit logs, alert rules.

### Pool Configuration

```go
config, _ := pgxpool.ParseConfig(databaseURL)
config.MaxConns = runtime.NumCPU() * 4
config.MinConns = runtime.NumCPU()
config.MaxConnLifetime = 1 * time.Hour
config.MaxConnIdleTime = 30 * time.Minute
config.HealthCheckPeriod = 30 * time.Second
```

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| `MaxConns` | `NumCPU * 4` | Prevents connection exhaustion; each query is short-lived |
| `MinConns` | `NumCPU` | Maintains warm pool for burst traffic |
| `MaxConnLifetime` | 1 hour | Recycles connections to pick up schema changes |
| `MaxConnIdleTime` | 30 minutes | Frees idle connections during low traffic |
| `HealthCheckPeriod` | 30 seconds | Detects stale connections before use |

### Read Replicas (Future)

For read-heavy workloads (dashboard queries, report generation):

```go
// Separate pool for read replicas
readConfig, _ := pgxpool.ParseConfig(readReplicaURL)
readConfig.MaxConns = runtime.NumCPU() * 8  // Higher — read queries are more numerous
```

### Monitoring

```sql
-- Check active connections
SELECT count(*) FROM pg_stat_activity WHERE datname = 'paryty';

-- Check connection state
SELECT state, count(*) FROM pg_stat_activity GROUP BY state;
```

---

## Dragonfly (Hot Store)

**Driver:** `go-redis/v9`

**Purpose:** Live topology, real-time metrics (last 5 minutes), active alerts, rate limiting, query cache.

### Pool Configuration

```go
client := redis.NewClient(&redis.Options{
    Addr:         dragonflyAddr,
    PoolSize:     10 * runtime.NumCPU(),
    MinIdleConns: runtime.NumCPU(),
    MaxRetries:   3,
    DialTimeout:  5 * time.Second,
    ReadTimeout:  3 * time.Second,
    WriteTimeout: 3 * time.Second,
    PoolTimeout:  4 * time.Second,
})
```

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| `PoolSize` | `10 * NumCPU` | Dragonfly handles concurrent connections efficiently (multi-threaded) |
| `MinIdleConns` | `NumCPU` | Keeps warm connections ready |
| `MaxRetries` | 3 | Tolerates transient network blips |
| `DialTimeout` | 5s | Connection establishment timeout |
| `ReadTimeout` | 3s | Prevents hung reads |
| `WriteTimeout` | 3s | Prevents hung writes |
| `PoolTimeout` | 4s | Wait for available connection |

### TLS (Production)

```go
client := redis.NewClient(&redis.Options{
    Addr:      dragonflyAddr,
    TLSConfig: &tls.Config{ServerName: "dragonfly.paryty.svc"},
    // ... same pool settings
})
```

### Monitoring

```bash
# Check Dragonfly connection stats
redis-cli -p 6379 INFO clients
# Look for: connected_clients, blocked_clients, tracking_clients
```

---

## QuestDB (Warm Store)

**Driver:** ILP writer (custom TCP) + pgxpool (PostgreSQL wire protocol)

**Purpose:** Historical metrics (30-90 days), aggregated data, trace spans, event logs.

### ILP Writer (InfluxDB Line Protocol)

High-throughput ingestion path. Uses a single persistent TCP connection with auto-reconnect:

```go
type ILPWriter struct {
    conn     net.Conn
    mu       sync.Mutex
    buffer   []byte
    batchSize int
    flushInterval time.Duration
}
```

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| Connection | Single persistent TCP | ILP is designed for single-writer high throughput |
| Buffer size | 64 KB | Batch writes for efficiency |
| Flush interval | 1 second | Balances latency vs throughput |
| Auto-reconnect | Yes, with exponential backoff | Handles QuestDB restarts |

### SQL Queries (PostgreSQL Wire Protocol)

For dashboard queries and ad-hoc analysis:

```go
config, _ := pgxpool.ParseConfig(questdbPGURL)
config.MaxConns = runtime.NumCPU() * 2
config.MinConns = 2
config.MaxConnLifetime = 30 * time.Minute
config.MaxConnIdleTime = 10 * time.Minute
```

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| `MaxConns` | `NumCPU * 2` | QuestDB SQL queries are fast; fewer connections needed |
| `MinConns` | 2 | Minimal warm pool (queries are bursty) |
| `MaxConnLifetime` | 30 min | QuestDB may close idle connections |
| `MaxConnIdleTime` | 10 min | Free idle connections quickly |

### Monitoring

```sql
-- QuestDB: check active connections
SELECT * FROM pg_stat_activity;

-- Check ILP ingestion rate
SELECT count() FROM metrics WHERE timestamp > dateadd('m', -5, now());
```

---

## SeaweedFS (Cold Store)

**Driver:** AWS S3 SDK (`aws-sdk-go-v2`)

**Purpose:** Long-term retention (1-7 years), timeline snapshots, compliance archives.

### Connection Configuration

```go
cfg, _ := config.LoadDefaultConfig(ctx,
    config.WithEndpointResolver(aws.EndpointResolverFunc(
        func(service, region string) (aws.Endpoint, error) {
            return aws.Endpoint{URL: seaweedfsEndpoint}, nil
        },
    )),
)

client := s3.NewFromConfig(cfg, func(o *s3.Options) {
    o.UsePathStyle = true  // Required for SeaweedFS
})
```

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| HTTP client pool | `MaxIdleConns: 100`, `MaxIdleConnsPerHost: 20` | S3 API is request/response, benefits from connection reuse |
| Request timeout | 30 seconds | Cold store operations are expected to be slower |
| Retry | 3 attempts with exponential backoff | Handles transient SeaweedFS issues |

### Monitoring

```bash
# Check SeaweedFS cluster status
curl http://localhost:9333/cluster/status

# Check volume status
curl http://localhost:8080/status
```

---

## Connection Pool Monitoring (All Stores)

### OpenTelemetry Metrics

All connection pools expose these metrics:

| Metric | Type | Description |
|--------|------|-------------|
| `paryty.db.pool.active` | Gauge | Active connections |
| `paryty.db.pool.idle` | Gauge | Idle connections |
| `paryty.db.pool.wait_count` | Counter | Connection wait events |
| `paryty.db.pool.wait_duration` | Histogram | Time waiting for connection |
| `paryty.db.pool.max` | Gauge | Configured max pool size |

### Alerting Rules

| Alert | Condition | Action |
|-------|-----------|--------|
| Pool exhausted | `active == max` for 5 min | Increase pool size or add replicas |
| High wait time | `wait_duration p99 > 100ms` | Check for slow queries or connection leaks |
| Connection churn | `wait_count > 100/min` | Increase `MinIdleConns` |

---

## Summary

| Store | Driver | Max Pool | Min Pool | Use Case |
|-------|--------|----------|----------|----------|
| PostgreSQL | pgxpool | `NumCPU * 4` | `NumCPU` | Control plane |
| Dragonfly | go-redis | `10 * NumCPU` | `NumCPU` | Hot data (real-time) |
| QuestDB (ILP) | Custom TCP | 1 | 1 | High-throughput ingestion |
| QuestDB (SQL) | pgxpool | `NumCPU * 2` | 2 | Dashboard queries |
| SeaweedFS | AWS S3 SDK | 100 idle conns | — | Cold storage |
