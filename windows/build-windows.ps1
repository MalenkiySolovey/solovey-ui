# Native Windows build; the caller supplies the supported Go/Node/CGO toolchain.
[CmdletBinding(PositionalBinding = $false)]
param(
    [ValidateSet('amd64', 'arm64')][string]$Architecture = 'amd64',
    [ValidateSet('full', 'core')][string]$Profile = 'full',
    [ValidateSet('sing-box', 'shtorm-extended')][string]$Provider = 'sing-box',
    [string]$OutputPath,
    [string]$FrontendDist,
    [string]$TrustRootsFile,
    [string]$PackageDirectory,
    [string]$ExpectedSourceFingerprint,
    [switch]$CrossCompile,
    [switch]$Help
)

$ErrorActionPreference = 'Stop'
if ($Help) {
    Write-Host 'Usage: build-windows.ps1 [-Architecture amd64|arm64] [-Profile full|core] [-Provider sing-box|shtorm-extended] [-OutputPath path] [-FrontendDist path] [-TrustRootsFile path -PackageDirectory new-path] [-CrossCompile]'
    exit 0
}
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
if ($PackageDirectory) {
    if ($OutputPath -or !$TrustRootsFile -or !$FrontendDist) { throw 'Packaging requires FrontendDist and TrustRootsFile, without OutputPath' }
    $PackageDirectory = [IO.Path]::GetFullPath($PackageDirectory)
    if (Test-Path -LiteralPath $PackageDirectory) { throw 'PackageDirectory must not already exist' }
    $OutputPath = Join-Path $PackageDirectory 'sui.exe'
}
if (!$OutputPath) { $OutputPath = Join-Path $repoRoot 'sui.exe' }
$OutputPath = [IO.Path]::GetFullPath($OutputPath)

function Invoke-Native([string]$Program, [string[]]$Arguments) {
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Program failed with exit code $LASTEXITCODE" }
}

$savedEnvironment = @{}
foreach ($name in @('GOOS', 'GOARCH', 'CGO_ENABLED', 'SOLOVEY_UI_PROFILE', 'GOFLAGS', 'GOWORK')) {
    $savedEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}
