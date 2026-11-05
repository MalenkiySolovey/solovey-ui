[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$InstallDir,
    [string]$PackageRoot
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
Import-Module (Join-Path $PSHOME 'Modules/Microsoft.PowerShell.Security/Microsoft.PowerShell.Security.psd1') -ErrorAction Stop
$installRoot = [IO.Path]::GetFullPath($InstallDir)
if ($installRoot -eq [IO.Path]::GetPathRoot($installRoot)) { throw 'An installation directory is required' }
$rootItem = Get-Item -LiteralPath $installRoot -Force
if (!$rootItem.PSIsContainer -or ($rootItem.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'Installation root must be an ordinary directory' }

if ($PackageRoot) {
    $library = Join-Path ([IO.Path]::GetFullPath($PackageRoot)) 'libcronet.dll'
    if (!(Test-Path -LiteralPath $library -PathType Leaf)) { throw 'Required native companion is missing' }
    $reader = [IO.BinaryReader]::new([IO.File]::OpenRead((Join-Path $installRoot 'sui.exe')))
    try {
        if ($reader.ReadUInt16() -ne 0x5A4D) { throw 'Panel executable is not PE' }
        $reader.BaseStream.Position = 0x3C
        $offset = $reader.ReadInt32()
        if ($offset -lt 64 -or $offset -gt ($reader.BaseStream.Length - 24)) { throw 'Panel PE header is invalid' }
        $reader.BaseStream.Position = $offset
        if ($reader.ReadUInt32() -ne 0x4550) { throw 'Panel PE signature is invalid' }
        $machine = $reader.ReadUInt16()
    } finally { $reader.Dispose() }
    $pins = @{
        0x8664 = '3217c6260fbca5f16072e0b79735742f40109a63bb0ff88fd6b96dd6b54a2928'
        0xAA64 = 'a75a1b99a7e31802cf67ee14763e5cb9db6a24aaf7062f86c49f2fc1c030fcf0'
    }
    $expected = $pins[[int]$machine]
    if (!$expected -or (Get-FileHash -LiteralPath $library -Algorithm SHA256).Hash.ToLowerInvariant() -ne $expected) { throw 'Native companion does not match the panel architecture pin' }
    $destination = Join-Path $installRoot 'libcronet.dll'
    if ([IO.Path]::GetFullPath($library) -ne $destination) { Copy-Item -LiteralPath $library -Destination $destination }
    if ((Get-FileHash -LiteralPath $destination -Algorithm SHA256).Hash.ToLowerInvariant() -ne $expected) { throw 'Installed native companion digest differs' }
}

# LocalSystem owns the service. The installing user also retains access for
# the documented manual ARM64 mode. Other local users must not read the
# database key, private certificates, credentials or private logs.
$identities = @('S-1-5-18', 'S-1-5-32-544', [Security.Principal.WindowsIdentity]::GetCurrent().User.Value) | Select-Object -Unique
$items = @()
foreach ($name in @('db', 'logs', 'cert')) {
    $directory = Join-Path $installRoot $name
    if (!(Test-Path -LiteralPath $directory)) { New-Item -ItemType Directory -Path $directory | Out-Null }
    $items += Get-Item -LiteralPath $directory -Force
    $items += Get-ChildItem -LiteralPath $directory -Force -Recurse
}
foreach ($item in $items) {
    if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Private state cannot contain reparse points' }
}
foreach ($item in $items) {
    $acl = Get-Acl -LiteralPath $item.FullName
    $acl.SetAccessRuleProtection($true, $false)
    foreach ($rule in @($acl.Access)) { [void]$acl.RemoveAccessRuleAll($rule) }
    $inheritance = [Security.AccessControl.InheritanceFlags]::None
    if ($item.PSIsContainer) { $inheritance = [Security.AccessControl.InheritanceFlags]'ContainerInherit, ObjectInherit' }
    foreach ($identity in $identities) {
        $sid = [Security.Principal.SecurityIdentifier]::new($identity)
        $rule = [Security.AccessControl.FileSystemAccessRule]::new($sid, [Security.AccessControl.FileSystemRights]::FullControl, $inheritance, [Security.AccessControl.PropagationFlags]::None, [Security.AccessControl.AccessControlType]::Allow)
        $acl.AddAccessRule($rule)
    }
    Set-Acl -LiteralPath $item.FullName -AclObject $acl
}
Write-Host 'Native companion and private Windows state are prepared'
