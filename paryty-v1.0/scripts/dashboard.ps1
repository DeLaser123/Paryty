# Paryty Live Metrics Dashboard
# Polls QuestDB every 10 seconds and displays a live summary.
# Usage: powershell -File scripts/dashboard.ps1

$ErrorActionPreference = "SilentlyContinue"
$questdb = "http://localhost:9000/exec"

function Query-QuestDB($sql) {
    $encoded = [System.Uri]::EscapeDataString($sql)
    try {
        $resp = Invoke-RestMethod -Uri "$questdb`?query=$encoded" -TimeoutSec 5
        return $resp
    } catch {
        return $null
    }
}

function Get-AgentSummary {
    # Latest CPU per agent
    $cpu = Query-QuestDB @"
SELECT agent_id,
       last(total_usage_pct) as cpu_pct,
       last(load_avg_1) as load1
FROM cpu_metrics
WHERE timestamp > dateadd('s', -30, now())
GROUP BY agent_id
"@

    # Latest Memory per agent
    $mem = Query-QuestDB @"
SELECT agent_id,
       last(total_bytes) as total,
       last(used_bytes) as used,
       last(available_bytes) as avail
FROM memory_metrics
WHERE timestamp > dateadd('s', -30, now())
GROUP BY agent_id
"@

    # Row counts per agent (last 2 min)
    $counts = Query-QuestDB @"
SELECT agent_id, table_name, cnt FROM (
  SELECT 'cpu' as table_name, agent_id, COUNT(*) as cnt FROM cpu_metrics WHERE timestamp > dateadd('m', -2, now()) GROUP BY agent_id
  UNION ALL
  SELECT 'memory', agent_id, COUNT(*) FROM memory_metrics WHERE timestamp > dateadd('m', -2, now()) GROUP BY agent_id
  UNION ALL
  SELECT 'disk', agent_id, COUNT(*) FROM disk_metrics WHERE timestamp > dateadd('m', -2, now()) GROUP BY agent_id
  UNION ALL
  SELECT 'network', agent_id, COUNT(*) FROM network_metrics WHERE timestamp > dateadd('m', -2, now()) GROUP BY agent_id
  UNION ALL
  SELECT 'process', agent_id, COUNT(*) FROM process_metrics WHERE timestamp > dateadd('m', -2, now()) GROUP BY agent_id
)
ORDER BY agent_id, table_name
"@

    # Process count per agent
    $proc = Query-QuestDB @"
SELECT agent_id, last(proc_count) as procs
FROM (
  SELECT agent_id, COUNT(*) as proc_count
  FROM process_metrics
  WHERE timestamp > dateadd('s', -30, now())
  GROUP BY agent_id, timestamp
)
GROUP BY agent_id
"@

    return @{ cpu = $cpu; mem = $mem; counts = $counts; proc = $proc }
}

function Format-Bytes($bytes) {
    if ($bytes -ge 1GB) { return "{0:N1} GB" -f ($bytes / 1GB) }
    if ($bytes -ge 1MB) { return "{0:N1} MB" -f ($bytes / 1MB) }
    if ($bytes -ge 1KB) { return "{0:N1} KB" -f ($bytes / 1KB) }
    return "$bytes B"
}

