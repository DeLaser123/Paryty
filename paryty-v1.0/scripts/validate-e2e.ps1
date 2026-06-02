# validate-e2e.ps1 - M10: End-to-End Validation
#
# Final enterprise-grade validation of the complete Paryty v1.0 system.
# Runs all test scenarios and validates the full pipeline.
#
# Prerequisites:
#   - All cluster services running (deploy-cluster.ps1)
#   - Synthetic data generator built (tests/synthetic/)
#   - Frontend accessible at http://localhost:3000
#
# Usage:
#   .\scripts\validate-e2e.ps1                        # Full validation
#   .\scripts\validate-e2e.ps1 -SkipFailover          # Skip failover tests
#   .\scripts\validate-e2e.ps1 -SkipPerformance        # Skip perf baseline

param(
    [switch]$SkipFailover,
    [switch]$SkipPerformance,
    [int]$QueryPort = 8082,
    [int]$IngestionPort = 8080,
    [int]$FrontendPort = 3000
)

$ErrorActionPreference = "Continue"
$projectRoot = Split-Path -Parent $PSScriptRoot

Write-Host "`n=== Paryty v1.0 End-to-End Validation ===" -ForegroundColor Cyan
Write-Host "Timestamp: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')" -ForegroundColor Gray

$results = @{ pass = 0; fail = 0; skip = 0 }

function Assert-True {
    param([string]$Name, [bool]$Condition, [string]$Detail = "")
    if ($Condition) {
        Write-Host "  PASS: $Name" -ForegroundColor Green
        $script:results.pass++
    } else {
        Write-Host "  FAIL: $Name $Detail" -ForegroundColor Red
        $script:results.fail++
    }
}

function Assert-Endpoint {
    param([string]$Name, [string]$Url, [string]$ExpectedField = "")
    try {
        $response = Invoke-RestMethod -Uri $Url -TimeoutSec 5 -ErrorAction Stop
        if ($ExpectedField) {
            $hasField = $null -ne $response.$ExpectedField
            Assert-True -Name $Name -Condition $hasField -Detail "(missing field: $ExpectedField)"
        } else {
            Assert-True -Name $Name -Condition $true
        }
        return $response
    } catch {
        Assert-True -Name $Name -Condition $false -Detail "($($_.Exception.Message))"
        return $null
    }
}

# --- M10.1: Run All Scenarios ---
Write-Host "`n--- M10.1: Test Scenarios ---" -ForegroundColor Yellow

$synthDir = Join-Path $projectRoot "tests\synthetic"
$scenarios = @("steady-state", "cpu-spike", "network-storm", "service-discovery", "alert-trigger")

foreach ($scenario in $scenarios) {
    Write-Host "  Running scenario: $scenario..." -ForegroundColor Gray
    try {
        Push-Location $synthDir
        $output = & go run . --scenario $scenario --interval 500ms --agents 3 --brokers localhost:19092 2>&1
        $exitCode = $LASTEXITCODE
        Pop-Location
        
        # Give the system time to process
        Start-Sleep -Seconds 3
        
        # Check data reached the query API
        $agents = $null
        try {
            $agents = Invoke-RestMethod -Uri "http://localhost:$QueryPort/api/v1/agents" -TimeoutSec 5 -ErrorAction Stop
        } catch {}
        
        $agentCount = if ($agents) { $agents.Count } else { 0 }
        $hasData = $null -ne $agents -and $agentCount -gt 0
        Assert-True -Name "Scenario '$scenario' - data visible via API" -Condition $hasData -Detail "(agents: $agentCount)"
    } catch {
        Pop-Location
        Assert-True -Name "Scenario '$scenario'" -Condition $false -Detail "($($_.Exception.Message))"
    }
}

# --- M10.2: Failover Testing ---
if (-not $SkipFailover) {
    Write-Host "`n--- M10.2: Failover Testing ---" -ForegroundColor Yellow
    
    Write-Host "  NOTE: Failover tests require manual intervention:" -ForegroundColor DarkYellow
    Write-Host "  1. Kill Redpanda: podman stop paryty-redpanda" -ForegroundColor Gray
    Write-Host "  2. Verify agent buffers data (check agent logs)" -ForegroundColor Gray
    Write-Host "  3. Restart Redpanda: podman start paryty-redpanda" -ForegroundColor Gray
    Write-Host "  4. Verify buffered data is replayed" -ForegroundColor Gray
    Write-Host "  5. Kill query service: podman stop paryty-query" -ForegroundColor Gray
    Write-Host "  6. Verify frontend shows 'Disconnected'" -ForegroundColor Gray
    Write-Host "  7. Restart query: podman start paryty-query" -ForegroundColor Gray
    Write-Host "  8. Verify frontend reconnects" -ForegroundColor Gray
    Write-Host ""
    
    $script:results.skip += 4
    Write-Host "  SKIP: Failover tests (manual intervention required)" -ForegroundColor DarkGray
} else {
    Write-Host "`n--- M10.2: Failover Testing --- SKIPPED ---" -ForegroundColor DarkGray
    $script:results.skip += 4
}

