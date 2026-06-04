# Phase 2: eBPF Network Observer — Implementation Plan

## Context

**Why**: Phase 1 (Agent Core) is ~85-90% complete — metal scrapers, communication layer, supervisor, config, and main entry point are all functional. The only significant gap is the eBPF module, which currently consists of 6 stub files (318 total LOC) that do nothing. Phase 2 transforms these stubs into a real eBPF-based network observer providing zero-instrumentation visibility into TCP connections, DNS resolutions, and HTTP traffic.

**What**: Implement ~10,500 LOC across 4 layers: eBPF infrastructure (Rust loader + C programs), TCP connection tracker, DNS mapper, and HTTP/SNI inspector. Locked decisions: **libbpf C + Rust loader** (NOT aya), **kprobes**, **SNI + plaintext HTTP**, **graceful /proc fallback** on non-eBPF systems. Database inspection is **deferred to Phase 3**.

**Constraint**: Development is on Windows. eBPF C programs can be written but not compiled/tested on Windows. All Rust code is `#[cfg(target_os = "linux")]` gated so `cargo check/test/clippy` pass on Windows in stub mode.

---

## Task Breakdown

### T1: Dependency & Build Foundation
**Files**: `agent/Cargo.toml` (MODIFY), `agent/build.rs` (MODIFY)
**Agent**: Direct edit
**LOC**: ~60

**Cargo.toml changes**:
- Add to `[target.'cfg(target_os = "linux")'.dependencies]`: `libbpf-rs = "0.23"`, `libbpf-sys = "1.4"`, `plain = "0.2"`
- Add to `[build-dependencies]`: `cc = "1.0"`

**build.rs changes**:
- Add `#[cfg(target_os = "linux")]` gated `compile_ebpf_programs()` function
- Check clang availability (skip with warning if not found — mirrors protoc pattern at line 21-23)
- Generate `vmlinux.h` via bpftool if not exists
- Compile each `.bpf.c` to `.bpf.o` in `OUT_DIR` with flags: `-g -O2 -target bpf -D__TARGET_ARCH_x86`
- Print `cargo:rerun-if-changed` for each source file

**Verify**: `cargo check` on Windows passes. On Linux: `cargo build` compiles `.bpf.o` files.

---

### T2: Configuration Extensions
**Files**: `agent/src/config/mod.rs` (MODIFY), `configs/agent/agent.yaml` (MODIFY)
**Agent**: Direct edit
**LOC**: ~25

- Add 3 fields to `EbpfConfig` with `#[serde(default = "...")]`:
  - `ring_buffer_size_kb: u32` (default: 256)
  - `poll_interval_ms: u64` (default: 100)
  - `fallback_to_proc: bool` (default: true)
- Update `agent.yaml` eBPF section with new fields
- Update test config in `communication/mod.rs` if needed for struct literal changes

**Verify**: `cargo check` + `cargo test` on Windows pass.

---

### T3: C eBPF Shared Header
**Files**: `agent/src/ebpf/bpf/common.h` (NEW — create directory)
**Agent**: `ebpf-engineer`
**LOC**: ~150

- Constants: `MAX_COMM_LEN=16`, `MAX_DOMAIN_LEN=256`, `MAX_HTTP_PATH_LEN=128`, `MAX_HTTP_HOST_LEN=128`
- Event type enum: `EVENT_TCP_CONNECT=1` through `EVENT_HTTPS_SNI=6`
- Structs with explicit padding to match Rust `#[repr(C)]`:
  - `struct tcp_event` — ~72 bytes
  - `struct dns_event` — ~296 bytes
  - `struct http_event` — ~282 bytes
- `#define RING_BUFFER_SIZE (256 * 1024)`

**Verify**: File exists, struct layouts match Rust side (verified in T10-T12).

---

### T4: TCP Tracker eBPF Program
**Files**: `agent/src/ebpf/bpf/tcp_tracker.bpf.c` (NEW)
**Agent**: `ebpf-engineer`
**LOC**: ~400 | **Depends on**: T3

- **kprobe/tcp_connect** — capture outgoing connections, store in `connect_info` hash map
- **kretprobe/tcp_v4_connect** — on success (ret==0), emit event via ring buffer
- **kprobe/tcp_set_state** — emit for TCP_CLOSE, TCP_TIME_WAIT, TCP_ESTABLISHED (accepts)
- Maps: `tcp_events` (ringbuf), `connect_info` (hash, 10K), `connections` (hash, 64K)
- Helpers: `emit_event()`, `make_conn_key()`, `format_ip()`
- License: GPL (required for kprobes)

**Verify**: clang compilation on Linux succeeds. BPF verifier accepts.

