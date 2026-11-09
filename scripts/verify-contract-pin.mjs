// Consumer verification only. Schema, codec and generator remain in Contracts.
import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
import {createHash} from 'node:crypto'
import {execFileSync} from 'node:child_process'
import {fileURLToPath} from 'node:url'

export const root=fileURLToPath(new URL('../',import.meta.url))
const modfiles=['go.mod','go.extended.mod']
const legacyShim=true
const modulePath='github.com/MalenkiySolovey/solovey-contracts'
const normalize=value=>value.replaceAll('\r\n','\n')
const digest=value=>createHash('sha256').update(value).digest('hex')
const read=(directory,name)=>normalize(fs.readFileSync(path.join(directory,name),'utf8'))
const run=(directory,args)=>execFileSync('go',args,{cwd:directory,encoding:'utf8',maxBuffer:16*1024*1024}).trim()

export function validateEnvironment(env) {
  assert.equal(env.GOWORK,'off','contract qualification requires GOWORK=off')
  assert.ok((env.GOFLAGS??'').split(/\s+/).every(flag=>!flag||flag==='-mod=readonly'),
    'contract verification rejects GOFLAGS source/modfile/overlay substitution')
}

export function validatePin(pin) {
  assert.equal(pin.schema,'solovey.contracts.consumer-pin/v1')
  assert.equal(pin.canonicalRepository,'MalenkiySolovey/solovey-contracts','wrong schema source')
  assert.equal(pin.module,modulePath,'wrong module source')
  assert.match(pin.version,/^v\d+\.\d+\.\d+$/,'immutable semantic module version required')
  assert.match(pin.sourceCommit,/^[a-f0-9]{40}$/)
  assert.match(pin.sourceTree,/^[a-f0-9]{40}$/)
  for(const key of ['moduleChecksum','goModChecksum'])assert.match(pin[key],/^h1:[A-Za-z0-9+/]{43}=$/)
  for(const key of ['manifestSHA256','schemaSHA256','generatedSHA256','vectorsSHA256','bundleSHA256'])
    assert.match(pin[key],/^[a-f0-9]{64}$/)
  assert.equal(pin.contractVersion,'solovey.management/v1')
}

export function validateRequirement(metadata,pin) {
  const requirements=(metadata.Require??[]).filter(item=>item.Path===modulePath)
  assert.equal(requirements.length,1,'missing/duplicate Contracts requirement')
  assert.equal(requirements[0].Version,pin.version,'provider-specific module pin mismatch')
  assert.ok(!requirements[0].Indirect,'Contracts must be a direct dependency')
  assert.ok(!(metadata.Replace??[]).some(item=>item.Old.Path===modulePath),'Contracts replace/source substitution rejected')
}

export function validateResolution(metadata,pin) {
  assert.ok(!metadata.Error,'missing module/producer')
  assert.equal(metadata.Path,pin.module,'wrong resolved module')
  assert.equal(metadata.Version,pin.version,'wrong resolved module version')
  assert.ok(!metadata.Replace&&!metadata.Main,'local/mutable source substitution rejected')
  assert.equal(metadata.Sum,pin.moduleChecksum,'module checksum mismatch')
  assert.equal(metadata.GoModSum,pin.goModChecksum,'module metadata checksum mismatch')
  if(metadata.Origin) {
    assert.equal(metadata.Origin.VCS,'git')
    assert.equal(metadata.Origin.Hash,pin.sourceCommit,'producer source commit mismatch')
    assert.equal(metadata.Origin.URL,`https://github.com/${pin.canonicalRepository}`)
  }
}

export function verifyBundle(directory,pin) {
  const manifestText=read(directory,'management/v1/manifest.json'),manifest=JSON.parse(manifestText)
  assert.equal(digest(manifestText),pin.manifestSHA256,'pinned manifest drift')
  for(const key of ['contractVersion','schemaSHA256','generatedSHA256','vectorsSHA256','bundleSHA256'])
    assert.equal(manifest[key],pin[key],`pin mismatch: ${key}`)
  assert.deepEqual(Object.keys(manifest.files).sort(),['README.md','codec.go','codec_test.go','schema.json','types.gen.go','vectors.json'])
  for(const [name,hash] of Object.entries(manifest.files))
    assert.equal(digest(read(directory,`management/v1/${name}`)),hash,`source bundle drift: ${name}`)
  const records=Object.entries(manifest.files).sort(([a],[b])=>a<b?-1:1).map(([name,hash])=>`${name}\0${hash}\n`).join('')
  assert.equal(digest(records),manifest.bundleSHA256,'canonical bundle digest mismatch')
  assert.equal(digest(read(directory,'tools/generate.mjs')),manifest.generator.sha256,'generator drift')
  const vectors=JSON.parse(read(directory,'management/v1/vectors.json'))
  assert.equal(vectors.version,'solovey.management.conformance/v1')
  assert.equal(vectors.cases.length,115)
  assert.equal(new Set(vectors.cases.map(v=>v.id)).size,115)
  return manifest
}

