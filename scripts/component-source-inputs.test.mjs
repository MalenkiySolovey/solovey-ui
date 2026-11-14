import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import {execFileSync} from 'node:child_process'
import {digest, frontendDigest, validateSourceLock, validateModuleResolution, validateModuleRequirement, verifySourceFiles, componentSourceRoot, componentManifestKey, validateBuildEnvironment, regularSourceTree, verifyModuleSource, verifyUIAPI, verifyComponentManifest} from './component-source-inputs.mjs'

function fixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(),'component sources '))
  t.after(() => fs.rmSync(root,{recursive:true,force:true}))
  const source = {id:'telegram',module:'github.com/MalenkiySolovey/solovey-component-telegram',version:'v0.1.0',sourceCommit:'1'.repeat(40),sourceTree:'2'.repeat(40),moduleChecksum:`h1:${'a'.repeat(43)}=`,goModChecksum:`h1:${'b'.repeat(43)}=`,manifestSHA256:'',frontendSHA256:'',files:{}}
  const directory = path.join(root,'.component-sources/telegram')
  const apiFiles = {'src/componentSystem/sdk.ts':'export const register = () => {}\n','package-lock.json':'{}\n','.node-version':'25.9.0\n'}
  for (const [name,bytes] of Object.entries(apiFiles)) { fs.mkdirSync(path.dirname(path.join(root,'frontend',name)),{recursive:true}); fs.writeFileSync(path.join(root,'frontend',name),bytes) }
  const apiPin = {repository:'MalenkiySolovey/solovey-ui',apiVersion:'1.0',sourceCommit:'1'.repeat(40),files:Object.fromEntries(Object.entries(apiFiles).map(([name,bytes])=>[name,digest(bytes)]))}
  const content = {'go.mod':'module example\n','go.sum':'sum\n','LICENSE':'GPL-3.0\n','NOTICE':'Attribution\n','SOURCE_PROVENANCE.json':'{}\n','HOST_UI_PIN.json':JSON.stringify(apiPin),'component.json':JSON.stringify({id:'telegram',frontend:{entries:['frontend/index.ts']}}),'frontend/index.ts':'export const register = () => import("./view.ts")\n','frontend/view.ts':'export default true\n'}
  for (const [name, bytes] of Object.entries(content)) {
    fs.mkdirSync(path.dirname(path.join(directory,name)),{recursive:true})
    fs.writeFileSync(path.join(directory,name),bytes)
    source.files[name] = digest(bytes)
  }
  source.manifestSHA256 = source.files['component.json']
  source.frontendSHA256 = frontendDigest(source.files)
  const lock = {schema:'solovey.component-source-inputs/v1',sources:[source]}
  fs.mkdirSync(path.join(root,'components/telegram'),{recursive:true})
  fs.writeFileSync(path.join(root,'components/sources.lock.json'),JSON.stringify(lock))
  fs.copyFileSync(path.join(directory,'component.json'),path.join(root,'components/telegram/component.json'))
  const resolution = {Path:source.module,Version:source.version,Sum:source.moduleChecksum,GoModSum:source.goModChecksum,Origin:{VCS:'git',Hash:source.sourceCommit,URL:`https://${source.module}`}}
  return {root,source,directory,lock,resolution}
}

test('pinned external input preserves manifest and resolves its frontend entry without an editable Panel copy',t => {
  const {root,lock,directory} = fixture(t)
  validateSourceLock(lock)
  assert.equal(componentSourceRoot('telegram',root),directory)
  assert.equal(componentManifestKey('telegram','frontend/index.ts',root),'../.component-sources/telegram/frontend/index.ts')
  assert.ok(!fs.existsSync(path.join(root,'components/telegram/frontend')))
})

