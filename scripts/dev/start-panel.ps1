param(
    [int] $StartupSeconds = 45,
    [switch] $Fresh,
    [switch] $OpenBrowser,
    [switch] $Build,
    [switch] $SkipFrontendBuild,
    [ValidateSet("full", "minimal", "core")]
    [string] $Profile = "",
    [string[]] $With = @(),
    [string[]] $Without = @(),
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]] $ComponentArgs = @()
)

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $scriptDir "..\.."))
$workspaceRoot = [System.IO.Path]::GetFullPath((Join-Path $repoRoot "..\.."))
$runtimeRoot = [System.IO.Path]::GetFullPath((Join-Path $repoRoot ".runtime\local-panel"))
$runtimeBase = [System.IO.Path]::GetFullPath((Join-Path $repoRoot ".runtime"))
$toolsEnv = Join-Path $workspaceRoot "tools\env.ps1"

if (!$runtimeRoot.StartsWith($runtimeBase, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Refusing to use runtime path outside .runtime: $runtimeRoot"
}

if (Test-Path $toolsEnv) {
    . $toolsEnv
    # tools/env.ps1 is dot-sourced on purpose so it can export Go/Node/Zig
    # environment variables, but it also defines $RepoRoot in the caller scope.
    # PowerShell variables are case-insensitive, so recompute the dev script
    # paths after loading it.
    $repoRoot = [System.IO.Path]::GetFullPath((Join-Path $scriptDir "..\.."))
    $workspaceRoot = [System.IO.Path]::GetFullPath((Join-Path $repoRoot "..\.."))
    $runtimeRoot = [System.IO.Path]::GetFullPath((Join-Path $repoRoot ".runtime\local-panel"))
    $runtimeBase = [System.IO.Path]::GetFullPath((Join-Path $repoRoot ".runtime"))
}

$devtools = Join-Path $workspaceRoot ".devtools"
$localGoBin = Join-Path $devtools "go\bin"
if (Test-Path (Join-Path $localGoBin "go.exe")) {
    $env:PATH = "$localGoBin;$env:PATH"
}

if (Test-Path $devtools) {
    $localNode = Get-ChildItem -LiteralPath $devtools -Directory -Filter "node-*-win-x64" -ErrorAction SilentlyContinue |
        Sort-Object Name -Descending |
        Select-Object -First 1
    if ($localNode -and (Test-Path (Join-Path $localNode.FullName "node.exe"))) {
        $env:PATH = "$($localNode.FullName);$env:PATH"
    }
}

$localZig = Join-Path $devtools "zig-x86_64-windows-0.16.0\zig.exe"
if ($IsWindows -or $env:OS -eq "Windows_NT") {
    if (Test-Path $localZig) {
        $env:PATH = "$(Split-Path -Parent $localZig);$env:PATH"
        if (!$env:CC) {
            $env:CC = "zig cc"
        }
    }
    if (!$env:CGO_ENABLED) {
        $env:CGO_ENABLED = "1"
    }
}

if ($Fresh -and (Test-Path $runtimeRoot)) {
    Remove-Item -LiteralPath $runtimeRoot -Recurse -Force
}

$dbDir = Join-Path $runtimeRoot "db"
$logDir = Join-Path $runtimeRoot "logs"
$pidFile = Join-Path $runtimeRoot "solovey-ui.pid"
$secretFile = Join-Path $runtimeRoot "secretbox.env"
$summaryFile = Join-Path $runtimeRoot "startup-summary.txt"

New-Item -ItemType Directory -Force -Path $dbDir, $logDir | Out-Null

function Split-ComponentList {
    param([string[]] $Values)

    $result = New-Object System.Collections.Generic.List[string]
    foreach ($value in $Values) {
        if ([string]::IsNullOrWhiteSpace($value)) {
            continue
        }
        foreach ($part in ($value -split ",")) {
            $id = $part.Trim()
            if ($id.Length -gt 0) {
                $result.Add($id)
            }
        }
    }
    # Unary comma keeps the (possibly empty) array from unrolling to $null
    # at the function boundary.
    return ,$result.ToArray()
}

function Parse-ComponentArgs {
    param([string[]] $Args)

    $parsed = @{
        Profile = ""
        With = @()
        Without = @()
    }

    for ($i = 0; $i -lt $Args.Count; $i++) {
        $arg = $Args[$i]
        if ($arg -match "^--?profile=(.+)$") {
            $parsed.Profile = $Matches[1]
            continue
        }
        if ($arg -match "^--?with=(.+)$") {
            $parsed.With += $Matches[1]
            continue
        }
        if ($arg -match "^--?without=(.+)$") {
            $parsed.Without += $Matches[1]
            continue
        }

        switch ($arg) {
            { $_ -in @("--profile", "-profile") } {
                if ($i + 1 -ge $Args.Count) { throw "$arg requires a value" }
                $i++
                $parsed.Profile = $Args[$i]
                continue
            }
            { $_ -in @("--with", "-with") } {
                if ($i + 1 -ge $Args.Count) { throw "$arg requires a value" }
                $i++
                $parsed.With += $Args[$i]
                continue
            }
            { $_ -in @("--without", "-without") } {
                if ($i + 1 -ge $Args.Count) { throw "$arg requires a value" }
                $i++
                $parsed.Without += $Args[$i]
                continue
            }
            default {
                throw "Unknown argument for start-panel.ps1: $arg"
            }
        }
    }

    return $parsed
}

function Get-AvailableComponents {
    $componentsRoot = Join-Path $repoRoot "components"
    if (!(Test-Path $componentsRoot)) {
        return @()
    }

    $components = @()
    foreach ($dir in Get-ChildItem -LiteralPath $componentsRoot -Directory | Sort-Object Name) {
        $manifestPath = Join-Path $dir.FullName "component.json"
        if (!(Test-Path $manifestPath)) {
            continue
        }
        $manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
        $components += [PSCustomObject]@{
            ID = [string] $manifest.id
            Delivery = [string] $manifest.delivery
            ManifestPath = $manifestPath
        }
    }
    return $components
}

function Resolve-ComponentSelection {
    param(
        [string] $RequestedProfile,
        [string[]] $RequestedWith,
        [string[]] $RequestedWithout,
        [object[]] $Available
    )

    if ([string]::IsNullOrWhiteSpace($RequestedProfile)) {
        $RequestedProfile = $env:SOLOVEY_UI_PROFILE
    }
    if ([string]::IsNullOrWhiteSpace($RequestedProfile)) {
        $RequestedProfile = "full"
    }
    if ($RequestedProfile -eq "core") {
        $RequestedProfile = "minimal"
    }
    if ($RequestedProfile -notin @("full", "minimal")) {
        throw "-Profile must be full, minimal or core: $RequestedProfile"
    }

    $availableByID = @{}
    foreach ($component in $Available) {
        $availableByID[$component.ID] = $component
    }

    $withIDs = Split-ComponentList $RequestedWith
    $withoutIDs = Split-ComponentList $RequestedWithout
    foreach ($id in @($withIDs) + @($withoutIDs)) {
        if (!$availableByID.ContainsKey($id)) {
            throw "Unknown component id: $id"
        }
    }

    $selected = New-Object System.Collections.Generic.HashSet[string]
    if ($withIDs.Count -gt 0) {
        foreach ($id in $withIDs) {
            [void] $selected.Add($id)
        }
    } elseif ($RequestedProfile -eq "full") {
        foreach ($component in $Available) {
            [void] $selected.Add($component.ID)
        }
    }

    foreach ($id in $withoutIDs) {
        [void] $selected.Remove($id)
    }

    $selectedIDs = @($selected | Sort-Object)
    $binaryProfile = "core"
    foreach ($id in $selectedIDs) {
        if ($availableByID[$id].Delivery -eq "in-process") {
            $binaryProfile = "full"
            break
        }
    }
    if ($selectedIDs.Count -gt 0 -and $binaryProfile -ne "full") {
        $binaryProfile = "full"
    }

    return [PSCustomObject]@{
        RequestedProfile = $RequestedProfile
        BinaryProfile = $binaryProfile
        FrontendProfile = $(if ($binaryProfile -eq "core") { "core" } else { "full" })
        SelectedIDs = $selectedIDs
        WithIDs = $withIDs
        WithoutIDs = $withoutIDs
    }
}

function Write-ComponentInstalledMetadata {
    param(
        [object[]] $Available,
        [string[]] $SelectedIDs,
        [string] $RequestedProfile,
        [string] $BinaryProfile,
        [bool] $CustomSelection
    )

    $componentsRoot = Join-Path $runtimeRoot "components"
    New-Item -ItemType Directory -Force -Path $componentsRoot | Out-Null

    $selected = @{}
    foreach ($id in $SelectedIDs) {
        $selected[$id] = $true
    }

    $items = @()
    foreach ($component in $Available) {
        if (!$selected.ContainsKey($component.ID)) {
            continue
        }
        $items += [ordered]@{
            id = $component.ID
            delivery = $component.Delivery
            installed = $true
        }

        $componentDir = Join-Path $componentsRoot $component.ID
        New-Item -ItemType Directory -Force -Path $componentDir | Out-Null
        Copy-Item -LiteralPath $component.ManifestPath -Destination (Join-Path $componentDir "component.json") -Force
    }

    $metadata = [ordered]@{
        version = 1
        profile = $(if ($CustomSelection) { "custom" } elseif ($RequestedProfile -eq "minimal") { "core" } else { "full" })
        binary = $BinaryProfile
        components = $items
    }
    $metadataPath = Join-Path $componentsRoot "installed.json"
    $metadata | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $metadataPath -Encoding ASCII
    $env:SUI_COMPONENTS_INSTALLED_FILE = $metadataPath
}

function Invoke-ComponentImportGeneration {
    param(
        [object] $Selection
    )

    $generator = Join-Path $repoRoot "scripts\generate-component-imports.mjs"
    if (!(Test-Path $generator)) {
        throw "Missing component import generator: $generator"
    }

    $node = Get-Command node -ErrorAction SilentlyContinue
    if (!$node) {
        throw "Node.js is required to generate backend component imports."
    }

    # Inline --name=value form: PowerShell 5.1 silently drops empty-string
    # arguments to native commands, so a space-separated "--selected-ids ''"
    # would make the next flag its value.
    $generatedIDs = ""
    if ($Selection.BinaryProfile -eq "core") {
        $generatedIDs = $Selection.SelectedIDs -join ","
    }
    & node $generator `
        "--profile=$($Selection.BinaryProfile)" `
        "--selected-ids=$generatedIDs" `
        "--out=$(Join-Path $repoRoot 'app\components_generated.go')"
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}

$parsedComponentArgs = Parse-ComponentArgs $ComponentArgs
if ($parsedComponentArgs.Profile) { $Profile = $parsedComponentArgs.Profile }
$With += $parsedComponentArgs.With
$Without += $parsedComponentArgs.Without

$availableComponents = Get-AvailableComponents
$componentSelection = Resolve-ComponentSelection `
    -RequestedProfile $Profile `
    -RequestedWith $With `
    -RequestedWithout $Without `
    -Available $availableComponents
$customComponentSelection = (Split-ComponentList $With).Count -gt 0 -or (Split-ComponentList $Without).Count -gt 0
Write-ComponentInstalledMetadata `
    -Available $availableComponents `
    -SelectedIDs $componentSelection.SelectedIDs `
    -RequestedProfile $componentSelection.RequestedProfile `
    -BinaryProfile $componentSelection.BinaryProfile `
    -CustomSelection $customComponentSelection

Write-Output "Component profile: requested=$($componentSelection.RequestedProfile), binary=$($componentSelection.BinaryProfile), frontend=$($componentSelection.FrontendProfile)"
if ($componentSelection.SelectedIDs.Count -gt 0) {
    Write-Output "Installed components: $($componentSelection.SelectedIDs -join ', ')"
} else {
    Write-Output "Installed components: none"
}

if (Test-Path $pidFile) {
    $existingPid = (Get-Content -LiteralPath $pidFile -Raw).Trim()
    if ($existingPid -match '^\d+$') {
        $existing = Get-Process -Id ([int] $existingPid) -ErrorAction SilentlyContinue
        if ($existing) {
            if ($Build) {
                Write-Output "Stopping existing Solovey UI process before rebuild (PID: $existingPid)..."
                Stop-Process -Id ([int] $existingPid) -Force
                Remove-Item -LiteralPath $pidFile -Force -ErrorAction SilentlyContinue
            } else {
                if (Test-Path $summaryFile) {
                    Get-Content -LiteralPath $summaryFile
                } else {
                    Write-Output "Solovey UI is already running."
                    Write-Output "PID: $existingPid"
                    Write-Output "URL: http://127.0.0.1:2095/app/"
                    Write-Output "Stop: .\scripts\dev\stop-panel.cmd"
                }
                exit 0
            }
        }
    }
}

if (!(Test-Path $secretFile)) {
    $bytes = New-Object byte[] 32
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $rng.GetBytes($bytes)
    } finally {
        $rng.Dispose()
    }
    $secret = [Convert]::ToBase64String($bytes)
    Set-Content -LiteralPath $secretFile -Value "SUI_SECRETBOX_KEY=$secret" -Encoding ASCII
}

$secretLine = Get-Content -LiteralPath $secretFile | Where-Object { $_ -like "SUI_SECRETBOX_KEY=*" } | Select-Object -First 1
if (!$secretLine) {
    throw "Missing SUI_SECRETBOX_KEY in $secretFile"
}

$env:SUI_DB_FOLDER = $dbDir
$env:SUI_DEBUG = "false"
$env:SUI_SECRETBOX_KEY = $secretLine.Substring("SUI_SECRETBOX_KEY=".Length).Trim()

$webIndex = Join-Path $repoRoot "web\html\index.html"
if ($Build -or !(Test-Path $webIndex)) {
    if ($SkipFrontendBuild) {
        if (!(Test-Path $webIndex)) {
            throw "web/html is missing. Run without -SkipFrontendBuild or build the frontend first."
        }
        Write-Output "Skipping frontend build because -SkipFrontendBuild was provided."
    } else {
        Write-Output "Building frontend..."
        Push-Location (Join-Path $repoRoot "frontend")
        try {
            $previousFrontendProfile = $env:SOLOVEY_UI_PROFILE
            $previousFrontendComponents = $env:SOLOVEY_UI_COMPONENT_IDS
            $env:SOLOVEY_UI_PROFILE = $componentSelection.FrontendProfile
            if ($componentSelection.FrontendProfile -eq "core") {
                $env:SOLOVEY_UI_COMPONENT_IDS = ""
            } else {
                Remove-Item Env:\SOLOVEY_UI_COMPONENT_IDS -ErrorAction SilentlyContinue
            }
            & npm ci
            if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
            & npm run build
            if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        } finally {
            if ($null -eq $previousFrontendProfile) {
                Remove-Item Env:\SOLOVEY_UI_PROFILE -ErrorAction SilentlyContinue
            } else {
                $env:SOLOVEY_UI_PROFILE = $previousFrontendProfile
            }
            if ($null -eq $previousFrontendComponents) {
                Remove-Item Env:\SOLOVEY_UI_COMPONENT_IDS -ErrorAction SilentlyContinue
            } else {
                $env:SOLOVEY_UI_COMPONENT_IDS = $previousFrontendComponents
            }
            Pop-Location
        }

        $webHtml = Join-Path $repoRoot "web\html"
        New-Item -ItemType Directory -Force -Path $webHtml | Out-Null
        Get-ChildItem -LiteralPath $webHtml -Force -ErrorAction SilentlyContinue | Remove-Item -Recurse -Force
        Copy-Item -Path (Join-Path $repoRoot "frontend\dist\*") -Destination $webHtml -Recurse -Force
    }
}

$binDir = Join-Path $repoRoot "bin"
$exe = Join-Path $binDir "solovey-ui.exe"
New-Item -ItemType Directory -Force -Path $binDir | Out-Null

if ($Build -or !(Test-Path $exe)) {
    Write-Output "Building backend..."
    Push-Location $repoRoot
    try {
        Invoke-ComponentImportGeneration -Selection $componentSelection
        $buildTags = "with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_tailscale"
        if ($componentSelection.BinaryProfile -eq "core") {
            $buildTags = "$buildTags,minimal"
        }
        & go build -tags $buildTags -o $exe main.go
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    } finally {
        Pop-Location
    }
}

$stdout = Join-Path $logDir "panel.out.log"
$stderr = Join-Path $logDir "panel.err.log"

$process = Start-Process `
    -FilePath $exe `
    -WorkingDirectory $repoRoot `
    -RedirectStandardOutput $stdout `
    -RedirectStandardError $stderr `
    -WindowStyle Hidden `
    -PassThru

Set-Content -LiteralPath $pidFile -Value $process.Id -Encoding ASCII

$url = "http://127.0.0.1:2095/app/"
$deadline = [DateTime]::UtcNow.AddSeconds($StartupSeconds)
$ready = $false

do {
    Start-Sleep -Milliseconds 500

    if ($process.HasExited) {
        Remove-Item -LiteralPath $pidFile -Force -ErrorAction SilentlyContinue
        throw "Panel exited early with code $($process.ExitCode). See $stdout and $stderr."
    }

    try {
        $response = Invoke-WebRequest -UseBasicParsing -Uri $url -TimeoutSec 2
        $ready = $response.StatusCode -ge 200 -and $response.StatusCode -lt 500
    } catch {
        $ready = $false
    }
} while (!$ready -and [DateTime]::UtcNow -lt $deadline)

if (!$ready) {
    Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $pidFile -Force -ErrorAction SilentlyContinue
    throw "Panel did not respond on $url within $StartupSeconds seconds. See $stdout and $stderr."
}

$summary = @(
    "Solovey UI is running."
    "PID: $($process.Id)"
    "URL: $url"
    "Runtime DB: $dbDir"
    "Logs: $logDir"
)

$adminFile = Join-Path $dbDir "initial-admin.txt"
if (Test-Path $adminFile) {
    $password = (Get-Content -LiteralPath $adminFile -Raw).Trim()
    $summary += "Initial admin username: admin"
    $summary += "Initial admin password: $password"
    $summary += "Delete after first login: $adminFile"
}

if ($OpenBrowser) {
    Start-Process $url
}

$summary += "Stop: .\scripts\dev\stop-panel.cmd"
$summary += "Clean runtime: .\scripts\dev\stop-panel.cmd -Clean"
$summary += "Summary file: $summaryFile"

Set-Content -LiteralPath $summaryFile -Value $summary -Encoding UTF8
Write-Output $summary
