import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { stripVTControlCharacters } from 'node:util'

test('Vitest receives one native path, unchanged arguments and propagates exit status',t => {
  const temporary=fs.mkdtempSync(path.join(os.tmpdir(),'frontend runner '))
  t.after(() => fs.rmSync(temporary,{recursive:true,force:true}))
  const frontend=path.join(temporary,'frontend with spaces')
  fs.mkdirSync(path.join(frontend,'scripts'),{recursive:true})
  fs.mkdirSync(path.join(frontend,'node_modules/vitest'),{recursive:true})
  const runner=path.join(frontend,'scripts/run-vitest.mjs')
  fs.copyFileSync(fileURLToPath(new URL('../frontend/scripts/run-vitest.mjs',import.meta.url)),runner)
  fs.writeFileSync(path.join(frontend,'node_modules/vitest/vitest.mjs'),'console.log(JSON.stringify({cwd:process.cwd(),args:process.argv.slice(2)}));process.exitCode=17')
  const aliases=[runner,path.join(frontend,'scripts','..','scripts','run-vitest.mjs')]
  if(process.platform==='win32')aliases.push(runner[0].toLowerCase()+runner.slice(1),runner[0].toUpperCase()+runner.slice(1))
  for(const alias of aliases) {
    const args=['run','src/a test.ts','--reporter','json','--testNamePattern','two words']
    const result=spawnSync(process.execPath,[alias,...args],{cwd:temporary,encoding:'utf8'})
    assert.equal(result.status,17,result.stderr)
    const observation=JSON.parse(result.stdout)
    assert.equal(observation.cwd,fs.realpathSync.native(frontend))
    assert.deepEqual(observation.args,args)
  }
})

test('real Windows Vitest works through a drive-letter alias',{skip:process.platform!=='win32'},() => {
  const frontend=fs.realpathSync.native(fileURLToPath(new URL('../frontend/',import.meta.url)))
  const alias=frontend[0].toLowerCase()+frontend.slice(1)
  const result=spawnSync(process.execPath,[path.join(alias,'scripts/run-vitest.mjs'),'run','src/features/inboundGuidance.test.ts'],{cwd:os.tmpdir(),encoding:'utf8',maxBuffer:4*1024*1024})
  assert.equal(result.status,0,result.stdout+result.stderr)
  assert.match(stripVTControlCharacters(result.stdout),/Tests\s+[1-9]\d* passed/)
})
