---
name: test-integration
description: Run cross-language integration tests for Paryty — protobuf round-trips, gRPC streaming, version compatibility, serialization contracts, and boundary resilience.
---

# Test Integration — Cross-Language Contract Testing

## Purpose
Verify that Paryty multi-language components (Rust, Go, TypeScript, Python) communicate correctly across all boundaries.

## Execution Steps

### Step 1: Proto Contract Validation
buf lint
buf breaking --against '.git#branch=main'
Expected: Zero lint errors. Zero breaking changes.

### Step 2: Proto Codegen
buf generate
Expected: Generated stubs for Rust (prost), Go (protobuf-go), TypeScript (protobuf-ts) match proto definitions.

### Step 3: Cross-Language Serialization Round-Trip
- Rust generates bytes, Go decodes, encodes, Rust decodes, compare
- Go generates bytes, TypeScript decodes, encodes, Go decodes, compare
- Verify: field values identical, unknown fields preserved, default values correct

Rust side: cargo test --test proto_roundtrip
Go side: go test -v -run TestProtoRoundTrip ./internal/proto/...
TypeScript side: npx vitest run src/__tests__/proto.test.ts

### Step 4: gRPC Streaming Contract
Test bidirectional streaming between Rust agent and Go cluster:
- Start mock cluster, run agent integration test
- Verify: connect within 10s, heartbeat every 30s, stale detection at 90s
- Verify: reconnection with exponential backoff, edge buffer replay, no data loss
- Verify: sequence numbers prevent duplicate processing

### Step 5: Version Compatibility
Test with stubs from previous proto version:
- Build agent with old proto stubs, test against current cluster
- Verify: old agent works with reduced functionality, no crashes

### Step 6: Compression Contract
Verify Zstd compression round-trips across Rust and Go:
- Rust compress, Go decompress, compare
- Go compress, Rust decompress, compare

## Checklist
- buf lint passes with zero errors
- buf breaking reports no unintentional breaking changes
- All proto message types round-trip correctly across Rust and Go
- All proto message types round-trip correctly across Go and TypeScript
- gRPC bidirectional streaming works (connect, heartbeat, reconnect, replay)
- Version compatibility verified (old agent with new cluster)
- Zstd compression round-trips correctly across languages
- Edge cases: empty messages, max-size messages (>1MB), zero values, null fields
- Sequence numbers prevent duplicate processing on reconnect

## Exit Protocol
- ALL pass: "Integration contract tests passed"
- Proto lint fails: Report violation, STOP
- Breaking change detected: Report field and impact, STOP
- Round-trip fails: Report message type and field mismatch, STOP
- Streaming fails: Report failure mode, STOP
- Compression fails: Report language pair, STOP
