[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$InstallDir
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$installRoot = [System.IO.Path]::GetFullPath($InstallDir)
$executable = Join-Path $installRoot "sui.exe"
if (-not (Test-Path -LiteralPath $executable -PathType Leaf)) {
    throw "Solovey UI executable is missing: $executable"
}

function Read-WithDefault {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Prompt,
        [Parameter(Mandatory = $true)]
        [string]$Default
    )

    $value = Read-Host "$Prompt [$Default]"
    if ([string]::IsNullOrWhiteSpace($value)) {
        return $Default
    }
    return $value
}

function ConvertTo-ValidatedPort {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Value,
        [Parameter(Mandatory = $true)]
        [string]$Name
    )

    $port = 0
    if (-not [int]::TryParse($Value, [ref]$port) -or $port -lt 1 -or $port -gt 65535) {
        throw "$Name must be an integer from 1 through 65535"
    }
    return $port
}

function ConvertTo-ValidatedPath {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Value,
        [Parameter(Mandatory = $true)]
        [string]$Name
    )

    if ($Value -notmatch '^/[A-Za-z0-9._/-]{0,126}/$') {
        throw "$Name must start and end with / and contain only letters, digits, dot, underscore, hyphen, or slash"
    }
    return $Value
}

function Invoke-SuiChecked {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments,
        [Parameter(Mandatory = $true)]
        [string[]]$RequiredOutput
    )

    $output = @(& $executable @Arguments 2>&1)
    $exitCode = $LASTEXITCODE
    $output | ForEach-Object { Write-Host $_ }
    $outputText = $output -join "`n"
    if ($exitCode -ne 0) {
        throw "sui.exe exited with status $exitCode"
    }
    foreach ($expected in $RequiredOutput) {
        if ($outputText.IndexOf($expected, [System.StringComparison]::Ordinal) -lt 0) {
            throw "sui.exe did not confirm: $expected"
        }
    }
}

Write-Host ""
Write-Host "Network configuration"
$panelPort = ConvertTo-ValidatedPort (Read-WithDefault "Panel port" "2095") "Panel port"
$panelPath = ConvertTo-ValidatedPath (Read-WithDefault "Panel path" "/app/") "Panel path"
$subscriptionPort = ConvertTo-ValidatedPort (Read-WithDefault "Subscription port" "2096") "Subscription port"
$subscriptionPath = ConvertTo-ValidatedPath (Read-WithDefault "Subscription path" "/sub/") "Subscription path"

Write-Host "Applying settings..."
Invoke-SuiChecked -Arguments @(
    "setting",
    "-port", $panelPort.ToString([Globalization.CultureInfo]::InvariantCulture),
    "-path", $panelPath,
    "-subPort", $subscriptionPort.ToString([Globalization.CultureInfo]::InvariantCulture),
    "-subPath", $subscriptionPath
) -RequiredOutput @(
    "set port success",
    "set path success",
    "set sub port success",
    "set sub path success"
)

Write-Host ""
Write-Host "Generating a protected administrator account..."
Invoke-SuiChecked -Arguments @("admin", "-reset") -RequiredOutput @("reset admin credentials success")
Write-Host "Save the administrator password shown above."

Write-Host ""
Write-Host "Configuration:"
Write-Host "  Panel port: $panelPort"
Write-Host "  Panel path: $panelPath"
Write-Host "  Subscription port: $subscriptionPort"
Write-Host "  Subscription path: $subscriptionPath"
Write-Host "  Administrator: admin"

$accessState = [ordered]@{
    panelPort       = $panelPort
    panelPath       = $panelPath
    subscriptionPort = $subscriptionPort
    subscriptionPath = $subscriptionPath
}
$accessState |
    ConvertTo-Json |
    Set-Content -LiteralPath (Join-Path $installRoot ".windows-access.json") -Encoding UTF8

$controlScript = Join-Path $installRoot "s-ui-windows.bat"
$shell = New-Object -ComObject WScript.Shell
$desktopShortcut = $shell.CreateShortcut((Join-Path ([Environment]::GetFolderPath("Desktop")) "Solovey UI.lnk"))
$desktopShortcut.TargetPath = $controlScript
$desktopShortcut.WorkingDirectory = $installRoot
$desktopShortcut.Description = "Solovey UI control panel"
$desktopShortcut.Save()

$startMenuDirectory = Join-Path ([Environment]::GetFolderPath("Programs")) "Solovey UI"
New-Item -ItemType Directory -Path $startMenuDirectory -Force | Out-Null
$startMenuShortcut = $shell.CreateShortcut((Join-Path $startMenuDirectory "Solovey UI Control Panel.lnk"))
$startMenuShortcut.TargetPath = $controlScript
$startMenuShortcut.WorkingDirectory = $installRoot
$startMenuShortcut.Description = "Solovey UI control panel"
$startMenuShortcut.Save()

$addresses = @(
    Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue |
        Where-Object { $_.IPAddress -ne "127.0.0.1" -and $_.AddressState -eq "Preferred" } |
        Select-Object -ExpandProperty IPAddress -Unique
)
if ($addresses.Count -gt 0) {
    Write-Host "Access URLs:"
    foreach ($address in $addresses) {
        Write-Host "  Panel: http://${address}:${panelPort}${panelPath}"
        Write-Host "  Subscription: http://${address}:${subscriptionPort}${subscriptionPath}"
    }
}
