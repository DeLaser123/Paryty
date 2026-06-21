<#
.SYNOPSIS
    Paryty pre-commit hook — scans staged changes for secrets with gitleaks.
.DESCRIPTION
    Runs `gitleaks protect --staged --verbose` against the current git index.
    If gitleaks is not installed, prints a warning and allows the commit to
    proceed (fail-open — never block developers on missing tooling).
.PARAMETER GitleaksConfig
    Path to the gitleaks configuration file. Defaults to ".gitleaks.toml"
    in the repository root.
.NOTES
    Install: .\scripts\install-pre-commit.ps1
    Manual:  Copy-Item scripts\pre-commit.ps1 .git\hooks\pre-commit.ps1
#>

param(
    [string]$GitleaksConfig = ".gitleaks.toml"
)

$ErrorActionPreference = "Stop"

# Resolve the repository root (where .git lives).
$repoRoot = & git rev-parse --show-toplevel 2>$null
if (-not $repoRoot) {
    Write-Host "[pre-commit] WARNING: not inside a git repository; skipping secret scan." -ForegroundColor Yellow
    exit 0
}

Set-Location -LiteralPath $repoRoot

# Locate gitleaks on PATH (or common install locations).
$gitleaks = Get-Command -Name "gitleaks" -CommandType Application -ErrorAction SilentlyContinue

# ---- gitleaks NOT installed: warn and allow ----
if (-not $gitleaks) {
    Write-Host "[pre-commit] WARNING: gitleaks is not installed." -ForegroundColor Yellow
    Write-Host "[pre-commit]   Install: winget install gitleaks"
    Write-Host "[pre-commit]   Or:     choco install gitleaks"
    Write-Host "[pre-commit]   Or:     go install github.com/gitleaks/gitleaks/v8@latest"
    Write-Host "[pre-commit]   Commit allowed (fail-open)." -ForegroundColor Yellow
    exit 0
}

# ---- Config file resolution ----
$configPath = Join-Path $repoRoot $GitleaksConfig
if (-not (Test-Path -LiteralPath $configPath)) {
    Write-Host "[pre-commit] WARNING: gitleaks config not found at $configPath" -ForegroundColor Yellow
    Write-Host "[pre-commit]   Running gitleaks with built-in rules only."
    $configArg = @()
} else {
    Write-Host "[pre-commit] Using config: $GitleaksConfig"
    $configArg = @("--config", $configPath)
}

# ---- Run gitleaks protect ----
Write-Host "[pre-commit] Scanning staged changes for secrets..."
$result = & $gitleaks protect --staged --verbose @configArg 2>&1
$exitCode = $LASTEXITCODE

# gitleaks exit codes:
#   0 = clean (no leaks found)
#   1 = leaks found
#   126 = unknown flag / error
if ($exitCode -eq 0) {
    Write-Host "[pre-commit] OK: no secrets detected in staged changes." -ForegroundColor Green
    exit 0
}

if ($exitCode -eq 1) {
    Write-Host ""
    Write-Host "============================================================" -ForegroundColor Red
    Write-Host "  GITLEAKS DETECTED SECRETS in your staged changes!" -ForegroundColor Red
    Write-Host "============================================================" -ForegroundColor Red
    Write-Host ""
    Write-Host "  If these are test credentials or false positives, either:"
    Write-Host "   1. Add # gitleaks:allow to the line"
    Write-Host "   2. Add a path/regex allowlist in .gitleaks.toml"
    Write-Host "   3. Use .gitleaksignore for specific hashes"
    Write-Host ""
    Write-Host "  Commit BLOCKED. Fix the leaks or whitelist them, then try again." -ForegroundColor Red
    exit 1
}

# Unknown error — fail open so devs are not blocked.
Write-Host "[pre-commit] WARNING: gitleaks exited with code $exitCode (unknown error)." -ForegroundColor Yellow
Write-Host "[pre-commit]   Commit allowed (fail-open)." -ForegroundColor Yellow
exit 0