for (const [field,value] of [['version','main'],['module','github.com/stranger/telegram'],['sourceCommit','latest'],['moduleChecksum',''],['frontendSHA256','0'.repeat(64)],['unexpected',true]]) {
  test(`reject invalid source identity ${field}`,t => {
    const {lock,source} = fixture(t)
    source[field] = value
    assert.throws(() => validateSourceLock(lock))
  })
}
test('reject duplicate IDs and missing provenance',t => {
  const {lock,source} = fixture(t)
  lock.sources.push(structuredClone(source))
  assert.throws(() => validateSourceLock(lock),/duplicate/)
  lock.sources.pop()
  delete source.files.NOTICE
  assert.throws(() => validateSourceLock(lock),/provenance/)
})
for (const name of ['../outside','frontend/../../outside','/outside','C:/outside','frontend\\outside','frontend/.git/config','frontend/space.','frontend/name:stream']) {
  test(`reject source path ${name}`,t => {
    const {source,lock} = fixture(t)
    source.files[name] = '0'.repeat(64)
    assert.throws(() => validateSourceLock(lock))
  })
}
for (const name of ['frontend/index.ts','frontend/view.ts','LICENSE','component.json']) {
  test(`reject changed or missing accepted file ${name}`,t => {
    const {directory,source} = fixture(t)
    fs.appendFileSync(path.join(directory,name),'changed')
    assert.throws(() => verifySourceFiles(directory,source),/drift/)
    fs.unlinkSync(path.join(directory,name))
    assert.throws(() => verifySourceFiles(directory,source))
  })
}
test('reject unpinned frontend module and a staging junction',t => {
  const {root,directory,source} = fixture(t)
  fs.writeFileSync(path.join(directory,'frontend/injected.ts'),'export default 1')
  assert.throws(() => verifySourceFiles(directory,source),/closure/)
  fs.unlinkSync(path.join(directory,'frontend/injected.ts'))
  fs.renameSync(directory,path.join(root,'alternate'))
  fs.symlinkSync(path.join(root,'alternate'),directory,process.platform==='win32'?'junction':'dir')
  assert.throws(() => componentSourceRoot('telegram',root),/directory|symlink/)
})
test('reject generated manifest drift',t => {
  const {root} = fixture(t)
  fs.appendFileSync(path.join(root,'components/telegram/component.json'),' ')
  assert.throws(() => componentSourceRoot('telegram',root),/manifest drift/)
})
for (const [field,value] of [['Replace',{Path:'../local'}],['Main',true],['Sum','wrong'],['Version','v0.2.0'],['Origin',{VCS:'git',Hash:'3'.repeat(40)}],['Origin',undefined]]) {
  test(`reject resolver substitution ${field}`,t => {
    const {source,resolution} = fixture(t)
    resolution[field] = value
    assert.throws(() => validateModuleResolution(resolution,source))
  })
}
test('require the same direct module in both provider graphs',t => {
  const {source,resolution} = fixture(t)
  validateModuleResolution(resolution,source)
  const metadata = {Require:[{Path:source.module,Version:source.version}]}
  validateModuleRequirement(metadata,source)
  assert.throws(() => validateModuleRequirement({},source),/pin required/)
  assert.throws(() => validateModuleRequirement({...metadata,Replace:[{Old:{Path:source.module},New:{Path:'../local'}}]},source),/replace/)
  metadata.Require[0].Version='v0.2.0'
  assert.throws(() => validateModuleRequirement(metadata,source),/mismatch/)
})
for (const env of [{GOWORK:'../go.work'},{GOWORK:'auto'},{GOFLAGS:'-overlay=shadow.json'},{GOFLAGS:'-modfile=local.mod'},{GOFLAGS:'-mod=mod'}]) {
  test(`reject build environment ${JSON.stringify(env)}`,() => assert.throws(() => validateBuildEnvironment(env)))
}
test('the canonical Extended selector cannot override explicit per-provider resolver checks',() => {
  validateBuildEnvironment({GOWORK:'off',GOFLAGS:'-mod=readonly -modfile=go.extended.mod'})
})

test('the regular module archive reconstructs the actual Git tree and rejects a wrong tree identity',t => {
  const {root,directory,source} = fixture(t)
  const metadata = path.join(root,'git-metadata')
  execFileSync('git',['init','--bare','--quiet',metadata],{stdio:'pipe'})
  const git = args => execFileSync('git',[`--git-dir=${metadata}`,`--work-tree=${directory}`,'-c','core.autocrlf=false',...args],{encoding:'utf8',stdio:'pipe'}).trim()
  git(['add','--all'])
  source.sourceTree = git(['write-tree'])
  assert.equal(regularSourceTree(directory),source.sourceTree)
  verifyModuleSource(directory,source)
  source.sourceTree = '0'.repeat(40)
  assert.throws(() => verifyModuleSource(directory,source),/Git source tree mismatch/)
})

test('offline pack keys require the accepted lock and do not depend on staged source',t => {
  const {root,directory} = fixture(t)
  fs.rmSync(directory,{recursive:true,force:true})
  assert.equal(componentManifestKey('telegram','frontend/index.ts',root),'../.component-sources/telegram/frontend/index.ts')
  assert.throws(() => componentSourceRoot('telegram',root))
})