---

### T5: DNS Mapper eBPF Program
**Files**: `agent/src/ebpf/bpf/dns_mapper.bpf.c` (NEW)
**Agent**: `ebpf-engineer`
**LOC**: ~350 | **Depends on**: T3

- **kprobe/udp_sendmsg** — detect DNS queries (port 53), emit metadata to ring buffer
- Domain marked as `<pending>` in eBPF; full DNS parsing deferred to userspace
- Map: `dns_events` (ringbuf)

**Verify**: clang compilation on Linux succeeds.

---

### T6: HTTP Inspector eBPF Program
**Files**: `agent/src/ebpf/bpf/http_inspector.bpf.c` (NEW)
**Agent**: `ebpf-engineer`
**LOC**: ~500 | **Depends on**: T3

- **kprobe/tcp_sendmsg** — detect HTTP (80,8080,8000,3000,5000) and HTTPS (443,8443) by port
- Flag as `EVENT_HTTP_REQUEST` or `EVENT_HTTPS_SNI`; payload parsing in userspace
- Map: `http_events` (ringbuf)
- `__always_inline` port filter helpers for verifier compliance

**Verify**: clang compilation on Linux succeeds.

---

### T7: eBPF Loader (Rust)
**Files**: `agent/src/ebpf/loader.rs` (REWRITE)
**Agent**: `rust-agent-engineer`
**LOC**: ~400 | **Depends on**: T1

- Embed `.bpf.o` via `include_bytes!(concat!(env!("OUT_DIR"), "/..."))` — gated `#[cfg(target_os = "linux")]`
- `EbpfPrograms` struct: `Option<Object>` for each program, `stub()` constructor
- `EbpfLoader::new(config: EbpfConfig)`
- `load() -> Result<EbpfPrograms>`: check capabilities → load each enabled program → attach kprobes
- `check_capabilities()`: kernel version via `/proc/sys/kernel/osrelease`, BTF via `/sys/kernel/btf/vmlinux`, CAP_BPF via `/proc/self/status`
- Non-Linux: returns `EbpfPrograms::stub()`

**Verify**: `cargo check` on Windows. `cargo clippy` clean.

---

### T8: /proc Fallback
**Files**: `agent/src/ebpf/proc_fallback.rs` (NEW)
**Agent**: `rust-agent-engineer`
**LOC**: ~250 | **Depends on**: T2

- `ProcObserver` with `Mutex<HashMap<ConnectionKey, ConnectionState>>`
- Parse `/proc/net/tcp` and `/proc/net/tcp6` — hex `ip:port` format
- Delta detection: new ESTABLISHED → emit, disappeared → emit CLOSED
- PID resolution via `/proc/[pid]/fd` inode matching (cached 30s)
- `pub async fn run(config, client)` — 5s polling loop, sends via `client.send_network_events()`
- Non-Linux: empty module

**Verify**: `cargo check` on Windows. On Linux: parses real `/proc/net/tcp`.

---

### T9: Module Root & ObserverMode
**Files**: `agent/src/ebpf/mod.rs` (REWRITE)
**Agent**: `rust-agent-engineer`
**LOC**: ~300 | **Depends on**: T7, T8

- Add `pub mod proc_fallback;`
- `ObserverMode` enum: `Ebpf`, `ProcFallback`, `Stub`
- `detect_observer_mode()`: cfg-gated capability detection
- Rewrite `run()`: match on mode → `run_ebpf()`, `proc_fallback::run()`, or sleep loop
- `run_ebpf()`: load programs → create trackers → event loop with `tokio::select!` on ring buffer poll + cancellation
- Every `poll_interval_ms`: poll ring buffers, drain events, batch → serialize → `client.send_network_events()`
- Remove `#![allow(dead_code)]`, update doc comments (libbpf not aya)

**Verify**: `cargo check` + `cargo clippy` on Windows.

---

### T10: TCP Tracker Userspace
**Files**: `agent/src/ebpf/tcp_tracker.rs` (REWRITE)
**Agent**: `rust-agent-engineer`
**LOC**: ~800 | **Depends on**: T1, T9

- `#[repr(C)]` struct `TcpEvent` matching `common.h::tcp_event` + `unsafe impl plain::Plain` with SAFETY comment
- `TcpTracker::new(config)` — holds `Mutex<HashMap<ConnectionKey, TrackedConnection>>`
- `setup_ring_buffer(obj) -> Result<RingBuffer>` — parse via `plain::from_bytes`, filter ports, update state
- `drain_events() -> Vec<NetworkEvent>` — evict stale (5min), convert to events
- Connection state machine: SYN_SENT → ESTABLISHED → TIME_WAIT → CLOSED
- IP formatting helpers
- **ZERO `unwrap()` calls**