export function assertRetired(directory) {
  for(const name of ['schema.json','codec.go','types.gen.go','codec_test.go'])
    assert.ok(!fs.existsSync(path.join(directory,'contracts/management/v1',name)),`duplicate editable contract: ${name}`)
  for(const name of ['generate.mjs','pin.mjs'])
    assert.ok(!fs.existsSync(path.join(directory,'contracts/tools',name)),`obsolete source-copy authority: ${name}`)
}

export function assertNoSchemaCopy(directory,names) {
  for(const name of names.filter(name=>name.endsWith('.json'))) {
    const filename=path.join(directory,name)
    if(fs.existsSync(filename))assert.ok(!/"x-contractVersion"\s*:\s*"solovey\.management\/v1"/.test(fs.readFileSync(filename,'utf8')),
      `duplicate editable management schema: ${name}`)
  }
}

export function renderLegacyShim(directory,pin) {
  const types=read(directory,'management/v1/types.gen.go')
  const codec=read(directory,'management/v1/codec.go')
  const aliases=[...types.matchAll(/^type ([A-Z]\w*) /gm)].map(match=>`type ${match[1]} = canonical.${match[1]}\n`).join('')
  const functions=[...codec.matchAll(/^func ([A-Z]\w*)\(([^)]*)\)([^\n{]+)\{/gm)].map(([,name,parameters,result])=>{
    const args=parameters.split(',').map(p=>p.trim().split(/\s+/)[0]).join(', ')
    return `func ${name}(${parameters})${result}{ return canonical.${name}(${args}) }\n`
  }).join('')
  assert.ok(aliases&&functions,'unrecognized public contract API')
  const text=`// Code generated from ${pin.module}@${pin.version}; DO NOT EDIT.\n// Source commit: ${pin.sourceCommit}\n// Deprecated: import ${pin.module}/management/v1 directly.\npackage managementv1\n\nimport canonical "${pin.module}/management/v1"\n\nconst Version = canonical.Version\nconst MaxDocumentBytes = canonical.MaxDocumentBytes\n\n${aliases}\n${functions}`
  const goroot=run(root,['env','GOROOT'])
  return normalize(execFileSync(path.join(goroot,'bin',process.platform==='win32'?'gofmt.exe':'gofmt'),[],{input:text,encoding:'utf8'}))
}

export function verifyConsumer({directory=root,sync=false}={}) {
  validateEnvironment(process.env)
  const pin=JSON.parse(read(directory,'contracts/PIN.json'))
  validatePin(pin)
  assertRetired(directory)
  const downloads=new Map(),resolutions=[]
  for(const modfile of modfiles) {
    const metadata=JSON.parse(run(directory,['mod','edit','-json',`-modfile=${modfile}`]))
    validateRequirement(metadata,pin)
    if(!downloads.has(pin.version)) {
      const downloaded=JSON.parse(run(directory,['mod','download','-json',`${pin.module}@${pin.version}`]))
      validateResolution(downloaded,pin)
      downloads.set(pin.version,downloaded)
    }
    const selected=JSON.parse(run(directory,['list','-m','-json','-mod=readonly',`-modfile=${modfile}`,pin.module]))
    validateResolution(selected,pin)
    const downloaded=downloads.get(pin.version)
    assert.equal(path.resolve(selected.Dir),path.resolve(downloaded.Dir),'selected source differs from verified module')
    verifyBundle(selected.Dir,pin)
    resolutions.push({modfile,module:selected.Path,version:selected.Version,sum:selected.Sum})
  }
  const source=downloads.get(pin.version).Dir
  const fixtureRoot=path.join(directory,'contracts/management/v1')
  fs.mkdirSync(fixtureRoot,{recursive:true})
  for(const name of ['vectors.json','manifest.json']) {
    const text=read(source,`management/v1/${name}`),filename=path.join(fixtureRoot,name)
    if(sync)fs.writeFileSync(filename,text)
    assert.equal(read(directory,`contracts/management/v1/${name}`),text,`derived fixture drift: ${name}`)
  }
  if(legacyShim) {
    const text=renderLegacyShim(source,pin),filename=path.join(fixtureRoot,'compat.go')
    if(sync)fs.writeFileSync(filename,text)
    assert.equal(read(directory,'contracts/management/v1/compat.go'),text,'derived compatibility shim drift')
  }
  const names=execFileSync('git',['ls-files','-z','--cached','--others','--exclude-standard'],{cwd:directory,encoding:'utf8'}).split('\0').filter(Boolean)
  assertNoSchemaCopy(directory,names)
  return {status:'PASS',module:pin.module,version:pin.version,sourceCommit:pin.sourceCommit,
    moduleChecksum:pin.moduleChecksum,bundleSHA256:pin.bundleSHA256,vectors:115,resolutions}
}

if(process.argv[1]&&path.resolve(process.argv[1])===fileURLToPath(import.meta.url)) {
  assert.ok(process.argv.slice(2).every(arg=>arg==='--sync-fixtures'),'only --sync-fixtures is supported')
  console.log(JSON.stringify(verifyConsumer({sync:process.argv.includes('--sync-fixtures')})))
}
