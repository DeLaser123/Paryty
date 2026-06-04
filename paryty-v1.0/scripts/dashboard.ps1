# Paryty Live Metrics Dashboard - Full Detail
# Polls QuestDB every 10 seconds and displays all metric values.
# Auto-discovers columns from QuestDB tables to pick up new metrics automatically.
# Usage: powershell -ExecutionPolicy Bypass -File scripts/dashboard.ps1

$ErrorActionPreference = "SilentlyContinue"
$questdb = "http://localhost:9000/exec"
$pct = [char]37  # '%' character - avoids PowerShell parsing issues

function Query-QuestDB($sql) {
    $encoded = [System.Uri]::EscapeDataString($sql)
    try {
        $resp = Invoke-RestMethod -Uri "$questdb`?query=$encoded" -TimeoutSec 5
        return $resp
    } catch {
        return $null
    }
}

function Format-Bytes($bytes) {
    if ($bytes -ge 1GB) { return "{0:N1} GB" -f ($bytes / 1GB) }
    if ($bytes -ge 1MB) { return "{0:N1} MB" -f ($bytes / 1MB) }
    if ($bytes -ge 1KB) { return "{0:N1} KB" -f ($bytes / 1KB) }
    return "$bytes B"
}

function Format-Rate($bps) {
    if ($bps -ge 1MB) { return "{0:N1} MB/s" -f ($bps / 1MB) }
    if ($bps -ge 1KB) { return "{0:N1} KB/s" -f ($bps / 1KB) }
    return "$bps B/s"
}

function Draw-Bar($pct, $width) {
    $filled = [math]::Min([int]($pct / 5), $width)
    $empty = [math]::Max($width - $filled, 0)
    return "[" + ("|" * $filled) + (" " * $empty) + "]"
}

function Color-Pct($pct) {
    if ($pct -gt 80) { return "Red" }
    if ($pct -gt 50) { return "Yellow" }
    return "Green"
}

function Get-TableColumns($tableName) {
    $result = Query-QuestDB "SELECT * FROM table_columns('$tableName')"
    if ($result -and $result.dataset) {
        # QuestDB returns 'column' as the column name (not 'column_name')
        return $result.dataset | ForEach-Object { $_[0] }
    }
    return @()
}

# Cache for table columns - refreshed periodically
$script:columnCache = @{}
$script:lastColumnRefresh = [datetime]::MinValue

function Ensure-ColumnCache {
    $now = Get-Date
    if (($now - $script:lastColumnRefresh).TotalSeconds -gt 60) {
        $tables = @("cpu_metrics", "memory_metrics", "disk_metrics", "network_metrics", "process_metrics", "container_metrics", "tcp_events", "dns_events", "http_events")
        foreach ($t in $tables) {
            $script:columnCache[$t] = Get-TableColumns $t
        }
        $script:lastColumnRefresh = $now
    }
}

function Has-Column($table, $col) {
    if ($script:columnCache.ContainsKey($table)) {
        return $script:columnCache[$table] -contains $col
    }
    return $false
}