Push-Location -LiteralPath $repoRoot
try {
    $go = (Get-Command go -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
    $node = (Get-Command node -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
    $npm = (Get-Command npm.cmd -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
    $hostArchitecture = (& $go env GOHOSTARCH).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Cannot determine the native Go host architecture' }
    if ($hostArchitecture -ne $Architecture -and (!$CrossCompile -or !$env:CC)) { throw "CGO cross-build requires explicit CrossCompile and CC; Go host is $hostArchitecture" }
    $env:GOOS = 'windows'
    $env:GOARCH = $Architecture
    $env:CGO_ENABLED = '1'
    $env:SOLOVEY_UI_PROFILE = $Profile
    $env:GOFLAGS = ''
    $env:GOWORK = 'off'
    $tags = 'with_quic,with_grpc,with_utls,with_acme,with_gvisor,with_tailscale'
    if ($PackageDirectory) { $tags += ',with_naive_outbound,with_purego,badlinkname,tfogo_checklinkname0' }
    if ($Profile -eq 'core') { $tags += ',minimal' }
    $planJSON = & $node scripts/core-provider-build.mjs plan --provider $Provider --tags $tags
    if ($LASTEXITCODE -ne 0) { throw 'Core provider source pin validation failed' }
    $plan = $planJSON | ConvertFrom-Json
    $sourceCommit = (& git rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Source commit is unavailable' }
    $sourceFingerprint = (& $node scripts/release-source.mjs .).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Source fingerprint is unavailable' }
    if ($ExpectedSourceFingerprint -and $sourceFingerprint -ne $ExpectedSourceFingerprint) { throw 'Source fingerprint changed before the producer' }
    if ($PackageDirectory -and (& git status --porcelain)) { throw 'Package source must be clean before generation' }
    if (!$FrontendDist) {
        Push-Location -LiteralPath (Join-Path $repoRoot 'frontend')
        try {
            Invoke-Native $npm @('ci')
            Invoke-Native $npm @('run', 'build')
        } finally { Pop-Location }
        $FrontendDist = Join-Path $repoRoot 'frontend/dist'
    }
    Invoke-Native $node @('scripts/check-frontend-profile.mjs', '--profile', $Profile, '--dist', $FrontendDist)
    Invoke-Native $node @('scripts/generate-component-imports.mjs', '--profile', $Profile)
    Invoke-Native $node @('scripts/frontend-assets.mjs', 'publish', '--dist', $FrontendDist, '--destination', 'web/html')
    $ldflags = '-buildid= -w -s -checklinkname=0'
    if ($TrustRootsFile) {
        $roots = (Get-Content -LiteralPath $TrustRootsFile -Raw).Trim()
        $decoded = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($roots)) | ConvertFrom-Json
        if (@($decoded).Count -lt 1 -or @($decoded).Count -gt 8) { throw 'Trust roots must contain one to eight public roots' }
        $ldflags += " -X github.com/MalenkiySolovey/solovey-ui/config/update.ReleaseTrustRootsBase64=$roots"
    }
    if ($PackageDirectory) { New-Item -ItemType Directory -Path $PackageDirectory | Out-Null }
    Invoke-Native $go (@('build') + @($plan.goArgs) + @('-trimpath', '-buildvcs=false', '-ldflags', $ldflags, '-tags', $plan.tags, '-o', $OutputPath, 'main.go'))
    if ($PackageDirectory) {
        # The selected lock authenticates both the engine and its companion.
        $module = "github.com/sagernet/cronet-go/lib/windows_$Architecture"
        Invoke-Native $go @('mod', 'download', "-modfile=$($plan.modFile)", $module)
        $libraryDir = (& $go list @($plan.goArgs) -m -f '{{.Dir}}' $module).Trim()
        if ($LASTEXITCODE -ne 0) { throw 'Cronet module directory is unavailable' }
        $expectedHash = @{
            amd64 = '3217c6260fbca5f16072e0b79735742f40109a63bb0ff88fd6b96dd6b54a2928'
            arm64 = 'a75a1b99a7e31802cf67ee14763e5cb9db6a24aaf7062f86c49f2fc1c030fcf0'
        }[$Architecture]
        $library = Join-Path $libraryDir 'libcronet.dll'
        if ((Get-FileHash -LiteralPath $library -Algorithm SHA256).Hash.ToLowerInvariant() -ne $expectedHash) { throw 'Cronet companion digest differs from its pin' }
        Copy-Item -LiteralPath $library -Destination (Join-Path $PackageDirectory 'libcronet.dll')
        Get-ChildItem -LiteralPath $PSScriptRoot -File | ForEach-Object { Copy-Item -LiteralPath $_.FullName -Destination $PackageDirectory }
        $metadata = @(
            "app=solovey-ui", "version=v$((Get-Content config/identity/version -Raw).Trim())", "commit=$sourceCommit",
            "source_fingerprint=$sourceFingerprint", "platform=windows/$Architecture", "profile=$Profile",
            "go=$(& $go version)", "sing_box=v$($plan.source.runtimeSemanticVersion)", "cronet_sha256=$expectedHash"
        )
        if ($Provider -eq 'shtorm-extended') {
            $metadata += & $node scripts/extended-integration-provenance.mjs build-info
            if ($LASTEXITCODE -ne 0) { throw 'Extended provenance is unavailable' }
        } else {
            $metadata += 'core_provider=sing-box'
            foreach ($script in @('quic', 'ssm')) {
                $metadata += & $node "scripts/$script-integration-provenance.mjs" build-info
                if ($LASTEXITCODE -ne 0) { throw 'Official provenance is unavailable' }
            }
        }
        $metadata += & $node scripts/anytls-integration-provenance.mjs build-info
        if ($LASTEXITCODE -ne 0) { throw 'AnyTLS provenance is unavailable' }
        [IO.File]::WriteAllText((Join-Path $PackageDirectory 'BUILD_INFO.txt'), (($metadata -join "`n") + "`n"), (New-Object Text.UTF8Encoding $false))
    }
    Write-Host "Built Windows $Architecture $Profile ${Provider}: $OutputPath"
} finally {
    foreach ($name in $savedEnvironment.Keys) { [Environment]::SetEnvironmentVariable($name, $savedEnvironment[$name], 'Process') }
    Pop-Location
}
