#Requires -RunAsAdministrator
<#
.SYNOPSIS
    Undoes the Qoder Kimi proxy setup (removes hosts entry and certificate).
#>

param()

$HostsFile = "$env:SystemRoot\System32\drivers\etc\hosts"
$Domain = "api.moonshot.cn"

Write-Host "Removing hosts entry for $Domain..." -ForegroundColor Yellow
$Lines = Get-Content $HostsFile
$NewLines = $Lines | Where-Object { $_ -notmatch "127\.0\.0\.1\s+$([regex]::Escape($Domain))" }
if ($Lines.Count -ne $NewLines.Count) {
    $NewLines | Set-Content $HostsFile -Encoding ASCII
    Write-Host "  Removed." -ForegroundColor Green
} else {
    Write-Host "  Entry not found. Nothing to remove." -ForegroundColor Green
}

Write-Host "Removing trusted certificate..." -ForegroundColor Yellow
$Certs = Get-ChildItem "Cert:\LocalMachine\Root" | Where-Object { $_.Subject -match "Qoder Kimi Proxy" }
if ($Certs) {
    foreach ($cert in $Certs) {
        Remove-Item $cert.PSPath
        Write-Host "  Removed: $($cert.Subject)" -ForegroundColor Green
    }
} else {
    Write-Host "  Certificate not found. Nothing to remove." -ForegroundColor Green
}

# Flush DNS cache
Write-Host "Flushing DNS cache..." -ForegroundColor Yellow
ipconfig /flushdns | Out-Null
Write-Host "  Done." -ForegroundColor Green
Write-Host ""
Write-Host "Undo complete. Qoder will now connect to the real api.moonshot.cn." -ForegroundColor Cyan
