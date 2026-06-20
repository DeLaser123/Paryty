---
description: Run cross-language integration tests — protobuf round-trips, gRPC streaming, serialization contracts
agent: integration-engineer
---
# Test Integration — Cross-Language Contract Testing

Verify that Paryty multi-language components communicate correctly across all boundaries.

## Execution Steps

### Step 1: Proto Contract Validation
```bash
buf lint
buf breaking --against '.git#branch=main'
```

### Step 2: Proto Codegen
```bash
buf generate
```

### Step 3: Cross-Language Serialization Round-Trip
- Rust → Go → Rust (compare field values)
- Go → TypeScript → Go (compare field values)
- Verify: field values identical, unknown fields preserved, default values correct

### Step 4: gRPC Streaming Contract
Test bidirectional streaming between Rust agent and Go cluster:
- Connect within 10s, heartbeat every 30s, stale detection at 90s
- Reconnection with exponential backoff
- Edge buffer replay, no data loss
- Sequence numbers prevent duplicate processing

### Step 5: Version Compatibility
- Old agent with current cluster (reduced functionality, no crashes)

### Step 6: Compression Contract
Verify Zstd compression round-trips across Rust and Go.

## Checklist
- [ ] `buf lint` passes with zero errors
- [ ] `buf breaking` reports no unintentional breaking changes
- [ ] All proto message types round-trip correctly across Rust and Go
- [ ] All proto message types round-trip correctly across Go and TypeScript
- [ ] gRPC bidirectional streaming works
- [ ] Version compatibility verified
- [ ] Zstd compression round-trips correctly
- [ ] Edge cases: empty messages, max-size messages (>1MB), zero values
- [ ] Sequence numbers prevent duplicate processing

## Exit Protocol
- ALL pass: "Integration contract tests passed" with raw output
- Any failure: Report specific failure mode, STOP

Show raw, unfiltered output from each step.
