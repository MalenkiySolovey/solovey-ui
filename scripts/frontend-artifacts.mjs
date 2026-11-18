// Build-time consumer only. Product admits immutable Frontend artifacts; HTTP never downloads UI.
import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
import {createHash} from 'node:crypto'
import {gunzipSync} from 'node:zlib'
import {fileURLToPath} from 'node:url'
import {verifyDist, publishAssets} from './frontend-assets.mjs'
export const productRoot = fileURLToPath(new URL('../', import.meta.url))
export const sha256 = bytes => createHash('sha256').update(bytes).digest('hex')
const sha = value => assert.match(value, /^[a-f0-9]{64}$/, 'trusted Frontend SHA256 required')
const keys = (value, names) => assert.deepEqual(Object.keys(value).sort(), [...names].sort(), 'unknown or missing Frontend identity fields')
export function safeFile(name) {
  assert.ok(typeof name === 'string' && name && name.length <= 300 && name === name.normalize('NFC') && !name.startsWith('/') && !/[\\<>:"|?*]/.test(name) && [...name].every(c=>c.codePointAt(0)>=32), 'unsafe Frontend archive path')
  for(const part of name.split('/')) assert.ok(part && part!=='.' && part!=='..' && !/[. ]$/.test(part) && !/^\.(?:git|hg|svn|bzr)$/i.test(part) && !/^(?:con|prn|aux|nul|com[0-9]|lpt[0-9])(?:\.|$)/i.test(part), 'unsafe Frontend archive segment')
  return name
}
export function unpackFrontendArchive(bytes, trustedHash, kind) {
  sha(trustedHash)
  assert.ok(bytes.length > 0 && bytes.length <= 32 * 1024 * 1024, 'Frontend archive size exceeds bound')
  assert.equal(sha256(bytes),trustedHash,'Frontend archive SHA256 mismatch')
  const raw=gunzipSync(bytes,{maxOutputLength:128*1024*1024}), payload=JSON.parse(raw)
  assert.equal(raw.toString('utf8'),JSON.stringify(payload),'non-canonical Frontend archive')
  keys(payload,['schema','kind','files'])
  assert.equal(payload.schema,'solovey.frontend-file-archive/v1');assert.equal(payload.kind,kind,'wrong Frontend archive kind')
  assert.ok(payload.files && typeof payload.files==='object' && !Array.isArray(payload.files))
  const names=Object.keys(payload.files)
  assert.ok(names.length>0 && names.length<=4096,'Frontend file count exceeds bound')
  assert.deepEqual(names,[...names].sort(),'unordered Frontend archive')
  const files=new Map(), cases=new Map(), directories=new Set(), fileNames=new Set()
  let total=0
  for(const name of names) {
    safeFile(name)
    let prefix=''
    for(const part of name.split('/')) {prefix=prefix?prefix+'/'+part:part;const lower=prefix.toLowerCase();assert.ok(!cases.has(lower)||cases.get(lower)===prefix,'Frontend archive case collision');cases.set(lower,prefix)}
    const lower=name.toLowerCase();assert.ok(!directories.has(lower),'Frontend file/directory collision')
    const parents=name.split('/');parents.pop()
    while(parents.length){const directory=parents.join('/').toLowerCase();assert.ok(!fileNames.has(directory),'Frontend file/directory collision');directories.add(directory);parents.pop()}
    assert.equal(typeof payload.files[name],'string','Frontend file bytes must be base64')
    const content=Buffer.from(payload.files[name],'base64');assert.equal(content.toString('base64'),payload.files[name],'invalid Frontend base64')
    assert.ok(content.length<=16*1024*1024 && (total+=content.length)<=96*1024*1024,'Frontend payload exceeds bound')
    files.set(name,content);fileNames.add(lower)
  }
  return files
}
export function validateFrontendPin(pin) {
  keys(pin,['schema','version','source','toolchain','input','sourceArchive','profiles'])
  assert.equal(pin.schema,'solovey.frontend-product-pin/v1')
  assert.match(pin.version,/^0\.[0-9]+\.[0-9]+$/,'immutable Frontend version required')
  keys(pin.source,['repo','commit','tree','filesSHA256'])
  assert.equal(pin.source.repo,'MalenkiySolovey/solovey-frontend')
  for(const name of ['commit','tree'])assert.match(pin.source[name],/^[a-f0-9]{40}$/,'immutable Frontend source required')
  sha(pin.source.filesSHA256)
  keys(pin.toolchain,['node','npm','packageLockSHA256']);assert.equal(pin.toolchain.node,'25.9.0');assert.equal(pin.toolchain.npm,'11.12.1');sha(pin.toolchain.packageLockSHA256)
  keys(pin.input,['manifestSHA256','archiveSHA256','productSource','sourceLockDigest'])
  for(const name of ['manifestSHA256','archiveSHA256','sourceLockDigest'])sha(pin.input[name])
  assert.match(pin.input.productSource,/^[a-f0-9]{40}$/)
  keys(pin.sourceArchive,['filename','sha256','filesSHA256'])
  assert.equal(pin.sourceArchive.filename,'frontend-source.json.gz');sha(pin.sourceArchive.sha256);assert.equal(pin.sourceArchive.filesSHA256,pin.source.filesSHA256)
  const expected=['full','core','minimal','core-empty-inputs','custom-fallback-html','custom-server-protection','custom-remote-outbound-subscriptions']
  keys(pin.profiles,expected)
  for(const [name,item] of Object.entries(pin.profiles)) {
    keys(item,['archive','archiveSHA256','provenance','provenanceSHA256','viteManifestSHA256','profile','componentIDs','inputManifestSHA256','files'])
    assert.equal(item.archive,'frontend-'+name+'.json.gz');assert.equal(item.provenance,'frontend-'+name+'.provenance.json')
    for(const field of ['archiveSHA256','provenanceSHA256','viteManifestSHA256','inputManifestSHA256'])sha(item[field])
    assert.ok(['full','core','minimal'].includes(item.profile));assert.ok(Array.isArray(item.componentIDs)&&item.componentIDs.length<=64)
    assert.deepEqual(item.componentIDs,[...new Set(item.componentIDs)].sort())
    for(const id of item.componentIDs)assert.match(id,/^[a-z0-9]+(?:-[a-z0-9]+)*$/)
    assert.ok(Number.isSafeInteger(item.files)&&item.files>0&&item.files<=4096)
    if(name!=='core-empty-inputs')assert.equal(item.inputManifestSHA256,pin.input.manifestSHA256)
  }
  const selections = {'full': ['full', ['fallback-html','import-xui','observability-extra','paid-subscriptions','panel-update-ui','remote-outbound-subscriptions','server-protection','telegram']], 'core': ['core', []], 'minimal': ['minimal', []], 'core-empty-inputs': ['core', []], 'custom-fallback-html': ['full', ['fallback-html']], 'custom-server-protection': ['full', ['server-protection']], 'custom-remote-outbound-subscriptions': ['full', ['remote-outbound-subscriptions']]}
  for(const [name,[profile,ids]] of Object.entries(selections)) { assert.equal(pin.profiles[name].profile,profile); assert.deepEqual(pin.profiles[name].componentIDs,ids) }
  assert.notEqual(pin.profiles['core-empty-inputs'].inputManifestSHA256,pin.input.manifestSHA256)
  return pin
}
export function readFrontendPin(root=productRoot) {
  const filename=path.join(root,'deploy/dependencies/solovey-frontend.json'), stat=fs.lstatSync(filename)
  assert.ok(stat.isFile()&&!stat.isSymbolicLink()&&stat.size<=128*1024,'unsafe Frontend Product pin')
  return validateFrontendPin(JSON.parse(fs.readFileSync(filename)))
}
export function selectFrontendArtifact(pin, env=process.env) {
  const profile=env.SOLOVEY_UI_PROFILE||'full'
  assert.ok(['full','core','minimal'].includes(profile),'unsupported Frontend profile')
  if(profile!=='full')return profile
  if(env.SOLOVEY_UI_COMPONENT_IDS===undefined)return 'full'
  const ids=[...new Set(env.SOLOVEY_UI_COMPONENT_IDS.split(',').map(x=>x.trim()).filter(Boolean))].sort()
  for(const id of ids)assert.match(id,/^[a-z0-9]+(?:-[a-z0-9]+)*$/,'invalid component selection')
  const match=Object.entries(pin.profiles).find(([,p])=>p.profile==='full' && JSON.stringify(p.componentIDs)===JSON.stringify(ids))
  assert.ok(match,'selected UI tuple has no admitted immutable Frontend artifact; qualify a new producer version')
  return match[0]
}
function cacheDirectory(root, cache) {
  const directory=path.resolve(cache||process.env.SOLOVEY_FRONTEND_CACHE||path.join(root,'.artifacts/frontend'))
  fs.mkdirSync(directory,{recursive:true});const stat=fs.lstatSync(directory)
  assert.ok(stat.isDirectory()&&!stat.isSymbolicLink(),'Frontend cache must be a regular directory')
  return fs.realpathSync.native(directory)
}
export async function frontendBlob(pin, filename, trustedHash, {root=productRoot,cache,offline=process.env.SOLOVEY_FRONTEND_OFFLINE==='1'}={}) {
  sha(trustedHash);assert.match(filename,/^[a-z0-9.-]+\.json(?:\.gz)?$/)
  const directory=path.join(cacheDirectory(root,cache),'blobs');fs.mkdirSync(directory,{recursive:true})
  assert.ok(!fs.lstatSync(directory).isSymbolicLink(),'Frontend blob directory cannot be a symlink')
  const target=path.join(directory,trustedHash)
  if(fs.existsSync(target)) {const stat=fs.lstatSync(target);assert.ok(stat.isFile()&&!stat.isSymbolicLink()&&stat.size<=32*1024*1024,'unsafe Frontend cache blob');const bytes=fs.readFileSync(target);assert.equal(sha256(bytes),trustedHash,'cached Frontend blob changed');return bytes}
  assert.ok(!offline,'accepted Frontend artifact unavailable in offline cache: '+trustedHash)
  const url='https://github.com/MalenkiySolovey/solovey-frontend/releases/download/v'+pin.version+'/'+filename
  const response=await fetch(url,{signal:AbortSignal.timeout(60000)})
  assert.ok(response.ok,'Frontend artifact download failed: '+response.status)
  const chunks=[];let size=0
  for await(const chunk of response.body){size+=chunk.length;assert.ok(size<=32*1024*1024,'Frontend download exceeds bound');chunks.push(chunk)}
  const bytes=Buffer.concat(chunks);assert.equal(sha256(bytes),trustedHash,'downloaded Frontend blob SHA256 mismatch')
  const temporary=path.join(directory,'.'+trustedHash+'-'+process.pid+'-'+Date.now())
  fs.writeFileSync(temporary,bytes,{flag:'wx'})
  try {
    try {if(!fs.existsSync(target))fs.renameSync(temporary,target)}
    catch(error) {if(!fs.existsSync(target))throw error}
    const current=fs.lstatSync(target);assert.ok(current.isFile()&&!current.isSymbolicLink()&&current.size<=32*1024*1024,'unsafe concurrent Frontend cache blob')
    assert.equal(sha256(fs.readFileSync(target)),trustedHash,'concurrent Frontend cache changed')
  }
  finally {if(fs.existsSync(temporary))fs.unlinkSync(temporary)}
  return bytes
}
function verifySourceFiles(directory, files, fixture=false) {
  const actual=[]
  const walk=(prefix='',depth=0)=>{
    assert.ok(depth<=24,'Frontend fixture depth exceeds bound')
    const current=path.join(directory,prefix),stat=fs.lstatSync(current)
    assert.ok(stat.isDirectory()&&!stat.isSymbolicLink(),'Frontend source directory must be regular')
    for(const name of fs.readdirSync(current).sort()) {
      if(!prefix&&fixture&&['node_modules','.component-inputs','dist','.test-results'].includes(name)){const generated=fs.lstatSync(path.join(current,name));assert.ok(generated.isDirectory()&&!generated.isSymbolicLink(),'Frontend fixture generated root must be regular');continue}
      const relative=safeFile(prefix?prefix+'/'+name:name),absolute=path.join(directory,relative),entry=fs.lstatSync(absolute)
      assert.ok(!entry.isSymbolicLink(),'Frontend source symlink rejected')
      if(entry.isDirectory())walk(relative,depth+1)
      else {assert.ok(entry.isFile()&&files.has(relative),'unexpected Frontend fixture file: '+relative);assert.ok(fs.readFileSync(absolute).equals(files.get(relative)),'changed Frontend fixture source: '+relative);actual.push(relative)}
    }
  }
  walk();assert.deepEqual(actual.sort(),[...files.keys()],'Frontend source closure differs')
}
function materializeFiles(directory,files,fixture=false) {
  if(fs.existsSync(directory)){verifySourceFiles(directory,files,fixture);return directory}
  fs.mkdirSync(path.dirname(directory),{recursive:true})
  const temporary=fs.mkdtempSync(path.join(path.dirname(directory),'.frontend-stage-'))
  try {
    for(const [name,bytes]of files){const target=path.join(temporary,safeFile(name));fs.mkdirSync(path.dirname(target),{recursive:true});fs.writeFileSync(target,bytes,{flag:'wx'})}
    verifySourceFiles(temporary,files)
    try {if(!fs.existsSync(directory))fs.renameSync(temporary,directory)}
    catch(error) {if(!fs.existsSync(directory))throw error}
    verifySourceFiles(directory,files,fixture)
    return directory
  } finally {assert.equal(path.dirname(temporary),path.dirname(directory));assert.ok(path.basename(temporary).startsWith('.frontend-stage-'));fs.rmSync(temporary,{recursive:true,force:true})}
}
function cacheNamespace(root,cache,name){const directory=path.join(cacheDirectory(root,cache),name);fs.mkdirSync(directory,{recursive:true});const stat=fs.lstatSync(directory);assert.ok(stat.isDirectory()&&!stat.isSymbolicLink(),'Frontend cache namespace must be regular');return directory}
export async function resolveFrontendSource({root=productRoot,pin=readFrontendPin(root),cache,offline,fixture=false}={}) {
  validateFrontendPin(pin)
  const files=unpackFrontendArchive(await frontendBlob(pin,pin.sourceArchive.filename,pin.sourceArchive.sha256,{root,cache,offline}),pin.sourceArchive.sha256,'source')
  const hashes=Object.fromEntries([...files].map(([name,bytes])=>[name,sha256(bytes)]))
  assert.equal(sha256(JSON.stringify(hashes)),pin.source.filesSHA256,'Frontend source file manifest differs')
  assert.equal(sha256(files.get('package-lock.json')),pin.toolchain.packageLockSHA256,'Frontend package lock drift')
  assert.equal(files.get('.node-version')?.toString('utf8').trim(),pin.toolchain.node,'Frontend Node identity drift')
  assert.ok(!files.has('go.mod'),'Frontend must not carry the unused legacy Go module')
  const directory=path.join(cacheNamespace(root,cache,fixture?'fixtures':'source'),pin.sourceArchive.sha256)
  return materializeFiles(directory,files,fixture)
}
export function verifyFrontendReceipt(pin,key,receiptBytes,archiveBytes) {
  validateFrontendPin(pin);const selected=pin.profiles[key];assert.ok(selected,'unadmitted Frontend profile')
  assert.equal(sha256(receiptBytes),selected.provenanceSHA256,'Frontend provenance SHA256 mismatch')
  const receipt=JSON.parse(receiptBytes)
  keys(receipt,['schema','version','source','sourceArchiveSHA256','toolchain','build','viteManifestSHA256','archiveSHA256','files'])
  assert.equal(receipt.schema,'solovey.frontend-distribution/v1');assert.equal(receipt.version,pin.version)
  assert.deepEqual(receipt.source,pin.source);assert.deepEqual(receipt.toolchain,pin.toolchain)
  assert.equal(receipt.sourceArchiveSHA256,pin.sourceArchive.sha256)
  assert.deepEqual(receipt.build,{profile:selected.profile,componentIDs:selected.componentIDs,inputManifestSHA256:selected.inputManifestSHA256})
  assert.equal(receipt.viteManifestSHA256,selected.viteManifestSHA256);assert.equal(receipt.archiveSHA256,selected.archiveSHA256)
  const files=unpackFrontendArchive(archiveBytes,selected.archiveSHA256,'distribution')
  assert.deepEqual(Object.keys(receipt.files),[...files.keys()],'Frontend archive file closure differs')
  for(const [name,bytes]of files){keys(receipt.files[name],['sha256','bytes']);assert.equal(sha256(bytes),receipt.files[name].sha256,'Frontend file digest mismatch: '+name);assert.equal(bytes.length,receipt.files[name].bytes);assert.ok(name.startsWith('dist/')||['LICENSE','NOTICE.md','THIRD_PARTY_NOTICES.json'].includes(name),'unexpected Frontend package file')}
  assert.equal(sha256(files.get('dist/.vite/manifest.json')),selected.viteManifestSHA256)
  const dist=new Map([...files].filter(([name])=>name.startsWith('dist/')).map(([name,bytes])=>[name.slice(5),bytes]))
  assert.equal(dist.size,selected.files)
  return {receipt,files,dist}
}
export async function resolveFrontendDistribution({root=productRoot,pin=readFrontendPin(root),key=selectFrontendArtifact(pin),cache,offline,destination}={}) {
  validateFrontendPin(pin);const selected=pin.profiles[key];assert.ok(selected,'unknown Frontend artifact tuple')
  const results=await Promise.allSettled([frontendBlob(pin,selected.provenance,selected.provenanceSHA256,{root,cache,offline}),frontendBlob(pin,selected.archive,selected.archiveSHA256,{root,cache,offline})])
  for(const result of results)if(result.status!=='fulfilled')throw result.reason
  const verified=verifyFrontendReceipt(pin,key,results[0].value,results[1].value)
  const packageDirectory=materializeFiles(path.join(cacheNamespace(root,cache,'distributions'),selected.archiveSHA256),verified.files)
  const dist=path.join(packageDirectory,'dist');assert.equal(verifyDist(dist).size,selected.files)
  if(destination)publishAssets(dist,destination)
  return {directory:destination?path.resolve(destination):dist,version:pin.version,source:pin.source,profile:key,archiveSHA256:selected.archiveSHA256,provenanceSHA256:selected.provenanceSHA256,viteManifestSHA256:selected.viteManifestSHA256,files:selected.files}
}

export async function verifyFrontendDistributionDirectory(directory,options={}) {
  const accepted=await resolveFrontendDistribution(options)
  const expected=verifyDist(accepted.directory),actual=verifyDist(directory)
  assert.deepEqual([...actual].map(([name,value])=>[name,value.sha256]),[...expected].map(([name,value])=>[name,value.sha256]),'Frontend directory differs from admitted immutable distribution')
  return {...accepted,directory:path.resolve(directory)}
}
