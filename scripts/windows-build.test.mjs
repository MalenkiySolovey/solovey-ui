import test from 'node:test'
import assert from 'node:assert/strict'
import os from 'node:os'
import fs from 'node:fs'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

test('native Windows entrypoint rejects unsupported parameters without prompting',{skip:process.platform!=='win32'},() => {
  const entry=fileURLToPath(new URL('../windows/build-windows.ps1',import.meta.url))
  const invoke=args => {
    // Windows PowerShell can start cold while the parallel asset test compiles
    // Go on hosted runners. Keep a deadline, but report spawn failures directly.
    const result=spawnSync('powershell.exe',['-NoLogo','-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass','-File',entry,...args],{cwd:os.tmpdir(),encoding:'utf8',windowsHide:true,timeout:60000})
    assert.ifError(result.error)
    return result
  }
  for(const args of [['-Architecture','mips'],['-Profile','partial'],['-Provider','unknown'],['-UnknownOption'],['-NoCGO']]) {
    const result=invoke(args)
    assert.equal(result.status,1,`${args.join(' ')}: ${result.stdout}${result.stderr}`)
  }
  const help=invoke(['-Help'])
  assert.equal(help.status,0,help.stderr)
  assert.match(help.stdout,/Usage: build-windows.ps1/)
})

test('Windows installation protects existing and newly created private state',{skip:process.platform!=='win32'},() => {
  const root=fs.mkdtempSync(path.join(os.tmpdir(),'solovey-windows-state-'))
  const entry=fileURLToPath(new URL('../windows/prepare-windows-install.ps1',import.meta.url))
  try {
    for(const name of ['db','logs','cert']) { fs.mkdirSync(path.join(root,name));fs.writeFileSync(path.join(root,name,'existing.txt'),'private fixture') }
    const result=spawnSync('powershell.exe',['-NoLogo','-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass','-File',entry,'-InstallDir',root],{encoding:'utf8',windowsHide:true,timeout:60000})
    assert.ifError(result.error);assert.equal(result.status,0,result.stdout+result.stderr)
    fs.writeFileSync(path.join(root,'db','new.txt'),'new private fixture')
    const observation=spawnSync('powershell.exe',['-NoProfile','-NonInteractive','-Command',
      '$ErrorActionPreference="Stop";Import-Module (Join-Path $PSHOME "Modules/Microsoft.PowerShell.Security/Microsoft.PowerShell.Security.psd1");$sid=[Security.Principal.WindowsIdentity]::GetCurrent().User.Value; $rows=@(Get-ChildItem -LiteralPath $env:SOLOVEY_WINDOWS_STATE_FIXTURE -Recurse -File | ForEach-Object { $acl=Get-Acl -LiteralPath $_.FullName; [pscustomobject]@{name=$_.Name;identities=@($acl.Access | ForEach-Object {$_.IdentityReference.Translate([Security.Principal.SecurityIdentifier]).Value})} }); [pscustomobject]@{user=$sid;rows=$rows}|ConvertTo-Json -Depth 6'],
      {env:{...process.env,SOLOVEY_WINDOWS_STATE_FIXTURE:root},encoding:'utf8',windowsHide:true,timeout:60000})
    assert.ifError(observation.error);assert.equal(observation.status,0,observation.stderr)
    const value=JSON.parse(observation.stdout);assert.equal(value.rows.length,4)
    for(const row of value.rows) assert.deepEqual(row.identities.sort(),['S-1-5-18','S-1-5-32-544',value.user].sort(),row.name)
    const missing=spawnSync('powershell.exe',['-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass','-File',entry,'-InstallDir',root,'-PackageRoot',root],{encoding:'utf8',windowsHide:true,timeout:60000})
    assert.equal(missing.status,1);assert.match(missing.stderr,/Required native companion is missing/)
  } finally { fs.rmSync(root,{recursive:true,force:true}) }
})
