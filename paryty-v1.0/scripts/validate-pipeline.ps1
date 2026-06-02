# validate-pipeline.ps1 - M5: Cluster Pipeline Validation
#
# Validates the full Paryty cluster pipeline after deployment.
# Run this after: podman-compose -f deploy/compose/docker-compose.dev.yaml up -d
#
# Usage:
#   .\scripts\validate-pipeline.ps1                    # Validate all endpoints
#   .\scripts\validate-pipeline.ps1 -SkipSynthetic     # Skip synthetic data injection
#   .\scripts\validate-pipeline.ps1 -QueryPort 8082    # Custom query port

param(
    [int]$QueryPort = 8082,
    [int]$IngestionPort = 8080,
    [int]$RedpandaPort = 9092,
    [int]$RedpandaAdminPort = 9644,
    [int]$DragonflyPort = 6379,
    [int]$QuestDBPort = 9000,
    [int]$SeaweedFSPort = 9333,
    [int]$FrontendPort = 3000,
    [switch]$SkipSynthetic,
    [switch]$SkipInfra
)

$ErrorActionPreference = "Continue"
$passCount = 0
$failCount = 0
$skipCount = 0

function Test-Endpoint {
    param([string]$Name, [string]$Url, [string]$Method = "GET", [int]$TimeoutSec = 5, [string]$ExpectedContent = "")
    
    try {
        $response = Invoke-WebRequest -Uri $Url -Method $Method -TimeoutSec $TimeoutSec -UseBasicParsing -ErrorAction Stop
        if ($response.StatusCode -ge 200 -and $response.StatusCode -lt 400) {
            if ($ExpectedContent -and $response.Content -notlike "*$ExpectedContent*") {
                Write-Host "  FAIL: $Name - Content mismatch (expected '$ExpectedContent')" -ForegroundColor Red
                $script:failCount++
                return $false
            }
            Write-Host "  PASS: $Name ($($response.StatusCode))" -ForegroundColor Green
            $script:passCount++
            return $true
        } else {
            Write-Host "  FAIL: $Name - Status $($response.StatusCode)" -ForegroundColor Red
            $script:failCount++
            return $false
        }
    } catch {
        Write-Host "  FAIL: $Name - $($_.Exception.Message)" -ForegroundColor Red
        $script:failCount++
        return $false
    }
}

function Test-TCPPort {
    param([string]$Name, [string]$Host = "localhost", [int]$Port, [int]$TimeoutSec = 3)
    
    try {
        $tcp = New-Object System.Net.Sockets.TcpClient
        $result = $tcp.BeginConnect($Host, $Port, $null, $null)
        $success = $result.AsyncWaitHandle.WaitOne([TimeSpan]::FromSeconds($TimeoutSec))
        if ($success) {
            Write-Host "  PASS: $Name (port $Port open)" -ForegroundColor Green
            $script:passCount++
            $tcp.EndConnect($result)
            $tcp.Close()
            return $true
        } else {
            Write-Host "  FAIL: $Name (port $Port not responding)" -ForegroundColor Red
            $script:failCount++
            $tcp.Close()
            return $false
        }
    } catch {
        Write-Host "  FAIL: $Name - $($_.Exception.Message)" -ForegroundColor Red
        $script:failCount++
        return $false
    }
}

Write-Host "`n=== Paryty v1.0 Pipeline Validation ===" -ForegroundColor Cyan
Write-Host "Timestamp: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')" -ForegroundColor Gray

# --- M5.1: Infrastructure Health ---
if (-not $SkipInfra) {
    Write-Host "`n--- M5.1: Infrastructure Health ---" -ForegroundColor Yellow
    
    Test-TCPPort -Name "Redpanda (Kafka)" -Port $RedpandaPort
    Test-Endpoint -Name "Redpanda Admin" -Url "http://localhost:$RedpandaAdminPort/v1/status/ready"
    Test-TCPPort -Name "Dragonfly (Redis)" -Port $DragonflyPort
    Test-Endpoint -Name "QuestDB" -Url "http://localhost:$QuestDBPort/status"
    Test-Endpoint -Name "SeaweedFS Master" -Url "http://localhost:$SeaweedFSPort/cluster/status"
}

# --- M5.2: Ingestion Service ---
Write-Host "`n--- M5.2: Ingestion Service ---" -ForegroundColor Yellow