# Main loop
while ($true) {
    $now = Get-Date -Format "HH:mm:ss"

    # Refresh column cache
    Ensure-ColumnCache

    # -- Build dynamic queries based on available columns --
    
    # CPU query - always include base columns, add new ones if they exist
    $cpuCols = @("agent_id", "last(total_usage_pct) as pct", "last(load_avg_1) as l1", "last(load_avg_5) as l5", "last(load_avg_15) as l15", "last(frequency_mhz) as freq", "last(context_switches) as ctx")
    if (Has-Column "cpu_metrics" "model_name") { $cpuCols += "last(model_name) as model" }
    if (Has-Column "cpu_metrics" "vendor_id") { $cpuCols += "last(vendor_id) as vendor" }
    if (Has-Column "cpu_metrics" "physical_cores") { $cpuCols += "last(physical_cores) as pcores" }
    if (Has-Column "cpu_metrics" "logical_cores") { $cpuCols += "last(logical_cores) as lcores" }
    
    $cpuLatest = Query-QuestDB @"
SELECT $($cpuCols -join ', ')
FROM cpu_metrics
WHERE timestamp > dateadd('s', -30, now())
GROUP BY agent_id
"@

    # Memory query
    $memCols = @("agent_id", "last(total_bytes) as total", "last(used_bytes) as used", "last(available_bytes) as avail", "last(cached_bytes) as cached", "last(swap_total_bytes) as swap_total", "last(swap_used_bytes) as swap_used")
    if (Has-Column "memory_metrics" "pressure_some_avg10") { $memCols += "last(pressure_some_avg10) as psi_some10" }
    if (Has-Column "memory_metrics" "pressure_some_avg60") { $memCols += "last(pressure_some_avg60) as psi_some60" }
    if (Has-Column "memory_metrics" "pressure_full_avg10") { $memCols += "last(pressure_full_avg10) as psi_full10" }
    
    $memLatest = Query-QuestDB @"
SELECT $($memCols -join ', ')
FROM memory_metrics
WHERE timestamp > dateadd('s', -30, now())
GROUP BY agent_id
"@

    # Disk query
    $diskCols = @("agent_id", "device", "mount_point", "last(total_bytes) as total", "last(used_bytes) as used", "last(read_bytes_per_sec) as rrate", "last(write_bytes_per_sec) as wrate", "last(iops_read) as riops", "last(iops_write) as wiops")
    if (Has-Column "disk_metrics" "is_ssd") { $diskCols += "last(is_ssd) as is_ssd" }
    if (Has-Column "disk_metrics" "utilization_pct") { $diskCols += "last(utilization_pct) as util" }
    
    $diskLatest = Query-QuestDB @"
SELECT $($diskCols -join ', ')
FROM disk_metrics
WHERE timestamp > dateadd('s', -30, now())
GROUP BY agent_id, device, mount_point
ORDER BY agent_id, mount_point
"@

    # Network query
    $netCols = @("agent_id", "interface", "last(rx_bytes_per_sec) as rx", "last(tx_bytes_per_sec) as tx", "last(rx_packets) as rxp", "last(tx_packets) as txp", "last(errors) as errs")
    if (Has-Column "network_metrics" "total_rx_bytes") { $netCols += "last(total_rx_bytes) as total_rx" }
    if (Has-Column "network_metrics" "total_tx_bytes") { $netCols += "last(total_tx_bytes) as total_tx" }
    if (Has-Column "network_metrics" "speed_mbps") { $netCols += "last(speed_mbps) as speed" }
    if (Has-Column "network_metrics" "is_up") { $netCols += "last(is_up) as is_up" }
    if (Has-Column "network_metrics" "tcp_established") { $netCols += "last(tcp_established) as tcp_est" }
    if (Has-Column "network_metrics" "tcp_time_wait") { $netCols += "last(tcp_time_wait) as tcp_tw" }
    if (Has-Column "network_metrics" "tcp_listen") { $netCols += "last(tcp_listen) as tcp_listen" }
    
    $netLatest = Query-QuestDB @"
SELECT $($netCols -join ', ')
FROM network_metrics
WHERE timestamp > dateadd('s', -30, now())
GROUP BY agent_id, interface
ORDER BY agent_id, interface
"@

    # Process query
    $procCols = @("agent_id", "name", "last(memory_bytes) as mem", "last(cpu_usage_pct) as cpu")
    if (Has-Column "process_metrics" "exe") { $procCols += "last(exe) as exe" }
    if (Has-Column "process_metrics" "disk_read_bytes") { $procCols += "last(disk_read_bytes) as dread" }
    if (Has-Column "process_metrics" "disk_written_bytes") { $procCols += "last(disk_written_bytes) as dwrite" }
    if (Has-Column "process_metrics" "user_id") { $procCols += "last(user_id) as uid" }
    
    $procTop = Query-QuestDB @"
SELECT $($procCols -join ', ')
FROM process_metrics
WHERE timestamp > dateadd('s', -30, now())
GROUP BY agent_id, name
ORDER BY mem DESC
LIMIT 15
"@

    $procCount = Query-QuestDB @"
SELECT agent_id, COUNT(DISTINCT name) as procs
FROM process_metrics
WHERE timestamp > dateadd('s', -30, now())
GROUP BY agent_id
"@

    # Container query
    $containerLatest = $null
    if (Has-Column "container_metrics" "container_id") {
        $containerLatest = Query-QuestDB @"
SELECT agent_id, container_id, last(name) as name, last(image) as image, last(status) as status,
       last(memory_limit_bytes) as mem_limit, last(cpu_quota) as cpu_quota
FROM container_metrics
WHERE timestamp > dateadd('s', -30, now())
GROUP BY agent_id, container_id
ORDER BY agent_id, name
"@
    }

    # Network events queries (eBPF / proc fallback)
    # Use 5-minute window for recent activity + all-time totals
    $tcpEvents = $null; $dnsEvents = $null; $httpEvents = $null
    $tcpTopConns = $null
    $tcpEventsAllTime = $null; $dnsEventsAllTime = $null; $httpEventsAllTime = $null
    if (Has-Column "tcp_events" "agent_id") {
        $tcpEvents = Query-QuestDB @"
SELECT agent_id, COUNT(*) as cnt,
       COUNT(DISTINCT destination_ip) as unique_dsts,
       COUNT(DISTINCT process_name) as unique_procs
FROM tcp_events
WHERE timestamp > dateadd('m', -5, now())
GROUP BY agent_id
"@
        $tcpEventsAllTime = Query-QuestDB @"
SELECT agent_id, COUNT(*) as total_cnt,
       COUNT(DISTINCT destination_ip) as total_dsts,
       COUNT(DISTINCT process_name) as total_procs,
       MIN(timestamp) as first_seen,
       MAX(timestamp) as last_seen
FROM tcp_events
GROUP BY agent_id
"@
        $dnsEvents = Query-QuestDB @"
SELECT agent_id, COUNT(*) as cnt,
       COUNT(DISTINCT query_name) as unique_domains
FROM dns_events
WHERE timestamp > dateadd('m', -5, now())
GROUP BY agent_id
"@
        $dnsEventsAllTime = Query-QuestDB @"
SELECT agent_id, COUNT(*) as total_cnt,
       COUNT(DISTINCT query_name) as total_domains,
       MIN(timestamp) as first_seen,
       MAX(timestamp) as last_seen
FROM dns_events
GROUP BY agent_id
"@
        $httpEvents = Query-QuestDB @"
SELECT agent_id, method, COUNT(*) as cnt,
       ROUND(AVG(latency_ms), 1) as avg_latency
FROM http_events
WHERE timestamp > dateadd('m', -5, now())
GROUP BY agent_id, method
ORDER BY cnt DESC
LIMIT 10
"@
        $httpEventsAllTime = Query-QuestDB @"
SELECT agent_id, COUNT(*) as total_cnt,
       COUNT(DISTINCT method) as methods,
       MIN(timestamp) as first_seen,
       MAX(timestamp) as last_seen
FROM http_events
GROUP BY agent_id
"@
        $tcpTopConns = Query-QuestDB @"
SELECT agent_id, source_ip, source_port, destination_ip, destination_port, state, process_name
FROM tcp_events
WHERE timestamp > dateadd('m', -5, now())
ORDER BY timestamp DESC
LIMIT 10
"@
    }

    # Flow counts - include container_metrics and network events if they exist
    $flowTables = @(
        "SELECT 'cpu' as table_name, agent_id, COUNT(*) as cnt FROM cpu_metrics WHERE timestamp > dateadd('m', -2, now()) GROUP BY agent_id",
        "SELECT 'memory', agent_id, COUNT(*) FROM memory_metrics WHERE timestamp > dateadd('m', -2, now()) GROUP BY agent_id",
        "SELECT 'disk', agent_id, COUNT(*) FROM disk_metrics WHERE timestamp > dateadd('m', -2, now()) GROUP BY agent_id",
        "SELECT 'network', agent_id, COUNT(*) FROM network_metrics WHERE timestamp > dateadd('m', -2, now()) GROUP BY agent_id",
        "SELECT 'process', agent_id, COUNT(*) FROM process_metrics WHERE timestamp > dateadd('m', -2, now()) GROUP BY agent_id"
    )
    if (Has-Column "container_metrics" "container_id") {
        $flowTables += "SELECT 'container', agent_id, COUNT(*) FROM container_metrics WHERE timestamp > dateadd('m', -2, now()) GROUP BY agent_id"
    }
    if (Has-Column "tcp_events" "agent_id") {
        $flowTables += "SELECT 'tcp_ev', agent_id, COUNT(*) FROM tcp_events WHERE timestamp > dateadd('m', -2, now()) GROUP BY agent_id"
        $flowTables += "SELECT 'dns_ev', agent_id, COUNT(*) FROM dns_events WHERE timestamp > dateadd('m', -2, now()) GROUP BY agent_id"
        $flowTables += "SELECT 'http_ev', agent_id, COUNT(*) FROM http_events WHERE timestamp > dateadd('m', -2, now()) GROUP BY agent_id"
    }
    
    $flowCounts = Query-QuestDB @"
SELECT agent_id, table_name, cnt FROM (
  $($flowTables -join "`n  UNION ALL`n  ")
)
ORDER BY agent_id, table_name
"@

    # -- Render --
    Clear-Host
    Write-Host "================================================================" -ForegroundColor Cyan
    Write-Host "  PARYTY LIVE METRICS DASHBOARD" -ForegroundColor Cyan
    Write-Host "  Refreshed: $now  (Ctrl+C to exit)" -ForegroundColor DarkGray
    Write-Host "================================================================" -ForegroundColor Cyan
    Write-Host ""

    if (-not $cpuLatest -or -not $cpuLatest.dataset) {
        Write-Host "  No agent data available. Is QuestDB running on port 9000?" -ForegroundColor Red
        Start-Sleep -Seconds 10
        continue
    }

    # Build column index maps for dynamic field access
    function Build-ColIndex($columns) {
        $map = @{}
        if ($columns) {
            for ($i = 0; $i -lt $columns.Count; $i++) {
                $map[$columns[$i].name] = $i
            }
        }
        return $map
    }

    $cpuColIdx = Build-ColIndex $cpuLatest.columns
    $memColIdx = Build-ColIndex $memLatest.columns
    $diskColIdx = Build-ColIndex $diskLatest.columns
    $netColIdx = Build-ColIndex $netLatest.columns

    foreach ($cpuRow in $cpuLatest.dataset) {
        $agentId = $cpuRow[0]
        $shortId = if ($agentId.Length -ge 8) { $agentId.Substring(0, 8) } else { $agentId }
        $cpuPct = [math]::Round($cpuRow[$cpuColIdx["pct"]], 1)
        $load1 = [math]::Round($cpuRow[$cpuColIdx["l1"]], 2)
        $load5 = [math]::Round($cpuRow[$cpuColIdx["l5"]], 2)
        $load15 = [math]::Round($cpuRow[$cpuColIdx["l15"]], 2)
        $freq = [math]::Round($cpuRow[$cpuColIdx["freq"]], 0)
        $ctx = $cpuRow[$cpuColIdx["ctx"]]

        # New CPU fields
        $cpuModel = if ($cpuColIdx.ContainsKey("model")) { $cpuRow[$cpuColIdx["model"]] } else { "" }
        $cpuVendor = if ($cpuColIdx.ContainsKey("vendor")) { $cpuRow[$cpuColIdx["vendor"]] } else { "" }
        $cpuPcores = if ($cpuColIdx.ContainsKey("pcores")) { $cpuRow[$cpuColIdx["pcores"]] } else { 0 }
        $cpuLcores = if ($cpuColIdx.ContainsKey("lcores")) { $cpuRow[$cpuColIdx["lcores"]] } else { 0 }

        Write-Host "  Agent: " -NoNewline; Write-Host "$shortId" -ForegroundColor Yellow -NoNewline
        if ($procCount -and $procCount.dataset) {
            $pcRow = $procCount.dataset | Where-Object { $_[0] -eq $agentId }
            if ($pcRow) { Write-Host "  ($($pcRow[1]) processes)" -ForegroundColor DarkGray }
            else { Write-Host "  (0 processes)" -ForegroundColor DarkYellow }
        } else { Write-Host "" }

        # -- CPU --
        $cpuColor = Color-Pct $cpuPct
        $cpuBar = Draw-Bar $cpuPct 20
        Write-Host "    CPU:  $cpuBar " -NoNewline; Write-Host "$cpuPct$pct" -ForegroundColor $cpuColor -NoNewline
        Write-Host "  freq: ${freq}MHz  load: $load1 / $load5 / $load15  ctx_sw: $ctx" -ForegroundColor DarkGray
        
        # Show new CPU fields if available
        if ($cpuModel -or $cpuVendor -or $cpuPcores -gt 0) {
            Write-Host "          " -NoNewline
            if ($cpuModel) { Write-Host "model: $cpuModel" -ForegroundColor DarkGray -NoNewline; Write-Host "  " -NoNewline }
            if ($cpuVendor) { Write-Host "vendor: $cpuVendor" -ForegroundColor DarkGray -NoNewline; Write-Host "  " -NoNewline }
            if ($cpuPcores -gt 0) { Write-Host "cores: ${cpuPcores}p/${cpuLcores}l" -ForegroundColor DarkGray }
            else { Write-Host "" }
        }

        # -- Memory --
        if ($memLatest -and $memLatest.dataset) {
            $memRow = $memLatest.dataset | Where-Object { $_[0] -eq $agentId }
            if ($memRow) {
                $mTotal = [long]$memRow[$memColIdx["total"]]
                $mUsed = [long]$memRow[$memColIdx["used"]]
                $mAvail = [long]$memRow[$memColIdx["avail"]]
                $mCached = [long]$memRow[$memColIdx["cached"]]
                $mSwapTotal = [long]$memRow[$memColIdx["swap_total"]]
                $mSwapUsed = [long]$memRow[$memColIdx["swap_used"]]
                $memPct = if ($mTotal -gt 0) { [math]::Round(($mUsed / $mTotal) * 100, 1) } else { 0 }
                $memBar = Draw-Bar $memPct 20
                $memColor = Color-Pct $memPct

                if ($mTotal -gt 0) {
                    Write-Host "    MEM:  $memBar " -NoNewline; Write-Host "$memPct$pct" -ForegroundColor $memColor -NoNewline
                    Write-Host "  ($(Format-Bytes $mUsed) / $(Format-Bytes $mTotal))" -ForegroundColor DarkGray -NoNewline
                    Write-Host "  cached: $(Format-Bytes $mCached)  avail: $(Format-Bytes $mAvail)" -ForegroundColor DarkGray
                } else {
                    Write-Host "    MEM:  " -NoNewline; Write-Host "[all zeros - scraper not collecting]" -ForegroundColor DarkYellow
                }

                if ($mSwapTotal -gt 0) {
                    $swapPct = [math]::Round(($mSwapUsed / $mSwapTotal) * 100, 1)
                    $swapUsedStr = Format-Bytes $mSwapUsed
                    $swapTotalStr = Format-Bytes $mSwapTotal
                    Write-Host ('    SWAP: {0} / {1} ({2}{3})' -f $swapUsedStr, $swapTotalStr, $swapPct, $pct) -ForegroundColor DarkGray
                }

                # Memory pressure (PSI) if available
                if ($memColIdx.ContainsKey("psi_some10")) {
                    $psiSome10 = [math]::Round([double]$memRow[$memColIdx["psi_some10"]], 2)
                    $psiSome60 = [math]::Round([double]$memRow[$memColIdx["psi_some60"]], 2)
                    $psiFull10 = [math]::Round([double]$memRow[$memColIdx["psi_full10"]], 2)
                    if ($psiSome10 -gt 0 -or $psiSome60 -gt 0 -or $psiFull10 -gt 0) {
                        $psiColor = if ($psiSome10 -gt 10) { "Red" } elseif ($psiSome10 -gt 5) { "Yellow" } else { "DarkGray" }
                        Write-Host "    PSI:  some: ${psiSome10}/${psiSome60}  full: ${psiFull10}" -ForegroundColor $psiColor
                    }
                }
            }
        }
        Write-Host ""

        # -- Disk --
        Write-Host "    DISK:" -ForegroundColor Cyan
        $diskHeader = "    {0,-12} {1,-10} {2,12} {3,12} {4,12} {5,12} {6,10} {7,10}" -f "Device", "Mount", "Total", "Used", "Read/s", "Write/s", "R-IOPS", "W-IOPS"
        if ($diskColIdx.ContainsKey("is_ssd")) { $diskHeader += "  SSD  Util" }
        Write-Host $diskHeader -ForegroundColor DarkGray
        Write-Host "    ------------------------------------------------------------------------------------------------" -ForegroundColor DarkGray
        if ($diskLatest -and $diskLatest.dataset) {
            $diskRows = $diskLatest.dataset | Where-Object { $_[0] -eq $agentId }
            foreach ($d in $diskRows) {
                $dev = $d[$diskColIdx["device"]]; $mnt = $d[$diskColIdx["mount_point"]]
                $dTotal = [long]$d[$diskColIdx["total"]]; $dUsed = [long]$d[$diskColIdx["used"]]
                $dRead = [long]$d[$diskColIdx["rrate"]]; $dWrite = [long]$d[$diskColIdx["wrate"]]
                $dRiops = [long]$d[$diskColIdx["riops"]]; $dWiops = [long]$d[$diskColIdx["wiops"]]
                $usedPct = if ($dTotal -gt 0) { [math]::Round(($dUsed / $dTotal) * 100, 0) } else { 0 }
                $zeroWarn = if ($dRead -eq 0 -and $dWrite -eq 0) { " *" } else { "" }
                $line = "    {0,-12} {1,-10} {2,12} {3,12} {4,12} {5,12} {6,10} {7,10}" -f $dev, $mnt, (Format-Bytes $dTotal), (Format-Bytes $dUsed), (Format-Rate $dRead), (Format-Rate $dWrite), $dRiops, $dWiops
                if ($diskColIdx.ContainsKey("is_ssd")) {
                    $isSsd = if ($d[$diskColIdx["is_ssd"]] -eq $true) { "Y" } else { "N" }
                    $util = if ($diskColIdx.ContainsKey("util")) { [math]::Round([double]$d[$diskColIdx["util"]], 1) } else { 0 }
                    $line += "  {0,3}  {1,5}{2}" -f $isSsd, "$util", $pct
                }
                Write-Host $line -NoNewline
                if ($zeroWarn) { Write-Host $zeroWarn -ForegroundColor DarkYellow } else { Write-Host "" }
            }
        }
        Write-Host ""

        # -- Network --
        Write-Host "    NET:" -ForegroundColor Cyan
        $netHeader = "    {0,-30} {1,12} {2,12} {3,12} {4,12} {5,8}" -f "Interface", "RX/s", "TX/s", "RX Pkts", "TX Pkts", "Err"
        if ($netColIdx.ContainsKey("total_rx")) { $netHeader += "  Total-RX     Total-TX  Speed  Up" }
        Write-Host $netHeader -ForegroundColor DarkGray
        Write-Host "    ------------------------------------------------------------------------------------------------" -ForegroundColor DarkGray
        if ($netLatest -and $netLatest.dataset) {
            $netRows = $netLatest.dataset | Where-Object { $_[0] -eq $agentId }
            foreach ($n in $netRows) {
                $iface = $n[$netColIdx["interface"]]
                $nRx = [long]$n[$netColIdx["rx"]]; $nTx = [long]$n[$netColIdx["tx"]]
                $nRxp = [long]$n[$netColIdx["rxp"]]; $nTxp = [long]$n[$netColIdx["txp"]]
                $nErr = [long]$n[$netColIdx["errs"]]
                $zeroWarn = if ($nRx -eq 0 -and $nTx -eq 0) { " *" } else { "" }
                $ifaceShort = if ($iface.Length -gt 28) { $iface.Substring(0, 25) + "..." } else { $iface }
                $line = "    {0,-30} {1,12} {2,12} {3,12} {4,12} {5,8}" -f $ifaceShort, (Format-Rate $nRx), (Format-Rate $nTx), $nRxp, $nTxp, $nErr
                if ($netColIdx.ContainsKey("total_rx")) {
                    $totalRx = [long]$n[$netColIdx["total_rx"]]
                    $totalTx = [long]$n[$netColIdx["total_tx"]]
                    $speed = if ($netColIdx.ContainsKey("speed")) { $n[$netColIdx["speed"]] } else { 0 }
                    $isUp = if ($netColIdx.ContainsKey("is_up") -and $n[$netColIdx["is_up"]] -eq $true) { "Y" } else { "N" }
                    $line += "  {0,12} {1,12} {2,6}  {3}" -f (Format-Bytes $totalRx), (Format-Bytes $totalTx), "${speed}M", $isUp
                }
                Write-Host $line -NoNewline
                if ($zeroWarn) { Write-Host $zeroWarn -ForegroundColor DarkYellow } else { Write-Host "" }
                
                # TCP stats if available
                if ($netColIdx.ContainsKey("tcp_est")) {
                    $tcpEst = [int]$n[$netColIdx["tcp_est"]]
                    $tcpTw = [int]$n[$netColIdx["tcp_tw"]]
                    $tcpListen = [int]$n[$netColIdx["tcp_listen"]]
                    if ($tcpEst -gt 0 -or $tcpTw -gt 0 -or $tcpListen -gt 0) {
                        Write-Host ("    {0,-30} TCP: est={1}  tw={2}  listen={3}" -f "", $tcpEst, $tcpTw, $tcpListen) -ForegroundColor DarkGray
                    }
                }
            }
        }
        Write-Host ""

        # -- Top Processes --
        Write-Host "    TOP PROCESSES (by memory):" -ForegroundColor Cyan
        $procHeader = "    {0,8} {1,-25} {2,14} {3,10}" -f "PID", "Name", "Memory", "CPU$pct"
        if ($procColIdx.ContainsKey("exe")) { $procHeader += "  Exe" }
        Write-Host $procHeader -ForegroundColor DarkGray
        Write-Host "    ----------------------------------------------------------------" -ForegroundColor DarkGray
        if ($procTop -and $procTop.dataset) {
            $procRows = $procTop.dataset | Where-Object { $_[0] -eq $agentId }
            foreach ($p in $procRows) {
                $pname = $p[1]; $pmem = [long]$p[2]; $pcpu = [math]::Round($p[3], 1)
                $pnameShort = if ($pname.Length -gt 23) { $pname.Substring(0, 20) + "..." } else { $pname }
                $cpuColor = if ($pcpu -gt 50) { "Red" } elseif ($pcpu -gt 10) { "Yellow" } else { "White" }
                $line = "    {0,8} {1,-25} {2,14}" -f "-", $pnameShort, (Format-Bytes $pmem)
                $line += " {0,10}" -f "$pcpu$pct"
                if ($procColIdx.ContainsKey("exe")) {
                    $exe = $p[$procColIdx["exe"]]
                    if ($exe) {
                        $exeShort = if ($exe.Length -gt 40) { "..." + $exe.Substring($exe.Length - 37) } else { $exe }
                        $line += "  $exeShort"
                    }
                }
                Write-Host $line -ForegroundColor $cpuColor
            }
        }
        Write-Host ""

        # -- Containers --
        if ($containerLatest -and $containerLatest.dataset) {
            $ctrRows = $containerLatest.dataset | Where-Object { $_[0] -eq $agentId }
            if ($ctrRows) {
                Write-Host "    CONTAINERS:" -ForegroundColor Cyan
                Write-Host ("    {0,-20} {1,-15} {2,-20} {3,12} {4,10}" -f "Container", "Name", "Image", "Mem Limit", "CPU Quota") -ForegroundColor DarkGray
                Write-Host "    ------------------------------------------------------------------------------------------------" -ForegroundColor DarkGray
                foreach ($c in $ctrRows) {
                    $cid = if ($c[1].Length -gt 18) { $c[1].Substring(0, 16) + ".." } else { $c[1] }
                    $cname = if ($c[2] -and $c[2].Length -gt 13) { $c[2].Substring(0, 10) + "..." } else { $c[2] }
                    $cimage = if ($c[3] -and $c[3].Length -gt 18) { $c[3].Substring(0, 15) + "..." } else { $c[3] }
                    $cstatus = $c[4]
                    $cmemLimit = [long]$c[5]
                    $ccpuQuota = [math]::Round([double]$c[6], 2)
                    Write-Host ("    {0,-20} {1,-15} {2,-20} {3,12} {4,10}" -f $cid, $cname, $cimage, (Format-Bytes $cmemLimit), "$ccpuQuota") -NoNewline
                    $statusColor = if ($cstatus -eq "running") { "Green" } else { "Yellow" }
                    Write-Host "  $cstatus" -ForegroundColor $statusColor
                }
                Write-Host ""
            }
        }
        # -- Network Events (eBPF / proc) --
        # Always show network events section with all-time totals
        $hasNetEvents = Has-Column "tcp_events" "agent_id"
        if ($hasNetEvents) {
            Write-Host "    NETWORK EVENTS (eBPF/proc):" -ForegroundColor Cyan
            
            # All-time totals first
            if ($tcpEventsAllTime -and $tcpEventsAllTime.dataset) {
                $tcpAllRows = @($tcpEventsAllTime.dataset | Where-Object { $_[0] -eq $agentId })
                foreach ($tr in $tcpAllRows) {
                    $tcpTotal = [int64]$tr[1]; $dsts = [int64]$tr[2]; $procs = [int64]$tr[3]; $first = [string]$tr[4]; $last = [string]$tr[5]
                    Write-Host "    TCP ALL-TIME:  $tcpTotal connections  |  Unique Destinations: $dsts  |  Unique Processes: $procs" -ForegroundColor Green
                    Write-Host "                   First: $first" -ForegroundColor DarkGray -NoNewline
                    Write-Host "  Last: $last" -ForegroundColor DarkGray
                }
            } else {
                Write-Host "    TCP ALL-TIME:  0 connections" -ForegroundColor DarkGray
            }
            
            # Recent activity (5-min window)
            if ($tcpEvents -and $tcpEvents.dataset) {
                $tcpRows = @($tcpEvents.dataset | Where-Object { $_[0] -eq $agentId })
                foreach ($tr in $tcpRows) {
                    $tcpCnt = [int64]$tr[1]; $dsts = [int64]$tr[2]; $procs = [int64]$tr[3]
                    Write-Host "    TCP last 5m:   $tcpCnt connections  |  Unique Destinations: $dsts  |  Unique Processes: $procs" -ForegroundColor Yellow
                }
            } else {
                Write-Host "    TCP last 5m:   0 connections" -ForegroundColor DarkGray
            }
            
            # DNS all-time + recent
            if ($dnsEventsAllTime -and $dnsEventsAllTime.dataset) {
                $dnsAllRows = @($dnsEventsAllTime.dataset | Where-Object { $_[0] -eq $agentId })
                foreach ($dr in $dnsAllRows) {
                    $dnsTotal = [int64]$dr[1]; $domains = [int64]$dr[2]; $first = [string]$dr[3]; $last = [string]$dr[4]
                    Write-Host "    DNS ALL-TIME:  $dnsTotal queries  |  Unique Domains: $domains" -ForegroundColor Green
                    Write-Host "                   First: $first" -ForegroundColor DarkGray -NoNewline
                    Write-Host "  Last: $last" -ForegroundColor DarkGray
                }
            } else {
                Write-Host "    DNS ALL-TIME:  0 queries" -ForegroundColor DarkGray
            }
            
            if ($dnsEvents -and $dnsEvents.dataset) {
                $dnsRows = $dnsEvents.dataset | Where-Object { $_[0] -eq $agentId }
                foreach ($dr in $dnsRows) {
                    $dnsCnt = $dr[1]; $domains = $dr[2]
                    Write-Host "    DNS last 5m:   $dnsCnt queries  |  Unique Domains: $domains" -ForegroundColor Yellow
                }
            } else {
                Write-Host "    DNS last 5m:   0 queries" -ForegroundColor DarkGray
            }
            
            # HTTP all-time + recent
            if ($httpEventsAllTime -and $httpEventsAllTime.dataset) {
                $httpAllRows = @($httpEventsAllTime.dataset | Where-Object { $_[0] -eq $agentId })
                foreach ($hr in $httpAllRows) {
                    $httpTotal = [int64]$hr[1]; $methods = [int64]$hr[2]; $first = [string]$hr[3]; $last = [string]$hr[4]
                    Write-Host "    HTTP ALL-TIME: $httpTotal requests  |  Methods: $methods" -ForegroundColor Green
                    Write-Host "                   First: $first" -ForegroundColor DarkGray -NoNewline
                    Write-Host "  Last: $last" -ForegroundColor DarkGray
                }
            } else {
                Write-Host "    HTTP ALL-TIME: 0 requests" -ForegroundColor DarkGray
            }
            
            if ($httpEvents -and $httpEvents.dataset) {
                $httpRows = $httpEvents.dataset | Where-Object { $_[0] -eq $agentId }
                if ($httpRows) {
                    Write-Host "    HTTP last 5m:" -ForegroundColor Yellow
                    Write-Host ("      {0,-8} {1,8} {2,12}" -f "Method", "Count", "Avg Latency") -ForegroundColor DarkGray
                    foreach ($hr in $httpRows) {
                        $method = $hr[1]; $hCnt = $hr[2]; $latency = $hr[3]
                        Write-Host ("      {0,-8} {1,8} {2,10}ms" -f $method, $hCnt, $latency) -ForegroundColor White
                    }
                }
            } else {
                Write-Host "    HTTP last 5m: 0 requests" -ForegroundColor DarkGray
            }
            
            # Recent connections (top 10)
            if ($tcpTopConns -and $tcpTopConns.dataset) {
                $topRows = @($tcpTopConns.dataset | Where-Object { $_[0] -eq $agentId })
                if ($topRows) {
                    Write-Host "    Recent TCP Connections (last 5 min):" -ForegroundColor Cyan
                    Write-Host ("      {0,-15} {1,-6} {2,-15} {3,-6} {4,-12} {5}" -f "Src IP", "SPort", "Dst IP", "DPort", "State", "Process") -ForegroundColor DarkGray
                    foreach ($tc in $topRows) {
                        $src = $tc[1]; $sp = $tc[2]; $dst = $tc[3]; $dp = $tc[4]; $state = $tc[5]; $proc = $tc[6]
                        $srcShort = if ($src.Length -gt 13) { $src.Substring(0,12)+".." } else { $src }
                        $dstShort = if ($dst.Length -gt 13) { $dst.Substring(0,12)+".." } else { $dst }
                        $procShort = if ($proc -and $proc.Length -gt 15) { $proc.Substring(0,12)+"..." } else { $proc }
                        $stateColor = if ($state -eq "ESTABLISHED") { "Green" } elseif ($state -eq "CLOSE_WAIT" -or $state -eq "TIME_WAIT") { "Yellow" } else { "White" }
                        Write-Host ("      {0,-15} {1,-6} {2,-15} {3,-6} {4,-12} {5}" -f $srcShort, $sp, $dstShort, $dp, $state, $procShort) -ForegroundColor $stateColor
                    }
                }
            }
            Write-Host ""
        }
    }

    # -- Data Flow Summary --
    Write-Host "  Data Flow (rows in last 2 min):" -ForegroundColor Cyan
    
    # Build dynamic header and format string
    $flowHeaders = @("Agent", "CPU", "Memory", "Disk", "Net", "Proc")
    if (Has-Column "container_metrics" "container_id") { $flowHeaders += "Cont" }
    if (Has-Column "tcp_events" "agent_id") { $flowHeaders += @("TCP", "DNS", "HTTP") }
    $headerLine = "  "
    foreach ($h in $flowHeaders) { $headerLine += "{0,-8}" -f $h }
    Write-Host $headerLine -ForegroundColor Cyan
    Write-Host ("  " + ("-" * ($flowHeaders.Count * 8))) -ForegroundColor DarkGray
    
    if ($flowCounts -and $flowCounts.dataset) {
        $grouped = @{}
        foreach ($row in $flowCounts.dataset) {
            $aid = if ($row[0].Length -ge 8) { $row[0].Substring(0, 8) } else { $row[0] }
            $tbl = $row[1]; $cnt = $row[2]
            if (-not $grouped.ContainsKey($aid)) { $grouped[$aid] = @{} }
            $grouped[$aid][$tbl] = $cnt
        }
        foreach ($aid in $grouped.Keys) {
            $t = $grouped[$aid]
            $cN = if ($t.ContainsKey("cpu")) { $t["cpu"] } else { 0 }
            $mN = if ($t.ContainsKey("memory")) { $t["memory"] } else { 0 }
            $dN = if ($t.ContainsKey("disk")) { $t["disk"] } else { 0 }
            $nN = if ($t.ContainsKey("network")) { $t["network"] } else { 0 }
            $pN = if ($t.ContainsKey("process")) { $t["process"] } else { 0 }
            $dataLine = "{0,-8}" -f $aid
            $dataLine += "{0,-8}" -f $cN
            $dataLine += "{0,-8}" -f $mN
            $dataLine += "{0,-8}" -f $dN
            $dataLine += "{0,-8}" -f $nN
            $dataLine += "{0,-8}" -f $pN
            if (Has-Column "container_metrics" "container_id") {
                $ctN = if ($t.ContainsKey("container")) { $t["container"] } else { 0 }
                $dataLine += "{0,-8}" -f $ctN
            }
            if (Has-Column "tcp_events" "agent_id") {
                $tcpN = if ($t.ContainsKey("tcp_ev")) { $t["tcp_ev"] } else { 0 }
                $dnsN = if ($t.ContainsKey("dns_ev")) { $t["dns_ev"] } else { 0 }
                $httpN = if ($t.ContainsKey("http_ev")) { $t["http_ev"] } else { 0 }
                $dataLine += "{0,-8}" -f $tcpN
                $dataLine += "{0,-8}" -f $dnsN
                $dataLine += "{0,-8}" -f $httpN
            }
            # Colorize: green if all network events present, yellow if partial
            $lineColor = "White"
            if (Has-Column "tcp_events" "agent_id") {
                $tcpN = if ($t.ContainsKey("tcp_ev")) { $t["tcp_ev"] } else { 0 }
                if ($tcpN -gt 0) { $lineColor = "Green" } else { $lineColor = "DarkGray" }
            }
            Write-Host $dataLine -ForegroundColor $lineColor
        }
    }
    Write-Host ""

    # -- Legend --
    $legendParts = @(
        @('  * ', 'DarkYellow'),
        @('= zero values (scraper issue). ', 'DarkGray'),
        @('green', 'Green'),
        @(' <50% ', 'DarkGray'),
        @('yellow', 'Yellow'),
        @(' <80% ', 'DarkGray'),
        @('red', 'Red'),
        @((' >=80' + $pct), 'DarkGray')
    )
    foreach ($part in $legendParts) {
        Write-Host $part[0] -ForegroundColor $part[1] -NoNewline
    }
    Write-Host ''
    Write-Host '  QuestDB console: http://localhost:9000' -ForegroundColor DarkGray
    Write-Host '  Columns auto-discovered every 60s. New metrics appear automatically.' -ForegroundColor DarkGray

    Start-Sleep -Seconds 10
}
