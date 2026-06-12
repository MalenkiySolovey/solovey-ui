param(
    [int] $StartupSeconds = 45,
    [switch] $Fresh,
    [switch] $OpenBrowser,
    [switch] $Build,
    [switch] $SkipFrontendBuild
)

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $scriptDir "..\.."))
$workspaceRoot = [System.IO.Path]::GetFullPath((Join-Path $repoRoot "..\.."))
$runtimeRoot = [System.IO.Path]::GetFullPath((Join-Path $repoRoot ".runtime\local-panel"))
$runtimeBase = [System.IO.Path]::GetFullPath((Join-Path $repoRoot ".runtime"))

if (!$runtimeRoot.StartsWith($runtimeBase, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Refusing to use runtime path outside .runtime: $runtimeRoot"
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

if (Test-Path $pidFile) {
    $existingPid = (Get-Content -LiteralPath $pidFile -Raw).Trim()
    if ($existingPid -match '^\d+$') {
        $existing = Get-Process -Id ([int] $existingPid) -ErrorAction SilentlyContinue
        if ($existing) {
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
            & npm ci
            if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
            & npm run build
            if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        } finally {
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
        & go build -o $exe main.go
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
