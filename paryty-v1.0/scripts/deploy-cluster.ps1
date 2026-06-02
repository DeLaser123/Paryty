# deploy-cluster.ps1 - Deploy Paryty cluster for testing
#
# Builds and deploys infrastructure + cluster services using Podman.
#
# Usage:
#   .\scripts\deploy-cluster.ps1                    # Full deploy (infra + cluster + frontend)
#   .\scripts\deploy-cluster.ps1 -InfraOnly         # Infrastructure only (Redpanda, Dragonfly, etc.)
#   .\scripts\deploy-cluster.ps1 -SkipBuild         # Skip Docker builds (use cached images)
#   .\scripts\deploy-cluster.ps1 -WithAgent         # Include agent (needs Linux/WSL2)

param(
    [switch]$InfraOnly,
    [switch]$SkipBuild,
    [switch]$WithAgent,
    [string]$ComposeCmd = "podman-compose"
)

$ErrorActionPreference = "Stop"

Write-Host "`n=== Paryty v1.0 Cluster Deployment ===" -ForegroundColor Cyan
Write-Host "Timestamp: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')" -ForegroundColor Gray

$projectRoot = Split-Path -Parent $PSScriptRoot
$composeFile = Join-Path $projectRoot "deploy\compose\docker-compose.dev.yaml"
$infraFile = Join-Path $projectRoot "deploy\compose\docker-compose.infra.yaml"

# Verify compose command exists
try {
    $null = & $ComposeCmd version 2>&1
    Write-Host "Using: $ComposeCmd" -ForegroundColor Gray
} catch {
    Write-Host "ERROR: '$ComposeCmd' not found. Install podman-compose or use docker-compose." -ForegroundColor Red
    exit 1
}

# Step 1: Start infrastructure
Write-Host "`n--- Step 1: Starting infrastructure ---" -ForegroundColor Yellow

if ($InfraOnly) {
    Write-Host "Running: $ComposeCmd -f $infraFile up -d" -ForegroundColor Gray
    & $ComposeCmd -f $infraFile up -d
} else {
    Write-Host "Running: $ComposeCmd -f $composeFile up -d --build" -ForegroundColor Gray
    if ($SkipBuild) {
        & $ComposeCmd -f $composeFile up -d
    } else {
        & $ComposeCmd -f $composeFile up -d --build
    }
}

if ($LASTEXITCODE -ne 0) {
    Write-Host "ERROR: Compose up failed (exit code $LASTEXITCODE)" -ForegroundColor Red
    exit 1
}

# Step 2: Wait for services to be healthy
Write-Host "`n--- Step 2: Waiting for services ---" -ForegroundColor Yellow

$maxWait = 60
$waited = 0

function Wait-ForService {
    param([string]$Name, [string]$Url, [int]$TimeoutSec = 60)
    
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    while ($sw.ElapsedMilliseconds -lt ($TimeoutSec * 1000)) {
        try {
            $response = Invoke-WebRequest -Uri $Url -TimeoutSec 2 -UseBasicParsing -ErrorAction Stop
            if ($response.StatusCode -eq 200) {
                Write-Host "  READY: $Name" -ForegroundColor Green
                return $true
            }
        } catch {}
        Start-Sleep -Seconds 2
    }
    Write-Host "  TIMEOUT: $Name (after ${TimeoutSec}s)" -ForegroundColor Red
    return $false
}

# Wait for infrastructure
Wait-ForService -Name "Redpanda" -Url "http://localhost:9644/v1/status/ready" -TimeoutSec 45
Wait-ForService -Name "QuestDB" -Url "http://localhost:9000/status" -TimeoutSec 30
Wait-ForService -Name "SeaweedFS" -Url "http://localhost:9333/cluster/status" -TimeoutSec 30

if (-not $InfraOnly) {
    # Wait for Paryty services
    Wait-ForService -Name "Ingestion" -Url "http://localhost:8080/health" -TimeoutSec 30
    Wait-ForService -Name "Query" -Url "http://localhost:8082/health" -TimeoutSec 30
    Wait-ForService -Name "Frontend" -Url "http://localhost:3000" -TimeoutSec 30
}

# Step 3: Initialize Redpanda topics
Write-Host "`n--- Step 3: Creating Redpanda topics ---" -ForegroundColor Yellow

$topics = @(
    @{ name = "paryty.metrics.raw"; partitions = 12 },
    @{ name = "paryty.metrics.aggregated"; partitions = 6 },
    @{ name = "paryty.traces"; partitions = 12 },
    @{ name = "paryty.events"; partitions = 6 },
    @{ name = "paryty.network.events"; partitions = 6 },
    @{ name = "paryty.topology.changes"; partitions = 3 },
    @{ name = "paryty.alerts"; partitions = 3 },
    @{ name = "paryty.dead-letter"; partitions = 3 }
)

foreach ($topic in $topics) {
    try {
        # Try using rpk inside the Redpanda container
        $result = & $ComposeCmd exec redpanda rpk topic create $topic.name --partitions $topic.partitions 2>&1
        if ($LASTEXITCODE -eq 0) {
            Write-Host "  PASS: Created topic '$($topic.name)'" -ForegroundColor Green
        } else {
            # Topic may already exist
            Write-Host "  INFO: Topic '$($topic.name)' - $($result | Select-Object -First 1)" -ForegroundColor DarkGray
        }
    } catch {
        Write-Host "  WARN: Could not create topic '$($topic.name)' - $($_.Exception.Message)" -ForegroundColor DarkYellow
    }
}

# Step 4: Inject synthetic data (optional)
if (-not $InfraOnly -and -not $SkipBuild) {
    Write-Host "`n--- Step 4: Injecting synthetic data ---" -ForegroundColor Yellow
    
    $synthDir = Join-Path $projectRoot "tests\synthetic"
    if (Test-Path $synthDir) {
        Write-Host "Running synthetic data generator (steady-state scenario, 3 ticks)..." -ForegroundColor Gray
        Push-Location $synthDir
        try {
            # Run just a few ticks to seed data
            $job = Start-Job -ScriptBlock {
                param($dir)
                Set-Location $dir
                go run . --scenario steady-state --interval 1s --agents 3 2>&1
            } -ArgumentList $synthDir
            
            # Let it run for 30 seconds then stop
            Start-Sleep -Seconds 30
            Stop-Job -Job $job
            $output = Receive-Job -Job $job
            Remove-Job -Job $job
            
            Write-Host "  Synthetic data injected" -ForegroundColor Green
        } catch {
            Write-Host "  WARN: Synthetic data injection failed - $($_.Exception.Message)" -ForegroundColor DarkYellow
        }
        Pop-Location
    }
}

# Summary
Write-Host "`n=== Deployment Complete ===" -ForegroundColor Cyan
Write-Host "Services:" -ForegroundColor White
Write-Host "  Redpanda:      http://localhost:9644" -ForegroundColor Gray
Write-Host "  Dragonfly:     localhost:6379" -ForegroundColor Gray
Write-Host "  QuestDB:       http://localhost:9000" -ForegroundColor Gray
Write-Host "  SeaweedFS:     http://localhost:9333" -ForegroundColor Gray
if (-not $InfraOnly) {
    Write-Host "  Ingestion:     http://localhost:8080" -ForegroundColor Gray
    Write-Host "  Query:         http://localhost:8082" -ForegroundColor Gray
    Write-Host "  Frontend:      http://localhost:3000" -ForegroundColor Gray
}
Write-Host "`nValidate with: .\scripts\validate-pipeline.ps1" -ForegroundColor White
