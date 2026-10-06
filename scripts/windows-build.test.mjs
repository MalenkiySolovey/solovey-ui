import test from 'node:test'
import assert from 'node:assert/strict'
import os from 'node:os'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

test('native Windows entrypoint rejects unsupported parameters without prompting',{skip:process.platform!=='win32'},() => {
  const entry=fileURLToPath(new URL('../windows/build-windows.ps1',import.meta.url))
  for(const args of [['-Architecture','mips'],['-Profile','partial'],['-UnknownOption'],['-NoCGO']]) {
    const result=spawnSync('powershell.exe',['-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass','-File',entry,...args],{cwd:os.tmpdir(),encoding:'utf8',timeout:15000})
    assert.equal(result.status,1,result.stdout+result.stderr)
    assert.equal(result.error,undefined)
  }
  const help=spawnSync('powershell.exe',['-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass','-File',entry,'-Help'],{cwd:os.tmpdir(),encoding:'utf8',timeout:15000})
  assert.equal(help.status,0,help.stderr)
  assert.match(help.stdout,/Usage: build-windows.ps1/)
})
