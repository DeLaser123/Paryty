<#
.SYNOPSIS
    Paryty load test orchestration script.
    Starts the Paryty cluster, runs baseline/burst/scale scenarios,
    collects results, and reports pass/fail against performance targets.

.DESCRIPTION
    Target budgets:
        - p99 ingestion latency: < 100ms
        - error rate:            < 0.1%

    Scenarios:
        baseline  - 100 agents, 5 minutes
        burst     - 1000 agents, 2 minutes
        scale     - 10000 agents, 10 minutes

.PARAMETER SkipClusterStart
    Skip starting the cluster (useful when the cluster is already running).

.PARAMETER ResultsFile
    Path to write the JSON results file. Defaults to "load_test_results.json"
    in the current directory.

.PARAMETER IngestionAddr
    Address of the ingestion service. Defaults to "localhost:8080".

.EXAMPLE
    .\run_all.ps1
    .\run_all.ps1 -SkipClusterStart -ResultsFile "custom_results.json"
    .\run_all.ps1 -IngestionAddr "paryty-ingestion:8080"
#>

param(
    [switch]$SkipClusterStart,
    [string]$ResultsFile = "load_test_results.json",
    [string]$IngestionAddr = "localhost:8080"
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Resolve-Path "$ScriptDir\..\.."
$ResultsPath = Join-Path (Get-Location) $ResultsFile

# ─── Performance targets ──────────────────────────────────────────
$Targets = @{
    IngestionLatencyP99Ms = 100
    ErrorRatePercent      = 0.1
}

# ─── Helper: Write colored output ──────────────────────────────────
function Write-Phase {
    param([string]$Message, [string]$Color = "Cyan")
    Write-Host "`n============================================================" -ForegroundColor $Color
    Write-Host "  $Message" -ForegroundColor $Color
    Write-Host "============================================================" -ForegroundColor $Color
}

function Write-Step {
    param([string]$Message)
    Write-Host "  → $Message" -ForegroundColor Yellow
}

function Write-Pass {
    param([string]$Message)
    Write-Host "  ✓ PASS: $Message" -ForegroundColor Green
}

function Write-Fail {
    param([string]$Message)
    Write-Host "  ✗ FAIL: $Message" -ForegroundColor Red
}

# ─── Step 0: Start cluster (unless skipped) ────────────────────────
if (-not $SkipClusterStart) {
    Write-Phase "Starting Paryty Cluster" -Color Cyan

    $ComposeFile = Join-Path $ProjectRoot "deploy\docker\docker-compose.yaml"
    if (-not (Test-Path $ComposeFile)) {
        $ComposeFile = Join-Path $ProjectRoot "deploy\docker\docker-compose.yml"
    }

    if (Test-Path $ComposeFile) {
        Write-Step "Starting services via docker-compose..."
        docker compose -f $ComposeFile up -d 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) {
            Write-Host "  WARNING: docker-compose may not be available. Attempting podman..."
            podman-compose -f $ComposeFile up -d 2>&1 | Out-Null
            if ($LASTEXITCODE -ne 0) {
                Write-Fail "Could not start cluster. Ensure docker-compose or podman-compose is installed."
                Write-Host "  Retry with -SkipClusterStart if cluster is already running."
                exit 1
            }
        }

        Write-Step "Waiting for services to be healthy (60s timeout)..."
        $healthy = $false
        for ($i = 0; $i -lt 60; $i++) {
            try {
                $response = Invoke-WebRequest -Uri "http://$IngestionAddr/healthz" -TimeoutSec 2 -ErrorAction SilentlyContinue
                if ($response.StatusCode -eq 200) {
                    $healthy = $true
                    Write-Pass "Cluster is healthy"
                    break
                }
            } catch {
                # Not ready yet
            }
            Start-Sleep -Seconds 1
        }
        if (-not $healthy) {
            Write-Fail "Cluster health check timed out"
            exit 1
        }
    } else {
        Write-Host "  WARNING: No docker-compose file found at $ComposeFile"
        Write-Host "  Proceeding with assumption that cluster is already running..."
    }
} else {
    Write-Host "Skipping cluster start (--SkipClusterStart specified)"
}

