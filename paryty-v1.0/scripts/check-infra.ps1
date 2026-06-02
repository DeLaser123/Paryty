# Paryty Infrastructure Health Check Script
# Checks all infrastructure services and reports pass/fail
# Usage: .\scripts\check-infra.ps1

$ErrorActionPreference = "Continue"
$allHealthy = $true

function Check-Service {
    param(
        [string]$Name,
        [string]$Url,
        [string]$Method = "GET"
    )
    
    Write-Host -NoNewline "  $Name ... "
    
    try {
        if ($Method -eq "REDIS") {
            $result = redis-cli -p ($Url -split ":")[1] ping 2>&1
            if ($result -match "PONG") {
                Write-Host "OK" -ForegroundColor Green
                return $true
            }
        } else {
            $response = Invoke-WebRequest -Uri $Url -Method $Method -TimeoutSec 5 -UseBasicParsing -ErrorAction Stop
            if ($response.StatusCode -eq 200) {
                Write-Host "OK" -ForegroundColor Green
                return $true
            }
        }
    } catch {
        # Connection errors
    }
    
    Write-Host "FAIL" -ForegroundColor Red
    return $false
}

Write-Host ""
Write-Host "=== Paryty Infrastructure Health Check ===" -ForegroundColor Cyan
Write-Host ""

# Redpanda
Write-Host "[Redpanda]" -ForegroundColor Yellow
$healthy = Check-Service -Name "Kafka API (9092)" -Url "http://localhost:9644/v1/status/ready"
if (-not $healthy) { $allHealthy = $false }

# Dragonfly
Write-Host "[Dragonfly]" -ForegroundColor Yellow
$healthy = Check-Service -Name "Redis Protocol (6379)" -Url "localhost:6379" -Method "REDIS"
if (-not $healthy) { $allHealthy = $false }

# QuestDB
Write-Host "[QuestDB]" -ForegroundColor Yellow
$healthy = Check-Service -Name "Web Console (9000)" -Url "http://localhost:9000/status"
if (-not $healthy) { $allHealthy = $false }

# SeaweedFS
Write-Host "[SeaweedFS]" -ForegroundColor Yellow
$healthy = Check-Service -Name "Master (9333)" -Url "http://localhost:9333/cluster/status"
if (-not $healthy) { $allHealthy = $false }
$healthy = Check-Service -Name "S3 Gateway (8333)" -Url "http://localhost:8333"
if (-not $healthy) { $allHealthy = $false }

Write-Host ""

if ($allHealthy) {
    Write-Host "All infrastructure services are healthy!" -ForegroundColor Green
    exit 0
} else {
    Write-Host "Some services are not healthy. Check the logs above." -ForegroundColor Red
    exit 1
}
