# Start WSL2 Linux agent
Write-Host "Starting Linux agent..."
$wslProc = Start-Process -FilePath wsl -ArgumentList '-d','Ubuntu','--','bash','-c','cp /mnt/d/__Projects/Paryty/paryty-v1.0/agent/target/release/paryty-agent-linux-x86_64 /tmp/al2 && chmod +x /tmp/al2 && /tmp/al2 -c /mnt/d/__Projects/Paryty/paryty-v1.0/configs/agent/agent-gai-tech-linux.yaml' -WindowStyle Hidden -PassThru
Write-Host "Linux agent started (PID: $($wslProc.Id))"

# Start Frontend Vite dev server
Write-Host "Starting frontend..."
Set-Location d:\__Projects\Paryty\paryty-v1.0\frontend
$frontendProc = Start-Process -FilePath npx -ArgumentList 'vite','--host' -WindowStyle Hidden -PassThru -RedirectStandardOutput 'dev-server.log' -RedirectStandardError 'dev-server-err.log'
Write-Host "Frontend started (PID: $($frontendProc.Id))"

Write-Host "Done! Waiting 5 seconds..."
Start-Sleep -Seconds 5
Write-Host "Checking processes..."
Get-Process -Id $wslProc.Id,$frontendProc.Id -ErrorAction SilentlyContinue | Select-Object Id,ProcessName,StartTime
