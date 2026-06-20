<#
.SYNOPSIS
    Paryty Development Environment Startup Script
.DESCRIPTION
    Single entry point for local development. Sources .env.dev as the port
    manifest, validates port availability, detects network context, and
    starts all containers via podman-compose.
.PARAMETER Mode
    "full" (default) starts all 13 services. "infra" starts only infrastructure.
.PARAMETER SkipValidation
    Skip port availability checks.
.PARAMETER DryRun
    Show what would happen without executing.
.EXAMPLE
    .\scripts\dev\start-dev.ps1
    .\scripts\dev\start-dev.ps1 -Mode infra
    .\scripts\dev\start-dev.ps1 -SkipValidation
#>
param(
    [ValidateSet("infra", "full")]
    [string]$Mode = "full",
    [switch]$SkipValidation,
    [switch]$DryRun
)

$ErrorActionPreference = "Stop"
$ProjectRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$EnvFile = Join-Path $ProjectRoot ".env.dev"

# ── Step 1: Load port manifest ──────────────────────────────
if (-not (Test-Path $EnvFile)) {
    Write-Host "ERROR: $EnvFile not found." -ForegroundColor Red
    exit 1
}

$envVars = @{}
Get-Content $EnvFile | ForEach-Object {
    $line = $_.Trim()
    if ($line -and -not $line.StartsWith("#") -and $line -match "^(\w+)=(.*)$") {
        $envVars[$Matches[1]] = $Matches[2]
        [Environment]::SetEnvironmentVariable($Matches[1], $Matches[2], "Process")
    }
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Paryty Dev Environment Startup" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Env file: $EnvFile"
Write-Host "Mode:     $Mode"

# ── Step 2: Auto-detect network context ─────────────────────
$netContext = "windows-host"
$hostIP = $envVars["PARYTY_HOST_IP"]

# Check if Podman machine is running and get its IP
try {
    $machineInspect = podman machine inspect podman-machine-default 2>&1
    if ($machineInspect -match '"CPUs"') {
        $netContext = "podman-vm"
        # Extract Podman VM IP from the network config
        $vmIP = wsl -d podman-machine-default -- bash -c "ip addr show eth0 2>/dev/null | grep 'inet ' | awk '{print `$2}' | cut -d/ -f1" 2>$null
        if ($vmIP -and $vmIP.Trim() -match "^\d+\.\d+\.\d+\.\d+$") {
            $hostIP = $vmIP.Trim()
        }
    }
} catch {
    # Podman machine not available
}

Write-Host "Network:  $netContext"
Write-Host "Host IP:  $hostIP"
Write-Host ""

# Update the derived variables with detected host IP
$clusterEndpoint = "${hostIP}:$($envVars['PARYTY_PORT_INGESTION'])"
[Environment]::SetEnvironmentVariable("PARYTY_HOST_IP", $hostIP, "Process")
[Environment]::SetEnvironmentVariable("PARYTY_CLUSTER_ENDPOINT", $clusterEndpoint, "Process")

# ── Step 3: Ensure Podman machine is running ────────────────
try {
    $psOutput = podman ps 2>&1
    if ($LASTEXITCODE -ne 0) {
        Write-Host "Starting Podman machine..." -ForegroundColor Yellow
        podman machine start podman-machine-default 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) {
            Write-Host "ERROR: Failed to start Podman machine." -ForegroundColor Red
            exit 1
        }
        Write-Host "  Podman machine started" -ForegroundColor Green
    }
} catch {
    Write-Host "ERROR: Podman not available. Install with: winget install RedHat.Podman" -ForegroundColor Red
    exit 1
}