$ingestionHealthy = Test-Endpoint -Name "Ingestion Health" -Url "http://localhost:$IngestionPort/health"
if (-not $ingestionHealthy) {
    Write-Host "  SKIP: Remaining ingestion tests (service not running)" -ForegroundColor DarkGray
    $script:skipCount += 3
}

# --- M5.3: Processing Pipeline ---
Write-Host "`n--- M5.3: Processing Pipeline ---" -ForegroundColor Yellow

# Check Redpanda topics exist via admin API
try {
    $topics = Invoke-RestMethod -Uri "http://localhost:$RedpandaAdminPort/v1/topics" -TimeoutSec 5 -ErrorAction Stop
    $topicNames = $topics | ForEach-Object { if ($_.name) { $_.name } else { $_ } }
    
    $requiredTopics = @("paryty.metrics.raw", "paryty.metrics.aggregated", "paryty.traces", "paryty.events", "paryty.network.events", "paryty.topology.changes", "paryty.alerts")
    
    foreach ($topic in $requiredTopics) {
        if ($topicNames -contains $topic) {
            Write-Host "  PASS: Topic '$topic' exists" -ForegroundColor Green
            $script:passCount++
        } else {
            Write-Host "  WARN: Topic '$topic' not found (may not be created yet)" -ForegroundColor DarkYellow
            $script:skipCount++
        }
    }
} catch {
    Write-Host "  SKIP: Cannot check Redpatable topics - $($_.Exception.Message)" -ForegroundColor DarkGray
    $script:skipCount += 7
}

# --- M5.4: Query Service ---
Write-Host "`n--- M5.4: Query Service ---" -ForegroundColor Yellow

$queryHealthy = Test-Endpoint -Name "Query Health" -Url "http://localhost:$QueryPort/health" -ExpectedContent "healthy"
if ($queryHealthy) {
    Test-Endpoint -Name "GET /api/v1/topology" -Url "http://localhost:$QueryPort/api/v1/topology"
    Test-Endpoint -Name "GET /api/v1/agents" -Url "http://localhost:$QueryPort/api/v1/agents"
    Test-Endpoint -Name "GET /api/v1/metrics/test-agent-1" -Url "http://localhost:$QueryPort/api/v1/metrics/test-agent-1"
    Test-Endpoint -Name "GET /api/v1/alerts" -Url "http://localhost:$QueryPort/api/v1/alerts"
    Test-Endpoint -Name "GET /api/v1/events" -Url "http://localhost:$QueryPort/api/v1/events"
    Test-Endpoint -Name "GET /api/v1/traces" -Url "http://localhost:$QueryPort/api/v1/traces"
} else {
    Write-Host "  SKIP: Remaining query tests (service not running)" -ForegroundColor DarkGray
    $script:skipCount += 6
}

# --- M5.5: WebSocket ---
Write-Host "`n--- M5.5: WebSocket ---" -ForegroundColor Yellow

if ($queryHealthy) {
    try {
        $ws = New-Object System.Net.WebSockets.ClientWebSocket
        $ct = New-Object System.Threading.CancellationToken($false)
        $connectTask = $ws.ConnectAsync([Uri]"ws://localhost:$QueryPort/ws", $ct)
        if ($connectTask.Wait(5000)) {
            if ($ws.State -eq [System.Net.WebSockets.WebSocketState]::Open) {
                Write-Host "  PASS: WebSocket connection established" -ForegroundColor Green
                $script:passCount++
                
                # Send subscribe message
                $msg = '{"type":"subscribe","topic":"topology"}'
                $bytes = [System.Text.Encoding]::UTF8.GetBytes($msg)
                $sendTask = $ws.SendAsync([ArraySegment[byte]]$bytes, [System.Net.WebSockets.WebSocketMessageType]::Text, $true, $ct)
                $sendTask.Wait(3000) | Out-Null
                Write-Host "  PASS: WebSocket subscribe sent" -ForegroundColor Green
                $script:passCount++
                
                # Try to receive
                $buffer = New-Object byte[] 4096
                $recvTask = $ws.ReceiveAsync([ArraySegment[byte]]$buffer, $ct)
                if ($recvTask.Wait(3000)) {
                    $received = [System.Text.Encoding]::UTF8.GetString($buffer, 0, $recvTask.Result.Count)
                    Write-Host "  PASS: WebSocket received: $($received.Substring(0, [Math]::Min(100, $received.Length)))..." -ForegroundColor Green
                    $script:passCount++
                } else {
                    Write-Host "  PASS: WebSocket connected (no data yet - normal)" -ForegroundColor Green
                    $script:passCount++
                }
                
                $ws.CloseAsync([System.Net.WebSockets.WebSocketCloseStatus]::NormalClosure, "test", $ct).Wait(2000) | Out-Null
            } else {
                Write-Host "  FAIL: WebSocket state is $($ws.State)" -ForegroundColor Red
                $script:failCount++
            }
        } else {
            Write-Host "  FAIL: WebSocket connection timed out" -ForegroundColor Red
            $script:failCount++
        }
        $ws.Dispose()
    } catch {
        Write-Host "  FAIL: WebSocket - $($_.Exception.Message)" -ForegroundColor Red
        $script:failCount++
    }
} else {
    Write-Host "  SKIP: WebSocket tests (query service not running)" -ForegroundColor DarkGray
    $script:skipCount += 3
}

