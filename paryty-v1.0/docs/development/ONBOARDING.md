# Paryty Developer Onboarding

Welcome to the Paryty team. This guide gets you from zero to a running development environment.

---

## Prerequisites

| Tool | Version | Purpose | Install |
|------|---------|---------|---------|
| **Go** | 1.25+ | Cluster services | [go.dev](https://go.dev/dl/) |
| **Rust** | 1.85+ | Agent core | [rustup.rs](https://rustup.rs/) |
| **Node.js** | 22+ | Frontend | [nodejs.org](https://nodejs.org/) |
| **Python** | 3.12+ | Intelligence layer | [python.org](https://www.python.org/) |
| **Podman** | 5.x | Containers | [podman.io](https://podman.io/) |
| **Podman Compose** | 1.x+ | Multi-container orchestration | `pip install podman-compose` |
| **protobuf/protoc** | Latest | gRPC code generation | [grpc.io](https://grpc.io/docs/protoc-installation/) |
| **buf** | Latest | Protobuf linting/generation | [buf.build](https://buf.build/docs/installation) |

### Optional Tools

| Tool | Purpose |
|------|---------|
| **grpcurl** | Test gRPC endpoints |
| **redis-cli** | Inspect Dragonfly |
| **rpk** | Redpanda CLI |
| **pgcli** | PostgreSQL CLI |

---

## Quick Start

### 1. Clone the Repository

```bash
git clone https://github.com/paryty/paryty-v1.0.git
cd paryty-v1.0
```

### 2. Start Infrastructure

```bash
# Start Redpanda, Dragonfly, QuestDB, PostgreSQL, SeaweedFS
podman-compose --env-file .env.dev -f deploy/compose/docker-compose.infra.yaml up -d

# Verify all infrastructure is healthy
podman ps --format "table {{.Names}}\t{{.Status}}"
```

### 3. Build & Run Cluster (Go)

```bash
cd cluster

# Build all services
go build ./cmd/query/...
go build ./cmd/ingestion/...
go build ./cmd/pipeline/...

# Run query service
go run ./cmd/query/...

# Or run tests
go test ./...
```

### 4. Build & Run Agent (Rust)

```bash
cd agent

# Build
cargo build

# Run
cargo run

# Run tests
cargo test
```

### 5. Start Frontend (TypeScript)

```bash
cd frontend

# Install dependencies
npm install

# Start dev server (hot-reload)
npm run dev
# Frontend at http://localhost:5173

# Run tests
npx vitest run
```

### 6. Start Intelligence Layer (Python)

```bash
cd intelligence

# Create virtual environment
python -m venv .venv
source .venv/bin/activate  # Linux/macOS
# .venv\Scripts\activate   # Windows

# Install dependencies
pip install -r requirements.txt

# Run
python -m intelligence.main
```

### 7. Verify Everything Works

```bash
# Query service health
curl http://localhost:8080/health

# Frontend
open http://localhost:5173

# QuestDB console
open http://localhost:9000

# Dragonfly
redis-cli -p 6379 ping
```

---

## Project Structure

```
paryty-v1.0/
├── agent/                  # Rust agent (eBPF, metrics collection)
│   ├── src/                # Agent source code
│   ├── Cargo.toml          # Rust dependencies
│   └── build.rs            # Build script (eBPF compilation)
├── cluster/                # Go cluster (ingestion, pipeline, storage, query)
│   ├── cmd/                # Entry points
│   │   ├── query/          # Query service (REST/WS/SSE)
│   │   ├── ingestion/      # Ingestion service (gRPC)
│   │   └── pipeline/       # Processing pipeline
│   ├── internal/           # Internal packages
│   └── go.mod              # Go dependencies
├── frontend/               # React + PixiJS visualization
│   ├── src/                # Frontend source code
│   ├── package.json        # Node dependencies
│   └── vite.config.ts      # Vite configuration
├── intelligence/           # Python ML services
│   ├── intelligence/       # Python package
│   └── requirements.txt    # Python dependencies
├── proto/                  # Protobuf definitions (shared across languages)
│   └── paryty/v1/          # Service and message definitions
├── deploy/                 # Deployment configurations
│   ├── compose/            # Podman Compose files
│   ├── helm/               # Helm charts
│   ├── kubernetes/         # Raw K8s manifests
│   └── docker/             # Dockerfiles
├── configs/                # Configuration templates
├── scripts/                # Build and deployment scripts
├── docs/                   # Documentation
├── tests/                  # Integration tests
├── .env.dev                # Development environment config
├── buf.yaml                # Buf protobuf config
└── buf.gen.yaml            # Buf code generation config
```

---

## Configuration

### Environment Variables

All port mappings and endpoints are defined in `.env.dev`. This is the **single source of truth** for local development.

Key variables:
- `PARYTY_PORT_QUERY` — Query service HTTP port (default: 8080)
- `PARYTY_PORT_INGESTION` — Ingestion gRPC port (default: 50052)
- `PARYTY_PORT_INTELLIGENCE` — Intelligence gRPC port (default: 50051)
- `PARYTY_PORT_FRONTEND` — Vite dev server port (default: 5173)

### Cluster Config

Cluster configuration lives in `configs/cluster/cluster.yaml`. Key sections:
- Redpanda broker addresses
- Storage tier endpoints
- Processing pipeline settings
- Tenant configuration

---

## Development Workflow

### Making Changes

1. **Create a branch:** `git checkout -b feat/your-feature`
2. **Make changes** in the relevant component
3. **Run verification:**
   - Rust: `cargo build && cargo clippy -- -D warnings && cargo test`
   - Go: `go build ./... && go vet ./... && go test -race -count=1 ./...`
   - TypeScript: `npx tsc --noEmit && npm run build && npx vitest run`
   - Python: `python -m py_compile *.py && mypy --strict . && pytest -v`
4. **Commit:** `git commit -m "feat: your feature description"`
5. **Push & PR:** `git push origin feat/your-feature`

### Protobuf Changes

If you modify `.proto` files:

```bash
# Generate code for all languages
buf generate

# Verify Rust codegen
cd agent && cargo build

# Verify Go codegen
cd cluster && go build ./...

# Verify TypeScript codegen
cd frontend && npm run build
```

---

## Testing

### Unit Tests

```bash
# Rust
cd agent && cargo test

# Go
cd cluster && go test -race -count=1 ./...

# TypeScript
cd frontend && npx vitest run

# Python
cd intelligence && pytest -v
```

### Integration Tests

```bash
# Cross-language integration tests
cd tests && go test -tags integration -v ./...
```

### Test Coverage

```bash
# Go
cd cluster && go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Rust
cd agent && cargo tarpaulin --out Html

# TypeScript
cd frontend && npx vitest run --coverage
```

---

## Debugging

### Go Services

```bash
# Run with Delve debugger
cd cluster
dlv debug ./cmd/query/ -- --config=../configs/cluster/cluster.yaml

# Enable race detector
go test -race ./...
```

### Rust Agent

```bash
# Run with backtrace
cd agent
RUST_BACKTRACE=1 cargo run

# Run with logging
RUST_LOG=debug cargo run
```

### Frontend

- Open Chrome DevTools → Sources tab
- React DevTools extension for component inspection
- Redux/Zustand DevTools for state inspection

---

## Coding Standards

Each language has a detailed coding bible. Key highlights:

| Language | Linter | Formatter | Style Guide |
|----------|--------|-----------|-------------|
| Rust | `cargo clippy` | `rustfmt` | Rust API Guidelines |
| Go | `golangci-lint` | `gofmt` | Google Go Style |
| TypeScript | `eslint` | Prettier | Google TS Style |
| Python | `ruff` | `ruff format` | Google Python Style |
| C (eBPF) | `-Wall -Wextra -Werror` | `clang-format` | NASA JPL Power of 10 |

---

## Architecture Overview

Paryty has a four-component architecture:

1. **Agent** (Rust) — Collects metrics via metal scraping, eBPF, and application SDK. Streams to cluster via gRPC.
2. **Cluster** (Go) — Ingests, processes, stores, and serves observability data. Uses Redpanda for streaming, tiered storage (Dragonfly/QuestDB/SeaweedFS).
3. **Frontend** (TypeScript + React + PixiJS) — GPU-accelerated topology visualization with real-time WebSocket updates.
4. **Intelligence Layer** (Python) — ML-powered forecasting, anomaly detection, and simulation.

See `docs/architecture/ARCHITECTURE.md` for the full architecture document.

---

## Key Contacts & Resources

- **Architecture docs:** `docs/architecture/`
- **Phase specs:** `docs/development/phase-*-hardened-spec.md`
- **API reference:** `docs/api/README.md`
- **Operations runbook:** `docs/operations/RUNBOOK.md`
- **Locked decisions:** `.qoder/rules/locked-decisions.md`
