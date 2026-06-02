#!/bin/bash
# Paryty Protocol Buffer Code Generation Script
#
# Prerequisites:
#   - buf CLI: https://buf.build/docs/installation/
#   - OR protoc with plugins:
#     - protoc-gen-go, protoc-gen-go-grpc (for Go)
#     - protoc-gen-es (for TypeScript)
#     - protoc + tonic-build (for Rust, handled by cargo build)
#
# Usage:
#   ./scripts/generate-protos.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "=== Paryty Proto Code Generation ==="
echo ""

# Method 1: Using buf (recommended)
if command -v buf &> /dev/null; then
    echo "Using buf for code generation..."
    cd "$PROJECT_ROOT"
    buf generate
    echo "Code generation complete with buf."
    echo ""
    echo "Generated files:"
    echo "  Rust:      agent/src/proto/"
    echo "  Go:        cluster/internal/proto/"
    echo "  TypeScript: frontend/src/proto/"
    exit 0
fi

echo "buf not found. Using protoc directly..."

# Method 2: Using protoc directly
if ! command -v protoc &> /dev/null; then
    echo "ERROR: Neither buf nor protoc found."
    echo "Install buf: https://buf.build/docs/installation/"
    echo "Or install protoc: https://grpc.io/docs/protoc-installation/"
    exit 1
fi

PROTO_DIR="$PROJECT_ROOT/proto"

# Generate Go code
echo "Generating Go code..."
mkdir -p "$PROJECT_ROOT/cluster/internal/proto"
protoc \
    --proto_path="$PROTO_DIR" \
    --go_out="$PROJECT_ROOT/cluster/internal/proto" --go_opt=paths=source_relative \
    --go-grpc_out="$PROJECT_ROOT/cluster/internal/proto" --go-grpc_opt=paths=source_relative \
    "$PROTO_DIR"/paryty/v1/*.proto
echo "  Go code generated in cluster/internal/proto/"

# Generate TypeScript code (requires @bufbuild/protoc-gen-es)
if command -v protoc-gen-es &> /dev/null; then
    echo "Generating TypeScript code..."
    mkdir -p "$PROJECT_ROOT/frontend/src/proto"
    protoc \
        --proto_path="$PROTO_DIR" \
        --es_out="$PROJECT_ROOT/frontend/src/proto" \
        --es_opt=target=ts \
        "$PROTO_DIR"/paryty/v1/*.proto
    echo "  TypeScript code generated in frontend/src/proto/"
else
    echo "  Skipping TypeScript (protoc-gen-es not found)"
    echo "  Install: npm install -g @bufbuild/protoc-gen-es"
fi

echo ""
echo "Rust code generation is handled by cargo build (build.rs)"
echo "Run: cd agent && cargo build"
echo ""
echo "=== Done ==="
