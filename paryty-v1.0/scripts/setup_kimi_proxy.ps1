#Requires -RunAsAdministrator
<#
.SYNOPSIS
    One-time setup for the Qoder Kimi HTTPS proxy workaround.
    Adds hosts entry and trusts the self-signed certificate.

.DESCRIPTION
    This script:
    1. Adds "127.0.0.1 api.moonshot.cn" to the Windows hosts file
    2. Generates the self-signed certificate (if not exists)
    3. Installs the certificate to the Trusted Root store

    Run as Administrator. Only needs to be run once.
#>

param()

$ErrorActionPreference = "Stop"
$HostsFile = "$env:SystemRoot\System32\drivers\etc\hosts"
$CertDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$CertFile = Join-Path $CertDir "moonshot_cert.pem"
$Domain = "api.moonshot.cn"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Qoder Kimi Proxy - Setup" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""

# Step 1: Hosts file entry
Write-Host "[1/3] Checking hosts file..." -ForegroundColor Yellow
$HostsContent = Get-Content $HostsFile -Raw
if ($HostsContent -match "127\.0\.0\.1\s+$Domain") {
    Write-Host "  Hosts entry already exists. Skipping." -ForegroundColor Green
} else {
    Write-Host "  Adding: 127.0.0.1 $Domain" -ForegroundColor White
    Add-Content -Path $HostsFile -Value "`n127.0.0.1 $Domain" -Encoding ASCII
    Write-Host "  Done." -ForegroundColor Green
}

# Step 2: Generate certificate
Write-Host "[2/3] Generating self-signed certificate..." -ForegroundColor Yellow
if (Test-Path $CertFile) {
    Write-Host "  Certificate already exists. Skipping generation." -ForegroundColor Green
} else {
    $ProxyScript = Join-Path $CertDir "kimi_https_proxy.py"
    # We'll generate via Python since the proxy script has the logic
    python (Join-Path $CertDir "gen_cert.py")
    if (-not (Test-Path $CertFile)) {
        Write-Host "  ERROR: Certificate generation failed!" -ForegroundColor Red
        exit 1
    }
    Write-Host "  Certificate generated." -ForegroundColor Green
}

# Step 3: Trust the certificate
Write-Host "[3/3] Installing certificate to Trusted Root store..." -ForegroundColor Yellow

# Check if already installed
$Thumbprint = & openssl x509 -in $CertFile -fingerprint -noout 2>$null
if ($Thumbprint) {
    $Thumbprint = ($Thumbprint -replace "sha256 Fingerprint=", "" -replace ":", "").Trim()
    $Existing = Get-ChildItem "Cert:\LocalMachine\Root" | Where-Object { $_.Thumbprint -eq $Thumbprint }
    if ($Existing) {
        Write-Host "  Certificate already trusted. Skipping." -ForegroundColor Green
    } else {
        # Convert PEM to DER for import
        $DerFile = Join-Path $CertDir "moonshot_cert.cer"
        & openssl x509 -in $CertFile -outform DER -out $DerFile 2>$null
        if (Test-Path $DerFile) {
            $Cert = New-Object System.Security.Cryptography.X509Certificates.X509Certificate2($DerFile)
            $Store = New-Object System.Security.Cryptography.X509Certificates.X509Store("Root", "LocalMachine")
            $Store.Open("ReadWrite")
            $Store.Add($Cert)
            $Store.Close()
            Write-Host "  Certificate installed to Trusted Root store." -ForegroundColor Green
            Remove-Item $DerFile -ErrorAction SilentlyContinue
        } else {
            # Fallback: use certutil
            Write-Host "  Using certutil fallback..." -ForegroundColor Yellow
            certutil -addstore -f "ROOT" $CertFile
            Write-Host "  Certificate installed via certutil." -ForegroundColor Green
        }
    }
} else {
    # Fallback if openssl not available: use certutil directly with PEM
    Write-Host "  openssl not found, using certutil..." -ForegroundColor Yellow
    certutil -addstore -f "ROOT" $CertFile
    Write-Host "  Certificate installed via certutil." -ForegroundColor Green
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Green
Write-Host "  Setup complete!" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Green
Write-Host ""
Write-Host "Next steps:" -ForegroundColor White
Write-Host "  1. Open a terminal and run:" -ForegroundColor White
Write-Host "     python `"$CertDir\kimi_https_proxy.py`"" -ForegroundColor Cyan
Write-Host "     (needs Admin for port 443)" -ForegroundColor Gray
Write-Host ""
Write-Host "  2. In Qoder: Settings > Models > + Add > Kimi" -ForegroundColor White
Write-Host "     Use your NVIDIA API key." -ForegroundColor Gray
Write-Host ""
Write-Host "  To undo this setup, run: .\undo_hosts.ps1" -ForegroundColor Gray
