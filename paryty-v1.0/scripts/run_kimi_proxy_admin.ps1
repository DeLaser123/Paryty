#Requires -RunAsAdministrator
<#
.SYNOPSIS
    One-click: configure hosts, install cert, flush DNS, then start the proxy.
    Run this as Administrator in a dedicated terminal window.
#>

param()

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$CertFile  = Join-Path $ScriptDir "moonshot_cert.pem"
$Domain    = "api.moonshot.cn"
$HostsFile = "$env:SystemRoot\System32\drivers\etc\hosts"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Kimi Proxy - One-Click Setup and Run"   -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# 1. Hosts entry
Write-Host "[1/3] Hosts file..." -ForegroundColor Yellow
$Lines = Get-Content $HostsFile
$HasEntry = $Lines | Where-Object { $_ -match "127\.0\.0\.1\s+$([regex]::Escape($Domain))" }
if ($HasEntry) {
    Write-Host "  Already present." -ForegroundColor Green
} else {
    Add-Content -Path $HostsFile -Value "`n127.0.0.1 $Domain" -Encoding ASCII
    Write-Host "  Added." -ForegroundColor Green
}

# 2. Install cert
Write-Host "[2/3] Trusting certificate..." -ForegroundColor Yellow
if (-not (Test-Path $CertFile)) {
    Write-Host "  ERROR: Certificate not found at $CertFile" -ForegroundColor Red
    Write-Host "  Run gen_cert.py first." -ForegroundColor Red
    exit 1
}
certutil -addstore -f "ROOT" $CertFile 2>&1 | Out-Null
Write-Host "  Installed." -ForegroundColor Green

# 3. Flush DNS
Write-Host "[3/3] Flushing DNS cache..." -ForegroundColor Yellow
ipconfig /flushdns | Out-Null
Write-Host "  Done." -ForegroundColor Green

Write-Host ""
Write-Host "========================================" -ForegroundColor Green
Write-Host "  Setup complete! Starting proxy..."     -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
Write-Host ""
Write-Host "  Press Ctrl+C to stop the proxy." -ForegroundColor Gray
Write-Host ""

# 4. Start proxy (blocking)
python (Join-Path $ScriptDir "kimi_https_proxy.py")
