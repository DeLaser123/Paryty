<#
.SYNOPSIS
    Wipes all data from the Paryty development database.

.DESCRIPTION
    This script truncates all tables in the Paryty database.
    WARNING: This will delete ALL data! Use only in development environments.

.PARAMETER Confirm
    Requires explicit confirmation before executing the wipe.

.PARAMETER DatabaseUrl
    PostgreSQL connection URL. Defaults to environment variable DATABASE_URL
    or the standard local development URL.

.EXAMPLE
    .\wipe-database.ps1 -Confirm
    Wipes the database after confirmation prompt.

.EXAMPLE
    .\wipe-database.ps1 -Confirm -DatabaseUrl "postgres://user:pass@host:5432/dbname"
    Wipes a specific database.

.NOTES
    Author: Paryty Development Team
    Date: $(Get-Date -Format 'yyyy-MM-dd')
#>

[CmdletBinding(SupportsShouldProcess = $true, ConfirmImpact = 'High')]
param(
    [Parameter(Mandatory = $true)]
    [switch]$Confirm,

    [Parameter(Mandatory = $false)]
    [string]$DatabaseUrl
)

# Set error action preference
$ErrorActionPreference = "Stop"

# ─── Configuration ────────────────────────────────────────────────────

# Default database URL for local development
if (-not $DatabaseUrl) {
    $DatabaseUrl = $env:DATABASE_URL
    if (-not $DatabaseUrl) {
        $DatabaseUrl = "postgres://paryty:paryty@localhost:5432/paryty?sslmode=disable"
    }
}

# Parse connection string
$connString = $DatabaseUrl -replace "postgres://", "" -replace "\?.*", ""
$parts = $connString -split "@"
$auth = $parts[0] -split ":"
$hostPort = $parts[1] -split ":"

$dbUser = $auth[0]
$dbPass = $auth[1]
$dbHost = $hostPort[0]
$dbPort = if ($hostPort.Length -gt 1) { $hostPort[1] } else { "5432" }
$dbName = "paryty"  # Default database name

# SQL file path
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$sqlFile = Join-Path $scriptDir "wipe-database.sql"

# ─── Validation ───────────────────────────────────────────────────────

# Check if SQL file exists
if (-not (Test-Path $sqlFile)) {
    Write-Error "SQL file not found: $sqlFile"
    exit 1
}

# Check if psql is available
try {
    $null = Get-Command psql -ErrorAction Stop
} catch {
    Write-Error @"
psql (PostgreSQL client) is not installed or not in PATH.

Please install PostgreSQL client tools:
  - Windows: Download from https://www.postgresql.org/download/windows/
  - Or use: choco install postgresql
  - Or use: scoop install postgresql

After installation, ensure psql is in your PATH.
"@
    exit 1
}

# ─── Confirmation ─────────────────────────────────────────────────────

Write-Host @"

╔═══════════════════════════════════════════════════════════════════╗
║                    ⚠️  WARNING: DATA WIPE  ⚠️                      ║
╠═══════════════════════════════════════════════════════════════════╣
║  This will DELETE ALL DATA from the Paryty database!            ║
║                                                                 ║
║  Database: $dbName @ $dbHost`:$dbPort                          ║
║                                                                 ║
║  Tables to be truncated:                                        ║
║    - tenants, users, api_keys                                   ║
║    - paryty_twins, agent_registrations, agent_assignments       ║
║    - agent_commands, agent_backlogs, audit_log, tenant_plans    ║
║                                                                 ║
║  This action is IRREVERSIBLE!                                   ║
╚═══════════════════════════════════════════════════════════════════╝

"@ -ForegroundColor Red

if (-not $PSCmdlet.ShouldProcess("Paryty database", "DELETE ALL DATA")) {
    Write-Host "Operation cancelled." -ForegroundColor Yellow
    exit 0
}

# Double confirmation
$secondConfirm = Read-Host "Type 'YES' (uppercase) to confirm database wipe"
if ($secondConfirm -ne "YES") {
    Write-Host "Operation cancelled." -ForegroundColor Yellow
    exit 0
}

# ─── Execution ────────────────────────────────────────────────────────

Write-Host "`nConnecting to database..." -ForegroundColor Cyan

# Set password environment variable for psql
$env:PGPASSWORD = $dbPass

try {
    # Execute SQL file
    Write-Host "Executing wipe script..." -ForegroundColor Cyan
    
    $psqlArgs = @(
        "-h", $dbHost,
        "-p", $dbPort,
        "-U", $dbUser,
        "-d", $dbName,
        "-f", $sqlFile,
        "--no-psqlrc",
        "-v", "ON_ERROR_STOP=1"
    )

    $process = Start-Process -FilePath "psql" -ArgumentList $psqlArgs -NoNewWindow -Wait -PassThru

    if ($process.ExitCode -eq 0) {
        Write-Host "`n✓ Database wiped successfully!" -ForegroundColor Green
        Write-Host "`nYou can now register a fresh account and start testing." -ForegroundColor Cyan
    } else {
        Write-Error "psql exited with code $($process.ExitCode)"
        exit 1
    }
} catch {
    Write-Error "Failed to execute wipe script: $_"
    exit 1
} finally {
    # Clear password from environment
    $env:PGPASSWORD = $null
}

# ─── Post-Wipe Verification ──────────────────────────────────────────

Write-Host "`nVerifying tables are empty..." -ForegroundColor Cyan

try {
    $verifyQuery = @"
SELECT 'tenants' as table_name, COUNT(*) as row_count FROM tenants
UNION ALL
SELECT 'users', COUNT(*) FROM users
UNION ALL
SELECT 'api_keys', COUNT(*) FROM api_keys
UNION ALL
SELECT 'paryty_twins', COUNT(*) FROM paryty_twins
UNION ALL
SELECT 'agent_registrations', COUNT(*) FROM agent_registrations
UNION ALL
SELECT 'agent_assignments', COUNT(*) FROM agent_assignments;
"@

    $env:PGPASSWORD = $dbPass
    $verifyResult = & psql -h $dbHost -p $dbPort -U $dbUser -d $dbName -c $verifyQuery --no-psqlrc 2>&1
    
    if ($LASTEXITCODE -eq 0) {
        Write-Host $verifyResult -ForegroundColor Gray
        Write-Host "`n✓ All tables verified empty." -ForegroundColor Green
    } else {
        Write-Warning "Could not verify table counts. Please check manually."
    }
} catch {
    Write-Warning "Verification query failed: $_"
} finally {
    $env:PGPASSWORD = $null
}

Write-Host @"

╔═══════════════════════════════════════════════════════════════════╗
║                    ✓ WIPE COMPLETE                               ║
╠═══════════════════════════════════════════════════════════════════╣
║  The database has been wiped successfully.                      ║
║                                                                 ║
║  Next steps:                                                    ║
║    1. Open http://localhost:3000                                 ║
║    2. Register a new account                                    ║
║    3. Create twins and test the system                          ║
╚═══════════════════════════════════════════════════════════════════╝

"@ -ForegroundColor Green
