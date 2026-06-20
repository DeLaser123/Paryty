---
name: test-integration
description: Run cross-language integration tests — protobuf round-trips, gRPC streaming, version compatibility, serialization contracts.
---
# Test Integration — Cross-Language Contract Testing

## Execution Steps

### Step 1: Proto Contract Validation
```bash
buf lint
buf breaking --against '.git#branch=main'
```

### Step 2: Cross-Language Serialization Round-Trip
- Rust → Go → Rust
- Go → TypeScript → Go

### Step 3: gRPC Streaming Contract
Test bidirectional streaming: connect, heartbeat, reconnect, edge buffer replay, sequence numbers.

### Step 4: Compression Contract
Verify Zstd round-trips across Rust and Go.

## Exit Protocol
- ALL pass: "Integration contract tests passed"
- Any fail: Report specific failure mode, STOP
- Show raw, unfiltered output
