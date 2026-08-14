[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Run the Windows uninstaller from an elevated terminal"
}

$serviceName = "solovey-ui"
$installRoot = [System.IO.Path]::GetFullPath((Join-Path $env:ProgramFiles "Solovey UI"))
$expectedRoot = [System.IO.Path]::GetFullPath("C:\Program Files\Solovey UI")
if (-not $installRoot.Equals($expectedRoot, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Refusing to remove an unexpected installation directory: $installRoot"
}

$service = Get-Service -Name $serviceName -ErrorAction SilentlyContinue
if ($null -ne $service) {
    if ($service.Status -ne [System.ServiceProcess.ServiceControllerStatus]::Stopped) {
        Stop-Service -Name $serviceName -Force
    }
    $service.Dispose()
    $service = $null
    $wrapper = Join-Path $installRoot "solovey-ui-service.exe"
    if (-not (Test-Path -LiteralPath $wrapper -PathType Leaf)) {
        throw "Service exists but its WinSW wrapper is missing: $wrapper"
    }
    & $wrapper uninstall
    if ($LASTEXITCODE -ne 0) {
        throw "Unable to uninstall the Solovey UI service"
    }
    for ($attempt = 0; $attempt -lt 20; $attempt++) {
        if ($null -eq (Get-Service -Name $serviceName -ErrorAction SilentlyContinue)) { break }
        Start-Sleep -Milliseconds 250
    }
    if ($null -ne (Get-Service -Name $serviceName -ErrorAction SilentlyContinue)) {
        throw "Solovey UI service is still registered"
    }
}

$desktopShortcut = Join-Path ([Environment]::GetFolderPath("Desktop")) "Solovey UI.lnk"
Remove-Item -LiteralPath $desktopShortcut -Force -ErrorAction SilentlyContinue
$startMenuDirectory = Join-Path ([Environment]::GetFolderPath("Programs")) "Solovey UI"
Remove-Item -LiteralPath $startMenuDirectory -Recurse -Force -ErrorAction SilentlyContinue
[Environment]::SetEnvironmentVariable("SUI_HOME", $null, "Machine")

$keepData = Read-Host "Keep database, logs, and certificates? [y/N]"
if ($keepData -match '^(?i:y|yes)$') {
    $runtimeFiles = @(
        ".windows-access.json",
        "configure-windows.ps1",
        "control-windows.ps1",
        "install-windows.bat",
        "README.md",
        "s-ui-windows.bat",
        "s-ui-windows.xml",
        "solovey-ui-service.exe",
        "solovey-ui-service.xml",
        "sui.exe",
        "uninstall-windows.bat",
        "uninstall-windows.ps1",
        "winsw.exe"
    )
    foreach ($name in $runtimeFiles) {
        Remove-Item -LiteralPath (Join-Path $installRoot $name) -Force -ErrorAction SilentlyContinue
    }
    Write-Host "Data preserved in $installRoot"
} elseif (Test-Path -LiteralPath $installRoot) {
    Set-Location -LiteralPath $env:SystemRoot
    Remove-Item -LiteralPath $installRoot -Recurse -Force
    if (Test-Path -LiteralPath $installRoot) {
        throw "Installation directory still exists after removal: $installRoot"
    }
}

Get-NetFirewallRule -DisplayName "Solovey UI Panel", "Solovey UI Subscription" -ErrorAction SilentlyContinue |
    Remove-NetFirewallRule -ErrorAction SilentlyContinue

Write-Host "Solovey UI uninstallation completed."
