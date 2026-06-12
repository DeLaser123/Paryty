#Requires -RunAsAdministrator
<#
.SYNOPSIS
    Updates the Paryty Agent API key.

.DESCRIPTION
    This script updates the API key used by the Paryty Agent service.
    It supports updating the key in the config file and optionally
    restarting the service for immediate effect.

.PARAMETER ApiKey
    The new API key to set.

.PARAMETER Restart
    Restart the service after updating the key (default: true).

.PARAMETER InstallPath
    Installation directory (default: C:\Paryty\Agent).

.EXAMPLE
    .\set-api-key.ps1 -ApiKey "pk_live_new_key_xxxxx"

.EXAMPLE
    .\set-api-key.ps1 -ApiKey "pk_live_new_key_xxxxx" -Restart:$false
#>

param(
    [Parameter(Mandatory=$true)]
    [string]$ApiKey,
    
    [bool]$Restart = $true,
    
    [string]$InstallPath = "C:\Paryty\Agent"
)

$ErrorActionPreference = "Stop"

$ServiceName = "ParytyAgent"
$ConfigPath = "$InstallPath\agent.yaml"
$BinaryPath = "$InstallPath\paryty-agent.exe"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Paryty Agent API Key Manager" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Validate API key format
if (-not $ApiKey.StartsWith("pk_live_")) {
    Write-Error "Invalid API key format. Key must start with 'pk_live_'"
    exit 1
}

# Check if installation exists
if (-not (Test-Path $BinaryPath)) {
    Write-Error "Agent binary not found at: $BinaryPath"
    exit 1
}

if (-not (Test-Path $ConfigPath)) {
    Write-Error "Config file not found at: $ConfigPath"
    exit 1
}

Write-Host "[1/3] Updating API key in config file..." -ForegroundColor Green
# Use the agent binary to update the key (ensures proper YAML handling)
& $BinaryPath --key $ApiKey --config $ConfigPath
if ($LASTEXITCODE -ne 0) {
    Write-Error "Failed to update API key"
    exit 1
}

Write-Host "[2/3] Updating service environment variable..." -ForegroundColor Green
& nssm set $ServiceName AppEnvironmentExtra "PARYTY_API_KEY=$ApiKey"

if ($Restart) {
    Write-Host "[3/3] Restarting service..." -ForegroundColor Green
    $service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
    if ($service -and $service.Status -eq "Running") {
        Restart-Service -Name $ServiceName
        Start-Sleep -Seconds 2
        $service = Get-Service -Name $ServiceName
        if ($service.Status -eq "Running") {
            Write-Host ""
            Write-Host "========================================" -ForegroundColor Green
            Write-Host "  API Key Updated Successfully!" -ForegroundColor Green
            Write-Host "========================================" -ForegroundColor Green
            Write-Host ""
            Write-Host "Service restarted and running with new key." -ForegroundColor Green
        } else {
            Write-Warning "Service failed to restart. Check logs."
        }
    } else {
        Write-Host "[!] Service not running. Key updated for next start." -ForegroundColor Yellow
    }
} else {
    Write-Host "[3/3] Skipping restart (use -Restart `$true to apply immediately)" -ForegroundColor Yellow
    Write-Host ""
    Write-Host "========================================" -ForegroundColor Green
    Write-Host "  API Key Updated!" -ForegroundColor Green
    Write-Host "========================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "Restart the service to apply the new key:" -ForegroundColor White
    Write-Host "  Restart-Service $ServiceName" -ForegroundColor White
}

Write-Host ""
Write-Host "Key Prefix: $($ApiKey.Substring(0, [Math]::Min(16, $ApiKey.Length)))..." -ForegroundColor White
Write-Host "Config:     $ConfigPath" -ForegroundColor White
Write-Host ""