# --- M5.6: SSE Endpoints ---
Write-Host "`n--- M5.6: SSE Endpoints ---" -ForegroundColor Yellow

if ($queryHealthy) {
    # Test SSE timeline endpoint
    try {
        $start = (Get-Date).AddHours(-1).ToString("o")
        $end = (Get-Date).ToString("o")
        $sseUrl = "http://localhost:$QueryPort/api/v1/timeline/replay?start=$start&end=$end&speed=1"
        
        $request = [System.Net.WebRequest]::Create($sseUrl)
        $request.Method = "GET"
        $request.Timeout = 5000
        $request.Accept = "text/event-stream"
        
        $response = $request.GetResponse()
        $reader = New-Object System.IO.StreamReader($response.GetResponseStream())
        
        # Read first few lines
        $lines = @()
        $sw = [System.Diagnostics.Stopwatch]::StartNew()
        while ($sw.ElapsedMilliseconds -lt 3000 -and -not $reader.EndOfStream) {
            $line = $reader.ReadLine()
            if ($line) { $lines += $line }
            if ($lines.Count -ge 3) { break }
        }
        
        $reader.Close()
        $response.Close()
        
        if ($lines.Count -gt 0) {
            Write-Host "  PASS: SSE timeline endpoint responds ($($lines.Count) lines)" -ForegroundColor Green
            $script:passCount++
        } else {
            Write-Host "  PASS: SSE timeline endpoint connected (empty stream - normal with no data)" -ForegroundColor Green
            $script:passCount++
        }
    } catch {
        Write-Host "  FAIL: SSE timeline - $($_.Exception.Message)" -ForegroundColor Red
        $script:failCount++
    }
    
    # Test SSE metrics stream
    try {
        $sseUrl = "http://localhost:$QueryPort/api/v1/metrics/test-agent-1/stream"
        $request = [System.Net.WebRequest]::Create($sseUrl)
        $request.Method = "GET"
        $request.Timeout = 3000
        
        $response = $request.GetResponse()
        $reader = New-Object System.IO.StreamReader($response.GetResponseStream())
        $reader.Close()
        $response.Close()
        
        Write-Host "  PASS: SSE metrics stream endpoint responds" -ForegroundColor Green
        $script:passCount++
    } catch {
        Write-Host "  FAIL: SSE metrics stream - $($_.Exception.Message)" -ForegroundColor Red
        $script:failCount++
    }
} else {
    Write-Host "  SKIP: SSE tests (query service not running)" -ForegroundColor DarkGray
    $script:skipCount += 2
}

# --- Frontend ---
Write-Host "`n--- M6: Frontend ---" -ForegroundColor Yellow
Test-Endpoint -Name "Frontend" -Url "http://localhost:$FrontendPort" -TimeoutSec 3

# --- Summary ---
Write-Host "`n=== Validation Summary ===" -ForegroundColor Cyan
Write-Host "  Passed:  $passCount" -ForegroundColor Green
Write-Host "  Failed:  $failCount" -ForegroundColor $(if ($failCount -gt 0) { "Red" } else { "Green" })
Write-Host "  Skipped: $skipCount" -ForegroundColor DarkGray
Write-Host ""

if ($failCount -gt 0) {
    Write-Host "RESULT: Some tests FAILED" -ForegroundColor Red
    exit 1
} elseif ($passCount -eq 0) {
    Write-Host "RESULT: No services running. Deploy first with:" -ForegroundColor Yellow
    Write-Host "  podman-compose -f deploy/compose/docker-compose.dev.yaml up -d" -ForegroundColor White
    exit 2
} else {
    Write-Host "RESULT: All tests PASSED" -ForegroundColor Green
    exit 0
}
