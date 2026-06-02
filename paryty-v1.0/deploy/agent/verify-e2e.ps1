# Paryty Phase 1 — End-to-End Verification Script
#
# Run this on your laptop AFTER:
#   1. podman-compose infrastructure is up
#   2. ingestion service is running
#   3. agent is running inside the VM
#
# Usage:
#   powershell -File deploy/agent/verify-e2e.ps1
#   # or with custom VM agent IP:
#   powershell -File deploy/agent/verify-e2e.ps1 -AgentSelfMetrics "192.168.1.50:9100"

param(
    [string]$IngestionHost = "localhost",
    [int]$IngestionPort = 50051,
    [string]$DragonflyHost = "localhost",
    [int]$DragonflyPort = 6379,
    [string]$QuestDBPGHost = "localhost",
    [int]$QuestDBPGPort = 8812,
    [string]$QuestDBILPHost = "localhost",
    [int]$QuestDBILPPort = 9009,
    [string]$RedpandaHost = "localhost",
    [int]$RedpandaPort = 9092,
    [string]$AgentSelfMetrics = "localhost:9100"
)

$pass = 0
$fail = 0
$warn = 0

function Check {
    param([string]$Name, [scriptblock]$Test, [string]$Fix = "")
    try {
        $result = & $Test
        if ($result) {
            Write-Host "  [PASS] $Name" -ForegroundColor Green
            $script:pass++
        } else {
            Write-Host "  [FAIL] $Name" -ForegroundColor Red
            if ($Fix) { Write-Host "         Fix: $Fix" -ForegroundColor Yellow }
            $script:fail++
        }
    } catch {
        Write-Host "  [FAIL] $Name - $($_.Exception.Message)" -ForegroundColor Red
        if ($Fix) { Write-Host "         Fix: $Fix" -ForegroundColor Yellow }
        $script:fail++
    }
}

function Warn {
    param([string]$Name, [string]$Message)
    Write-Host "  [WARN] $Name - $Message" -ForegroundColor Yellow
    $script:warn++
}

Write-Host ""
Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  Paryty Phase 1 - End-to-End Verification" -ForegroundColor Cyan
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""

# ── Layer 1: Infrastructure Services ──────────────────────────────
Write-Host "--- Infrastructure Services ---" -ForegroundColor White

Check "Redpanda (Kafka) reachable on ${RedpandaHost}:${RedpandaPort}" {
    $tcp = New-Object System.Net.Sockets.TcpClient
    $tcp.Connect($RedpandaHost, $RedpandaPort)
    $tcp.Close()
    $true
} "Start with: podman-compose -f deploy/compose/docker-compose.dev.yaml up -d"

Check "Dragonfly (Redis) reachable on ${DragonflyHost}:${DragonflyPort}" {
    $tcp = New-Object System.Net.Sockets.TcpClient
    $tcp.Connect($DragonflyHost, $DragonflyPort)
    $tcp.Close()
    $true
} "Start with: podman-compose -f deploy/compose/docker-compose.dev.yaml up -d"

Check "QuestDB PG port reachable on ${QuestDBPGHost}:${QuestDBPGPort}" {
    $tcp = New-Object System.Net.Sockets.TcpClient
    $tcp.Connect($QuestDBPGHost, $QuestDBPGPort)
    $tcp.Close()
    $true
} "Start with: podman-compose -f deploy/compose/docker-compose.dev.yaml up -d"

Check "QuestDB ILP port reachable on ${QuestDBILPHost}:${QuestDBILPPort}" {
    $tcp = New-Object System.Net.Sockets.TcpClient
    $tcp.Connect($QuestDBILPHost, $QuestDBILPPort)
    $tcp.Close()
    $true
} "Start with: podman-compose -f deploy/compose/docker-compose.dev.yaml up -d"

Write-Host ""

# ── Layer 2: Cluster Ingestion Service ────────────────────────────
Write-Host "--- Cluster Ingestion Service ---" -ForegroundColor White

Check "Ingestion gRPC port reachable on ${IngestionHost}:${IngestionPort}" {
    $tcp = New-Object System.Net.Sockets.TcpClient
    $tcp.Connect($IngestionHost, $IngestionPort)
    $tcp.Close()
    $true
} "Start with: cd cluster && go run ./cmd/ingestion/ -config ../configs/cluster/cluster.yaml"

Write-Host ""

# ── Layer 3: Agent (VM) ──────────────────────────────────────────
Write-Host "--- Agent (VM Node) ---" -ForegroundColor White

Check "Agent self-metrics endpoint reachable on ${AgentSelfMetrics}" {
    $tcp = New-Object System.Net.Sockets.TcpClient
    $parts = $AgentSelfMetrics.Split(":")
    $tcp.Connect($parts[0], [int]$parts[1])
    $stream = $tcp.GetStream()
    $writer = New-Object System.IO.StreamWriter($stream)
    $writer.WriteLine("GET / HTTP/1.0`r`nHost: localhost`r`n`r`n")
    $writer.Flush()
    $reader = New-Object System.IO.StreamReader($stream)
    $response = $reader.ReadLine()
    $tcp.Close()
    $response -match "200 OK"
} "Check VM agent is running: systemctl status paryty-agent"

Write-Host ""

# ── Layer 4: Data Flow Verification ──────────────────────────────
Write-Host "--- Data Flow (requires redis-cli and psql) ---" -ForegroundColor White

# Check if redis-cli is available
$hasRedisCli = $null -ne (Get-Command redis-cli -ErrorAction SilentlyContinue)
if ($hasRedisCli) {
    Check "Dragonfly has agent metrics (paryty:*:metrics:*:latest)" {
        $keys = redis-cli -h $DragonflyHost -p $DragonflyPort KEYS "paryty:*:metrics:*:latest" 2>$null
        $keys.Count -gt 0
    } "Agent may not have connected yet. Check agent logs: journalctl -u paryty-agent -f"
} else {
    Warn "redis-cli not found" "Install redis-cli to verify Dragonfly data, or check manually: podman exec <dragonfly-container> redis-cli KEYS 'paryty:*'"
}

# Check if psql is available
$hasPsql = $null -ne (Get-Command psql -ErrorAction SilentlyContinue)
if ($hasPsql) {
    Check "QuestDB has CPU metrics" {
        $result = psql -h $QuestDBPGHost -p $QuestDBPGPort -U admin -d paryty -t -c "SELECT count(*) FROM cpu_metrics" 2>$null
        [int]$result.Trim() -gt 0
    } "Agent may not have sent data yet, or ILP ingestion failed. Check ingestion logs."

    Check "QuestDB has memory metrics" {
        $result = psql -h $QuestDBPGHost -p $QuestDBPGPort -U admin -d paryty -t -c "SELECT count(*) FROM memory_metrics" 2>$null
        [int]$result.Trim() -gt 0
    } "Check agent logs for memory collector errors."
} else {
    Warn "psql not found" "Install psql to verify QuestDB data, or check QuestDB web console at http://localhost:9000"
}

Write-Host ""

# ── Summary ───────────────────────────────────────────────────────
Write-Host "============================================" -ForegroundColor Cyan
Write-Host "  Results: $pass passed, $fail failed, $warn warnings" -ForegroundColor $(if ($fail -gt 0) { "Red" } else { "Green" })
Write-Host "============================================" -ForegroundColor Cyan
Write-Host ""

if ($fail -gt 0) {
    Write-Host "Some checks failed. Review the fixes above." -ForegroundColor Red
    exit 1
} else {
    Write-Host "All checks passed. Phase 1 end-to-end flow verified." -ForegroundColor Green
    exit 0
}
