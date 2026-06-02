# Phase 1 Hardened Specification — Foundation & Data Pipeline

**Target LOC**: ~15,000 (3,500 Rust agent + 3,000 Rust comm + 4,000 Go ingestion + 2,500 Go hot store + 2,000 Go warm store)

**Goal**: End-to-end metric collection, transport, ingestion, and storage. After Phase 1, a Paryty agent running on a Linux host collects metal metrics, compresses them, sends via gRPC to the cluster, which ingests, publishes to Redpanda, stores in Dragonfly (hot) and QuestDB (warm).

**Estimated Duration**: 5-7 days with AI agent execution

**Architectural Decisions Locked**:
1. Config: YAML + env overrides
2. Edge Buffer: SQLite for disk spillover
3. Redpanda Client: franz-go
4. QuestDB Ingestion: ILP (InfluxDB Line Protocol)
5. Tenant Isolation: Full tenant isolation
6. Compression: Zstd for all
7. Health Checks: Shallow liveness + deep readiness

---

## Table of Contents

1. [Pre-Phase Setup](#1-pre-phase-setup)
2. [Layer 1: Complete Metal Scrapers (Rust)](#2-layer-1-complete-metal-scrapers-rust)
3. [Layer 2: Communication Layer Hardening (Rust)](#3-layer-2-communication-layer-hardening-rust)
4. [Layer 3: Ingestion Service (Go)](#4-layer-3-ingestion-service-go)
5. [Layer 4: Hot Store Integration (Go)](#5-layer-4-hot-store-integration-go)
6. [Layer 5: Warm Store Integration (Go)](#6-layer-5-warm-store-integration-go)
7. [Verification Gates](#7-verification-gates)
8. [Contingency & Rollback](#8-contingency--rollback)

---

## Agents & Skills Required

### Agents
- `paryty-metal-scraper` — Rust metal scraper implementation
- `paryty-comm-layer` — Communication layer (gRPC, buffer, compression, flow control)
- `paryty-ingestion` — gRPC ingestion service
- `paryty-storage-tier` — Storage tier implementation (hot/warm/cold)
- `paryty-stream-engine` — Redpanda stream engine

### Oracles
- `oracle-rust` — Rust language authority (zero tolerance: no unwrap, no unsafe without SAFETY)
- `oracle-go` — Go language authority (no ignored errors, table-driven tests)
- `oracle-security` — Security review for every change

### Skills
- `paryty-generate-protos` — Proto compilation (if proto changes needed)
- `paryty-build-agent` — Agent build pipeline
- `paryty-build-cluster` — Cluster build pipeline

### Rules
- `coding-standards` — Language-specific coding standards
- `architecture` — Architecture rules (no layer skipping, gRPC agent↔cluster, REST cluster↔frontend)

---

## 1. Pre-Phase Setup

### 1.1 Dependency Verification

Before any code changes, verify these dependencies compile and link correctly.

**Rust Dependencies** (`agent/Cargo.toml`):
```toml
[dependencies]
# Core
tokio = { version = "1.37", features = ["full"] }
serde = { version = "1.0", features = ["derive"] }
serde_json = "1.0"
tracing = "0.1"
tracing-subscriber = { version = "0.3", features = ["json", "env-filter"] }
anyhow = "1.0"
thiserror = "1.0"
uuid = { version = "1.8", features = ["v4"] }
chrono = "0.4"

# gRPC
tonic = "0.11"
prost = "0.12"

# Compression
zstd = "0.13"

# System (Linux-only)
[target.'cfg(target_os = "linux")'.dependencies]
procfs = "0.16"
libc = "0.2"

# System (cross-platform fallback)
sysinfo = "0.30"

# Edge buffer (SQLite)
rusqlite = { version = "0.31", features = ["bundled"] }

# Serialization for buffer
bincode = "1.3"
```

**Go Dependencies** (`cluster/go.mod`):
```
go 1.22
github.com/twmb/franz-go v1.16
github.com/twmb/franz-go/pkg/kadm v1.10
github.com/redis/go-redis/v9 v9.5
github.com/jackc/pgx/v5 v5.5
github.com/minio/minio-go/v7 v7.0
google.golang.org/grpc v1.63
google.golang.org/protobuf v1.33
go.uber.org/zap v1.27
github.com/gin-gonic/gin v1.9
```

**Action**: Run `cargo check` in `agent/` and `go build ./...` in `cluster/`. Both MUST succeed before proceeding.

**Verification Command**:
```bash
# Agent
cd agent && cargo check 2>&1 | tail -5
# Cluster
cd cluster && go build ./... 2>&1 | tail -5
```

**If either fails**: Fix compilation errors before proceeding. Do NOT skip this step.

---

## 2. Layer 1: Complete Metal Scrapers (Rust)

**Target**: ~3,500 LOC across 8 files
**Agent**: `paryty-metal-scraper`
**Oracle**: `oracle-rust`

### 2.1 File Inventory

| File | Current LOC | Target LOC | Status |
|------|------------|------------|--------|
| `agent/src/metal/cpu.rs` | 197 | 450 | Partial — missing per-core freq, context switches |
| `agent/src/metal/memory.rs` | 120 | 350 | Partial — missing per-process, memory pressure |
| `agent/src/metal/disk.rs` | 240 | 400 | Partial — missing queue depth, disk type detection |
| `agent/src/metal/network.rs` | 183 | 350 | Partial — missing retransmits, RTT estimation |
| `agent/src/metal/process.rs` | 181 | 400 | Partial — missing CPU% calculation, fd count |
| `agent/src/metal/container.rs` | 158 | 300 | Partial — missing image name, resource limits |
| `agent/src/metal/batch.rs` | 122 | 250 | Needs proper error handling, retry logic |
| `agent/src/metal/mod.rs` | ~20 | 100 | Needs trait definitions, collector registry |

### 2.2 CPU Collector (`agent/src/metal/cpu.rs`)

**Current State**: Working Linux `/proc/stat` parser with `sysinfo` fallback. Has delta calculation with `AtomicU64`.

**What's Missing**:
1. Per-core frequency from `/sys/devices/system/cpu/cpu*/cpufreq/scaling_cur_freq`
2. Context switches from `/proc/stat` (ctxt field)
3. CPU topology (physical cores, logical cores, sockets)
4. Load average from `/proc/loadavg`
5. CPU temperature from `/sys/class/thermal/thermal_zone*/temp` (if available)

**Implementation Specification**:

```rust
// File: agent/src/metal/cpu.rs
// Complete CPU collector with Linux /proc and /sys parsing

use std::collections::HashMap;
use std::path::Path;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Mutex;
use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use tracing::{debug, warn, instrument};

/// CPU metrics snapshot collected from the host
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CpuSnapshot {
    /// Total CPU usage percentage (0-100 per core, sum/ncores for total)
    pub total_usage_pct: f64,
    /// Per-core usage percentages
    pub per_core_pct: Vec<f64>,
    /// Load averages (1m, 5m, 15m)
    pub load_avg_1m: f64,
    pub load_avg_5m: f64,
    pub load_avg_15m: f64,
    /// Current frequency in MHz (per-core if available)
    pub frequency_mhz: f64,
    /// Context switches since boot
    pub context_switches: u64,
    /// Number of physical cores
    pub physical_cores: u32,
    /// Number of logical cores (including hyperthreading)
    pub logical_cores: u32,
    /// CPU model name
    pub model_name: String,
    /// Collection timestamp
    pub timestamp: chrono::DateTime<chrono::Utc>,
}

/// Internal state for delta calculation
struct CpuState {
    prev_total: u64,
    prev_idle: u64,
    prev_per_core: Vec<(u64, u64)>, // (total, idle) per core
}

pub struct CpuCollector {
    state: Mutex<CpuState>,
    config: crate::config::CpuConfig,
}

impl CpuCollector {
    pub fn new(config: crate::config::CpuConfig) -> Self {
        Self {
            state: Mutex::new(CpuState {
                prev_total: 0,
                prev_idle: 0,
                prev_per_core: Vec::new(),
            }),
            config,
        }
    }

    /// Collect CPU metrics. Called on each collection interval.
    /// Returns CpuSnapshot with all CPU-related metrics.
    #[instrument(skip(self))]
    pub fn collect(&self) -> Result<CpuSnapshot> {
        // Step 1: Parse /proc/stat for CPU usage
        let stat = self.parse_proc_stat()?;

        // Step 2: Parse /proc/loadavg for load averages
        let (load_1m, load_5m, load_15m) = self.parse_loadavg()?;

        // Step 3: Get CPU frequency
        let freq = self.get_cpu_frequency();

        // Step 4: Get context switches from /proc/stat
        let ctxt = self.get_context_switches();

        // Step 5: Get CPU topology
        let (physical, logical, model) = self.get_cpu_topology();

        Ok(CpuSnapshot {
            total_usage_pct: stat.total_pct,
            per_core_pct: stat.per_core_pct,
            load_avg_1m: load_1m,
            load_avg_5m: load_5m,
            load_avg_15m: load_15m,
            frequency_mhz: freq,
            context_switches: ctxt,
            physical_cores: physical,
            logical_cores: logical,
            model_name: model,
            timestamp: chrono::Utc::now(),
        })
    }

    /// Parse /proc/stat to get CPU usage percentages
    /// Uses delta calculation: (current - previous) for accurate per-interval usage
    fn parse_proc_stat(&self) -> Result<CpuStatResult> {
        let content = std::fs::read_to_string("/proc/stat")
            .context("Failed to read /proc/stat")?;

        let mut total_pct = 0.0;
        let mut per_core_pct = Vec::new();

        // Parse aggregate CPU line
        // Format: cpu  user nice system idle iowait irq softirq steal
        for line in content.lines() {
            if line.starts_with("cpu ") || line.starts_with("cpu") {
                let fields: Vec<u64> = line.split_whitespace()
                    .skip(1) // Skip "cpu" or "cpuN"
                    .filter_map(|s| s.parse().ok())
                    .collect();

                if fields.len() < 4 { continue; }

                let idle = fields[3] + fields.get(4).unwrap_or(&0); // idle + iowait
                let total: u64 = fields.iter().sum();

                if line.starts_with("cpu ") {
                    // Aggregate CPU
                    let mut state = self.state.lock().unwrap();
                    if state.prev_total > 0 {
                        let delta_total = total - state.prev_total;
                        let delta_idle = idle - state.prev_idle;
                        if delta_total > 0 {
                            total_pct = (1.0 - (delta_idle as f64 / delta_total as f64)) * 100.0;
                        }
                    }
                    state.prev_total = total;
                    state.prev_idle = idle;
                } else if self.config.per_core {
                    // Per-core CPU
                    let mut state = self.state.lock().unwrap();
                    let core_idx = line[3..].split_whitespace().next()
                        .and_then(|s| s.parse::<usize>().ok())
                        .unwrap_or(0);

                    while state.prev_per_core.len() <= core_idx {
                        state.prev_per_core.push((0, 0));
                    }

                    let (prev_total, prev_idle) = state.prev_per_core[core_idx];
                    if prev_total > 0 {
                        let delta_total = total - prev_total;
                        let delta_idle = idle - prev_idle;
                        if delta_total > 0 {
                            per_core_pct.push((1.0 - (delta_idle as f64 / delta_total as f64)) * 100.0);
                        }
                    }
                    state.prev_per_core[core_idx] = (total, idle);
                }
            }
        }

        Ok(CpuStatResult { total_pct, per_core_pct })
    }

    /// Parse /proc/loadavg for load averages
    fn parse_loadavg(&self) -> Result<(f64, f64, f64)> {
        let content = std::fs::read_to_string("/proc/loadavg")
            .context("Failed to read /proc/loadavg")?;

        let parts: Vec<&str> = content.split_whitespace().collect();
        if parts.len() < 3 {
            anyhow::bail!("Invalid /proc/loadavg format");
        }

        let load_1m: f64 = parts[0].parse().context("parse load_1m")?;
        let load_5m: f64 = parts[1].parse().context("parse load_5m")?;
        let load_15m: f64 = parts[2].parse().context("parse load_15m")?;

        Ok((load_1m, load_5m, load_15m))
    }

    /// Get CPU frequency from /sys (Linux) or sysinfo (fallback)
    fn get_cpu_frequency(&self) -> f64 {
        // Try /sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq first
        if let Ok(content) = std::fs::read_to_string("/sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq") {
            if let Ok(khz) = content.trim().parse::<f64>() {
                return khz / 1000.0; // Convert kHz to MHz
            }
        }

        // Try /proc/cpuinfo
        if let Ok(content) = std::fs::read_to_string("/proc/cpuinfo") {
            for line in content.lines() {
                if line.starts_with("cpu MHz") {
                    if let Some(val) = line.split(':').nth(1) {
                        if let Ok(mhz) = val.trim().parse::<f64>() {
                            return mhz;
                        }
                    }
                }
            }
        }

        0.0 // Unknown
    }

    /// Get context switches from /proc/stat
    fn get_context_switches(&self) -> u64 {
        if let Ok(content) = std::fs::read_to_string("/proc/stat") {
            for line in content.lines() {
                if line.starts_with("ctxt") {
                    if let Some(val) = line.split_whitespace().nth(1) {
                        return val.parse().unwrap_or(0);
                    }
                }
            }
        }
        0
    }

    /// Get CPU topology from /proc/cpuinfo and /sys
    fn get_cpu_topology(&self) -> (u32, u32, String) {
        let logical = std::fs::read_dir("/sys/devices/system/cpu")
            .map(|entries| {
                entries.filter_map(|e| e.ok())
                    .filter(|e| e.file_name().to_string_lossy().starts_with("cpu"))
                    .filter(|e| {
                        e.file_name().to_string_lossy()[3..]
                            .chars().all(|c| c.is_ascii_digit())
                    })
                    .count() as u32
            })
            .unwrap_or(num_cpus::get() as u32);

        // Parse model name from /proc/cpuinfo
        let mut model = String::new();
        if let Ok(content) = std::fs::read_to_string("/proc/cpuinfo") {
            for line in content.lines() {
                if line.starts_with("model name") {
                    if let Some(val) = line.split(':').nth(1) {
                        model = val.trim().to_string();
                        break;
                    }
                }
            }
        }

        // Physical cores (approximate: logical / 2 if hyperthreading)
        let physical = logical / 2; // Simplified; real impl should parse siblings

        (physical, logical, model)
    }
}

struct CpuStatResult {
    total_pct: f64,
    per_core_pct: Vec<f64>,
}
```

**Coding Discipline (Rust-specific)**:
- `unwrap()` is ONLY allowed inside `Mutex::lock()` (which cannot poison in practice)
- All file I/O MUST use `anyhow::context()` for error messages
- All functions that do I/O MUST be `#[instrument(skip(self))]` for tracing
- NO `unsafe` blocks in metal scrapers
- NO `println!` — use `tracing::{info, warn, debug, error}`
- All `Vec` allocations in collectors MUST have capacity hints where size is known
- Use `&str` over `String` in hot paths where possible

**Test Specification**:
```rust
// File: agent/src/metal/cpu.rs (test module at bottom)

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_proc_stat_format() {
        // Test that parser handles standard /proc/stat format
        let content = "cpu  12345 0 67890 123456 789 0 123 0 0 0\ncpu0 12345 0 67890 123456 789 0 123 0 0 0\n";
        // Verify parsing doesn't panic and returns reasonable values
    }

    #[test]
    fn test_cpu_usage_percentage_range() {
        // Usage must be 0-100% per core
        let collector = CpuCollector::new(CpuConfig::default());
        // First call sets baseline (returns 0)
        let snap1 = collector.collect().unwrap();
        // Second call returns delta
        let snap2 = collector.collect().unwrap();
        assert!(snap2.total_usage_pct >= 0.0 && snap2.total_usage_pct <= 100.0);
    }

    #[test]
    fn test_loadavg_parsing() {
        let collector = CpuCollector::new(CpuConfig::default());
        let (l1, l5, l15) = collector.parse_loadavg().unwrap();
        assert!(l1 >= 0.0);
        assert!(l5 >= 0.0);
        assert!(l15 >= 0.0);
    }

    #[test]
    fn test_frequency_not_zero_on_real_hardware() {
        let freq = CpuCollector::new(CpuConfig::default()).get_cpu_frequency();
        // On most systems, frequency should be > 0
        // This test documents when frequency is unavailable
        if freq == 0.0 {
            eprintln!("WARNING: CPU frequency unavailable on this system");
        }
    }
}
```

### 2.3 Memory Collector (`agent/src/metal/memory.rs`)

**Current State**: Working `/proc/meminfo` parser with HashMap-based field extraction.

**What's Missing**:
1. Per-process memory from `/proc/[pid]/status` (VmRSS, VmSize, VmSwap)
2. Memory pressure from `/proc/pressure/memory` (if available, Linux 4.20+)
3. NUMA awareness from `/sys/devices/system/node/node*/meminfo`
4. Hugepages info from `/proc/meminfo` (HugePages_Total, HugePages_Free)

**Implementation Specification**:

```rust
// File: agent/src/metal/memory.rs

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MemorySnapshot {
    pub total_bytes: u64,
    pub used_bytes: u64,
    pub free_bytes: u64,
    pub available_bytes: u64,
    pub cached_bytes: u64,
    pub buffer_bytes: u64,
    pub swap_total_bytes: u64,
    pub swap_used_bytes: u64,
    pub usage_pct: f64,
    /// Memory pressure (some/full avg10, avg60, avg300) - Linux 4.20+
    pub pressure: Option<MemoryPressure>,
    /// Per-process top memory consumers (top N by RSS)
    pub top_processes: Vec<ProcessMemory>,
    pub timestamp: chrono::DateTime<chrono::Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MemoryPressure {
    pub some_avg10: f64,
    pub some_avg60: f64,
    pub some_avg300: f64,
    pub full_avg10: f64,
    pub full_avg60: f64,
    pub full_avg300: f64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProcessMemory {
    pub pid: u32,
    pub name: String,
    pub rss_bytes: u64,
    pub vsz_bytes: u64,
    pub swap_bytes: u64,
}

impl MemoryCollector {
    pub fn new(config: crate::config::MemoryConfig) -> Self { ... }

    pub fn collect(&self) -> Result<MemorySnapshot> {
        let meminfo = self.parse_meminfo()?;
        let pressure = self.read_pressure().ok(); // Optional, not all kernels
        let top_procs = if self.config.per_process {
            self.get_top_process_memory(self.config.top_n_processes)
        } else {
            Vec::new()
        };

        Ok(MemorySnapshot {
            total_bytes: meminfo.get("MemTotal").copied().unwrap_or(0),
            used_bytes: /* calculated */,
            // ... all fields
            pressure,
            top_processes: top_procs,
            timestamp: chrono::Utc::now(),
        })
    }

    fn parse_meminfo(&self) -> Result<HashMap<String, u64>> {
        // Parse /proc/meminfo: "MemTotal:       16384000 kB"
        let content = std::fs::read_to_string("/proc/meminfo")
            .context("read /proc/meminfo")?;
        let mut map = HashMap::new();
        for line in content.lines() {
            let parts: Vec<&str> = line.split(':').collect();
            if parts.len() == 2 {
                let key = parts[0].trim();
                let val_str = parts[1].trim().split_whitespace().next().unwrap_or("0");
                if let Ok(val) = val_str.parse::<u64>() {
                    map.insert(key.to_string(), val * 1024); // kB to bytes
                }
            }
        }
        Ok(map)
    }

    fn read_pressure(&self) -> Result<MemoryPressure> {
        // Parse /proc/pressure/memory
        // Format: "some avg10=0.00 avg60=0.00 avg300=0.00 total=0"
        //         "full avg10=0.00 avg60=0.00 avg300=0.00 total=0"
        let content = std::fs::read_to_string("/proc/pressure/memory")
            .context("read /proc/pressure/memory")?;
        // Parse each line...
        // Return MemoryPressure
    }

    fn get_top_process_memory(&self, n: usize) -> Vec<ProcessMemory> {
        // Iterate /proc/[pid]/status
        // Extract VmRSS, VmSize, VmSwap, Name
        // Sort by RSS descending, take top N
        // BOUND: Only scan first 1000 PIDs to avoid excessive I/O
    }
}
```

### 2.4 Disk Collector (`agent/src/metal/disk.rs`)

**Current State**: Working `/proc/diskstats` parser with rate calculation.

**What's Missing**:
1. Queue depth from `/sys/block/{device}/queue/nr_requests`
2. Disk type detection (SSD vs HDD) from `/sys/block/{device}/queue/rotational`
3. IO latency histogram from `/proc/diskstats` (field 10: io_ticks)
4. Filesystem usage from `statvfs` (total/used/free bytes per mount)
5. Mount point mapping from `/proc/mounts`

**Implementation Specification**:

```rust
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DiskSnapshot {
    pub devices: Vec<DiskDevice>,
    pub timestamp: chrono::DateTime<chrono::Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DiskDevice {
    pub device_name: String,        // "sda", "nvme0n1"
    pub mount_point: String,        // "/", "/data"
    pub filesystem_type: String,     // "ext4", "xfs"
    pub total_bytes: u64,
    pub used_bytes: u64,
    pub free_bytes: u64,
    pub read_bytes_per_sec: f64,
    pub write_bytes_per_sec: f64,
    pub read_ops_per_sec: f64,
    pub write_ops_per_sec: f64,
    pub io_latency_ms: f64,         // Average IO wait time
    pub queue_depth: f64,           // Current queue depth
    pub is_ssd: bool,               // rotational=0 → SSD
    pub utilization_pct: f64,       // io_ticks delta / time delta
}

impl DiskCollector {
    pub fn collect(&self) -> Result<DiskSnapshot> {
        // Step 1: Read /proc/diskstats for I/O counters
        let diskstats = self.parse_diskstats()?;

        // Step 2: Read /proc/mounts for mount points
        let mounts = self.parse_mounts()?;

        // Step 3: For each mounted filesystem, get usage via statvfs
        // Step 4: Detect SSD/HDD from /sys/block/{dev}/queue/rotational
        // Step 5: Calculate rates from previous snapshot (delta / interval)
        // Step 6: Filter out loop devices, ramfs, etc.
    }
}
```

### 2.5 Network Collector (`agent/src/metal/network.rs`)

**Current State**: Working `/proc/net/dev` parser with rate calculation. Has `tcp_retransmits: 0` and `estimated_rtt_ms: 0.0` as TODOs.

**What's Missing**:
1. TCP retransmits from `/proc/net/snmp` (Tcp: RetransSegs field)
2. RTT estimation from `/proc/net/tcp` (rto field, or use ss-like parsing)
3. Connection states from `/proc/net/tcp` and `/proc/net/tcp6`
4. Bandwidth utilization (bytes/sec vs interface speed from `/sys/class/net/{iface}/speed`)

**Implementation Specification**:

```rust
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NetworkSnapshot {
    pub interfaces: Vec<NetworkInterface>,
    pub tcp_stats: TcpStats,
    pub timestamp: chrono::DateTime<chrono::Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NetworkInterface {
    pub name: String,               // "eth0", "lo"
    pub rx_bytes_per_sec: f64,
    pub tx_bytes_per_sec: f64,
    pub rx_packets_per_sec: f64,
    pub tx_packets_per_sec: f64,
    pub rx_errors: u64,
    pub tx_errors: u64,
    pub rx_dropped: u64,
    pub tx_dropped: u64,
    pub speed_mbps: u64,            // Link speed from /sys/class/net/{name}/speed
    pub is_up: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TcpStats {
    pub retransmits: u64,           // From /proc/net/snmp
    pub active_connections: u32,    // ESTABLISHED count
    pub time_wait: u32,             // TIME_WAIT count
    pub listen: u32,                // LISTEN count
}

impl NetworkCollector {
    pub fn collect(&self) -> Result<NetworkSnapshot> {
        // Step 1: Parse /proc/net/dev for interface counters
        // Step 2: Calculate rates from delta
        // Step 3: Parse /proc/net/snmp for TCP retransmits
        // Step 4: Parse /proc/net/tcp for connection states
        // Step 5: Get link speed from /sys/class/net/{name}/speed
        // Step 6: Filter loopback unless configured to include
    }

    fn parse_tcp_snmp(&self) -> Result<TcpStats> {
        // /proc/net/snmp format:
        // Tcp: RtoAlgorithm RtoMin RtoMax MaxConn ActiveOpens PassiveOpens AttemptFails EstabResets CurrEstab InSegs OutSegs RetransSegs InErrs OutRsts
        // Tcp: 1 200 120000 -1 12345 6789 123 45 67 89012 34567 89 0 123
        // We need: RetransSegs (field 12), CurrEstab (field 9)
    }
}
```

### 2.6 Process Collector (`agent/src/metal/process.rs`)

**Current State**: Working `/proc/[pid]/stat` parser. Has `cpu_usage_percent: 0.0` as TODO.

**What's Missing**:
1. CPU usage calculation (delta of utime+stime between samples)
2. Container ID from `/proc/[pid]/cgroup`
3. Start time from `/proc/[pid]/stat` field 22 (starttime in clock ticks)
4. FD count from `/proc/[pid]/fd` directory entry count
5. Command line from `/proc/[pid]/cmdline`

**Implementation Specification**:

```rust
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProcessInfo {
    pub pid: u32,
    pub parent_pid: u32,
    pub name: String,
    pub command_line: String,
    pub cpu_usage_pct: f64,        // Calculated from utime+stime delta
    pub rss_bytes: u64,
    pub vsz_bytes: u64,
    pub status: String,            // "R", "S", "D", "Z", "T"
    pub thread_count: u32,
    pub fd_count: u32,
    pub container_id: String,      // From cgroup, empty if not in container
    pub started_at: chrono::DateTime<chrono::Utc>,
}

pub struct ProcessCollector {
    prev_cpu_times: Mutex<HashMap<u32, (u64, u64)>>, // pid → (utime, stime)
    prev_sample_time: Mutex<Option<Instant>>,
    config: ProcessConfig,
}

impl ProcessCollector {
    pub fn collect(&self) -> Result<Vec<ProcessInfo>> {
        let now = Instant::now();
        let prev_times = self.prev_cpu_times.lock().unwrap();
        let prev_time = self.prev_sample_time.lock().unwrap();

        let mut processes = Vec::with_capacity(256);

        // Iterate /proc/[pid]/
        for entry in std::fs::read_dir("/proc")? {
            let entry = entry?;
            let name = entry.file_name().to_string_lossy().to_string();
            let pid: u32 = match name.parse() {
                Ok(p) => p,
                Err(_) => continue, // Skip non-numeric entries
            };

            // Parse /proc/[pid]/stat
            let stat = self.parse_pid_stat(pid)?;

            // Calculate CPU usage from delta
            let cpu_pct = if let (Some(prev), Some(prev_t)) = (prev_times.get(&pid), *prev_time) {
                let delta_utime = stat.utime - prev.0;
                let delta_stime = stat.stime - prev.1;
                let delta_time = now.duration_since(prev_t).as_secs_f64();
                if delta_time > 0.0 {
                    ((delta_utime + delta_stime) as f64 / delta_time) * 100.0
                } else {
                    0.0
                }
            } else {
                0.0
            };

            // Get FD count (count entries in /proc/[pid]/fd)
            let fd_count = std::fs::read_dir(format!("/proc/{}/fd", pid))
                .map(|d| d.count() as u32)
                .unwrap_or(0);

            // Get container ID from cgroup
            let container_id = self.get_container_id(pid);

            // Get command line
            let cmdline = std::fs::read_to_string(format!("/proc/{}/cmdline", pid))
                .map(|s| s.replace('\0', " ").trim().to_string())
                .unwrap_or_default();

            processes.push(ProcessInfo {
                pid,
                parent_pid: stat.ppid,
                name: stat.comm,
                command_line: cmdline,
                cpu_usage_pct: cpu_pct,
                rss_bytes: stat.rss * 4096, // pages to bytes
                vsz_bytes: stat.vsize,
                status: stat.state,
                thread_count: stat.num_threads,
                fd_count,
                container_id,
                started_at: self.parse_start_time(stat.starttime),
            });
        }

        // Update previous state for next delta calculation
        // ... update prev_cpu_times and prev_sample_time

        // Sort by CPU usage descending, limit to configured max
        processes.sort_by(|a, b| b.cpu_usage_pct.partial_cmp(&a.cpu_usage_pct).unwrap());
        processes.truncate(self.config.max_processes);

        Ok(processes)
    }
}
```

### 2.7 Container Collector (`agent/src/metal/container.rs`)

**Current State**: Working cgroup v1/v2 parser. Basic container detection.

**What's Missing**:
1. Image name from container runtime API or cgroup labels
2. Container resource limits (memory limit, CPU quota)
3. Container status (running, paused, stopped)
4. Runtime-specific metadata (Docker labels, Podman annotations)

### 2.8 Batch Orchestrator (`agent/src/metal/batch.rs`)

**Current State**: Basic collect-and-send loop.

**What's Missing**:
1. Error isolation per collector (one failing collector shouldn't stop others)
2. Collection timing metrics (how long each collector takes)
3. Conditional collection based on config (skip disabled collectors)
4. Proper retry on send failure with edge buffer fallback

### 2.9 Metal Module Root (`agent/src/metal/mod.rs`)

**Target**: Define collector trait and registry.

```rust
// File: agent/src/metal/mod.rs

pub mod cpu;
pub mod memory;
pub mod disk;
pub mod network;
pub mod process;
pub mod container;
pub mod batch;

/// Trait that all metal collectors must implement
pub trait Collector: Send + Sync {
    /// Human-readable name for logging
    fn name(&self) -> &str;

    /// Collect metrics. Returns serialized JSON or error.
    /// Each collector is isolated — a failure in one MUST NOT affect others.
    fn collect_json(&self) -> Result<serde_json::Value>;

    /// Whether this collector is enabled in config
    fn is_enabled(&self) -> bool;
}

/// Registry of all metal collectors
pub struct CollectorRegistry {
    collectors: Vec<Box<dyn Collector>>,
}

impl CollectorRegistry {
    pub fn from_config(config: &crate::config::MetalConfig) -> Self {
        let mut collectors: Vec<Box<dyn Collector>> = Vec::new();

        if config.cpu.enabled {
            collectors.push(Box::new(cpu::CpuCollector::new(config.cpu.clone())));
        }
        if config.memory.enabled {
            collectors.push(Box::new(memory::MemoryCollector::new(config.memory.clone())));
        }
        // ... disk, network, process, container

        Self { collectors }
    }

    /// Collect all enabled metrics, isolating failures per collector
    pub fn collect_all(&self) -> MetalBatch {
        let mut batch = MetalBatch::default();

        for collector in &self.collectors {
            match collector.collect_json() {
                Ok(json) => batch.add(collector.name(), json),
                Err(e) => {
                    tracing::error!(
                        collector = collector.name(),
                        error = %e,
                        "Collector failed"
                    );
                    batch.add_error(collector.name(), e.to_string());
                }
            }
        }

        batch
    }
}
```

---

## 3. Layer 2: Communication Layer Hardening (Rust)

**Target**: ~3,000 LOC across 6 files
**Agent**: `paryty-comm-layer`
**Oracle**: `oracle-rust`

### 3.1 File Inventory

| File | Current LOC | Target LOC | Status |
|------|------------|------------|--------|
| `agent/src/communication/mod.rs` | 266 | 500 | Partial — needs full Client lifecycle |
| `agent/src/communication/grpc_client.rs` | 176 | 400 | Stub — endpoint not wired from config |
| `agent/src/communication/compression.rs` | 161 | 250 | Good — needs Zstd streaming support |
| `agent/src/communication/edge_buffer.rs` | 293 | 500 | In-memory only — needs SQLite persistence |
| `agent/src/communication/reconnect.rs` | 168 | 250 | Good — needs backoff config from YAML |
| `agent/src/communication/flow_control.rs` | 218 | 350 | Good — needs server-driven flow control |

### 3.2 gRPC Client Hardening (`grpc_client.rs`)

**Current State**: Has `ConnectionState` enum and basic `connect()` with tonic. Endpoint is `String::new()` — not wired.

**Required Changes**:

1. **Wire endpoint from config**: Read `cluster_endpoint` from `AgentConfig`
2. **Implement proper TLS**: Use `tonic::transport::ClientTlsConfig`
3. **Add compression**: Enable `tonic::codec::CompressionEncoding::Gzip` (internal gRPC compression, separate from payload compression)
4. **Implement reconnection**: On disconnect, trigger `ReconnectionEngine`
5. **Wire proto-generated client**: Use `IngestionServiceClient<Channel>`
6. **Implement StreamMetrics**: Bidirectional streaming with automatic reconnect

```rust
// File: agent/src/communication/grpc_client.rs

use tonic::transport::{Channel, ClientTlsConfig, Endpoint};
use crate::proto::paryty::v1::ingestion_client::IngestionServiceClient;

pub struct GrpcClient {
    client: Option<IngestionServiceClient<Channel>>,
    config: CommunicationConfig,
    state: Arc<Mutex<ConnectionState>>,
}

impl GrpcClient {
    pub async fn connect(&mut self) -> Result<()> {
        let endpoint = Endpoint::from_shared(self.config.cluster_endpoint.clone())
            .context("invalid endpoint URI")?
            .timeout(Duration::from_secs(30))
            .tcp_keepalive(Some(Duration::from_secs(30)))
            .http2_keep_alive_interval(Duration::from_secs(10))
            .keep_alive_timeout(Duration::from_secs(10));

        // Apply TLS if configured
        let endpoint = if self.config.tls_enabled {
            endpoint.tls_config(ClientTlsConfig::new())?
        } else {
            endpoint
        };

        let channel = endpoint.connect().await
            .context("connect to cluster")?;

        let client = IngestionServiceClient::new(channel)
            .send_compressed(tonic::codec::CompressionEncoding::Gzip)
            .accept_compressed(tonic::codec::CompressionEncoding::Gzip);

        self.client = Some(client);
        *self.state.lock().unwrap() = ConnectionState::Connected;

        Ok(())
    }

    pub async fn send_batch(&mut self, batch: MetricBatch) -> Result<SendBatchResponse> {
        let client = self.client.as_mut()
            .context("not connected")?;

        let response = client.send_batch(batch).await
            .context("send batch")?;

        Ok(response.into_inner())
    }

    pub async fn register_agent(&mut self, registration: AgentRegistration) -> Result<AgentRegistrationResponse> {
        let client = self.client.as_mut()
            .context("not connected")?;

        let response = client.register_agent(registration).await
            .context("register agent")?;

        Ok(response.into_inner())
    }

    pub async fn heartbeat(&mut self, request: HeartbeatRequest) -> Result<HeartbeatResponse> {
        let client = self.client.as_mut()
            .context("not connected")?;

        let response = client.heartbeat(request).await
            .context("heartbeat")?;

        Ok(response.into_inner())
    }
}
```

### 3.3 SQLite Edge Buffer (`edge_buffer.rs`)

**Current State**: In-memory `VecDeque` ring buffer with TTL eviction. Has disk spillover config but implementation is in-memory only.

**Decision**: SQLite for disk persistence (Decision 2A).

**Implementation Specification**:

```rust
// File: agent/src/communication/edge_buffer.rs

use rusqlite::{Connection, params};
use std::path::PathBuf;

pub struct EdgeBuffer {
    memory: VecDeque<BufferEntry>,
    db: Option<Connection>,  // SQLite for disk spillover
    config: EdgeBufferConfig,
    stats: BufferStats,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BufferEntry {
    pub id: i64,
    pub topic: String,         // "metal", "ebpf", "supervisor"
    pub data: Vec<u8>,         // Compressed payload
    pub created_at: Instant,
    pub priority: Priority,
    pub retry_count: u32,
}

impl EdgeBuffer {
    pub fn new(config: EdgeBufferConfig) -> Result<Self> {
        let db = if config.disk_spillover_enabled {
            let db_path = config.disk_path.clone()
                .unwrap_or_else(|| PathBuf::from("/var/lib/paryty/agent/buffer.db"));
            let conn = Self::init_sqlite(&db_path)?;
            Some(conn)
        } else {
            None
        };

        Ok(Self {
            memory: VecDeque::with_capacity(config.max_memory_entries),
            db,
            config,
            stats: BufferStats::default(),
        })
    }

    fn init_sqlite(path: &PathBuf) -> Result<Connection> {
        // Ensure parent directory exists
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent)?;
        }

        let conn = Connection::open(path)?;

        // WAL mode for concurrent read/write
        conn.execute_batch("
            PRAGMA journal_mode=WAL;
            PRAGMA synchronous=NORMAL;
            PRAGMA cache_size=1000;
            PRAGMA temp_store=MEMORY;
        ")?;

        // Create table if not exists
        conn.execute("
            CREATE TABLE IF NOT EXISTS buffer (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                topic TEXT NOT NULL,
                data BLOB NOT NULL,
                priority INTEGER NOT NULL DEFAULT 2,
                retry_count INTEGER NOT NULL DEFAULT 0,
                created_at INTEGER NOT NULL,
                sent_at INTEGER
            )
        ", [])?;

        // Index for fast retrieval
        conn.execute("
            CREATE INDEX IF NOT EXISTS idx_buffer_unsent
            ON buffer(priority ASC, created_at ASC)
            WHERE sent_at IS NULL
        ", [])?;

        Ok(conn)
    }

    /// Push entry to buffer. If memory is full, spill to SQLite.
    pub fn push(&mut self, topic: &str, data: Vec<u8>, priority: Priority) -> Result<()> {
        if self.memory.len() < self.config.max_memory_entries {
            // Keep in memory
            self.memory.push_back(BufferEntry {
                id: 0,
                topic: topic.to_string(),
                data,
                created_at: Instant::now(),
                priority,
                retry_count: 0,
            });
        } else if let Some(ref db) = self.db {
            // Spill to disk
            db.execute(
                "INSERT INTO buffer (topic, data, priority, created_at) VALUES (?1, ?2, ?3, ?4)",
                params![topic, data, priority as u32, chrono::Utc::now().timestamp()],
            )?;
        }
        // If no disk and memory full, drop oldest (ring buffer behavior)
        self.stats.total_pushed += 1;
        Ok(())
    }

    /// Drain buffer: memory first (priority-sorted), then disk
    pub fn drain(&mut self, max_entries: usize) -> Vec<BufferEntry> {
        let mut entries = Vec::with_capacity(max_entries);

        // Drain memory first
        while entries.len() < max_entries {
            match self.memory.pop_front() {
                Some(entry) => entries.push(entry),
                None => break,
            }
        }

        // Drain disk if needed
        if entries.len() < max_entries {
            if let Some(ref db) = self.db {
                let remaining = max_entries - entries.len();
                // SELECT from disk buffer, ordered by priority, limit remaining
                // Mark as sent in DB
            }
        }

        entries
    }

    /// Evict expired entries
    pub fn evict_expired(&mut self) {
        let cutoff = Instant::now() - Duration::from_secs(self.config.ttl_hours * 3600);
        // Remove from memory
        self.memory.retain(|e| e.created_at > cutoff);
        // Remove from disk
        if let Some(ref db) = self.db {
            let cutoff_ts = chrono::Utc::now().timestamp() - (self.config.ttl_hours as i64 * 3600);
            db.execute("DELETE FROM buffer WHERE created_at < ?1", params![cutoff_ts]).ok();
        }
    }
}
```

### 3.4 Client Lifecycle (`mod.rs`)

**Required Changes**:
1. Wire `GrpcClient` with actual proto-generated client
2. Implement proper `send_metrics` flow: compress → buffer → send
3. Implement `start_reconnection_loop` that drains buffer on reconnect
4. Add heartbeat loop (separate tokio task)
5. Add shutdown signal handling

```rust
// File: agent/src/communication/mod.rs

pub struct Client {
    grpc: GrpcClient,
    reconnect: ReconnectionEngine,
    buffer: EdgeBuffer,
    compressor: Compressor,
    flow_control: FlowControl,
    config: CommunicationConfig,
    shutdown: tokio::sync::watch::Sender<bool>,
}

impl Client {
    pub async fn new(config: CommunicationConfig) -> Result<Self> {
        let grpc = GrpcClient::new(config.clone());
        let reconnect = ReconnectionEngine::new(config.reconnect.clone());
        let buffer = EdgeBuffer::new(config.edge_buffer.clone())?;
        let compressor = Compressor::new(config.compression.clone());
        let flow_control = FlowControl::new(config.flow_control.clone());
        let (shutdown, _) = tokio::sync::watch::channel(false);

        Ok(Self { grpc, reconnect, buffer, compressor, flow_control, config, shutdown })
    }

    /// Main entry: collect metrics, compress, buffer, send
    pub async fn send_metrics(&self, topic: &str, data: &[u8]) -> Result<()> {
        // Step 1: Compress with Zstd
        let compressed = self.compressor.compress(data)?;

        // Step 2: Check flow control
        if self.flow_control.should_drop(topic) {
            tracing::debug!(topic, "Dropped due to flow control");
            return Ok(());
        }

        // Step 3: Buffer (memory → SQLite spillover)
        let priority = self.flow_control.priority_for(topic);
        self.buffer.push(topic, compressed, priority)?;

        // Step 4: Try to send immediately
        if self.grpc.is_connected() {
            self.flush_buffer().await?;
        }

        Ok(())
    }

    /// Drain buffer and send all entries
    async fn flush_buffer(&self) -> Result<()> {
        loop {
            let entries = self.buffer.drain(100);
            if entries.is_empty() { break; }

            for entry in entries {
                match self.grpc.send_compressed(entry.topic, entry.data).await {
                    Ok(_) => self.buffer.mark_sent(entry.id),
                    Err(e) => {
                        self.buffer.mark_retry(entry.id);
                        tracing::warn!(error = %e, "Failed to send, will retry");
                        return Err(e);
                    }
                }
            }
        }
        Ok(())
    }

    /// Background task: heartbeat every 30s
    pub fn start_heartbeat_loop(&self) {
        let client = self.clone();
        tokio::spawn(async move {
            let mut interval = tokio::time::interval(Duration::from_secs(30));
            loop {
                interval.tick().await;
                if let Err(e) = client.grpc.heartbeat(HeartbeatRequest {
                    agent_id: client.config.agent_id.clone(),
                    // ... other fields
                }).await {
                    tracing::warn!(error = %e, "Heartbeat failed");
                }
            }
        });
    }

    /// Background task: reconnect on disconnect
    pub fn start_reconnection_loop(&self) {
        let client = self.clone();
        tokio::spawn(async move {
            loop {
                if !client.grpc.is_connected() {
                    tracing::info!("Attempting reconnection...");
                    match client.reconnect.attempt().await {
                        Ok(()) => {
                            client.reconnect.reset();
                            // Drain buffer after reconnect
                            let _ = client.flush_buffer().await;
                        }
                        Err(e) => {
                            tracing::warn!(error = %e, "Reconnect failed");
                        }
                    }
                }
                tokio::time::sleep(Duration::from_secs(5)).await;
            }
        });
    }
}
```

### 3.5 Compression (`compression.rs`)

**Current State**: Good implementation with Zstd level 3 for metrics, Snappy for traces.

**Required Changes**:
1. Decision 6A: Remove Snappy, use Zstd for ALL compression
2. Add streaming compression for large payloads
3. Add compression ratio tracking for self-metrics

### 3.6 Flow Control (`flow_control.rs`)

**Current State**: Credit-based backpressure with priority queues. Well-implemented.

**Required Changes**:
1. Wire server-driven flow control from `ClusterToAgent.FlowControl` messages
2. Apply server-specified `sampling_rate` and `desired_interval_ms`
3. Handle `pause` signal from server

---

## 4. Layer 3: Ingestion Service (Go)

**Target**: ~4,000 LOC across 5 files
**Agent**: `paryty-ingestion`
**Oracle**: `oracle-go`

### 4.1 File Inventory

| File | Current LOC | Target LOC | Status |
|------|------------|------------|--------|
| `cluster/cmd/ingestion/main.go` | 139 | 300 | Good — needs config loading from YAML |
| `cluster/internal/api/ingestion/grpc_adapter.go` | 267 | 600 | Good — needs validation, tenant routing |
| `cluster/internal/api/ingestion/service.go` | 132 | 400 | Good — needs tenant isolation, error handling |
| `cluster/internal/api/ingestion/grpc_adapter_test.go` | 191 | 400 | Good — needs integration tests |
| `cluster/internal/stream/stream.go` | 85 | 200 | Good — needs health check integration |
| `cluster/internal/stream/producer.go` | 114 | 300 | Good — needs batch compression, idempotency |
| `cluster/internal/stream/consumer.go` | 117 | 300 | Good — needs dead letter queue support |
| `cluster/internal/stream/topics.go` | 104 | 200 | Good — needs tenant-scoped topics |

### 4.2 Config Loading (`main.go`)

**Current State**: Uses `getEnv()` for each config value. No YAML loading.

**Required Changes**:
1. Load `configs/cluster/cluster.yaml` using `gopkg.in/yaml.v3`
2. Support env overrides for all values (PARYTY_PORT, PARYTY_REDPANDA_URL, etc.)
3. Add validation on startup (fail fast if required values missing)

```go
// File: cluster/internal/config/config.go

package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Cluster ClusterConfig `yaml:"cluster"`
}

type ClusterConfig struct {
	Name       string           `yaml:"name"`
	Tenant     string           `yaml:"tenant"`
	Ingestion  IngestionConfig  `yaml:"ingestion"`
	Stream     StreamConfig     `yaml:"stream_engine"`
	Storage    StorageConfig    `yaml:"storage"`
}

type IngestionConfig struct {
	Port           int    `yaml:"port"`
	MaxConnections int    `yaml:"max_connections"`
	TLSEnabled     bool   `yaml:"tls"`
}

type StreamConfig struct {
	Type    string         `yaml:"type"`
	Topics  []TopicConfig  `yaml:"topics"`
}

type TopicConfig struct {
	Name       string `yaml:"name"`
	Partitions int    `yaml:"partitions"`
	Retention  string `yaml:"retention"`
}

type StorageConfig struct {
	Hot  HotStorageConfig  `yaml:"hot"`
	Warm WarmStorageConfig `yaml:"warm"`
	Cold ColdStorageConfig `yaml:"cold"`
}

type HotStorageConfig struct {
	Type     string `yaml:"type"`
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
	PoolSize int    `yaml:"pool_size"`
}

type WarmStorageConfig struct {
	Type     string `yaml:"type"`
	Addr     string `yaml:"addr"`
	Database string `yaml:"database"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	MaxConns int    `yaml:"max_conns"`
}

type ColdStorageConfig struct {
	Type     string `yaml:"type"`
	Endpoint string `yaml:"endpoint"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
}

// Load reads config from file with env overrides
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Apply env overrides
	applyEnvOverrides(&cfg)

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("PARYTY_PORT"); v != "" {
		// parse and set
	}
	if v := os.Getenv("PARYTY_REDPANDA_URL"); v != "" {
		// parse and set
	}
	// ... all env overrides
}

func (c *Config) Validate() error {
	if c.Cluster.Ingestion.Port == 0 {
		return fmt.Errorf("ingestion.port is required")
	}
	if len(c.Cluster.Stream.Topics) == 0 {
		return fmt.Errorf("at least one stream topic is required")
	}
	return nil
}
```

### 4.3 gRPC Adapter Hardening (`grpc_adapter.go`)

**Current State**: Working proto-to-domain conversion. Has `RegisterAgent`, `SendBatch`, `Heartbeat`, `StreamMetrics`, `ReportNetworkEvents`.

**Required Changes**:
1. Add input validation (reject empty agent_id, invalid timestamps)
2. Add tenant routing (extract tenant from API key or agent registration)
3. Add rate limiting per agent (configurable max batches/minute)
4. Add request logging with correlation ID
5. Add metrics (prometheus counters for accepted/rejected/errored)
6. Add graceful shutdown (drain in-flight requests)
7. Wire flow control from cluster config (not hardcoded 10000ms)

```go
// File: cluster/internal/api/ingestion/grpc_adapter.go

// Add validation to SendBatch
func (a *IngestionGRPCAdapter) SendBatch(ctx context.Context, req *pb.MetricBatch) (*pb.SendBatchResponse, error) {
	// Validate request
	if req.AgentId == "" {
		return &pb.SendBatchResponse{
			Accepted: false,
			Error: &pb.Error{Code: "INVALID_REQUEST", Message: "agent_id is required"},
		}, nil
	}

	if req.Timestamp == nil {
		return &pb.SendBatchResponse{
			Accepted: false,
			Error: &pb.Error{Code: "INVALID_REQUEST", Message: "timestamp is required"},
		}, nil
	}

	// Check rate limit
	if !a.rateLimiter.Allow(req.AgentId) {
		return &pb.SendBatchResponse{
			Accepted: false,
			Error: &pb.Error{Code: "RATE_LIMITED", Message: "too many requests"},
		}, nil
	}

	// Convert and process
	batch := metricBatchFromProto(req)
	if err := a.svc.SendBatch(ctx, req.AgentId, batch); err != nil {
		a.logger.Error("batch processing failed",
			zap.String("agent_id", req.AgentId),
			zap.Error(err),
		)
		return &pb.SendBatchResponse{
			Accepted: false,
			Error: &pb.Error{Code: "INGESTION_ERROR", Message: err.Error()},
		}, nil
	}

	return &pb.SendBatchResponse{
		Accepted:   true,
		BatchId:    fmt.Sprintf("batch-%d", time.Now().UnixNano()),
		ServerTime: timestamppb.Now(),
	}, nil
}
```

### 4.4 Ingestion Service Hardening (`service.go`)

**Current State**: Basic `RegisterAgent`, `SendBatch`, `Heartbeat` with in-memory `sync.Map` cache.

**Required Changes**:
1. Add tenant isolation (all operations scoped to tenant_id)
2. Add agent state persistence to hot store (currently only in-memory)
3. Add batch validation (reject malformed batches)
4. Add dead letter queue for failed batches
5. Add metrics collection (batches received, latency, errors)

```go
// File: cluster/internal/api/ingestion/service.go

// Add tenant context
type TenantContext struct {
	TenantID string
}

// SendBatch with tenant isolation and DLQ
func (s *IngestionService) SendBatch(ctx context.Context, agentID string, batch *models.MetricBatch) error {
	// Validate batch
	if batch == nil {
		return fmt.Errorf("nil batch")
	}

	// Update agent heartbeat
	if agent, ok := s.agents.Load(agentID); ok {
		info := agent.(*models.AgentInfo)
		info.LastHeartbeat = time.Now()
		info.Status = models.AgentStatusOnline
	}

	// Store in hot storage (async, non-blocking)
	go func() {
		if err := s.store.SetLatestMetrics(context.Background(), agentID, batch); err != nil {
			s.logger.Error("hot store write failed",
				zap.String("agent_id", agentID),
				zap.Error(err),
			)
		}
	}()

	// Publish to stream
	if err := s.stream.Producer().Publish(ctx, stream.TopicMetricsRaw, agentID, batch); err != nil {
		// Send to DLQ instead of failing
		s.logger.Error("stream publish failed, sending to DLQ",
			zap.String("agent_id", agentID),
			zap.Error(err),
		)
		s.stream.Producer().Publish(ctx, stream.TopicDLQ, agentID, batch)
		return fmt.Errorf("publish to stream: %w", err)
	}

	return nil
}
```

### 4.5 Stream Producer Hardening (`producer.go`)

**Current State**: Working franz-go producer with `Publish` and `PublishBatch`.

**Required Changes**:
1. Add idempotent producer (enable `kgo.IdempotentWrite()`)
2. Add compression (Zstd at producer level)
3. Add partition key strategy (tenant_id + agent_id)
4. Add delivery timeout
5. Add metrics (produced count, error count, latency)

```go
// File: cluster/internal/stream/producer.go

func NewProducer(cfg Config, logger *zap.Logger) (*Producer, error) {
	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(cfg.ClientID),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.ProducerLinger(5 * time.Millisecond),
		kgo.RecordPartitioner(kgo.StickyPartitioner()),
		// NEW: Idempotent producer
		kgo.IdempotentWrite(),
		// NEW: Compression
		kgo.ProducerBatchCompression(kgo.ZstdCompression()),
		// NEW: Delivery timeout
		kgo.ProduceRequestTimeout(30 * time.Second),
		// NEW: Max buffer
		kgo.MaxBufferedRecords(10000),
	}
	// ...
}
```

### 4.6 Stream Consumer Hardening (`consumer.go`)

**Current State**: Working franz-go consumer with `Start` loop.

**Required Changes**:
1. Add dead letter queue support (on handler error, publish to DLQ topic)
2. Add consumer lag monitoring
3. Add graceful shutdown with in-flight message draining
4. Add retry logic with exponential backoff per message

### 4.7 Tenant-Scoped Topics (`topics.go`)

**Current State**: Static topic names like `paryty.metrics.raw`.

**Decision 5A**: Full tenant isolation.

**Required Changes**:
1. Topic naming: `paryty.{tenant}.{type}.{subtype}`
2. Partition key: `{tenant_id}:{agent_id}`
3. Update `InitializeTopics` to create tenant-scoped topics
4. Update `TopicMetricsRaw` etc. to be functions: `TopicMetricsRaw(tenant string) string`

```go
// File: cluster/internal/stream/topics.go

// Topic name functions (tenant-scoped)
func TopicMetricsRaw(tenant string) string {
	return fmt.Sprintf("paryty.%s.metrics.raw", tenant)
}
func TopicMetricsAgg(tenant string) string {
	return fmt.Sprintf("paryty.%s.metrics.aggregated", tenant)
}
func TopicTraces(tenant string) string {
	return fmt.Sprintf("paryty.%s.traces", tenant)
}
func TopicEvents(tenant string) string {
	return fmt.Sprintf("paryty.%s.events", tenant)
}
func TopicDLQ(tenant string) string {
	return fmt.Sprintf("paryty.%s.dead-letter", tenant)
}

// InitializeTopics creates topics for a tenant
func (m *TopicManager) InitializeTopics(ctx context.Context, tenant string) error {
	requiredTopics := []struct {
		name        string
		partitions  int32
		replication int16
	}{
		{TopicMetricsRaw(tenant), 12, 1},
		{TopicMetricsAgg(tenant), 6, 1},
		{TopicTraces(tenant), 12, 1},
		{TopicEvents(tenant), 6, 1},
		{TopicDLQ(tenant), 3, 1},
	}
	for _, topic := range requiredTopics {
		if err := m.EnsureTopic(ctx, topic.name, topic.partitions, topic.replication); err != nil {
			return fmt.Errorf("ensure topic %s: %w", topic.name, err)
		}
	}
	return nil
}
```

### 4.8 Health Check Integration

**Decision 7A**: Shallow liveness + deep readiness.

```go
// File: cluster/internal/api/ingestion/health.go

// LivenessCheck: Is the process alive?
// Returns 200 if the gRPC server is listening.
func (s *IngestionService) LivenessCheck() error {
	return nil // Process is alive if this handler runs
}

// ReadinessCheck: Is the service ready to accept traffic?
// Checks: StreamEngine connected, Hot store connected, Warm store connected
func (s *IngestionService) ReadinessCheck(ctx context.Context) error {
	// Check stream engine
	if err := s.stream.Producer().Ping(ctx); err != nil {
		return fmt.Errorf("stream engine not ready: %w", err)
	}
	// Check hot store
	if err := s.store.HotStore().Ping(ctx); err != nil {
		return fmt.Errorf("hot store not ready: %w", err)
	}
	return nil
}
```

---

## 5. Layer 4: Hot Store Integration (Go)

**Target**: ~2,500 LOC
**Agent**: `paryty-storage-tier`
**Oracle**: `oracle-go`

### 5.1 File Inventory

| File | Current LOC | Target LOC | Status |
|------|------------|------------|--------|
| `cluster/internal/storage/store.go` | 195 | 500 | Partial — needs proper Store constructor |
| `cluster/internal/storage/hot/dragonfly.go` | 213 | 400 | Good — needs batch operations, TTL config |

### 5.2 Store Orchestrator (`store.go`)

**Current State**: Has `Store` struct with `SetTopology`, `GetTopology`, `StoreMetricBatch`, `GetLatestMetrics`, `QueryMetrics`. References `hot.Client`, `warm.Client`, `cold.Client`.

**Required Changes**:
1. Implement `New(ctx, Config) (*Store, error)` constructor
2. Implement `Close()` with proper shutdown order
3. Implement `Ping()` for health checks
4. Add metrics for each tier (latency, error rate)
5. Add circuit breaker for each tier

```go
// File: cluster/internal/storage/store.go

package storage

import (
	"context"
	"fmt"

	"github.com/paryty/paryty-v1.0/cluster/internal/models"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage/cold"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage/hot"
	"github.com/paryty/paryty-v1.0/cluster/internal/storage/warm"
)

type Config struct {
	Hot  hot.Config  `yaml:"hot"`
	Warm warm.Config `yaml:"warm"`
	Cold cold.Config `yaml:"cold"`
}

type Store struct {
	hot  *hot.Client
	warm *warm.Client
	cold *cold.Client
}

// New creates a new Store with all three tiers initialized
func New(ctx context.Context, cfg Config) (*Store, error) {
	// Initialize hot store (Dragonfly)
	hotClient := hot.New(cfg.Hot)
	if err := hotClient.Ping(ctx); err != nil {
		return nil, fmt.Errorf("hot store ping: %w", err)
	}

	// Initialize warm store (QuestDB)
	warmClient, err := warm.New(ctx, cfg.Warm)
	if err != nil {
		return nil, fmt.Errorf("warm store init: %w", err)
	}
	if err := warmClient.Ping(ctx); err != nil {
		return nil, fmt.Errorf("warm store ping: %w", err)
	}

	// Initialize cold store (SeaweedFS)
	coldClient, err := cold.New(ctx, cfg.Cold)
	if err != nil {
		return nil, fmt.Errorf("cold store init: %w", err)
	}

	return &Store{
		hot:  hotClient,
		warm: warmClient,
		cold: coldClient,
	}, nil
}

func (s *Store) Close() {
	s.hot.Close()
	s.warm.Close()
	s.cold.Close()
}

// HotStore returns the hot storage client for direct access
func (s *Store) HotStore() *hot.Client {
	return s.hot
}

// SetAgentState stores agent state in hot storage
func (s *Store) SetAgentState(ctx context.Context, agent *models.AgentInfo) error {
	return s.hot.SetAgentState(ctx, agent)
}

// GetAgentState retrieves agent state from hot storage
func (s *Store) GetAgentState(ctx context.Context, agentID string) (*models.AgentInfo, error) {
	return s.hot.GetAgentState(ctx, agentID)
}

// GetAllAgentStates retrieves all agent states from hot storage
func (s *Store) GetAllAgentStates(ctx context.Context) ([]models.AgentInfo, error) {
	return s.hot.GetAllAgentStates(ctx)
}

// SetTopology stores topology in hot storage
func (s *Store) SetTopology(ctx context.Context, topo *models.Topology) error {
	return s.hot.SetTopology(ctx, topo)
}

// GetTopology retrieves topology from hot storage
func (s *Store) GetTopology(ctx context.Context) (*models.Topology, error) {
	return s.hot.GetTopology(ctx)
}

// StoreMetricBatch writes to hot + warm (async)
func (s *Store) StoreMetricBatch(ctx context.Context, batch *models.MetricBatch) error {
	// Write to hot (synchronous, fast)
	if err := s.hot.SetLatestMetrics(ctx, batch.AgentID, batch); err != nil {
		return fmt.Errorf("hot store write: %w", err)
	}

	// Write to warm (async, non-blocking)
	go func() {
		if err := s.warm.InsertMetricBatch(context.Background(), batch); err != nil {
			// Log error, don't fail the request
		}
	}()

	return nil
}

// GetLatestMetrics retrieves latest metrics from hot storage
func (s *Store) GetLatestMetrics(ctx context.Context, agentID string) (*models.MetricBatch, error) {
	return s.hot.GetLatestMetrics(ctx, agentID)
}

// QueryMetrics queries metrics from warm storage
func (s *Store) QueryMetrics(ctx context.Context, agentID string, metricName string, start, end time.Time) ([]models.Metric, error) {
	return s.warm.QueryMetrics(ctx, agentID, metricName, start, end)
}
```

### 5.3 Dragonfly Client Hardening (`dragonfly.go`)

**Current State**: Working Redis-compatible client with JSON serialization.

**Required Changes**:
1. Make TTL configurable (currently hardcoded `5 * time.Minute`)
2. Add batch operations (MSET for multiple keys)
3. Add connection pool health check
4. Add retry on connection error
5. Add tenant-scoped key prefixes

```go
// File: cluster/internal/storage/hot/dragonfly.go

// Update key prefixes to include tenant
func metricsKey(tenant, agentID string) string {
	return fmt.Sprintf("paryty:%s:metrics:%s:latest", tenant, agentID)
}

func agentStateKey(tenant, agentID string) string {
	return fmt.Sprintf("paryty:%s:agent:%s", tenant, agentID)
}

func topologyKey(tenant string) string {
	return fmt.Sprintf("paryty:%s:topology:current", tenant)
}
```

---

## 6. Layer 5: Warm Store Integration (Go)

**Target**: ~2,000 LOC
**Agent**: `paryty-storage-tier`
**Oracle**: `oracle-go`

### 6.1 File Inventory

| File | Current LOC | Target LOC | Status |
|------|------------|------------|--------|
| `cluster/internal/storage/warm/questdb.go` | 262 | 500 | Good — needs ILP, table creation, batch insert |

### 6.2 QuestDB Client Hardening (`questdb.go`)

**Current State**: Working PostgreSQL-compatible client with pgx. Has `InsertMetric`, `InsertMetricBatch`, `QueryMetrics`, `QueryAggregatedMetrics`, `InsertSpan`, `QueryTraces`.

**Decision 4A**: Use ILP (InfluxDB Line Protocol) for ingestion.

**Required Changes**:
1. Add ILP sender (QuestDB's native high-throughput ingestion protocol)
2. Add table auto-creation (CREATE TABLE IF NOT EXISTS)
3. Add batch insert optimization (use ILP instead of individual INSERTs)
4. Add connection pool health check
5. Add retry logic with exponential backoff
6. Add tenant-scoped table partitioning

```go
// File: cluster/internal/storage/warm/questdb.go

// Add ILP client for high-throughput ingestion
import (
	"net"
	"fmt"
	"time"
)

// ILPSender sends data via InfluxDB Line Protocol
// ILP is QuestDB's native high-throughput ingestion protocol
// Format: table_name,tag1=value1,tag2=value2 field1=value1,field2=value2 timestamp

type ILPSender struct {
	conn   net.Conn
	addr   string
	buffer []byte
}

func NewILPSender(addr string) (*ILPSender, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to ILP: %w", err)
	}
	return &ILPSender{
		conn:   conn,
		addr:   addr,
		buffer: make([]byte, 0, 64*1024),
	}, nil
}

// SendMetric sends a metric via ILP
func (s *ILPSender) SendMetric(table string, tags map[string]string, fields map[string]interface{}, ts time.Time) error {
	// Format: table,tag1=val1,tag2=val2 field1=val1,field2=val2 timestamp_ns
	var line strings.Builder
	line.WriteString(table)

	for k, v := range tags {
		line.WriteString(",")
		line.WriteString(k)
		line.WriteString("=")
		line.WriteString(v)
	}

	line.WriteString(" ")
	first := true
	for k, v := range fields {
		if !first {
			line.WriteString(",")
		}
		first = false
		line.WriteString(k)
		line.WriteString("=")
		line.WriteString(fmt.Sprintf("%v", v))
	}

	line.WriteString(" ")
	line.WriteString(fmt.Sprintf("%d", ts.UnixNano()))
	line.WriteString("\n")

	s.buffer = append(s.buffer, line.String()...)...

	// Flush if buffer is large enough
	if len(s.buffer) >= 32*1024 {
		return s.Flush()
	}
	return nil
}

// Flush sends buffered data
func (s *ILPSender) Flush() error {
	if len(s.buffer) == 0 {
		return nil
	}
	_, err := s.conn.Write(s.buffer)
	s.buffer = s.buffer[:0]
	return err
}

// InsertMetricBatchILP inserts a batch using ILP (high-throughput)
func (c *Client) InsertMetricBatchILP(batch *models.MetricBatch) error {
	ilp, err := NewILPSender(c.cfg.ILPAddr)
	if err != nil {
		return fmt.Errorf("create ILP sender: %w", err)
	}
	defer ilp.Close()

	// Send CPU metrics
	for _, cpu := range batch.CPU {
		ilp.SendMetric("cpu_metrics",
			map[string]string{"agent_id": cpu.AgentID},
			map[string]interface{}{
				"total_usage_pct": cpu.TotalUsagePct,
				"load_avg_1m":     cpu.LoadAvg1m,
				"load_avg_5m":     cpu.LoadAvg5m,
				"load_avg_15m":    cpu.LoadAvg15m,
			},
			cpu.Timestamp,
		)
	}

	// Send memory, disk, network metrics similarly...

	return ilp.Flush()
}

// Table auto-creation
func (c *Client) EnsureTables(ctx context.Context) error {
	tables := []string{
		`CREATE TABLE IF NOT EXISTS cpu_metrics (
			timestamp TIMESTAMP,
			agent_id SYMBOL,
			total_usage_pct DOUBLE,
			per_core_pct DOUBLE[],
			load_avg_1m DOUBLE,
			load_avg_5m DOUBLE,
			load_avg_15m DOUBLE,
			frequency_mhz DOUBLE,
			context_switches LONG
		) TIMESTAMP(timestamp) PARTITION BY DAY WAL DEDUP ENABLED timestamp`,

		`CREATE TABLE IF NOT EXISTS memory_metrics (
			timestamp TIMESTAMP,
			agent_id SYMBOL,
			total_bytes LONG,
			used_bytes LONG,
			available_bytes LONG,
			cached_bytes LONG,
			swap_total_bytes LONG,
			swap_used_bytes LONG
		) TIMESTAMP(timestamp) PARTITION BY DAY WAL DEDUP ENABLED timestamp`,

		`CREATE TABLE IF NOT EXISTS disk_metrics (
			timestamp TIMESTAMP,
			agent_id SYMBOL,
			device SYMBOL,
			mount_point SYMBOL,
			total_bytes LONG,
			used_bytes LONG,
			read_bytes_per_sec DOUBLE,
			write_bytes_per_sec DOUBLE,
			iops_read DOUBLE,
			iops_write DOUBLE,
			io_latency_ms DOUBLE,
			queue_depth DOUBLE
		) TIMESTAMP(timestamp) PARTITION BY DAY WAL DEDUP ENABLED timestamp`,

		`CREATE TABLE IF NOT EXISTS network_metrics (
			timestamp TIMESTAMP,
			agent_id SYMBOL,
			interface SYMBOL,
			rx_bytes_per_sec DOUBLE,
			tx_bytes_per_sec DOUBLE,
			rx_packets DOUBLE,
			tx_packets DOUBLE,
			errors LONG,
			tcp_retransmits LONG,
			estimated_rtt_ms DOUBLE
		) TIMESTAMP(timestamp) PARTITION BY DAY WAL DEDUP ENABLED timestamp`,

		`CREATE TABLE IF NOT EXISTS process_metrics (
			timestamp TIMESTAMP,
			agent_id SYMBOL,
			pid INT,
			name SYMBOL,
			cpu_usage_pct DOUBLE,
			rss_bytes LONG,
			thread_count INT,
			fd_count INT,
			container_id SYMBOL
		) TIMESTAMP(timestamp) PARTITION BY DAY WAL DEDUP ENABLED timestamp`,

		`CREATE TABLE IF NOT EXISTS aggregated_metrics (
			timestamp TIMESTAMP,
			agent_id SYMBOL,
			metric_name SYMBOL,
			window INTERVAL,
			agg_type SYMBOL,
			value DOUBLE
		) TIMESTAMP(timestamp) PARTITION BY DAY WAL DEDUP ENABLED timestamp`,

		`CREATE TABLE IF NOT EXISTS spans (
			timestamp TIMESTAMP,
			trace_id SYMBOL,
			span_id SYMBOL,
			parent_span_id SYMBOL,
			name SYMBOL,
			kind SYMBOL,
			service_name SYMBOL,
			start_time TIMESTAMP,
			end_time TIMESTAMP,
			duration LONG,
			status SYMBOL
		) TIMESTAMP(timestamp) PARTITION BY DAY WAL DEDUP ENABLED timestamp`,
	}

	for _, query := range tables {
		if _, err := c.pool.Exec(ctx, query); err != nil {
			return fmt.Errorf("create table: %w", err)
		}
	}

	return nil
}
```

---

## 7. Verification Gates

### 7.1 Per-Layer Verification

After completing each layer, run these verification commands:

**Layer 1 (Metal Scrapers)**:
```bash
cd agent
cargo check          # Must succeed with zero warnings
cargo clippy          # Must have zero warnings
cargo test            # All tests must pass
cargo fmt --check     # Must be properly formatted
```

**Layer 2 (Communication)**:
```bash
cd agent
cargo check
cargo clippy
cargo test
cargo bench           # If benchmarks exist
cargo fmt --check
```

**Layer 3 (Ingestion)**:
```bash
cd cluster
go build ./...        # Must succeed
go vet ./...          # Must pass
go test ./...         # All tests must pass
golangci-lint run     # Must pass (if configured)
```

**Layer 4-5 (Storage)**:
```bash
cd cluster
go build ./...        # Must succeed
go test ./internal/storage/...  # Storage tests must pass
```

### 7.2 Integration Verification

After all layers are complete, verify end-to-end:

```bash
# 1. Start infrastructure (Dragonfly, QuestDB, Redpanda)
docker compose -f deploy/compose/docker-compose.infra.yaml up -d

# 2. Start cluster
cd cluster && go run ./cmd/ingestion/

# 3. Start agent (Linux required for /proc)
cd agent && cargo run --release

# 4. Verify data flow
# - Agent logs should show "Batch processed"
# - Cluster logs should show "Agent registered" and "Batch processed"
# - Dragonfly should have agent state: redis-cli GET "paryty:default:agent:{id}"
# - QuestDB should have metrics: SELECT count() FROM cpu_metrics
```

### 7.3 Performance Targets

| Metric | Target | How to Measure |
|--------|--------|----------------|
| Agent collection latency | < 10ms per collector | Tracing spans |
| Agent memory usage | < 50MB RSS | `/proc/self/status` |
| gRPC round-trip latency | < 5ms (same datacenter) | gRPC interceptors |
| Ingestion throughput | > 10,000 batches/sec | Load test |
| Hot store write latency | < 1ms | Redis LATENCY command |
| Warm store write latency | < 10ms (ILP) | QuestDB metrics |
| Edge buffer disk spillover | Works when memory full | Manual test |
| Compression ratio | > 3:1 for typical metrics | Agent self-metrics |

---

## 8. Contingency & Rollback

### 8.1 Rollback Strategy

Each layer is independently rollbackable:

1. **Metal Scrapers**: Revert to `sysinfo`-only collection (existing code works)
2. **Communication**: Revert to in-memory buffer (existing code works, just loses persistence)
3. **Ingestion**: Revert to env-only config (existing code works)
4. **Hot Store**: Already implemented, just needs wiring
5. **Warm Store**: Fall back to PostgreSQL INSERT (existing code works, just slower)

### 8.2 Known Risks

| Risk | Mitigation |
|------|------------|
| QuestDB ILP connection unstable | Fall back to PostgreSQL INSERT |
| SQLite edge buffer corruption | WAL mode + periodic integrity check |
| Redis/Dragonfly connection pool exhaustion | Circuit breaker + bounded pool |
| Agent crash during collection | Each collector is isolated, others continue |
| gRPC stream disconnect | Reconnection engine with exponential backoff |

### 8.3 Phase 1 Exit Criteria

Phase 1 is COMPLETE when ALL of these are true:

- [ ] Agent collects all metal metrics (CPU, memory, disk, network, process, container)
- [ ] Agent compresses and sends via gRPC
- [ ] Agent buffers to SQLite on disk when cluster unavailable
- [ ] Agent reconnects automatically after disconnect
- [ ] Cluster ingests via gRPC and publishes to Redpanda
- [ ] Cluster stores in Dragonfly (hot) with configurable TTL
- [ ] Cluster stores in QuestDB (warm) via ILP
- [ ] Cluster creates tables automatically on startup
- [ ] Health checks work (liveness + readiness)
- [ ] All tests pass (cargo test + go test)
- [ ] Zero clippy warnings, zero go vet warnings
- [ ] End-to-end data flow verified manually

---

## Appendix A: Proto Message Reference

Key proto messages used in Phase 1 (from `proto/paryty/v1/agent.proto`):

- `AgentRegistration` — Agent registration request
- `AgentRegistrationResponse` — Registration response with config
- `MetricBatch` — Top-level metric batch (contains CPU, memory, disk, network, process, container)
- `CpuMetric` — CPU usage data
- `MemoryMetric` — Memory usage data
- `DiskMetric` — Disk I/O data
- `NetworkMetric` — Network I/O data
- `ProcessMetric` — Per-process data
- `ContainerMetric` — Container data
- `HeartbeatRequest` / `HeartbeatResponse` — Heartbeat exchange
- `FlowControl` — Server-to-agent rate control

## Appendix B: Config Schema Reference

**Agent Config** (`configs/agent/agent.yaml`):
```yaml
agent:
  id: "auto"                           # UUID or "auto" to generate
  cluster_endpoint: "grpc://host:443"   # gRPC endpoint
  api_key: ""                           # For authentication
  layers:
    metal:
      enabled: true
      interval: 10s                     # Collection interval
      cpu:
        per_core: true
        per_process: true
      memory:
        per_process: true
        top_n_processes: 10
      disk:
        enabled: true
        exclude_devices: ["loop*"]
      network:
        enabled: true
        exclude_interfaces: ["lo"]
      process:
        enabled: true
        max_processes: 50
      container:
        enabled: true
  communication:
    protocol: grpc
    tls_enabled: true
    compression: zstd
    edge_buffer:
      max_memory_mb: 100
      disk_spillover_enabled: true
      disk_path: "/var/lib/paryty/agent/buffer.db"
      ttl_hours: 24
    reconnect:
      base_delay_ms: 100
      max_delay_ms: 30000
      jitter: 0.5
    flow_control:
      backpressure_threshold: 0.9
      adaptive_sampling: true
  logging:
    level: info
    format: json
```

**Cluster Config** (`configs/cluster/cluster.yaml`):
```yaml
cluster:
  name: "paryty-prod"
  tenant: "default"
  ingestion:
    port: 443
    tls: true
    max_connections: 10000
  stream_engine:
    type: "redpanda"
    brokers: ["localhost:9092"]
    topics:
      - name: "paryty.default.metrics.raw"
        partitions: 12
        retention: "7d"
  storage:
    hot:
      type: "dragonfly"
      addr: "localhost:6379"
      ttl: "5m"
      pool_size: 10
    warm:
      type: "questdb"
      addr: "localhost:8812"
      database: "paryty"
      username: "paryty"
      password: "paryty"
      max_conns: 10
      ilp_addr: "localhost:9009"
    cold:
      type: "seaweedfs"
      endpoint: "localhost:8333"
```