test('the UI export, toolchain lock and API version cannot drift from the accepted producer contract',t => {
  const {root,directory} = fixture(t)
  verifyUIAPI(root,directory)
  fs.appendFileSync(path.join(root,'frontend/src/componentSystem/sdk.ts'),'changed')
  assert.throws(() => verifyUIAPI(root,directory),/UI API input drift/)
  const pin = JSON.parse(fs.readFileSync(path.join(directory,'HOST_UI_PIN.json')))
  pin.apiVersion='2.0'
  fs.writeFileSync(path.join(directory,'HOST_UI_PIN.json'),JSON.stringify(pin))
  assert.throws(() => verifyUIAPI(root,directory),/unsupported UI contribution API/)
})

test('an observation contribution can pin its narrow public UI exports without changing existing pins',t => {
  const {root,directory} = fixture(t)
  const file='src/componentSystem/observation.ts'
  fs.writeFileSync(path.join(root,'frontend',file),'export const observation = true\n')
  const pin=JSON.parse(fs.readFileSync(path.join(directory,'HOST_UI_PIN.json')))
  pin.files[file]=digest(fs.readFileSync(path.join(root,'frontend',file)))
  const write=()=>fs.writeFileSync(path.join(directory,'HOST_UI_PIN.json'),JSON.stringify(pin))
  write(); verifyUIAPI(root,directory)
  fs.appendFileSync(path.join(root,'frontend',file),'changed')
  assert.throws(()=>verifyUIAPI(root,directory),/UI API input drift/)
  delete pin.files[file];pin.files['src/private.ts']=digest('private');write()
  assert.throws(()=>verifyUIAPI(root,directory),/unsupported UI API inputs/)
})

test('an offline component pack cannot substitute its canonical manifest',t => {
  const {root,directory} = fixture(t)
  const filename=path.join(directory,'component.json')
  verifyComponentManifest('telegram',filename,root)
  fs.appendFileSync(filename,' ')
  assert.throws(() => verifyComponentManifest('telegram',filename,root),/manifest drift/)
})
test('a component HTTP contribution can pin its narrow public UI exports without changing existing pins',t => {
  const {root,directory} = fixture(t)
  const file='src/componentSystem/api.ts'
  fs.writeFileSync(path.join(root,'frontend',file),'export const api = true\n')
  const pin=JSON.parse(fs.readFileSync(path.join(directory,'HOST_UI_PIN.json')))
  pin.files[file]=digest(fs.readFileSync(path.join(root,'frontend',file)))
  const write=()=>fs.writeFileSync(path.join(directory,'HOST_UI_PIN.json'),JSON.stringify(pin))
  write(); verifyUIAPI(root,directory)
  fs.appendFileSync(path.join(root,'frontend',file),'changed')
  assert.throws(()=>verifyUIAPI(root,directory),/UI API input drift/)
  delete pin.files[file];pin.files['src/private.ts']=digest('private');write()
  assert.throws(()=>verifyUIAPI(root,directory),/unsupported UI API inputs/)
})

test('a contribution lifecycle export is independently pinned and rejects private inputs',t => {
  const {root,directory} = fixture(t)
  const sdk=fs.readFileSync(path.join(root,'frontend/src/componentSystem/sdk.ts'))
  const file='src/componentSystem/contributions.ts'
  fs.writeFileSync(path.join(root,'frontend',file),'export const contribution = true\n')
  const pin=JSON.parse(fs.readFileSync(path.join(directory,'HOST_UI_PIN.json')))
  pin.files[file]=digest(fs.readFileSync(path.join(root,'frontend',file)))
  const write=()=>fs.writeFileSync(path.join(directory,'HOST_UI_PIN.json'),JSON.stringify(pin))
  write(); verifyUIAPI(root,directory)
  assert.deepEqual(fs.readFileSync(path.join(root,'frontend/src/componentSystem/sdk.ts')),sdk)
  fs.appendFileSync(path.join(root,'frontend',file),'changed')
  assert.throws(()=>verifyUIAPI(root,directory),/UI API input drift/)
  delete pin.files[file];pin.files['src/componentSystem/registry.ts']=digest('private');write()
  assert.throws(()=>verifyUIAPI(root,directory),/unsupported UI API inputs/)
})
