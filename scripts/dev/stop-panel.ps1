param(
    [switch] $Clean
)

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $scriptDir "..\.."))
$runtimeRoot = [System.IO.Path]::GetFullPath((Join-Path $repoRoot ".runtime\local-panel"))
$runtimeBase = [System.IO.Path]::GetFullPath((Join-Path $repoRoot ".runtime"))

if (!$runtimeRoot.StartsWith($runtimeBase, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Refusing to use runtime path outside .runtime: $runtimeRoot"
}

$pidFile = Join-Path $runtimeRoot "solovey-ui.pid"

if (Test-Path $pidFile) {
    $pidValue = (Get-Content -LiteralPath $pidFile -Raw).Trim()
    if ($pidValue -match '^\d+$') {
        $process = Get-Process -Id ([int] $pidValue) -ErrorAction SilentlyContinue
        if ($process) {
            Stop-Process -Id $process.Id -Force
            Wait-Process -Id $process.Id -Timeout 10 -ErrorAction SilentlyContinue
            Write-Output "Stopped Solovey UI PID $pidValue"
        }
    }
    Remove-Item -LiteralPath $pidFile -Force -ErrorAction SilentlyContinue
} else {
    Write-Output "No local Solovey UI PID file found."
}

if ($Clean) {
    if (Test-Path $runtimeRoot) {
        Remove-Item -LiteralPath $runtimeRoot -Recurse -Force
        Write-Output "Removed runtime: $runtimeRoot"
    }
}