# Main loop
while ($true) {
    $now = Get-Date -Format "HH:mm:ss"
    $data = Get-AgentSummary

    Clear-Host
    Write-Host "============================================================" -ForegroundColor Cyan
    Write-Host "  PARYTY LIVE METRICS DASHBOARD" -ForegroundColor Cyan
    Write-Host "  Refreshed: $now  (Ctrl+C to exit)" -ForegroundColor DarkGray
    Write-Host "============================================================" -ForegroundColor Cyan
    Write-Host ""

    # --- Per-agent summary ---
    if ($data.cpu -and $data.cpu.dataset) {
        foreach ($row in $data.cpu.dataset) {
            $agentId = $row[0]
            $shortId = $agentId.Substring(0, 8)
            $cpuPct = [math]::Round($row[1], 1)
            $load1 = [math]::Round($row[2], 2)

            Write-Host "  Agent: " -NoNewline; Write-Host "$shortId" -ForegroundColor Yellow

            $cpuBar = "[" + ("|" * [math]::Min([int]($cpuPct / 5), 20)) + (" " * [math]::Max(20 - [int]($cpuPct / 5), 0)) + "]"
            $cpuColor = if ($cpuPct -gt 80) { "Red" } elseif ($cpuPct -gt 50) { "Yellow" } else { "Green" }
            Write-Host "    CPU:  $cpuBar " -NoNewline; Write-Host "$cpuPct%" -ForegroundColor $cpuColor -NoNewline; Write-Host "  load: $load1"

            # Memory for this agent
            if ($data.mem -and $data.mem.dataset) {
                $memRow = $data.mem.dataset | Where-Object { $_[0] -eq $agentId }
                if ($memRow) {
                    $total = [long]$memRow[1]
                    $used = [long]$memRow[2]
                    $memPct = if ($total -gt 0) { [math]::Round(($used / $total) * 100, 1) } else { 0 }
                    $memBar = "[" + ("|" * [math]::Min([int]($memPct / 5), 20)) + (" " * [math]::Max(20 - [int]($memPct / 5), 0)) + "]"
                    $memColor = if ($memPct -gt 80) { "Red" } elseif ($memPct -gt 50) { "Yellow" } else { "Green" }
                    Write-Host "    MEM:  $memBar " -NoNewline; Write-Host "$memPct%" -ForegroundColor $memColor -NoNewline; Write-Host "  ($(Format-Bytes $used) / $(Format-Bytes $total))"
                }
            }
            Write-Host ""
        }
    } else {
        Write-Host "  No agent data available. Is QuestDB running on port 9000?" -ForegroundColor Red
        Write-Host ""
    }

    # --- Data flow table ---
    Write-Host "  Data Flow (rows in last 2 min):" -ForegroundColor Cyan
    Write-Host "  ---------------------------------------------------------"
    Write-Host ("  {0,-12} {1,8} {2,8} {3,8} {4,8} {5,8}" -f "Agent", "CPU", "Memory", "Disk", "Network", "Process")
    Write-Host "  ---------------------------------------------------------"

    if ($data.counts -and $data.counts.dataset) {
        $grouped = @{}
        foreach ($row in $data.counts.dataset) {
            $aid = $row[0].Substring(0, 8)
            $tbl = $row[1]
            $cnt = $row[2]
            if (-not $grouped.ContainsKey($aid)) { $grouped[$aid] = @{} }
            $grouped[$aid][$tbl] = $cnt
        }
        foreach ($aid in $grouped.Keys) {
            $t = $grouped[$aid]
            $cpuN = if ($t.ContainsKey("cpu")) { $t["cpu"] } else { 0 }
            $memN = if ($t.ContainsKey("memory")) { $t["memory"] } else { 0 }
            $dskN = if ($t.ContainsKey("disk")) { $t["disk"] } else { 0 }
            $netN = if ($t.ContainsKey("network")) { $t["network"] } else { 0 }
            $prcN = if ($t.ContainsKey("process")) { $t["process"] } else { 0 }

            $prcColor = if ($prcN -eq 0) { "DarkYellow" } else { "White" }
            Write-Host ("  {0,-12} {1,8} {2,8} {3,8} {4,8} " -f $aid, $cpuN, $memN, $dskN, $netN) -NoNewline
            Write-Host ("{0,8}" -f $prcN) -ForegroundColor $prcColor
        }
    } else {
        Write-Host "  No data flow detected." -ForegroundColor DarkGray
    }

    Write-Host ""
    Write-Host "  " -NoNewline; Write-Host "Legend: " -ForegroundColor DarkGray -NoNewline
    Write-Host "Process=0 " -ForegroundColor DarkYellow -NoNewline
    Write-Host "on WSL2 (expected). Bars: " -ForegroundColor DarkGray -NoNewline
    Write-Host "green" -ForegroundColor Green -NoNewline
    Write-Host " <50% " -ForegroundColor DarkGray -NoNewline
    Write-Host "yellow" -ForegroundColor Yellow -NoNewline
    Write-Host " <80% " -ForegroundColor DarkGray -NoNewline
    Write-Host "red" -ForegroundColor Red -NoNewline
    Write-Host " >=80%" -ForegroundColor DarkGray
    Write-Host ""
    Write-Host "  QuestDB console: http://localhost:9000" -ForegroundColor DarkGray

    Start-Sleep -Seconds 10
}
