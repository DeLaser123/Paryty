$files = @(
    "cluster\go.mod",
    "cluster\go.sum"
)
foreach ($file in $files) {
    $path = Join-Path $PSScriptRoot $file
    if (Test-Path $path) {
        $bytes = [System.IO.File]::ReadAllBytes($path)
        if ($bytes.Length -ge 3 -and $bytes[0] -eq 0xEF -and $bytes[1] -eq 0xBB -and $bytes[2] -eq 0xBF) {
            $newBytes = $bytes[3..($bytes.Length - 1)]
            [System.IO.File]::WriteAllBytes($path, $newBytes)
            Write-Host "Fixed BOM in $file"
        } else {
            Write-Host "No BOM found in $file"
        }
    }
}
