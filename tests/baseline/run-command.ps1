param(
    [Parameter(Mandatory = $true)]
    [string]$Group,

    [Parameter(Mandatory = $true)]
    [string]$Name,

    [string]$CommandLine = "",

    [string]$WorkingDirectory = ".",

    [string]$SkipReason = "",

    [switch]$ContinueOnError
)

$ErrorActionPreference = "Stop"

function Assert-SafeResultSegment {
    param(
        [string]$Value,
        [string]$Label
    )

    if ($Value -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]*$') {
        throw "$Label must be a single safe path segment: $Value"
    }
}

function Resolve-RepositoryPath {
    param(
        [string]$RepositoryRoot,
        [string]$Value
    )

    $candidate = if ([System.IO.Path]::IsPathRooted($Value)) {
        [System.IO.Path]::GetFullPath($Value)
    } else {
        [System.IO.Path]::GetFullPath((Join-Path $RepositoryRoot $Value))
    }
    $comparison = if ($IsWindows -or $env:OS -eq 'Windows_NT') {
        [System.StringComparison]::OrdinalIgnoreCase
    } else {
        [System.StringComparison]::Ordinal
    }
    $prefix = $RepositoryRoot.TrimEnd(
        [System.IO.Path]::DirectorySeparatorChar,
        [System.IO.Path]::AltDirectorySeparatorChar
    ) + [System.IO.Path]::DirectorySeparatorChar

    if (-not $candidate.Equals($RepositoryRoot, $comparison) -and
        -not $candidate.StartsWith($prefix, $comparison)) {
        throw "WorkingDirectory must remain inside the repository: $Value"
    }
    if (-not (Test-Path -LiteralPath $candidate -PathType Container)) {
        throw "WorkingDirectory does not exist: $candidate"
    }
    return $candidate
}

function Escape-Xml {
    param([string]$Value)

    if ($null -eq $Value) {
        return ""
    }

    return $Value.
        Replace("&", "&amp;").
        Replace("<", "&lt;").
        Replace(">", "&gt;").
        Replace('"', "&quot;").
        Replace("'", "&apos;")
}

Assert-SafeResultSegment $Group "Group"
Assert-SafeResultSegment $Name "Name"

$repositoryRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot "../.."))
$resolvedWorkingDirectory = Resolve-RepositoryPath $repositoryRoot $WorkingDirectory
$groupDir = Join-Path (Join-Path $repositoryRoot "tests/baseline") $Group
New-Item -ItemType Directory -Force -Path $groupDir | Out-Null

$base = Join-Path $groupDir $Name
$txtPath = "$base.txt"
$xmlPath = "$base.junit.xml"
$start = Get-Date
$exitCode = 0
$output = ""
$status = "passed"

if ($SkipReason -ne "") {
    $status = "skipped"
    $output = $SkipReason
} else {
    Push-Location $resolvedWorkingDirectory
    try {
        if ($IsWindows -or $env:OS -eq 'Windows_NT') {
            $shell = if ($env:ComSpec) { $env:ComSpec } else { "cmd.exe" }
            $output = & $shell /d /s /c $CommandLine 2>&1 | Out-String
        } else {
            $output = & /bin/sh -c $CommandLine 2>&1 | Out-String
        }
        $exitCode = $LASTEXITCODE
        if ($null -eq $exitCode) {
            $exitCode = 0
        }
        if ($exitCode -ne 0) {
            $status = "failed"
        }
    } catch {
        $exitCode = 127
        $status = "failed"
        $output = $_ | Out-String
    } finally {
        Pop-Location
    }
}

$end = Get-Date
$duration = [Math]::Max(0, ($end - $start).TotalSeconds)
$header = @(
    "# Command: $CommandLine",
    "# WorkingDirectory: $resolvedWorkingDirectory",
    "# Status: $status",
    "# ExitCode: $exitCode",
    "# Started: $($start.ToString('o'))",
    "# Finished: $($end.ToString('o'))",
    ""
) -join "`r`n"

Set-Content -LiteralPath $txtPath -Value ($header + $output) -Encoding UTF8

$escapedOutput = Escape-Xml $output
if ($status -eq "skipped") {
    $junit = "<?xml version=`"1.0`" encoding=`"UTF-8`"?>`n<testsuite name=`"$Name`" tests=`"1`" failures=`"0`" errors=`"0`" skipped=`"1`" time=`"$duration`">`n  <testcase classname=`"baseline.$Group`" name=`"$Name`" time=`"$duration`">`n    <skipped message=`"skipped`">$escapedOutput</skipped>`n  </testcase>`n</testsuite>`n"
} elseif ($exitCode -eq 0) {
    $junit = "<?xml version=`"1.0`" encoding=`"UTF-8`"?>`n<testsuite name=`"$Name`" tests=`"1`" failures=`"0`" errors=`"0`" skipped=`"0`" time=`"$duration`">`n  <testcase classname=`"baseline.$Group`" name=`"$Name`" time=`"$duration`">`n    <system-out>$escapedOutput</system-out>`n  </testcase>`n</testsuite>`n"
} else {
    $junit = "<?xml version=`"1.0`" encoding=`"UTF-8`"?>`n<testsuite name=`"$Name`" tests=`"1`" failures=`"1`" errors=`"0`" skipped=`"0`" time=`"$duration`">`n  <testcase classname=`"baseline.$Group`" name=`"$Name`" time=`"$duration`">`n    <failure message=`"exit code $exitCode`">$escapedOutput</failure>`n    <system-out>$escapedOutput</system-out>`n  </testcase>`n</testsuite>`n"
}

Set-Content -LiteralPath $xmlPath -Value $junit -Encoding UTF8

if ($exitCode -ne 0 -and -not $ContinueOnError) {
    exit $exitCode
}

exit 0
