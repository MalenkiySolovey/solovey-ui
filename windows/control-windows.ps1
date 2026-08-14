[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$serviceName = "solovey-ui"
$configuredRoot = [Environment]::GetEnvironmentVariable("SUI_HOME", "Machine")
if ([string]::IsNullOrWhiteSpace($configuredRoot)) {
    $configuredRoot = $PSScriptRoot
}
$installRoot = [System.IO.Path]::GetFullPath($configuredRoot)
$executable = Join-Path $installRoot "sui.exe"
$serviceWrapper = Join-Path $installRoot "solovey-ui-service.exe"
$accessStatePath = Join-Path $installRoot ".windows-access.json"

function Wait-ForEnter {
    Read-Host "Press Enter to continue" | Out-Null
}

function Require-File {
    param([Parameter(Mandatory = $true)][string]$Path)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "Required file is missing: $Path"
    }
}

function Invoke-ServiceWrapper {
    param([Parameter(Mandatory = $true)][ValidateSet("install", "uninstall")][string]$Action)
    Require-File $serviceWrapper
    & $serviceWrapper $Action
    if ($LASTEXITCODE -ne 0) {
        throw "Service wrapper action '$Action' failed with status $LASTEXITCODE"
    }
}

function Read-AccessState {
    if (-not (Test-Path -LiteralPath $accessStatePath -PathType Leaf)) {
        return $null
    }
    $state = Get-Content -LiteralPath $accessStatePath -Raw | ConvertFrom-Json
    $panelPort = 0
    $subscriptionPort = 0
    if (-not [int]::TryParse([string]$state.panelPort, [ref]$panelPort) -or
        -not [int]::TryParse([string]$state.subscriptionPort, [ref]$subscriptionPort) -or
        $panelPort -lt 1 -or $panelPort -gt 65535 -or
        $subscriptionPort -lt 1 -or $subscriptionPort -gt 65535) {
        throw "Stored Windows access ports are invalid"
    }
    foreach ($value in @([string]$state.panelPath, [string]$state.subscriptionPath)) {
        if ($value -notmatch '^/[A-Za-z0-9._/-]{0,126}/$') {
            throw "Stored Windows access path is invalid"
        }
    }
    return $state
}

function Show-AccessUrls {
    $state = Read-AccessState
    if ($null -eq $state) {
        Require-File $executable
        & $executable uri
        if ($LASTEXITCODE -ne 0) {
            throw "Unable to read panel URI"
        }
        return
    }

    $addresses = @("localhost") + @(
        Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue |
            Where-Object { $_.IPAddress -ne "127.0.0.1" -and $_.AddressState -eq "Preferred" } |
            Select-Object -ExpandProperty IPAddress -Unique
    )
    foreach ($address in $addresses) {
        Write-Host "Panel: http://${address}:$($state.panelPort)$($state.panelPath)"
        Write-Host "Subscription: http://${address}:$($state.subscriptionPort)$($state.subscriptionPath)"
    }
}

function Open-Panel {
    $state = Read-AccessState
    if ($null -eq $state) {
        throw "Run the Windows installer configuration before opening the panel"
    }
    $uri = "http://localhost:$($state.panelPort)$($state.panelPath)"
    Start-Process $uri
}

function Invoke-MenuAction {
    param([Parameter(Mandatory = $true)][string]$Choice)

    switch ($Choice) {
        "1" { Start-Service -Name $serviceName; Write-Host "Service started." }
        "2" { Stop-Service -Name $serviceName; Write-Host "Service stopped." }
        "3" { Restart-Service -Name $serviceName; Write-Host "Service restarted." }
        "4" { Get-Service -Name $serviceName | Format-Table -AutoSize }
        "5" {
            $logs = Join-Path $installRoot "logs"
            if (-not (Test-Path -LiteralPath $logs -PathType Container)) { throw "Logs directory is missing: $logs" }
            Start-Process -FilePath $logs
        }
        "6" { Open-Panel }
        "7" {
            Require-File $executable
            Push-Location $installRoot
            try { & $executable } finally { Pop-Location }
        }
        "8" {
            $serviceAction = Read-Host "Enter install, uninstall, or cancel"
            switch ($serviceAction.ToLowerInvariant()) {
                "install" {
                    if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64" -or $env:PROCESSOR_ARCHITEW6432 -eq "ARM64") {
                        throw "Automatic WinSW service installation is unavailable on Windows ARM64"
                    }
                    Invoke-ServiceWrapper "install"
                }
                "uninstall" { Invoke-ServiceWrapper "uninstall" }
                "cancel" { return }
                default { throw "Unknown service action" }
            }
        }
        "9" { Start-Process -FilePath $installRoot }
        "10" {
            Require-File $executable
            & $executable setting -show
            if ($LASTEXITCODE -ne 0) { throw "Unable to read panel settings" }
            & $executable admin -show
            if ($LASTEXITCODE -ne 0) { throw "Unable to read administrator status" }
        }
        "11" { Show-AccessUrls }
        default { throw "Choose a number from 0 through 11" }
    }
}

while ($true) {
    Clear-Host
    Write-Host "Solovey UI Windows Control Panel"
    Write-Host "Installation: $installRoot"
    Write-Host ""
    Write-Host "1. Start service"
    Write-Host "2. Stop service"
    Write-Host "3. Restart service"
    Write-Host "4. Show service status"
    Write-Host "5. Open logs"
    Write-Host "6. Open panel"
    Write-Host "7. Run manually"
    Write-Host "8. Install or uninstall service"
    Write-Host "9. Open installation directory"
    Write-Host "10. Show configuration"
    Write-Host "11. Show access URLs"
    Write-Host "0. Exit"

    $choice = Read-Host "Select an option"
    if ($choice -eq "0") { break }
    try {
        Invoke-MenuAction $choice
    } catch {
        Write-Host "Error: $($_.Exception.Message)" -ForegroundColor Red
    }
    Wait-ForEnter
}
