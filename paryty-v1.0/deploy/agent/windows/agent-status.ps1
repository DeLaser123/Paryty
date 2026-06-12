<#
.SYNOPSIS
    Shows Paryty Agent service status and health information.

.DESCRIPTION
    This script displays comprehensive status information about the
    Paryty Agent service including:
    - Service status
    - Configuration details
    - Recent logs
    - Health metrics

.PARAMETER ShowLogs
    Show recent log entries (default: true).

.PARAMETER LogLines
    Number of log lines to show (default: 20).

.EXAMPLE
    .\agent-status.ps1

.EXAMPLE
    .\agent-status.ps1 -ShowLogs -LogLines 50
#>

param(
    [bool]$ShowLogs = $true,
    [int]$LogLines = 20
)

$ServiceName = "ParytyAgent"
$InstallPath = "C:\Paryty\Agent"
$ConfigPath = "$InstallPath\agent.yaml"
$LogPath = "$InstallPath\logs"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Paryty Agent Status" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Service Status
Write-Host "Service Status:" -ForegroundColor White
$service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($service) {
    $statusColor = switch ($service.Status) {
        "Running" { "Green" }
        "Stopped" { "Red" }
        default { "Yellow" }
    }
    Write-Host "  Name:   $ServiceName" -ForegroundColor White
    Write-Host "  Status: $($service.Status)" -ForegroundColor $statusColor
    Write-Host "  Start:  $($service.StartType)" -ForegroundColor White
    
    # Get process info
    $process = Get-Process -Name "paryty-agent" -ErrorAction SilentlyContinue
    if ($process) {
        Write-Host "  PID:    $($process.Id)" -ForegroundColor White
        Write-Host "  Memory: $([math]::Round($process.WorkingSet64 / 1MB, 2)) MB" -ForegroundColor White
        Write-Host "  CPU:    $([math]::Round($process.CPU, 2)) seconds" -ForegroundColor White
    }
} else {
    Write-Host "  [!] Service not installed" -ForegroundColor Red
}

Write-Host ""

# Configuration
Write-Host "Configuration:" -ForegroundColor White
if (Test-Path $ConfigPath) {
    Write-Host "  Config: $ConfigPath" -ForegroundColor White
    
    # Read API key prefix from config
    $configContent = Get-Content $ConfigPath -Raw
    if ($configContent -match 'api_key:\s*"([^"]+)"') {
        $apiKey = $Matches[1]
        $prefix = $apiKey.Substring(0, [Math]::Min(16, $apiKey.Length))
        Write-Host "  API Key: ${prefix}..." -ForegroundColor White
    }
    
    # Read endpoint from config
    if ($configContent -match 'cluster_endpoint:\s*"([^"]+)"') {
        Write-Host "  Endpoint: $($Matches[1])" -ForegroundColor White
    }
} else {
    Write-Host "  [!] Config file not found" -ForegroundColor Red
}

Write-Host ""

# Environment Variables
Write-Host "Environment Variables:" -ForegroundColor White
$envApiKey = [System.Environment]::GetEnvironmentVariable("PARYTY_API_KEY", "Machine")
$envEndpoint = [System.Environment]::GetEnvironmentVariable("PARYTY_CLUSTER_ENDPOINT", "Machine")
if ($envApiKey) {
    $prefix = $envApiKey.Substring(0, [Math]::Min(16, $envApiKey.Length))
    Write-Host "  PARYTY_API_KEY: ${prefix}..." -ForegroundColor White
} else {
    Write-Host "  PARYTY_API_KEY: (not set)" -ForegroundColor Gray
}
if ($envEndpoint) {
    Write-Host "  PARYTY_CLUSTER_ENDPOINT: $envEndpoint" -ForegroundColor White
} else {
    Write-Host "  PARYTY_CLUSTER_ENDPOINT: (not set)" -ForegroundColor Gray
}

Write-Host ""

# Installation
Write-Host "Installation:" -ForegroundColor White
if (Test-Path $InstallPath) {
    Write-Host "  Path: $InstallPath" -ForegroundColor White
    
    $binary = Get-Item "$InstallPath\paryty-agent.exe" -ErrorAction SilentlyContinue
    if ($binary) {
        Write-Host "  Binary: $($binary.Length / 1MB) MB (modified: $($binary.LastWriteTime))" -ForegroundColor White
    }
    
    if (Test-Path $LogPath) {
        $logFiles = Get-ChildItem $LogPath -ErrorAction SilentlyContinue
        Write-Host "  Logs: $($logFiles.Count) files" -ForegroundColor White
    }
} else {
    Write-Host "  [!] Installation directory not found" -ForegroundColor Red
}

Write-Host ""

# Recent Logs
if ($ShowLogs -and (Test-Path "$LogPath\agent-output.log")) {
    Write-Host "Recent Logs (last $LogLines lines):" -ForegroundColor White
    Write-Host "----------------------------------------" -ForegroundColor Gray
    Get-Content "$LogPath\agent-output.log" -Tail $LogLines | ForEach-Object {
        # Color code based on log level
        if ($_ -match '"level":"ERROR"') {
            Write-Host $_ -ForegroundColor Red
        } elseif ($_ -match '"level":"WARN"') {
            Write-Host $_ -ForegroundColor Yellow
        } elseif ($_ -match '"level":"INFO"') {
            Write-Host $_ -ForegroundColor Green
        } else {
            Write-Host $_ -ForegroundColor Gray
        }
    }
    Write-Host "----------------------------------------" -ForegroundColor Gray
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
