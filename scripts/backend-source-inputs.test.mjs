import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import {digest,regularSourceTree} from './component-source-inputs.mjs'
import {backendModule,validateBackendSourceLock,verifyBackendModuleSource,verifyProductBackendRetirement,backendSourceProperties} from './backend-source-inputs.mjs'

function fixture(t) {
  const root=fs.mkdtempSync(path.join(os.tmpdir(),'backend-source-test-'))
  t.after(()=>fs.rmSync(root,{recursive:true,force:true}))
  fs.mkdirSync(path.join(root,'tools'))
  const provenance=JSON.stringify({sourceRepository:'https://github.com/MalenkiySolovey/solovey-ui',sourceCommit:'1'.repeat(40),sourceTree:'2'.repeat(40),files:[{source:'backend/owner.go',output:'owner.go'}]})
  for(const [name,bytes] of Object.entries({'go.mod':'module '+backendModule+'\n','go.sum':'','LICENSE':'original license','NOTICE':'original attribution','README.md':'integration','tools/verify-source.mjs':'// source check\n','owner.go':'package backend\n','SOURCE_PROVENANCE.json':provenance}))fs.writeFileSync(path.join(root,name),bytes)
  const source={schema:'solovey.backend-source-inputs/v1',module:backendModule,version:'v0.1.0',sourceCommit:'3'.repeat(40),sourceTree:regularSourceTree(root),moduleChecksum:'h1:'+Buffer.alloc(32,1).toString('base64'),goModChecksum:'h1:'+Buffer.alloc(32,2).toString('base64'),provenanceSHA256:digest(provenance)}
  return {root,source}
}

test('Backend source identity rejects floating versions, foreign owners and hidden substitutions',()=>{
 const good={schema:'solovey.backend-source-inputs/v1',module:backendModule,version:'v0.1.0',sourceCommit:'1'.repeat(40),sourceTree:'2'.repeat(40),moduleChecksum:'h1:'+Buffer.alloc(32,1).toString('base64'),goModChecksum:'h1:'+Buffer.alloc(32,2).toString('base64'),provenanceSHA256:'3'.repeat(64)}
 assert.equal(validateBackendSourceLock(good),good)
 for(const mutate of [s=>{s.version='main'},s=>{s.module='example.invalid/backend'},s=>{s.sourceCommit='main'},s=>{s.moduleChecksum='unverified'},s=>{s.provenanceSHA256='unknown'},s=>{s.replace='../backend'}]){
  const changed=structuredClone(good);mutate(changed);assert.throws(()=>validateBackendSourceLock(changed))
 }
})

test('Backend archive byte drift is rejected even when module coordinates are unchanged',t=>{
 const {root,source}=fixture(t)
 assert.equal(verifyBackendModuleSource(root,source),source)
 fs.writeFileSync(path.join(root,'owner.go'),'package backend\n// changed bytes\n')
 assert.throws(()=>verifyBackendModuleSource(root,source),/Git source tree mismatch/)
})

test('Backend provenance cannot be replaced by a different original owner',t=>{
 const {root,source}=fixture(t)
 fs.writeFileSync(path.join(root,'SOURCE_PROVENANCE.json'),JSON.stringify({sourceRepository:'https://example.invalid/foreign'}))
 const changed={...source,sourceTree:regularSourceTree(root)}
 assert.throws(()=>verifyBackendModuleSource(root,changed),/provenance mismatch/)
 changed.provenanceSHA256=digest(fs.readFileSync(path.join(root,'SOURCE_PROVENANCE.json')))
 assert.throws(()=>verifyBackendModuleSource(root,changed),/original Backend source/)
})

test('Product source retirement rejects a second editable Backend implementation',t=>{
 const {root}=fixture(t)
 verifyProductBackendRetirement(root)
 fs.mkdirSync(path.join(root,'backend/storage'),{recursive:true})
 verifyProductBackendRetirement(root)
 fs.writeFileSync(path.join(root,'backend/storage/connection.go'),'package storage\n')
 assert.throws(()=>verifyProductBackendRetirement(root),/must be retired/)
})

test('Backend BOM properties bind the same source authority, tree and provenance',t=>{
 const {source}=fixture(t)
 const properties=Object.fromEntries(backendSourceProperties(source).map(p=>[p.name,p.value]))
 assert.equal(properties['solovey:sourceAuthority'],'https://'+backendModule)
 assert.equal(properties['solovey:sourceCommit'],source.sourceCommit)
 assert.equal(properties['solovey:sourceTree'],source.sourceTree)
 assert.equal(properties['solovey:sourceProvenanceSHA256'],source.provenanceSHA256)
})

test('declared native protection suite resolves Product and independent Backend owner packages', async () => {
 const {execFileSync}=await import('node:child_process')
 const {fileURLToPath}=await import('node:url')
 const root=fileURLToPath(new URL('../',import.meta.url))
 const workflow=fs.readFileSync(path.join(root,'.github/workflows/ci.yml'),'utf8')
 const command=workflow.split(/\r?\n/).find(line=>line.includes('node scripts/go-test.mjs')&&line.includes('./components/server-protection/...'))
 assert.ok(command,'native protection boundary suite command missing')
 const patterns=command.match(/\s--\s+(.+)$/)?.[1].trim().split(/\s+/)
 assert.ok(patterns?.length>=5,'native protection package selection missing')
 const tags=workflow.match(/^\s+EXTENDED_TAGS:\s*([^\s]+)\s*$/m)?.[1]
 assert.ok(tags,'native provider build tags missing')
 const selected=execFileSync('go',['list','-tags',tags,...patterns],{cwd:root,encoding:'utf8',env:{...process.env,GOWORK:'off',GOFLAGS:'-mod=readonly -modfile=go.extended.mod'},maxBuffer:4*1024*1024}).trim().split(/\r?\n/)
 assert.ok(selected.includes(backendModule+'/operations/coordination'),'canonical durable operation owner is not exercised')
 assert.ok(selected.includes('github.com/MalenkiySolovey/solovey-ui/internal/ops/privilegedbroker'),'Product privilege boundary is not exercised')
})
