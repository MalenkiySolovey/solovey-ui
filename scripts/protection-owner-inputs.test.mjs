import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import assert from 'node:assert/strict'
import {test} from 'node:test'
import {regularSourceTree} from './component-source-inputs.mjs'
import {materializeOwnerSource} from './protection-owner-inputs.mjs'

function fixture(t) {
  const directory=fs.mkdtempSync(path.join(os.tmpdir(),'solovey-owner-source-fixture-'))
  const source=path.join(directory,'accepted'),target=path.join(directory,'generated')
  fs.mkdirSync(source)
  fs.writeFileSync(path.join(source,'go.mod'),'module example.test/feature\n\ngo 1.26.11\n')
  fs.writeFileSync(path.join(source,'feature.go'),'package feature\nfunc Value() int { return 7 }\n')
  t.after(()=>fs.rmSync(directory,{recursive:true,force:true}))
  return {directory,source,target,tree:regularSourceTree(source)}
}

test('materialized test source preserves every accepted production byte',t=>{
  const f=fixture(t),result=materializeOwnerSource(f.source,f.target,f.tree)
  assert.equal(result.files,2)
  assert.equal(regularSourceTree(f.source),f.tree)
  fs.writeFileSync(path.join(f.target,'feature.go'),'package feature\n')
  assert.notEqual(regularSourceTree(f.target),f.tree)
  assert.equal(regularSourceTree(f.source),f.tree,'test input must not alias the module cache')
})

test('archive drift cannot become a qualified owner source',t=>{
  const f=fixture(t)
  fs.appendFileSync(path.join(f.source,'feature.go'),'// changed\n')
  assert.throws(()=>materializeOwnerSource(f.source,f.target,f.tree),/archive source drift/)
  assert.equal(fs.existsSync(f.target),false)
})

test('existing or nested destinations are preserved',t=>{
  const f=fixture(t)
  fs.mkdirSync(f.target);fs.writeFileSync(path.join(f.target,'sentinel'),'preserved')
  assert.throws(()=>materializeOwnerSource(f.source,f.target,f.tree),/preserve existing/)
  assert.equal(fs.readFileSync(path.join(f.target,'sentinel'),'utf8'),'preserved')
  assert.throws(()=>materializeOwnerSource(f.source,path.join(f.source,'nested'),f.tree),/outside/)
})

test('source junctions and symlinks cannot expand qualified inputs',t=>{
  const f=fixture(t),foreign=path.join(f.directory,'foreign')
  fs.mkdirSync(foreign);fs.writeFileSync(path.join(foreign,'secret'),'foreign')
  fs.symlinkSync(foreign,path.join(f.source,'external'),process.platform==='win32'?'junction':'dir')
  assert.throws(()=>materializeOwnerSource(f.source,f.target,f.tree),/unsupported source tree entry/)
  assert.equal(fs.existsSync(f.target),false)
})
