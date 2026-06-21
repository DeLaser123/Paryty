<#
.SYNOPSIS
    Installs the Paryty pre-commit hook into .git/hooks/pre-commit.ps1
.DESCRIPTION
    Copies scripts\pre-commit.ps1 into the repository's .git\hooks\ directory
    so it runs on every `git commit`. Requires a PowerShell-capable git (Git
    for Windows invokes .git\hooks\pre-commit via cmd.exe, which delegates
    to PowerShell for .ps1 files automatically).
.NOTES
    Run from the repository root:
        powershell -ExecutionPolicy Bypass -File scripts\install-pre-commit.ps1
    Or from any directory:
        powershell -ExecutionPolicy Bypass -File <repo>\scripts\install-pre-commit.ps1
#>

$ErrorActionPreference = "Stop"

# Resolve repository root.
$repoRoot = & git rev-parse --show-toplevel 2>$null
if (-not $repoRoot) {
    Write-Error "ERROR: not inside a git repository. Run this from the Paryty repo root."
    exit 1
}

$hooksDir = Join-Path $repoRoot ".git\hooks"
if (-not (Test-Path -LiteralPath $hooksDir)) {
    New-Item -ItemType Directory -Path $hooksDir -Force | Out-Null
}

$sourceScript = Join-Path $repoRoot "scripts\pre-commit.ps1"
$destScript   = Join-Path $hooksDir "pre-commit.ps1"

if (-not (Test-Path -LiteralPath $sourceScript)) {
    Write-Error "ERROR: source hook not found at $sourceScript"
    exit 1
}

Copy-Item -LiteralPath $sourceScript -Destination $destScript -Force
Write-Host "[install-pre-commit] Copied $sourceScript" -ForegroundColor Green
Write-Host "[install-pre-commit]   -> $destScript" -ForegroundColor Green

# Git for Windows invokes .git/hooks/pre-commit (no extension) if it exists.
# On Windows, .ps1 files need an intermediary. We create a thin .cmd wrapper
# that invokes PowerShell.
$wrapperPath = Join-Path $hooksDir "pre-commit"
$wrapperContent = @"
@echo off
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0pre-commit.ps1"
exit /b %ERRORLEVEL%
"@
Set-Content -LiteralPath $wrapperPath -Value $wrapperContent -Encoding ASCII

Write-Host "[install-pre-commit] Created wrapper: $wrapperPath" -ForegroundColor Green
Write-Host ""
Write-Host "[install-pre-commit] Pre-commit hook installed successfully." -ForegroundColor Green
Write-Host "[install-pre-commit] The hook will scan staged changes with gitleaks on every commit."
Write-Host "[install-pre-commit] To uninstall: Remove-Item '$destScript'; Remove-Item '$wrapperPath'"
