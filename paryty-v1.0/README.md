# Paryty v1.0

**A Distributed Observability Operating System**

Paryty is a next-generation observability platform that provides digital twin capabilities for software applications.

## Key Features

- **GPU-Accelerated Visualization** � PixiJS-powered topology maps with particle animations
- **Real-Time Data Flow** � See data flowing through your system in real-time
- **7-Day Forecasting** � Predictive analytics for capacity planning
- **Timeline Replay** � Go back in time and replay system states
- **Simulation Drills** � Test what-if scenarios before they happen
- **Self-Hostable** � Deploy on your own infrastructure or use our managed service

## Project Structure

- **agent/** � Paryty Agent (Rust + Go SDK)
- **cluster/** � Paryty Cluster (Go services)
- **frontend/** � Paryty Frontend (React + PixiJS)
- **docs/** � Documentation
- **configs/** � Configuration templates
- **scripts/** � Build and deployment scripts
- **deploy/** � Deployment configurations

## Technology Stack

- **Agent:** Rust, gRPC SDK (language-agnostic), eBPF, Zstd
- **Cluster:** Go (Gin), Redpanda, QuestDB, Dragonfly, SeaweedFS, OpenTelemetry
- **Frontend:** React 18, TypeScript, PixiJS, D3-force, Zustand, Vite
- **Communication:** gRPC + Protobuf (internal), REST + GraphQL + WebSocket + SSE (frontend)
- **Infrastructure:** Podman, Kubernetes (future)

## Live Metrics Dashboard

A PowerShell script that polls QuestDB and displays a real-time summary of all agent metrics.

### Prerequisites

- QuestDB running on `localhost:9000` (default infrastructure setup)
- At least one Paryty Agent running and sending data

### Usage

From PowerShell, run:

```powershell
cd D:\__Projects\Paryty\paryty-v1.0
powershell -ExecutionPolicy Bypass -File .\scripts\dashboard.ps1
```

The dashboard refreshes every 10 seconds and shows:

- **CPU usage** per agent with color-coded bars (green < 50%, yellow < 80%, red >= 80%)
- **Memory usage** with actual GB/MB values
- **Data flow table** — row counts per metric table (cpu, memory, disk, network, process) per agent
- **Process metrics** — highlighted in yellow when zero (expected on WSL2 due to `/proc` limitations)

Press `Ctrl+C` to exit.

### What the dashboard looks like

```
============================================================
  PARYTY LIVE METRICS DASHBOARD
  Refreshed: 16:13:09  (Ctrl+C to exit)
============================================================

  Agent: 22c19aee
    CPU:  [|||||||             ] 34.9%  load: 0.0
    MEM:  [                    ] 0%  (0 B / 0 B)

  Agent: a83524ce
    CPU:  [|                   ] 3.4%  load: 0.15
    MEM:  [|||                 ] 17.1%  (1.3 GB / 7.8 GB)

  Data Flow (rows in last 2 min):
  ---------------------------------------------------------
  Agent             CPU   Memory     Disk  Network  Process
  ---------------------------------------------------------
  22c19aee           12       12       24       24     5045
  a83524ce           12       12       60      120        0
```

### Troubleshooting

- **"No agent data available"** — Ensure QuestDB is running on port 9000 and at least one agent is connected.
- **Process = 0 for an agent** — Expected on WSL2. The agent auto-detects WSL2 and uses a fallback collection strategy that skips process metrics due to `/proc` limitations. On real Linux or Windows, process metrics are collected normally.
- **Memory shows 0 B for Windows agent** — The Windows agent uses a different memory reporting path via `sysinfo`. The data is still being collected; this is a display formatting issue in the dashboard query.

## License

Proprietary � Paryty Inc.