# ── Step 4: Port validation ─────────────────────────────────
if (-not $SkipValidation) {
    Write-Host "--- Port Validation ---" -ForegroundColor Yellow

    $requiredPorts = @(
        @{Name="Redpanda Kafka";    Port=[int]$envVars['PARYTY_PORT_REDPANDA_KAFKA']},
        @{Name="Redpanda Admin";    Port=[int]$envVars['PARYTY_PORT_REDPANDA_ADMIN']},
        @{Name="Dragonfly";         Port=[int]$envVars['PARYTY_PORT_DRAGONFLY']},
        @{Name="QuestDB Web";       Port=[int]$envVars['PARYTY_PORT_QUESTDB_WEB']},
        @{Name="QuestDB ILP";       Port=[int]$envVars['PARYTY_PORT_QUESTDB_ILP']},
        @{Name="QuestDB PG";        Port=[int]$envVars['PARYTY_PORT_QUESTDB_PG']},
        @{Name="PostgreSQL";        Port=[int]$envVars['PARYTY_PORT_POSTGRES']},
        @{Name="SeaweedFS Master";  Port=[int]$envVars['PARYTY_PORT_SWFS_MASTER']},
        @{Name="SeaweedFS Volume";  Port=[int]$envVars['PARYTY_PORT_SWFS_VOLUME']},
        @{Name="SeaweedFS Filer";   Port=[int]$envVars['PARYTY_PORT_SWFS_FILER']},
        @{Name="SeaweedFS S3";      Port=[int]$envVars['PARYTY_PORT_SWFS_S3']},
        @{Name="Ingestion gRPC";    Port=[int]$envVars['PARYTY_PORT_INGESTION']},
        @{Name="Query HTTP";        Port=[int]$envVars['PARYTY_PORT_QUERY']},
        @{Name="Intelligence gRPC"; Port=[int]$envVars['PARYTY_PORT_INTELLIGENCE']},
        @{Name="Frontend";          Port=[int]$envVars['PARYTY_PORT_FRONTEND']}
    )

    $conflicts = @()
    foreach ($entry in $requiredPorts) {
        $port = $entry.Port
        $listener = netstat -ano 2>$null | Select-String ":$port\s.*LISTENING"
        if ($listener) {
            $conflicts += "$($entry.Name) (port $port) is already in use"
        }
    }

    if ($conflicts.Count -gt 0) {
        Write-Host "  PORT CONFLICTS:" -ForegroundColor Red
        $conflicts | ForEach-Object { Write-Host "    - $_" -ForegroundColor Red }
        Write-Host "  Resolve conflicts or use -SkipValidation to proceed anyway." -ForegroundColor Yellow
        exit 1
    }
    Write-Host "  All $($requiredPorts.Count) ports available" -ForegroundColor Green
    Write-Host ""
}

# ── Step 5: Start services ──────────────────────────────────
$composeDir = Join-Path (Join-Path $ProjectRoot "deploy") "compose"
$composeFile = if ($Mode -eq "full") {
    Join-Path $composeDir "docker-compose.dev.yaml"
} else {
    Join-Path $composeDir "docker-compose.infra.yaml"
}

if ($DryRun) {
    Write-Host "[DryRun] Would run:" -ForegroundColor Yellow
    Write-Host "  cd $composeDir"
    Write-Host "  podman-compose --env-file $EnvFile -f $composeFile up -d"
    exit 0
}

Write-Host "--- Starting $Mode services ---" -ForegroundColor Yellow
Push-Location $composeDir
try {
    podman-compose --env-file $EnvFile -f $composeFile up -d 2>&1
    if ($LASTEXITCODE -ne 0) {
        Write-Host "WARNING: Some containers may have failed to start. Check 'podman ps -a'." -ForegroundColor Yellow
    }
} finally {
    Pop-Location
}

# ── Step 6: Wait for services ───────────────────────────────
Write-Host ""
Write-Host "--- Waiting for services (30s) ---" -ForegroundColor Yellow
Start-Sleep -Seconds 30

# ── Step 7: Health checks ───────────────────────────────────
Write-Host ""
Write-Host "--- Health Checks ---" -ForegroundColor Yellow

