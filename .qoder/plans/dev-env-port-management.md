# Plan: Paryty Local Development Environment — Port Management & Service Orchestration

## Context

The Paryty local dev environment suffers from recurring port conflicts and endpoint confusion:
- 11 agent config files use **5 different IP addresses** for the same service
- Port 8080 is claimed by both SeaweedFS Volume and the Query service
- No single source of truth for port assignments — ports are hardcoded in compose files, agent configs, frontend `.env`, and Go configs
- WSL2 networking quirks mean `localhost` doesn't reliably forward to Podman container ports

The goal: a permanent, centralized solution inspired by Google's approach — **one env file to rule them all** — with startup validation and automatic network-context detection.

## Solution

### 1. Create `.env.dev` — Single Source of Truth

**File:** `paryty-v1.0/.env.dev` (CREATE)

Centralizes ALL port assignments and derived URLs. Every consumer reads from this file.

**Port Assignment Table (Final):**

| Port | Service | Notes |
|------|---------|-------|
| 5432 | PostgreSQL | Control plane DB |
| 6379 | Dragonfly | Redis-compatible cache |
| 8080 | Paryty Query | REST/WS/SSE — **was 8082, aligns with vite.config.ts default** |
| 8333 | SeaweedFS S3 | S3-compatible object API |
| 8812 | QuestDB PG Wire | |
| 8888 | SeaweedFS Filer | |
| 9000 | QuestDB Web | |
| 9009 | QuestDB ILP | |
| 9092 | Redpanda Kafka | |
| 9100+ | Agent Self-Metrics | Prometheus scrape |
| 9333 | SeaweedFS Master | |
| 9644 | Redpanda Admin | |
| 18080 | SeaweedFS Volume | **was 8080, moved to free 8080 for Query** |
| 50051 | Paryty Intelligence | gRPC |
| 50052 | Paryty Ingestion | gRPC |
| 5173 | Paryty Frontend | Vite dev server |

**Key changes:**
- Query service moves from host port 8082 to **8080** (matches `vite.config.ts` line 8 default)
- SeaweedFS Volume moves from host port 8080 to **18080** (avoids conflict, consistent with its gRPC port 18080)

### 2. Create `start-dev.ps1` — Startup with Port Validation

**File:** `paryty-v1.0/scripts/dev/start-dev.ps1` (CREATE)

Single entry point that:
1. Sources `.env.dev` into process environment
2. Auto-detects network context (Windows host / WSL2 / Podman VM)
3. Validates all required ports are free (TCP connect test)
4. Starts containers via `podman-compose --env-file .env.dev`
5. Waits for health checks
6. Prints service map and agent launch instructions

### 3. Update Compose Files — Variable Substitution

**Files:**
- `paryty-v1.0/deploy/compose/docker-compose.dev.yaml` (MODIFY)
- `paryty-v1.0/deploy/compose/docker-compose.infra.yaml` (MODIFY)

Replace all hardcoded port mappings with `${PARYTY_PORT_*}` variables. Example:
```yaml
# Before:
ports: ["8082:8080"]
# After:
ports: ["${PARYTY_PORT_QUERY}:8080"]
```

Container-internal addresses (e.g., `redpanda:9092`) stay unchanged — they're resolved by Podman DNS.

### 4. Update Frontend `.env`

**File:** `paryty-v1.0/frontend/.env` (MODIFY)

```env
VITE_API_URL=http://127.0.0.1:8080
VITE_WS_URL=ws://127.0.0.1:8080
```

Aligns with `vite.config.ts` defaults (port 8080 instead of 8082).

### 5. Agent Config Strategy — No File Changes

The Rust agent already supports `PARYTY_CLUSTER_ENDPOINT` env var override (config/mod.rs:320). The startup script sets this automatically based on detected network context. The 11 agent config files become documentation-only artifacts — the env var always wins.

**Network Context Resolution:**
- Windows host agent → `127.0.0.1:50052`
- WSL2 agent → `<gateway-ip>:50052` (auto-detected)
- Podman VM agent → `<vm-ip>:50052` (auto-detected)

### 6. Update `cluster.yaml` Defaults

**File:** `paryty-v1.0/configs/cluster/cluster.yaml` (MODIFY)

Change `localhost:` to `127.0.0.1:` in storage addresses (lines 27, 179, 201, 202, 256). This prevents IPv6 resolution issues. These values are only used when running services outside containers — the compose env vars override them.

## Files Summary

| Action | File | Purpose |
|--------|------|---------|
| CREATE | `paryty-v1.0/.env.dev` | Single source of truth for all ports/endpoints |
| CREATE | `paryty-v1.0/scripts/dev/start-dev.ps1` | Startup validation + auto-detection |
| MODIFY | `paryty-v1.0/deploy/compose/docker-compose.dev.yaml` | Variable substitution for ports |
| MODIFY | `paryty-v1.0/deploy/compose/docker-compose.infra.yaml` | Same |
| MODIFY | `paryty-v1.0/frontend/.env` | Align port to 8080 |
| MODIFY | `paryty-v1.0/configs/cluster/cluster.yaml` | `localhost` → `127.0.0.1` |

## What Does NOT Change
- Agent YAML configs (11 files) — env var override takes precedence
- Go config package — already supports env overrides
- Rust agent config — already supports env overrides
- `vite.config.ts` — already defaults to 127.0.0.1:8080

## Verification
1. Run `.\scripts\dev\start-dev.ps1` — ports validated, containers started
2. All 14 containers healthy (podman ps)
3. `curl http://127.0.0.1:8080/health` returns query service health
4. `curl http://127.0.0.1:5173` returns frontend HTML
5. `curl http://127.0.0.1:9000` returns QuestDB web console
6. Start Windows agent: `$env:PARYTY_CLUSTER_ENDPOINT="127.0.0.1:50052"; .\paryty-agent.exe -c agent.yaml` — connects and registers
7. Start WSL2 agent: `PARYTY_CLUSTER_ENDPOINT="<gateway-ip>:50052" ./paryty-agent -c agent.yaml` — connects and registers