# --- M10.3: Performance Baseline ---
if (-not $SkipPerformance) {
    Write-Host "`n--- M10.3: Performance Baseline ---" -ForegroundColor Yellow
    
    # Measure query latency
    $latencies = @()
    for ($i = 0; $i -lt 10; $i++) {
        $sw = [System.Diagnostics.Stopwatch]::StartNew()
        try {
            $null = Invoke-RestMethod -Uri "http://localhost:$QueryPort/api/v1/agents" -TimeoutSec 5 -ErrorAction Stop
            $latencies += $sw.ElapsedMilliseconds
        } catch {
            $latencies += -1
        }
    }
    
    $validLatencies = $latencies | Where-Object { $_ -ge 0 }
    if ($validLatencies.Count -gt 0) {
        $avg = ($validLatencies | Measure-Object -Average).Average
        $min = ($validLatencies | Measure-Object -Minimum).Minimum
        $max = ($validLatencies | Measure-Object -Maximum).Maximum
        
        Write-Host "  Query Latency (10 requests):" -ForegroundColor Gray
        Write-Host "    Avg: $([math]::Round($avg, 1))ms" -ForegroundColor Gray
        Write-Host "    Min: ${min}ms" -ForegroundColor Gray
        Write-Host "    Max: ${max}ms" -ForegroundColor Gray
        
        Assert-True -Name "Query latency < 100ms avg" -Condition ($avg -lt 100) -Detail "(avg: $([math]::Round($avg, 1))ms)"
    } else {
        Assert-True -Name "Query latency measurement" -Condition $false -Detail "(no successful requests)"
    }
    
    # Check service health endpoints
    $services = @(
        @{ Name = "Ingestion"; Url = "http://localhost:$IngestionPort/health" },
        @{ Name = "Query"; Url = "http://localhost:$QueryPort/health" },
        @{ Name = "Frontend"; Url = "http://localhost:$FrontendPort" }
    )
    
    foreach ($svc in $services) {
        try {
            $null = Invoke-WebRequest -Uri $svc.Url -TimeoutSec 3 -UseBasicParsing -ErrorAction Stop
            Assert-True -Name "$($svc.Name) service healthy" -Condition $true
        } catch {
            Assert-True -Name "$($svc.Name) service healthy" -Condition $false -Detail "($($_.Exception.Message))"
        }
    }
} else {
    Write-Host "`n--- M10.3: Performance Baseline --- SKIPPED ---" -ForegroundColor DarkGray
    $script:results.skip += 4
}

# --- M10.4: Final Checks ---
Write-Host "`n--- M10.4: Final Validation ---" -ForegroundColor Yellow

# Check topology has nodes
$topology = Assert-Endpoint -Name "Topology has nodes" -Url "http://localhost:$QueryPort/api/v1/topology" -ExpectedField "nodes"

# Check alerts endpoint
$alerts = Assert-Endpoint -Name "Alerts endpoint responsive" -Url "http://localhost:$QueryPort/api/v1/alerts"

# Check traces endpoint
$traces = Assert-Endpoint -Name "Traces endpoint responsive" -Url "http://localhost:$QueryPort/api/v1/traces"

# Check events endpoint
$events = Assert-Endpoint -Name "Events endpoint responsive" -Url "http://localhost:$QueryPort/api/v1/events"

# --- Summary ---
Write-Host "`n=== End-to-End Validation Summary ===" -ForegroundColor Cyan
Write-Host "  Passed:  $($results.pass)" -ForegroundColor Green
Write-Host "  Failed:  $($results.fail)" -ForegroundColor $(if ($results.fail -gt 0) { "Red" } else { "Green" })
Write-Host "  Skipped: $($results.skip)" -ForegroundColor DarkGray
Write-Host ""

if ($results.fail -gt 0) {
    Write-Host "RESULT: Some validations FAILED" -ForegroundColor Red
    Write-Host "Review the failures above and fix before release." -ForegroundColor Yellow
    exit 1
} else {
    Write-Host "RESULT: All validations PASSED" -ForegroundColor Green
    Write-Host "Paryty v1.0 is ready for release!" -ForegroundColor Green
    exit 0
}
