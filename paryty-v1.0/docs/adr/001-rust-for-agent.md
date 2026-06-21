# ADR-001: Rust for Agent Core

## Status: Accepted

## Date: 2025-01-15

## Context

The Paryty agent runs on customer infrastructure — potentially thousands of nodes. It must be:
- Memory-safe (runs with elevated privileges for eBPF)
- Low-CPU (must not impact host workloads — target < 2% CPU)
- Low-memory (target < 50 MB RSS)
- Cross-platform (Linux, Windows, macOS)
- Capable of loading eBPF programs (requires kernel-level interop)

## Decision

**Rust** for the agent core. Not Go, not C++.

## Rationale

| Criterion | Rust | Go | C++ |
|-----------|------|----|-----|
| Memory safety | Guaranteed at compile time | GC-managed (pauses) | Manual (error-prone) |
| CPU overhead | Zero-cost abstractions | GC pauses | Zero-cost abstractions |
| Memory footprint | ~2 MB binary, ~5 MB RSS | ~10 MB binary, ~20 MB RSS | ~1 MB binary, ~5 MB RSS |
| eBPF integration | libbpf via FFI (mature) | aya (less mature) | libbpf native |
| Cross-compilation | Excellent (cross-rs) | Excellent (GOOS/GOARCH) | Complex (toolchains) |
| Async runtime | Tokio (battle-tested) | Goroutines (excellent) | Manual or Boost.Asio |
| Community trend | Growing rapidly | Stable | Stable/declining |

**Key factors:**
1. **Memory safety without GC** — The agent runs on customer machines; GC pauses are unacceptable.
2. **eBPF integration** — libbpf (C) is the reference implementation. Rust's FFI to libbpf is mature and well-documented. Go's aya library is less mature for CO-RE.
3. **Zero-cost abstractions** — Iterator chains, pattern matching, and generics compile to the same code as hand-written loops.
4. **Tokio async runtime** — Handles thousands of concurrent gRPC streams with minimal overhead.

## Consequences

- **Smaller talent pool** than Go — mitigated by Rust's growing adoption in infrastructure tooling (ripgrep, fd, bat, Firecracker, Tokio)
- **Longer compile times** — mitigated by incremental compilation and `cargo check` for fast feedback
- **Steeper learning curve** — mitigated by comprehensive coding bible (`coding-standards-rust.md`)
- **Excellent runtime performance** — agent meets < 2% CPU and < 50 MB memory targets

## Alternatives Considered

1. **Go** — Rejected: GC pauses, larger memory footprint, aya (Go eBPF) less mature than libbpf
2. **C++** — Rejected: Memory safety risks, manual memory management on customer infrastructure is unacceptable
3. **Zig** — Rejected: Immature ecosystem, no eBPF story
