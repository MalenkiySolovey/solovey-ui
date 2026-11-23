import test from 'node:test'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import assert from 'node:assert/strict'
import {gzipSync} from 'node:zlib'
import {productRoot,readFrontendPin,validateFrontendPin,selectFrontendArtifact,unpackFrontendArchive,verifyFrontendReceipt,frontendBlob,resolveFrontendDistribution,verifyFrontendDistributionDirectory,resolveFrontendSource,sha256} from './frontend-artifacts.mjs'
const pin=readFrontendPin(), clone=value=>JSON.parse(JSON.stringify(value))
function fixture(t){const parent=fs.realpathSync.native(os.tmpdir()),root=fs.mkdtempSync(path.join(parent,'solovey artifact-negative '));fs.mkdirSync(path.join(root,'deploy/dependencies'),{recursive:true});fs.copyFileSync(path.join(productRoot,'deploy/dependencies/solovey-frontend.json'),path.join(root,'deploy/dependencies/solovey-frontend.json'));t.after(()=>{const actual=fs.realpathSync.native(root);assert.ok(path.dirname(actual)===parent&&path.basename(actual).startsWith('solovey artifact-negative '));fs.rmSync(actual,{recursive:true,force:true})});return root}
function encode(files,kind='distribution'){return gzipSync(Buffer.from(JSON.stringify({schema:'solovey.frontend-file-archive/v1',kind,files})),{level:9})}
test('Product admits only exact producer coordinates/toolchain/profiles',()=>{
 validateFrontendPin(pin)
 for(const edit of [p=>p.source.commit='main',p=>p.source.repo='other/frontend',p=>p.version='latest',p=>p.toolchain.node='99.0.0',p=>p.toolchain.packageLockSHA256='',p=>p.input.manifestSHA256='',p=>p.input.extra=true,p=>p.profiles.full.archive='latest.json.gz',p=>p.profiles.full.inputManifestSHA256='0'.repeat(64),p=>p.profiles.core.profile='full',p=>p.profiles['custom-fallback-html'].componentIDs=['telegram'],p=>p.profiles.minimal.componentIDs=['panel-update-ui']]){const p=clone(pin);edit(p);assert.throws(()=>validateFrontendPin(p))}
})
test('Product profile selection rejects unqualified UI tuples and runtime URLs',()=>{
 assert.equal(selectFrontendArtifact(pin,{}),'full');assert.equal(selectFrontendArtifact(pin,{SOLOVEY_UI_PROFILE:'core'}),'core')
 assert.equal(selectFrontendArtifact(pin,{SOLOVEY_UI_COMPONENT_IDS:'fallback-html'}),'custom-fallback-html')
 for(const env of [{SOLOVEY_UI_PROFILE:'other'},{SOLOVEY_UI_COMPONENT_IDS:'unknown'},{SOLOVEY_UI_COMPONENT_IDS:'https://attacker.invalid/ui.js'},{SOLOVEY_UI_COMPONENT_IDS:'fallback-html,telegram'},{SOLOVEY_UI_COMPONENT_IDS:'panel-update-ui'}])assert.throws(()=>selectFrontendArtifact(pin,env))
 const former = clone(pin)
 former.profiles.full.componentIDs = [...former.profiles.full.componentIDs, 'panel-update-ui'].sort()
 assert.throws(() => validateFrontendPin(former))
})
test('Zapret is in new Full producer pins only; Core and rollback stay exact',()=>{
 const previous=clone(pin)
 previous.version='0.1.6'
 previous.profiles.full.componentIDs=previous.profiles.full.componentIDs.filter(id=>id!=='zapret')
 validateFrontendPin(previous)
 const next=clone(previous)
 next.version='0.1.7'
 assert.throws(()=>validateFrontendPin(next))
 next.profiles.full.componentIDs.push('zapret')
 validateFrontendPin(next)
 next.profiles.core.componentIDs.push('zapret')
 assert.throws(()=>validateFrontendPin(next))
})
test('Product archive trust rejects missing/wrong digest and untrusted format',()=>{
 const bytes=encode({'dist/index.html':'YQ=='})
 assert.throws(()=>unpackFrontendArchive(bytes,undefined,'distribution'))
 assert.throws(()=>unpackFrontendArchive(bytes,'0'.repeat(64),'distribution'),/SHA256 mismatch/)
 assert.throws(()=>unpackFrontendArchive(bytes,sha256(bytes),'source'),/kind/)
 for(const files of [{'../escape':'YQ=='},{'dist/a':'YQ==','dist/A':'YQ=='},{'dist/a':'YQ==','dist/a/b':'YQ=='},{'dist/a':'***'},{'dist\\escape':'YQ=='},{'dist/a':{link:'outside'}}]){const b=encode(files);assert.throws(()=>unpackFrontendArchive(b,sha256(b),'distribution'))}
 const duplicate=gzipSync(Buffer.from('{"schema":"solovey.frontend-file-archive/v1","kind":"distribution","files":{"a":"YQ==","a":"Yg=="}}'));assert.throws(()=>unpackFrontendArchive(duplicate,sha256(duplicate),'distribution'),/canonical/)
})
test('Product offline resolution never downloads missing inputs',async t=>{
 const root=fixture(t);await assert.rejects(()=>frontendBlob(pin,pin.sourceArchive.filename,pin.sourceArchive.sha256,{root,cache:root,offline:true}),/offline cache/)
})
test('Product rejects changed cached bytes and a cache symlink',async t=>{
 const root=fixture(t);fs.mkdirSync(path.join(root,'blobs'));fs.writeFileSync(path.join(root,'blobs',pin.sourceArchive.sha256),'untrusted')
 await assert.rejects(()=>frontendBlob(pin,pin.sourceArchive.filename,pin.sourceArchive.sha256,{root,cache:root,offline:true}),/changed/)
 fs.unlinkSync(path.join(root,'blobs',pin.sourceArchive.sha256));const actual=path.join(root,'actual');fs.writeFileSync(actual,'untrusted');const linked=path.join(root,'linked');fs.mkdirSync(linked);fs.symlinkSync(linked,path.join(root,'blobs',pin.sourceArchive.sha256),process.platform==='win32'?'junction':'dir')
 await assert.rejects(()=>frontendBlob(pin,pin.sourceArchive.filename,pin.sourceArchive.sha256,{root,cache:root,offline:true}),/unsafe/)
})
test('accepted external distribution and provenance resolve offline after verified fetch',async()=>{
 const accepted=await resolveFrontendDistribution({key:'full'}), offline=await resolveFrontendDistribution({key:'full',offline:true})
 assert.deepEqual(offline,accepted);assert.equal(accepted.archiveSHA256,pin.profiles.full.archiveSHA256)
})
test('Product rejects wrong receipt source/profile/input and missing/extra distribution files',async()=>{
 const p=pin.profiles.full, receiptBytes=await frontendBlob(pin,p.provenance,p.provenanceSHA256), archiveBytes=await frontendBlob(pin,p.archive,p.archiveSHA256)
 verifyFrontendReceipt(pin,'full',receiptBytes,archiveBytes)
 assert.throws(()=>verifyFrontendReceipt(pin,'core',receiptBytes,archiveBytes),/provenance SHA256/)
 for(const edit of [r=>r.source.commit='0'.repeat(40),r=>r.build.profile='core',r=>r.build.inputManifestSHA256='0'.repeat(64),r=>r.toolchain.node='99.0.0',r=>r.files['dist/extra.js']={sha256:'0'.repeat(64),bytes:1},r=>delete r.files['dist/index.html']]){
  const receipt=JSON.parse(receiptBytes), patchedPin=clone(pin);edit(receipt);const bytes=Buffer.from(JSON.stringify(receipt));patchedPin.profiles.full.provenanceSHA256=sha256(bytes);assert.throws(()=>verifyFrontendReceipt(patchedPin,'full',bytes,archiveBytes))
 }
 const corrupted=Buffer.from(archiveBytes);corrupted[20]^=1;assert.throws(()=>verifyFrontendReceipt(pin,'full',receiptBytes,corrupted),/SHA256/)
})

