import test from 'node:test'
import assert from 'node:assert/strict'
import os from 'node:os'
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
  for(const args of [['-Architecture','mips'],['-Profile','partial'],['-UnknownOption'],['-NoCGO']]) {
    const result=invoke(args)
    assert.equal(result.status,1,`${args.join(' ')}: ${result.stdout}${result.stderr}`)
  }
  const help=invoke(['-Help'])
  assert.equal(help.status,0,help.stderr)
  assert.match(help.stdout,/Usage: build-windows.ps1/)
})
