# setup-vm.ps1 - M7: Linux VM Setup for Paryty Agent Testing
#
# Creates a Hyper-V VM with Ubuntu 22.04+ for agent testing with real /proc and eBPF.
#
# Prerequisites:
#   - Windows 10/11 Pro with Hyper-V enabled
#   - Ubuntu 22.04+ ISO downloaded
#   - At least 8GB free RAM (4GB for VM)
#
# Usage:
#   .\scripts\setup-vm.ps1 -IsoPath "C:\ISOs\ubuntu-22.04-live-server-amd64.iso"
#   .\scripts\setup-vm.ps1 -IsoPath "..." -VMName "paryty-agent-vm" -Memory 4GB

param(
    [Parameter(Mandatory=$true)]
    [string]$IsoPath,
    
    [string]$VMName = "paryty-agent-vm",
    [long]$MemoryBytes = 4GB,
    [int]$CPUs = 2,
    [long]$DiskBytes = 40GB,
    [string]$SwitchName = "Default Switch",
    [string]$VHDPath = ""
)

$ErrorActionPreference = "Stop"

Write-Host "`n=== Paryty v1.0 Linux VM Setup ===" -ForegroundColor Cyan

# Verify Hyper-V is available
try {
    $hvFeature = Get-WindowsOptionalFeature -Online -FeatureName Microsoft-Hyper-V-All -ErrorAction Stop
    if ($hvFeature.State -ne "Enabled") {
        Write-Host "ERROR: Hyper-V is not enabled. Enable it with:" -ForegroundColor Red
        Write-Host "  Enable-WindowsOptionalFeature -Online -FeatureName Microsoft-Hyper-V-All -NoRestart" -ForegroundColor White
        Write-Host "  Then restart your computer." -ForegroundColor White
        exit 1
    }
    Write-Host "Hyper-V: Enabled" -ForegroundColor Green
} catch {
    Write-Host "ERROR: Cannot check Hyper-V status - $($_.Exception.Message)" -ForegroundColor Red
    exit 1
}

# Verify ISO exists
if (-not (Test-Path $IsoPath)) {
    Write-Host "ERROR: ISO not found: $IsoPath" -ForegroundColor Red
    exit 1
}
Write-Host "ISO: $IsoPath" -ForegroundColor Gray

# Check if VM already exists
$existingVM = Get-VM -Name $VMName -ErrorAction SilentlyContinue
if ($existingVM) {
    Write-Host "WARNING: VM '$VMName' already exists (State: $($existingVM.State))" -ForegroundColor Yellow
    $response = Read-Host "Delete and recreate? (y/N)"
    if ($response -ne 'y') {
        Write-Host "Aborted." -ForegroundColor Yellow
        exit 0
    }
    if ($existingVM.State -ne "Off") {
        Stop-VM -Name $VMName -Force
    }
    Remove-VM -Name $VMName -Force
    Write-Host "Removed existing VM" -ForegroundColor Gray
}

# Set VHD path
if (-not $VHDPath) {
    $VHDPath = Join-Path (Get-VMHost).VirtualHardDiskPath "$VMName.vhdx"
}

# Create VM
Write-Host "`n--- Creating VM: $VMName ---" -ForegroundColor Yellow

$vm = New-VM -Name $VMName `
    -MemoryStartupBytes $MemoryBytes `
    -Generation 2 `
    -NewVHDPath $VHDPath `
    -NewVHDSizeBytes $DiskBytes `
    -SwitchName $SwitchName

# Configure VM
Set-VMProcessor -VMName $VMName -Count $CPUs
Set-VMMemory -VMName $VMName -DynamicMemoryEnabled $true -MinimumBytes 2GB -MaximumBytes $MemoryBytes

# Enable secure boot (required for Ubuntu)
Set-VMFirmware -VMName $VMName -SecureBootTemplate MicrosoftUEFICertificateAuthority

# Add DVD drive with ISO
Add-VMDvdDrive -VMName $VMName -Path $IsoPath

# Set boot order (DVD first for installation)
$dvd = Get-VMDvdDrive -VMName $VMName
Set-VMFirmware -VMName $VMName -FirstBootDevice $dvd

# Enable nested virtualization (for eBPF in containers)
Set-VMProcessor -VMName $VMName -ExposeVirtualizationExtensions $true

# Enable guest services (for file copy)
Enable-VMIntegrationService -VMName $VMName -Name "Guest Service Interface"

# Configure networking - allow VM to access host
# Add a second network adapter for host-only communication
# Add-VMNetworkAdapter -VMName $VMName -SwitchName "Internal" -Name "HostOnly"

Write-Host "VM created successfully:" -ForegroundColor Green
Write-Host "  Name:     $VMName" -ForegroundColor Gray
Write-Host "  Memory:   $($MemoryBytes / 1GB) GB (dynamic 2-$($MemoryBytes / 1GB) GB)" -ForegroundColor Gray
Write-Host "  CPUs:     $CPUs" -ForegroundColor Gray
Write-Host "  Disk:     $($DiskBytes / 1GB) GB" -ForegroundColor Gray
Write-Host "  VHD:      $VHDPath" -ForegroundColor Gray

Write-Host "`n--- Next Steps ---" -ForegroundColor Yellow
Write-Host "1. Start the VM:" -ForegroundColor White
Write-Host "   Start-VM -Name $VMName" -ForegroundColor Gray
Write-Host "2. Connect to VM console:" -ForegroundColor White
Write-Host "   vmconnect localhost $VMName" -ForegroundColor Gray
Write-Host "3. Install Ubuntu 22.04+ (server edition recommended)" -ForegroundColor White
Write-Host "4. After installation, run the setup script inside the VM:" -ForegroundColor White
Write-Host "   curl -sSL http://<host-ip>:8000/scripts/vm-setup.sh | bash" -ForegroundColor Gray
Write-Host "   OR copy scripts/vm-setup.sh to the VM and run it" -ForegroundColor Gray
Write-Host "5. Configure agent to connect to host cluster:" -ForegroundColor White
Write-Host "   export PARYTY_CLUSTER_ADDR=<host-ip>:8080" -ForegroundColor Gray
Write-Host ""