**Verify**: `cargo check`, `cargo clippy`, struct size unit test.

---

### T11: DNS Mapper Userspace
**Files**: `agent/src/ebpf/dns_mapper.rs` (REWRITE)
**Agent**: `rust-agent-engineer`
**LOC**: ~500 | **Depends on**: T1, T9

- `#[repr(C)]` struct `DnsEventRaw` matching `common.h::dns_event`
- **FIX existing `unwrap()` violations** at lines 30 and 35
- `DnsMapper::new(config)` — `Mutex<HashMap<String, DnsEntry>>`
- `process_event(raw)` — extract domain, update cache
- `resolve(domain) -> Option<DnsEntry>` — cache lookup
- `correlate_ip(ip) -> Option<String>` — **KEY integration**: reverse IP→domain lookup for HTTP/SNI enrichment
- `drain_events() -> Vec<NetworkEvent>` — evict expired (5min TTL)
- **ZERO `unwrap()` calls**

**Verify**: `cargo check`, `cargo clippy`, `grep unwrap` returns 0 matches.

---

### T12: HTTP Inspector Userspace
**Files**: `agent/src/ebpf/http_inspector.rs` (REWRITE)
**Agent**: `rust-agent-engineer`
**LOC**: ~700 | **Depends on**: T1, T9, T11

- `#[repr(C)]` struct `HttpEventRaw` matching `common.h::http_event`
- **SNI Extractor**: Parse TLS ClientHello → find extension 0x0000 → extract hostname
  - Bounds-check every byte access
- **HTTP/1.1 Parser**: Parse request line + Host header; parse response status line
- `HttpInspector::new(config)` — stores reference to DnsMapper for IP correlation
- `process_event(raw, payload) -> Option<NetworkEvent>`:
  - HTTP → parse request, build NetworkEvent
  - HTTPS → extract SNI, correlate with DNS cache, build NetworkEvent with `method: "TLS"`
- Unit tests for SNI extraction and HTTP parsing

**Verify**: `cargo check`, `cargo clippy`, SNI unit tests pass.

---

### T13: Integration & Verification
**Files**: Fixes only as needed
**Agent**: Direct verification
**LOC**: ~50 | **Depends on**: T1-T12

**Windows (dev machine)**:
```
cargo check              # must pass
cargo clippy -- -D warnings  # must pass (zero warnings)
cargo test               # must pass
```

**Linux (VM/WSL2)**:
```
cargo build --release    # compiles .bpf.o files
cargo test               # all tests
sudo cargo test          # eBPF loading tests
```

**eBPF verification** (root):
```
sudo bpftool prog list | grep paryty
sudo bpftool map list | grep paryty
# Generate traffic: curl http://example.com
# Check agent logs for TCP/DNS/HTTP events
```

**Fallback verification**: Set `enabled: true` on Windows → verify stub mode, no crash.

---

## Execution Order & Parallelism

```
[T1, T2, T3]  ──────────────────────────────────────── parallel, no deps
       │
       ├── T4, T5, T6  ─────────────────────────────── parallel (after T3)
       ├── T7  ──────────────────────────────────────── after T1
       └── T8  ──────────────────────────────────────── after T2
              │
              └── T9  ───────────────────────────────── after T7 + T8
                     │
                     └── T10, T11, T12  ─────────────── parallel (after T9)
                            │
                            └── T13  ────────────────── after all
```

**Critical path**: T1 → T7 → T9 → T10/T11/T12 → T13

## Specialist Agent Mapping

| Task | Agent |
|------|-------|
| T1 (Cargo.toml, build.rs) | Direct edit |
| T2 (Config) | Direct edit |
| T3-T6 (C eBPF programs) | `ebpf-engineer` |
| T7-T12 (Rust userspace) | `rust-agent-engineer` |
| T13 (Integration) | Direct verification + `/verify-rust` |

## Exit Criteria

- [ ] eBPF C programs compile with clang on Linux
- [ ] eBPF programs load and attach on Linux 5.4+
- [ ] TCP tracker captures connection events in real-time
- [ ] DNS mapper intercepts DNS queries and caches results
- [ ] HTTP inspector extracts SNI from TLS traffic
- [ ] HTTP inspector parses plaintext HTTP requests
- [ ] /proc fallback works on systems without eBPF
- [ ] Non-Linux systems run in stub mode without errors
- [ ] `cargo check` passes on Windows
- [ ] `cargo clippy -- -D warnings` passes (zero warnings)
- [ ] `cargo test` passes on Windows
- [ ] Zero `unwrap()` calls in eBPF module
