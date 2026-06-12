#Requires -RunAsAdministrator
<#
.SYNOPSIS
    Uninstalls Paryty Agent Windows Service.

.DESCRIPTION
    This script removes the Paryty Agent Windows Service and optionally
    removes the installation directory.

.PARAMETER RemoveFiles
    Remove installation directory and logs (default: false).

.EXAMPLE
    .\uninstall-service.ps1 -RemoveFiles
#>

param(
    [switch]$RemoveFiles
)

$ErrorActionPreference = "Stop"

$ServiceName = "ParytyAgent"
$InstallPath = "C:\Paryty\Agent"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Paryty Agent Service Uninstaller" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Check if service exists
$service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if (-not $service) {
    Write-Host "[!] Service '$ServiceName' not found." -ForegroundColor Yellow
    if ($RemoveFiles -and (Test-Path $InstallPath)) {
        Write-Host "    Removing installation files..." -ForegroundColor Yellow
        Remove-Item -Path $InstallPath -Recurse -Force
    }
    exit 0
}

Write-Host "[1/3] Stopping service..." -ForegroundColor Green
if ($service.Status -eq "Running") {
    Stop-Service -Name $ServiceName -Force
    Start-Sleep -Seconds 2
}

Write-Host "[2/3] Removing service..." -ForegroundColor Green
& nssm remove $ServiceName confirm

Write-Host "[3/3] Cleaning up..." -ForegroundColor Green
if ($RemoveFiles) {
    if (Test-Path $InstallPath) {
        Write-Host "  Removing installation directory..." -ForegroundColor Yellow
        Remove-Item -Path $InstallPath -Recurse -Force
        Write-Host "  Removed: $InstallPath" -ForegroundColor Green
    }
} else {
    Write-Host "  Installation files preserved at: $InstallPath" -ForegroundColor White
    Write-Host "  Use -RemoveFiles to delete them." -ForegroundColor White
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Green
Write-Host "  Uninstallation Complete!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
