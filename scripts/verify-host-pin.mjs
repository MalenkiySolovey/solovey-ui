// Consumer verification. Shared implementations are maintained in solovey-host.
import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
import {createHash} from 'node:crypto'
import {execFileSync} from 'node:child_process'
import {fileURLToPath} from 'node:url'

export const root=fileURLToPath(new URL('../',import.meta.url))
const modulePath='github.com/MalenkiySolovey/solovey-host'
const digest=value=>createHash('sha256').update(value).digest('hex')
const read=(directory,name)=>fs.readFileSync(path.join(directory,name),'utf8').replaceAll('\r\n','\n')
const run=(directory,args)=>execFileSync('go',args,{cwd:directory,encoding:'utf8',maxBuffer:16*1024*1024}).trim()

export function validateEnvironment(env) {
  assert.equal(env.GOWORK,'off','Host qualification requires GOWORK=off')
  assert.ok((env.GOFLAGS??'').split(/\s+/).every(flag=>!flag||flag==='-mod=readonly'),
    'Host verification rejects GOFLAGS source/modfile/overlay substitution')
}

export function validatePin(pin) {
  assert.equal(pin.schema,'solovey.host.consumer-pin/v1')
  assert.equal(pin.canonicalRepository,'MalenkiySolovey/solovey-host','wrong Host repository')
  assert.equal(pin.module,modulePath,'wrong Host module')
  assert.match(pin.version,/^v\d+\.\d+\.\d+$/,'immutable semantic module version required')
  for(const key of ['sourceCommit','sourceTree'])assert.match(pin[key],/^[a-f0-9]{40}$/)
  for(const key of ['moduleChecksum','goModChecksum'])assert.match(pin[key],/^h1:[A-Za-z0-9+/]{43}=$/)
  assert.match(pin.fixturesSHA256,/^[a-f0-9]{64}$/)
  assert.match(pin.valuesSHA256,/^[a-f0-9]{64}$/)
  assert.deepEqual(pin.dependencies,{'github.com/robfig/cron/v3':'v3.0.1'},'unreviewed Host dependency closure')
  assert.ok(Object.keys(pin.files).length>=6,'Host source hashes required')
  for(const [name,hash] of Object.entries(pin.files)) {
    assert.ok(!path.isAbsolute(name)&&!name.split('/').includes('..'),'invalid source path')
    assert.match(hash,/^[a-f0-9]{64}$/)
  }
}

export function validateRequirement(metadata,pin) {
  const requirements=(metadata.Require??[]).filter(item=>item.Path===modulePath)
  assert.equal(requirements.length,1,'missing/duplicate Host requirement')
  assert.equal(requirements[0].Version,pin.version,'provider-specific Host pin mismatch')
  assert.ok(!requirements[0].Indirect,'Host must be a direct dependency')
  assert.ok(!(metadata.Replace??[]).some(item=>item.Old.Path===modulePath),'Host replace/source substitution rejected')
}

export function validateResolution(metadata,pin) {
  assert.ok(!metadata.Error,'missing Host source')
  assert.equal(metadata.Path,pin.module,'wrong resolved Host module')
  assert.equal(metadata.Version,pin.version,'wrong resolved Host version')
  assert.ok(!metadata.Replace&&!metadata.Main,'local/mutable Host source substitution rejected')
  assert.equal(metadata.Sum,pin.moduleChecksum,'Host module checksum mismatch')
  assert.equal(metadata.GoModSum,pin.goModChecksum,'Host metadata checksum mismatch')
  if(metadata.Origin) {
    assert.equal(metadata.Origin.VCS,'git')
    assert.equal(metadata.Origin.Hash,pin.sourceCommit,'Host source commit mismatch')
    assert.equal(metadata.Origin.URL,`https://github.com/${pin.canonicalRepository}`)
  }
}

export function verifyFiles(directory,pin) {
  for(const [name,hash] of Object.entries(pin.files))
    assert.equal(digest(fs.readFileSync(path.join(directory,name))),hash,`Host source drift: ${name}`)
  const fixture=read(directory,'networkidentity/testdata/snapshots.json')
  assert.equal(digest(fixture),pin.fixturesSHA256,'Host fixture drift')
  const bundle=JSON.parse(fixture)
  assert.equal(bundle.schema,'host-network-identity-fixtures/v1')
  assert.equal(bundle.cases.length,27)
  assert.equal(new Set(bundle.cases.map(item=>item.name)).size,27)
  const values=read(directory,'qualification/testdata/values.json')
  assert.equal(digest(values),pin.valuesSHA256,'Host neutral-value fixture drift')
  assert.equal(Object.keys(JSON.parse(values)).length,13)
  return {network:fixture,values}
}

export function assertRetired(directory) {
  assert.ok(!fs.existsSync(path.join(directory,'internal/networkidentity')),'duplicate editable Network Identity implementation')
  const shim=path.join(directory,'componenthost/hostref/local.go')
  if(fs.existsSync(shim))assert.equal(read(directory,'componenthost/hostref/local.go'),
    '// Package hostref preserves the public Local Host import path.\n// Deprecated: import github.com/MalenkiySolovey/solovey-host/hostref.\npackage hostref\n\nimport canonical "github.com/MalenkiySolovey/solovey-host/hostref"\n\ntype Ref = canonical.Ref\n\nfunc Local() Ref { return canonical.Local() }\n',
    'Local Host compatibility path must only forward to its canonical owner')
}

export function verifyConsumer({directory=root,sync=false}={}) {
  validateEnvironment(process.env)
  const pin=JSON.parse(read(directory,'host/PIN.json'))
  validatePin(pin)
  assertRetired(directory)
  const downloaded=JSON.parse(run(directory,['mod','download','-json',`${pin.module}@${pin.version}`]))
  validateResolution(downloaded,pin)
  const fixtures=verifyFiles(downloaded.Dir,pin),resolutions=[]
  const modfiles=fs.existsSync(path.join(directory,'go.extended.mod'))?['go.mod','go.extended.mod']:['go.mod']
  for(const modfile of modfiles) {
    validateRequirement(JSON.parse(run(directory,['mod','edit','-json',`-modfile=${modfile}`])),pin)
    const selected=JSON.parse(run(directory,['list','-m','-json','-mod=readonly',`-modfile=${modfile}`,pin.module]))
    validateResolution(selected,pin)
    assert.equal(path.resolve(selected.Dir),path.resolve(downloaded.Dir),'selected Host source differs from verified module')
    resolutions.push({modfile,module:selected.Path,version:selected.Version,sum:selected.Sum})
  }
  for(const [name,text] of [['snapshots.json',fixtures.network],['values.json',fixtures.values]]) {
    const filename=path.join(directory,'host/fixtures',name)
    if(sync) { fs.mkdirSync(path.dirname(filename),{recursive:true}); fs.writeFileSync(filename,text) }
    assert.equal(read(directory,`host/fixtures/${name}`),text,`derived Host fixture differs from accepted module: ${name}`)
  }
  return {status:'PASS',module:pin.module,version:pin.version,sourceCommit:pin.sourceCommit,
    moduleChecksum:pin.moduleChecksum,fixturesSHA256:pin.fixturesSHA256,valuesSHA256:pin.valuesSHA256,
    networkFixtures:27,valueFixtures:13,resolutions}
}

if(process.argv[1]&&path.resolve(process.argv[1])===fileURLToPath(import.meta.url)) {
  assert.ok(process.argv.slice(2).every(arg=>arg==='--sync-fixtures'),'only --sync-fixtures is supported')
  console.log(JSON.stringify(verifyConsumer({sync:process.argv.includes('--sync-fixtures')})))
}
