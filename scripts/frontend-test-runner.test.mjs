import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { stripVTControlCharacters } from 'node:util'
import { digest } from './component-source-inputs.mjs'
import { prepareFrontendBuildInputs } from './frontend-build-inputs.mjs'
import { buildInputSchema, frontendAPIFiles } from '../frontend/scripts/component-build-inputs.mjs'

test('Vitest receives one native path, unchanged arguments and propagates exit status',t => {
  const temporary=fs.mkdtempSync(path.join(os.tmpdir(),'frontend runner '))
  t.after(() => fs.rmSync(temporary,{recursive:true,force:true}))
  const frontend=path.join(temporary,'frontend with spaces')
  fs.mkdirSync(path.join(frontend,'scripts'),{recursive:true})
  fs.mkdirSync(path.join(frontend,'node_modules/vitest'),{recursive:true})
  const runner=path.join(frontend,'scripts/run-vitest.mjs')
  fs.copyFileSync(fileURLToPath(new URL('../frontend/scripts/run-vitest.mjs',import.meta.url)),runner)
  for(const name of ['component-build-inputs.mjs','component-source-integrity.mjs'])fs.copyFileSync(fileURLToPath(new URL('../frontend/scripts/'+name,import.meta.url)),path.join(frontend,'scripts',name))
  for(const name of frontendAPIFiles) { const target=path.join(frontend,name);fs.mkdirSync(path.dirname(target),{recursive:true});fs.writeFileSync(target,'fixture') }
  const sourceLock={schema:'solovey.component-source-inputs/v1',sources:[]}
  const input={schema:buildInputSchema,sourceLock,sourceLockDigest:digest(JSON.stringify(sourceLock)),frontendFiles:Object.fromEntries(frontendAPIFiles.map(name=>[name,digest('fixture')])),components:[]}
  const bytes=JSON.stringify(input)
  fs.mkdirSync(path.join(frontend,'.component-inputs/components'),{recursive:true})
  fs.writeFileSync(path.join(frontend,'.component-inputs/manifest.json'),bytes)
  const env={...process.env,SOLOVEY_UI_INPUTS_SHA256:digest(bytes)}
  fs.writeFileSync(path.join(frontend,'node_modules/vitest/vitest.mjs'),'console.log(JSON.stringify({cwd:process.cwd(),args:process.argv.slice(2)}));process.exitCode=17')
  const aliases=[runner,path.join(frontend,'scripts','..','scripts','run-vitest.mjs')]
  if(process.platform==='win32')aliases.push(runner[0].toLowerCase()+runner.slice(1),runner[0].toUpperCase()+runner.slice(1))
  for(const alias of aliases) {
    const args=['run','src/a test.ts','--reporter','json','--testNamePattern','two words']
    const result=spawnSync(process.execPath,[alias,...args],{cwd:temporary,encoding:'utf8',env})
    assert.equal(result.status,17,result.stderr)
    const observation=JSON.parse(result.stdout)
    assert.equal(observation.cwd,fs.realpathSync.native(frontend))
    assert.deepEqual(observation.args,args)
  }
})

test('real Windows Vitest works through a drive-letter alias',{skip:process.platform!=='win32'},() => {
  const frontend=fs.realpathSync.native(fileURLToPath(new URL('../frontend/',import.meta.url)))
  const alias=frontend[0].toLowerCase()+frontend.slice(1)
  const inputs=prepareFrontendBuildInputs(path.dirname(frontend))
  const result=spawnSync(process.execPath,[path.join(alias,'scripts/run-vitest.mjs'),'run','src/features/inboundGuidance.test.ts'],{env:{...process.env,SOLOVEY_UI_INPUTS_SHA256:inputs.manifestSHA256},cwd:os.tmpdir(),encoding:'utf8',maxBuffer:4*1024*1024})
  assert.equal(result.status,0,result.stdout+result.stderr)
  assert.match(stripVTControlCharacters(result.stdout),/Tests\s+[1-9]\d* passed/)
})
