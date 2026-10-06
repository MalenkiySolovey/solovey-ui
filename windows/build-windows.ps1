# Native Windows build; the caller supplies the supported Go/Node/CGO toolchain.
[CmdletBinding(PositionalBinding = $false)]
param(
    [ValidateSet('amd64', 'arm64')][string]$Architecture = 'amd64',
    [ValidateSet('full', 'core')][string]$Profile = 'full',
    [string]$OutputPath,
    [switch]$Help
)

$ErrorActionPreference = 'Stop'
if ($Help) {
    Write-Host 'Usage: build-windows.ps1 [-Architecture amd64|arm64] [-Profile full|core] [-OutputPath path]'
    exit 0
}
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
if (!$OutputPath) { $OutputPath = Join-Path $repoRoot 'sui.exe' }
$OutputPath = [IO.Path]::GetFullPath($OutputPath)

function Invoke-Native([string]$Program, [string[]]$Arguments) {
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Program failed with exit code $LASTEXITCODE" }
}

$savedEnvironment = @{}
foreach ($name in @('GOOS', 'GOARCH', 'CGO_ENABLED', 'SOLOVEY_UI_PROFILE')) {
    $savedEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}
Push-Location -LiteralPath $repoRoot
try {
    $go = (Get-Command go -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
    $node = (Get-Command node -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
    $npm = (Get-Command npm.cmd -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
    $hostArchitecture = (& $go env GOHOSTARCH).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Cannot determine the native Go host architecture' }
    if ($hostArchitecture -ne $Architecture) { throw "CGO build requires native $Architecture; Go host is $hostArchitecture" }
    $env:GOOS = 'windows'
    $env:GOARCH = $Architecture
    $env:CGO_ENABLED = '1'
    $env:SOLOVEY_UI_PROFILE = $Profile
    Push-Location -LiteralPath (Join-Path $repoRoot 'frontend')
    try {
        Invoke-Native $npm @('ci')
        Invoke-Native $npm @('run', 'build')
    } finally { Pop-Location }
    Invoke-Native $node @('scripts/check-frontend-profile.mjs', '--profile', $Profile, '--dist', 'frontend/dist')
    Invoke-Native $node @('scripts/generate-component-imports.mjs', '--profile', $Profile)
    Invoke-Native $node @('scripts/frontend-assets.mjs', 'publish', '--dist', 'frontend/dist', '--destination', 'web/html')
    $tags = 'with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_tailscale'
    if ($Profile -eq 'core') { $tags += ',minimal' }
    Invoke-Native $go @('build', '-ldflags', '-w -s -checklinkname=0', '-tags', $tags, '-o', $OutputPath, 'main.go')
    Write-Host "Built Windows $Architecture ${Profile}: $OutputPath"
} finally {
    foreach ($name in $savedEnvironment.Keys) { [Environment]::SetEnvironmentVariable($name, $savedEnvironment[$name], 'Process') }
    Pop-Location
}