# ─── Helper: Run a single scenario via go test ─────────────────────
function Invoke-Scenario {
    param(
        [string]$ScenarioFile,
        [string]$Name,
        [int]$DurationSeconds,
        [int]$NumAgents
    )

    Write-Phase "Running scenario: $Name ($NumAgents agents)" -Color Magenta

    $testName = switch ($Name) {
        "baseline" { "TestLoadBaseline" }
        "burst"    { "TestLoadBurst" }
        "scale"    { "TestLoadScale" }
        default    { "TestLoadBaseline" }
    }

    $startTime = Get-Date

    # Run the go test for this specific scenario
    Push-Location "$ScriptDir"
    try {
        $output = go test -v -run "^$testName$" -count=1 -timeout "$($DurationSeconds + 120)s" . 2>&1
        $exitCode = $LASTEXITCODE
    } finally {
        Pop-Location
    }

    $endTime = Get-Date
    $elapsed = ($endTime - $startTime).TotalSeconds

    # Parse results from go test output
    $totalRequests = 0
    $successCount = 0
    $errorCount = 0
    $avgLatency = 0
    $p50Latency = 0
    $p90Latency = 0
    $p99Latency = 0
    $maxLatency = 0
    $minLatency = 0
    $errorRate = 0.0

    # Parse structured output from run_test.go printResults / validateResults
    foreach ($line in $output) {
        if ($line -match "Total Requests:\s+(\d+)") { $totalRequests = [int]$Matches[1] }
        if ($line -match "Success:\s+(\d+)") { $successCount = [int]$Matches[1] }
        if ($line -match "Errors:\s+(\d+)") { $errorCount = [int]$Matches[1] }
        if ($line -match "Error Rate:\s+([\d.]+)%") { $errorRate = [double]$Matches[1] }
        if ($line -match "Average Latency:\s+(\d+)ms") { $avgLatency = [int]$Matches[1] }
        if ($line -match "Min Latency:\s+(\d+)ms") { $minLatency = [int]$Matches[1] }
        if ($line -match "Max Latency:\s+(\d+)ms") { $maxLatency = [int]$Matches[1] }
        if ($line -match "P50 Latency:\s+(\d+)ms") { $p50Latency = [int]$Matches[1] }
        if ($line -match "P90 Latency:\s+(\d+)ms") { $p90Latency = [int]$Matches[1] }
        if ($line -match "P99 Latency:\s+(\d+)ms") { $p99Latency = [int]$Matches[1] }
    }

    $passed = $exitCode -eq 0

    # Evaluate against targets
    $latencyPass = $p99Latency -le $Targets.IngestionLatencyP99Ms
    $errorRatePass = $errorRate -le $Targets.ErrorRatePercent

    if ($passed -and $latencyPass -and $errorRatePass) {
        Write-Pass "$Name : p99=$p99Latency ms | error_rate=$errorRate% | requests=$totalRequests"
    } else {
        if (-not $passed) {
            Write-Fail "$Name : go test returned exit code $exitCode"
        }
        if (-not $latencyPass) {
            Write-Fail "$Name : p99 latency $p99Latency ms exceeds target $($Targets.IngestionLatencyP99Ms) ms"
        }
        if (-not $errorRatePass) {
            Write-Fail "$Name : error rate $errorRate% exceeds target $($Targets.ErrorRatePercent)%"
        }
    }

    return @{
        scenario       = $Name
        passed         = ($passed -and $latencyPass -and $errorRatePass)
        elapsed_s      = [math]::Round($elapsed, 1)
        total_requests = $totalRequests
        success_count  = $successCount
        error_count    = $errorCount
        error_rate_pct = $errorRate
        avg_latency_ms = $avgLatency
        min_latency_ms = $minLatency
        max_latency_ms = $maxLatency
        p50_latency_ms = $p50Latency
        p90_latency_ms = $p90Latency
        p99_latency_ms = $p99Latency
        targets        = @{
            p99_latency_ms = $Targets.IngestionLatencyP99Ms
            error_rate_pct = $Targets.ErrorRatePercent
        }
        raw_output     = ($output -join "`n")
    }
}

# ─── Run scenarios ─────────────────────────────────────────────────
$results = @()

# Scenario 1: Baseline (100 agents, 5 min)
$results += Invoke-Scenario -ScenarioFile "baseline.yaml" -Name "baseline" `
    -DurationSeconds 300 -NumAgents 100

# Scenario 2: Burst (1000 agents, 2 min)
$results += Invoke-Scenario -ScenarioFile "burst.yaml" -Name "burst" `
    -DurationSeconds 120 -NumAgents 1000

# Scenario 3: Scale (10000 agents, 10 min)
$results += Invoke-Scenario -ScenarioFile "scale.yaml" -Name "scale" `
    -DurationSeconds 600 -NumAgents 10000

# ─── Generate results JSON ─────────────────────────────────────────
Write-Phase "Generating Results" -Color Cyan

$report = @{
    generated_at   = (Get-Date).ToString("o")
    project        = "paryty"
    ingestion_addr = $IngestionAddr
    targets        = $Targets
    scenarios      = $results | ForEach-Object {
        @{
            scenario       = $_.scenario
            passed         = $_.passed
            elapsed_s      = $_.elapsed_s
            total_requests = $_.total_requests
            success_count  = $_.success_count
            error_count    = $_.error_count
            error_rate_pct = $_.error_rate_pct
            avg_latency_ms = $_.avg_latency_ms
            min_latency_ms = $_.min_latency_ms
            max_latency_ms = $_.max_latency_ms
            p50_latency_ms = $_.p50_latency_ms
            p90_latency_ms = $_.p90_latency_ms
            p99_latency_ms = $_.p99_latency_ms
        }
    }
    overall_passed = ($results | Where-Object { -not $_.passed }).Count -eq 0
}

$report | ConvertTo-Json -Depth 5 | Set-Content -Path $ResultsPath -Encoding UTF8
Write-Step "Results written to: $ResultsPath"

# ─── Summary ───────────────────────────────────────────────────────
Write-Phase "Load Test Summary" -Color $(if ($report.overall_passed) { "Green" } else { "Red" })

foreach ($r in $results) {
    $status = if ($r.passed) { "✓ PASS" } else { "✗ FAIL" }
    $color = if ($r.passed) { "Green" } else { "Red" }
    Write-Host "  $status  $($r.scenario)" -ForegroundColor $color
    Write-Host "         p99=$($r.p99_latency_ms)ms  err_rate=$($r.error_rate_pct)%  requests=$($r.total_requests)"
}

Write-Host ""
if ($report.overall_passed) {
    Write-Host "  ALL SCENARIOS PASSED" -ForegroundColor Green
    exit 0
} else {
    Write-Host "  SOME SCENARIOS FAILED — see $ResultsPath for details" -ForegroundColor Red
    exit 1
}