$checks = @(
    @{Name="Query API"; Url="http://127.0.0.1:$($envVars['PARYTY_PORT_QUERY'])/health"},
    @{Name="QuestDB";   Url="http://127.0.0.1:$($envVars['PARYTY_PORT_QUESTDB_WEB'])/"},
    @{Name="PostgreSQL"; Url="tcp://127.0.0.1:$($envVars['PARYTY_PORT_POSTGRES'])"}
)

foreach ($check in $checks) {
    try {
        if ($check.Url.StartsWith("tcp://")) {
            $port = [int]($check.Url -replace ".*:", "" -replace "/", "")
            $tcp = New-Object System.Net.Sockets.TcpClient
            $tcp.Connect("127.0.0.1", $port)
            $tcp.Close()
            Write-Host "  $($check.Name): OK" -ForegroundColor Green
        } else {
            $response = Invoke-WebRequest -Uri $check.Url -UseBasicParsing -TimeoutSec 5 -ErrorAction Stop
            Write-Host "  $($check.Name): OK ($($response.StatusCode))" -ForegroundColor Green
        }
    } catch {
        Write-Host "  $($check.Name): NOT READY" -ForegroundColor Yellow
    }
}

# ── Step 8: Print service map ───────────────────────────────
Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Paryty Service Map" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "  Infrastructure:" -ForegroundColor White
Write-Host "    Redpanda Kafka:     127.0.0.1:$($envVars['PARYTY_PORT_REDPANDA_KAFKA'])"
Write-Host "    Redpanda Admin:     127.0.0.1:$($envVars['PARYTY_PORT_REDPANDA_ADMIN'])"
Write-Host "    Dragonfly:          127.0.0.1:$($envVars['PARYTY_PORT_DRAGONFLY'])"
Write-Host "    QuestDB Web:        127.0.0.1:$($envVars['PARYTY_PORT_QUESTDB_WEB'])"
Write-Host "    QuestDB ILP:        127.0.0.1:$($envVars['PARYTY_PORT_QUESTDB_ILP'])"
Write-Host "    QuestDB PG:         127.0.0.1:$($envVars['PARYTY_PORT_QUESTDB_PG'])"
Write-Host "    PostgreSQL:         127.0.0.1:$($envVars['PARYTY_PORT_POSTGRES'])"
Write-Host "    SeaweedFS Master:   127.0.0.1:$($envVars['PARYTY_PORT_SWFS_MASTER'])"
Write-Host "    SeaweedFS Volume:   127.0.0.1:$($envVars['PARYTY_PORT_SWFS_VOLUME'])"
Write-Host "    SeaweedFS Filer:    127.0.0.1:$($envVars['PARYTY_PORT_SWFS_FILER'])"
Write-Host "    SeaweedFS S3:       127.0.0.1:$($envVars['PARYTY_PORT_SWFS_S3'])"
Write-Host ""
Write-Host "  Application Services:" -ForegroundColor White
Write-Host "    Ingestion gRPC:     $hostIP`:$($envVars['PARYTY_PORT_INGESTION'])"
Write-Host "    Query HTTP:         127.0.0.1:$($envVars['PARYTY_PORT_QUERY'])"
Write-Host "    Intelligence gRPC:  127.0.0.1:$($envVars['PARYTY_PORT_INTELLIGENCE'])"
Write-Host "    Frontend:           127.0.0.1:$($envVars['PARYTY_PORT_FRONTEND'])"
Write-Host ""
Write-Host "  Agents (run natively on host):" -ForegroundColor White
Write-Host "    Windows: `$env:PARYTY_CLUSTER_ENDPOINT = '$clusterEndpoint'"
Write-Host "             .\paryty-agent.exe -c configs\agent\agent.yaml"
Write-Host "    WSL2:    export PARYTY_CLUSTER_ENDPOINT='$clusterEndpoint'"
Write-Host "             ./paryty-agent -c configs/agent/agent.yaml"
Write-Host ""
