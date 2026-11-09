import fs from 'node:fs'
import path from 'node:path'
import os from 'node:os'
import assert from 'node:assert/strict'
import {createHash} from 'node:crypto'
import {execFileSync} from 'node:child_process'
import {test} from 'node:test'
import {root,validateEnvironment,validatePin,validateRequirement,validateResolution,verifyBundle,assertRetired,assertNoSchemaCopy} from './verify-contract-pin.mjs'

const pin=JSON.parse(fs.readFileSync(path.join(root,'contracts/PIN.json')))
const downloaded=JSON.parse(execFileSync('go',['mod','download','-json',`${pin.module}@${pin.version}`],{cwd:root,encoding:'utf8'}))
const requirement=JSON.parse(execFileSync('go',['mod','edit','-json'],{cwd:root,encoding:'utf8'}))
const clone=value=>structuredClone(value)
function fixture(run) {
  const directory=fs.mkdtempSync(path.join(os.tmpdir(),'solovey-contract-consumer-'))
  try {
    fs.mkdirSync(path.join(directory,'tools'),{recursive:true})
    fs.mkdirSync(path.join(directory,'management/v1'),{recursive:true})
    // Go's authenticated module cache is read-only; mutations belong only to
    // new writable fixture files, without changing cache permissions or bytes.
    for(const name of fs.readdirSync(path.join(downloaded.Dir,'management/v1')))
      fs.writeFileSync(path.join(directory,'management/v1',name),fs.readFileSync(path.join(downloaded.Dir,'management/v1',name)))
    fs.writeFileSync(path.join(directory,'tools/generate.mjs'),fs.readFileSync(path.join(downloaded.Dir,'tools/generate.mjs')))
    run(directory,clone(pin))
  } finally {
    assert.equal(path.dirname(directory),os.tmpdir())
    assert.ok(path.basename(directory).startsWith('solovey-contract-consumer-'))
    fs.rmSync(directory,{recursive:true,force:true})
  }
}

test('real immutable module and all 115 fixtures verify',()=>{
  validatePin(pin)
  validateRequirement(requirement,pin)
  validateResolution(downloaded,pin)
  assert.equal(verifyBundle(downloaded.Dir,pin).vectorsSHA256,pin.vectorsSHA256)
})
for(const name of ['schema.json','vectors.json','types.gen.go','codec.go','manifest.json'])
  test(`altered ${name} with old digest is rejected`,()=>fixture((directory,current)=>{
    fs.appendFileSync(path.join(directory,'management/v1',name),'\n')
    assert.throws(()=>verifyBundle(directory,current))
  }))
test('altered generator with old manifest is rejected',()=>fixture((directory,current)=>{
  fs.appendFileSync(path.join(directory,'tools/generate.mjs'),'\n')
  assert.throws(()=>verifyBundle(directory,current),/generator drift/)
}))
test('bad canonical bundle digest is rejected independently of manifest pin',()=>fixture((directory,current)=>{
  const filename=path.join(directory,'management/v1/manifest.json'),manifest=JSON.parse(fs.readFileSync(filename))
  manifest.bundleSHA256=current.bundleSHA256='0'.repeat(64)
  const text=JSON.stringify(manifest)+'\n'
  current.manifestSHA256=createHash('sha256').update(text).digest('hex')
  fs.writeFileSync(filename,text)
  assert.throws(()=>verifyBundle(directory,current),/canonical bundle digest/)
}))
for(const field of ['canonicalRepository','module'])
  test(`wrong schema ${field} is rejected`,()=>{
    const current=clone(pin);current[field]='example.invalid/wrong-source'
    assert.throws(()=>validatePin(current),/wrong/)
  })
test('mutable module source is rejected',()=>{
  const current=clone(pin);current.version='main'
  assert.throws(()=>validatePin(current),/immutable/)
})
test('wrong module version is rejected',()=>{
  const current=clone(downloaded);current.Version='v9.0.0'
  assert.throws(()=>validateResolution(current,pin),/wrong resolved module version/)
})
test('missing module/producer is rejected',()=>assert.throws(()=>validateResolution({Error:'not found'},pin),/missing module/))
test('provider-specific module pin mismatch is rejected',()=>{
  const current=clone(requirement);current.Require.find(item=>item.Path===pin.module).Version='v1.0.1'
  assert.throws(()=>validateRequirement(current,pin),/provider-specific module pin mismatch/)
})
test('source substitution through replace is rejected',()=>{
  const current=clone(requirement);current.Replace=[{Old:{Path:pin.module},New:{Path:'../solovey-contracts'}}]
  assert.throws(()=>validateRequirement(current,pin),/replace\/source substitution/)
  const selected=clone(downloaded);selected.Replace={Dir:'../solovey-contracts'}
  assert.throws(()=>validateResolution(selected,pin),/source substitution/)
})
test('source substitution through go.work and GOFLAGS overlay is rejected',()=>{
  assert.throws(()=>validateEnvironment({GOWORK:'/tmp/foreign/go.work'}),/GOWORK=off/)
  assert.throws(()=>validateEnvironment({GOWORK:'off',GOFLAGS:'-overlay=foreign.json'}),/substitution/)
  assert.throws(()=>validateEnvironment({GOWORK:'off',GOFLAGS:'-modfile=foreign.mod'}),/substitution/)
})
test('wrong producer source commit and checksum are rejected',()=>{
  const source=clone(downloaded);source.Origin={VCS:'git',URL:`https://github.com/${pin.canonicalRepository}`,Hash:'0'.repeat(40)}
  assert.throws(()=>validateResolution(source,pin),/producer source commit/)
  const checksum=clone(downloaded);checksum.Sum='h1:'+'A'.repeat(43)+'='
  assert.throws(()=>validateResolution(checksum,pin),/module checksum/)
})
test('duplicate editable schema and obsolete copy writer are rejected',()=>fixture(directory=>{
  const target=path.join(directory,'contracts/management/v1')
  fs.mkdirSync(target,{recursive:true})
  fs.writeFileSync(path.join(target,'schema.json'),'{}')
  assert.throws(()=>assertRetired(directory),/duplicate editable/)
  fs.unlinkSync(path.join(target,'schema.json'))
  fs.mkdirSync(path.join(directory,'contracts/tools'),{recursive:true})
  fs.writeFileSync(path.join(directory,'contracts/tools/pin.mjs'),'// obsolete')
  assert.throws(()=>assertRetired(directory),/obsolete source-copy authority/)
}))
test('a schema copied to an arbitrary source filename is rejected',()=>fixture(directory=>{
  fs.copyFileSync(path.join(directory,'management/v1/schema.json'),path.join(directory,'shadow.json'))
  assert.throws(()=>assertNoSchemaCopy(directory,['shadow.json']),/duplicate editable management schema/)
}))