test('canonical source and explicit audit fixture reject edited source and cache namespace junctions',async t=>{
 const root=fixture(t),bytes=await frontendBlob(pin,pin.sourceArchive.filename,pin.sourceArchive.sha256)
 fs.mkdirSync(path.join(root,'blobs'));fs.writeFileSync(path.join(root,'blobs',pin.sourceArchive.sha256),bytes)
 const source=await resolveFrontendSource({root,cache:root,offline:true})
 fs.appendFileSync(path.join(source,'src/main.ts'),'\n// changed')
 await assert.rejects(()=>resolveFrontendSource({root,cache:root,offline:true}),/changed Frontend fixture source/)
 const fixtureSource=await resolveFrontendSource({root,cache:root,offline:true,fixture:true}),outside=path.join(root,'outside');fs.mkdirSync(outside)
 fs.symlinkSync(outside,path.join(fixtureSource,'node_modules'),process.platform==='win32'?'junction':'dir')
 await assert.rejects(()=>resolveFrontendSource({root,cache:root,offline:true,fixture:true}),/generated root must be regular/)
 const other=fixture(t);fs.mkdirSync(path.join(other,'blobs'));fs.writeFileSync(path.join(other,'blobs',pin.sourceArchive.sha256),bytes)
 fs.symlinkSync(outside,path.join(other,'source'),process.platform==='win32'?'junction':'dir')
 await assert.rejects(()=>resolveFrontendSource({root:other,cache:other,offline:true}),/namespace must be regular/)
})

test('packaging rejects edited, missing and extra files in a caller-supplied distribution',async t=>{
 const root=fixture(t),accepted=await resolveFrontendDistribution({key:'core'})
 const directory=path.join(root,'dist');fs.cpSync(accepted.directory,directory,{recursive:true})
 await verifyFrontendDistributionDirectory(directory,{key:'core',offline:true})
 const index=path.join(directory,'index.html'),original=fs.readFileSync(index)
 fs.appendFileSync(index,'\n<script>untrusted()</script>')
 await assert.rejects(()=>verifyFrontendDistributionDirectory(directory,{key:'core',offline:true}),/differs/)
 fs.writeFileSync(index,original);fs.writeFileSync(path.join(directory,'extra.js'),'untrusted')
 await assert.rejects(()=>verifyFrontendDistributionDirectory(directory,{key:'core',offline:true}))
 fs.unlinkSync(path.join(directory,'extra.js'));fs.unlinkSync(index)
 await assert.rejects(()=>verifyFrontendDistributionDirectory(directory,{key:'core',offline:true}))
})
