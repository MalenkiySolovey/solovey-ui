import fs from 'node:fs'
import path from 'node:path'
import os from 'node:os'
import assert from 'node:assert/strict'
import {execFileSync} from 'node:child_process'
import {test} from 'node:test'
import {root,validateEnvironment,validatePin,validateRequirement,validateResolution,verifyFiles,assertRetired} from './verify-host-pin.mjs'

const pin=JSON.parse(fs.readFileSync(path.join(root,'host/PIN.json')))
const downloaded=JSON.parse(execFileSync('go',['mod','download','-json',`${pin.module}@${pin.version}`],{cwd:root,encoding:'utf8'}))
const requirement=JSON.parse(execFileSync('go',['mod','edit','-json'],{cwd:root,encoding:'utf8'}))
const clone=value=>structuredClone(value)

test('accepted immutable Host source and canonical fixtures verify',()=>{
  validateEnvironment(process.env)
  validatePin(pin)
  validateRequirement(requirement,pin)
  validateResolution(downloaded,pin)
  assert.equal(JSON.parse(verifyFiles(downloaded.Dir,pin)).cases.length,27)
})
for(const field of ['canonicalRepository','module','version','sourceCommit','sourceTree','moduleChecksum','goModChecksum','fixturesSHA256'])
  test(`invalid Host ${field} is rejected`,()=>{
    const current=clone(pin);current[field]='untrusted-source'
    assert.throws(()=>validatePin(current))
  })
for(const field of ['Path','Version','Sum','GoModSum'])test(`wrong resolved Host ${field} is rejected`,()=>{
  const current=clone(downloaded);current[field]='untrusted-source'
  assert.throws(()=>validateResolution(current,pin))
})
test('unaccepted Host source commit is rejected',()=>{
  assert.throws(()=>validateResolution({...downloaded,Origin:{VCS:'git',URL:`https://github.com/${pin.canonicalRepository}`,Hash:'0'.repeat(40)}},pin),/source commit/)
})
test('missing source and local replacement are rejected',()=>{
  assert.throws(()=>validateResolution({Error:'not found'},pin),/missing Host/)
  assert.throws(()=>validateResolution({...downloaded,Replace:{Dir:'../solovey-host'}},pin),/substitution/)
  const metadata=clone(requirement)
  metadata.Replace=[{Old:{Path:pin.module},New:{Path:'../solovey-host'}}]
  assert.throws(()=>validateRequirement(metadata,pin),/substitution/)
})
test('provider pin skew and workspace or overlay substitution are rejected',()=>{
  const metadata=clone(requirement)
  metadata.Require.find(item=>item.Path===pin.module).Version='v99.0.0'
  assert.throws(()=>validateRequirement(metadata,pin),/pin mismatch/)
  assert.throws(()=>validateEnvironment({GOWORK:'somewhere/go.work'}),/GOWORK/)
  assert.throws(()=>validateEnvironment({GOWORK:'off',GOFLAGS:'-overlay=foreign.json'}),/substitution/)
})
test('altered Host algorithm and duplicate former implementation are rejected',()=>{
  const directory=fs.mkdtempSync(path.join(os.tmpdir(),'solovey-host-consumer-'))
  try {
    for(const name of Object.keys(pin.files)) {
      fs.mkdirSync(path.dirname(path.join(directory,name)),{recursive:true})
      fs.writeFileSync(path.join(directory,name),fs.readFileSync(path.join(downloaded.Dir,name)))
    }
    fs.appendFileSync(path.join(directory,'networkidentity/snapshot.go'),'\n')
    assert.throws(()=>verifyFiles(directory,pin),/source drift/)
    fs.mkdirSync(path.join(directory,'internal/networkidentity'),{recursive:true})
    assert.throws(()=>assertRetired(directory),/duplicate editable/)
  } finally {
    assert.equal(path.dirname(path.resolve(directory)),path.resolve(os.tmpdir()))
    assert.ok(path.basename(directory).startsWith('solovey-host-consumer-'))
    fs.rmSync(directory,{recursive:true,force:true})
  }
})
