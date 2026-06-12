#Requires -RunAsAdministrator
<#
.SYNOPSIS
    Installs Paryty Agent as a Windows Service using NSSM.

.DESCRIPTION
    This script installs the Paryty Agent as a Windows Service that:
    - Survives machine reboots
    - Restarts automatically on failure
    - Runs under SYSTEM account for full host access
    - Logs to Windows Event Log and file

.PARAMETER ApiKey
    The API key for tenant authentication.

.PARAMETER ClusterEndpoint
    The cluster endpoint (default: localhost:50052).

.PARAMETER InstallPath
    Installation directory (default: C:\Paryty\Agent).

.EXAMPLE
    .\install-service.ps1 -ApiKey "pk_live_xxxxx"
#>

param(
    [Parameter(Mandatory=$true)]
    [string]$ApiKey,
    
    [string]$ClusterEndpoint = "localhost:50052",
    
    [string]$InstallPath = "C:\Paryty\Agent"
)

$ErrorActionPreference = "Stop"

# Service configuration
$ServiceName = "ParytyAgent"
$ServiceDisplayName = "Paryty Agent"
$ServiceDescription = "Paryty monitoring agent for host metrics collection"
$BinaryName = "paryty-agent.exe"
$ConfigName = "agent.yaml"
$LogPath = "$InstallPath\logs"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Paryty Agent Service Installer" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Check if NSSM is available
$nssmPath = Get-Command nssm -ErrorAction SilentlyContinue
if (-not $nssmPath) {
    Write-Host "[!] NSSM not found. Installing via winget..." -ForegroundColor Yellow
    winget install nssm --accept-source-agreements --accept-package-agreements
    if ($LASTEXITCODE -ne 0) {
        Write-Error "Failed to install NSSM. Please install manually: https://nssm.cc/download"
        exit 1
    }
    # Refresh PATH
    $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")
}

Write-Host "[1/6] Creating installation directory..." -ForegroundColor Green
New-Item -ItemType Directory -Force -Path $InstallPath | Out-Null
New-Item -ItemType Directory -Force -Path $LogPath | Out-Null

Write-Host "[2/6] Copying agent binary..." -ForegroundColor Green
$sourceBinary = "$PSScriptRoot\..\..\..\agent\target\release\$BinaryName"
if (-not (Test-Path $sourceBinary)) {
    Write-Error "Agent binary not found at: $sourceBinary"
    exit 1
}
Copy-Item -Path $sourceBinary -Destination "$InstallPath\$BinaryName" -Force

Write-Host "[3/6] Creating configuration file..." -ForegroundColor Green
$configContent = @"
# Paryty Agent Configuration
# Installed: $(Get-Date -Format "yyyy-MM-dd HH:mm:ss")

agent:
  id: "auto"
  cluster_endpoint: "$ClusterEndpoint"
  api_key: "$ApiKey"
  self_metrics:
    enabled: true
    port: 9100

layers:
  metal:
    enabled: true
    interval: "10s"
    cpu_per_core: true
    cpu_per_process: true
    memory_rss: true
    disk_io: true
    network_io: true
    process_tree: true
    container_detection: true

  ebpf:
    enabled: false
    tcp_connections: false
    dns_resolution: false
    http_inspection: false
    db_inspection: false
    exclude_ports: [22, 53, 443]
    exclude_ips: ["127.0.0.1", "::1"]
    ring_buffer_size_kb: 256
    poll_interval_ms: 100
    fallback_to_proc: true

  supervisor:
    enabled: false
    health_checks: []
    log_tailing: []

communication:
  protocol: grpc
  tls:
    enabled: false
  compression: zstd
  edge_buffer:
    enabled: true
    max_size_mb: 100
    retention_hours: 24
  flow_control:
    backpressure_enabled: true
    adaptive_sampling: true
    priority_queues:
      - name: "critical"
        topics: ["alerts", "errors"]
        priority: 1
      - name: "normal"
        topics: ["metrics", "traces"]
        priority: 2

logging:
  level: info
  format: json
  output: stdout
"@
Set-Content -Path "$InstallPath\$ConfigName" -Value $configContent

Write-Host "[4/6] Installing Windows Service..." -ForegroundColor Green
# Stop existing service if running
$existingService = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($existingService) {
    Write-Host "  Stopping existing service..." -ForegroundColor Yellow
    Stop-Service -Name $ServiceName -Force -ErrorAction SilentlyContinue
    & nssm remove $ServiceName confirm 2>$null
}

# Install service with NSSM
& nssm install $ServiceName "$InstallPath\$BinaryName"
& nssm set $ServiceName AppParameters "--config `"$InstallPath\$ConfigName`""
& nssm set $ServiceName DisplayName $ServiceDisplayName
& nssm set $ServiceName Description $ServiceDescription
& nssm set $ServiceName Start SERVICE_AUTO_START
& nssm set $ServiceName ObjectName LocalSystem
& nssm set $ServiceName AppDirectory $InstallPath

# Configure logging
& nssm set $ServiceName AppStdout "$LogPath\agent-output.log"
& nssm set $ServiceName AppStderr "$LogPath\agent-error.log"
& nssm set $ServiceName AppRotateFiles 1
& nssm set $ServiceName AppRotateBytes 10485760  # 10 MB

# Configure restart behavior (enterprise-grade)
& nssm set $ServiceName AppExit Default Restart
& nssm set $ServiceName AppRestartDelay 5000  # 5 seconds

Write-Host "[5/6] Configuring environment variables..." -ForegroundColor Green
& nssm set $ServiceName AppEnvironmentExtra "PARYTY_API_KEY=$ApiKey" "PARYTY_CLUSTER_ENDPOINT=$ClusterEndpoint"

Write-Host "[6/6] Starting service..." -ForegroundColor Green
Start-Service -Name $ServiceName

# Verify service is running
Start-Sleep -Seconds 2
$service = Get-Service -Name $ServiceName
if ($service.Status -eq "Running") {
    Write-Host ""
    Write-Host "========================================" -ForegroundColor Green
    Write-Host "  Installation Successful!" -ForegroundColor Green
    Write-Host "========================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "Service Name: $ServiceName" -ForegroundColor White
    Write-Host "Status:       $($service.Status)" -ForegroundColor Green
    Write-Host "Install Path: $InstallPath" -ForegroundColor White
    Write-Host "Config File:  $InstallPath\$ConfigName" -ForegroundColor White
    Write-Host "Log Path:     $LogPath" -ForegroundColor White
    Write-Host ""
    Write-Host "Management Commands:" -ForegroundColor Cyan
    Write-Host "  Start:   Start-Service $ServiceName" -ForegroundColor White
    Write-Host "  Stop:    Stop-Service $ServiceName" -ForegroundColor White
    Write-Host "  Status:  Get-Service $ServiceName" -ForegroundColor White
    Write-Host "  Logs:    Get-Content $LogPath\agent-output.log -Tail 50" -ForegroundColor White
    Write-Host ""
    Write-Host "To update API key:" -ForegroundColor Cyan
    Write-Host "  $InstallPath\$BinaryName --key <new-api-key>" -ForegroundColor White
    Write-Host "  Restart-Service $ServiceName" -ForegroundColor White
    Write-Host ""
} else {
    Write-Error "Service failed to start. Check logs at: $LogPath"
    exit 1
}
