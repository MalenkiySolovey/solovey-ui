// Product composition and explicit previous-source artifact rollback qualification.
import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
import {spawnSync} from 'node:child_process'
import {readFrontendPin, resolveFrontendDistribution, resolveFrontendSource, unpackFrontendArchive, sha256} from './frontend-artifacts.mjs'
import {prepareFrontendBuildInputs} from './frontend-build-inputs.mjs'
import {productRoot} from './component-source-inputs.mjs'
import {verifyDist, publishAssets} from './frontend-assets.mjs'
assert.equal(process.argv[2],'--out');assert.ok(process.argv[3]&&process.argv.length===4)
const out=path.resolve(process.argv[3]);assert.ok(!fs.existsSync(out),'composition proof output must be fresh');fs.mkdirSync(out,{recursive:true})
const pin=readFrontendPin(), input=prepareFrontendBuildInputs()
assert.equal(input.manifestSHA256,pin.input.manifestSHA256);assert.equal(input.sourceLockDigest,pin.input.sourceLockDigest)
const source=await resolveFrontendSource(), report={schema:'solovey.frontend-product-composition/v1',source:pin.source,version:pin.version,input,profiles:{},rollback:[]}
for(const key of Object.keys(pin.profiles)){
 const accepted=await resolveFrontendDistribution({key}),inventory=verifyDist(accepted.directory),directory=path.join(out,key)
 publishAssets(accepted.directory,directory)
 assert.deepEqual([...verifyDist(directory)].map(([n,v])=>[n,v.sha256]),[...inventory].map(([n,v])=>[n,v.sha256]))
 report.profiles[key]={...accepted,directory:key,files:inventory.size}
}
const previousPin=JSON.parse(fs.readFileSync(path.join(productRoot,'tests/fixtures/frontend-rollback/pin.json')))
assert.equal(previousPin.schema,'solovey.frontend-rollback-pin/v1');assert.equal(previousPin.source.repo,'MalenkiySolovey/solovey-ui')
assert.match(previousPin.source.commit,/^[a-f0-9]{40}$/);assert.match(previousPin.archive.sha256,/^[a-f0-9]{64}$/)
assert.equal(previousPin.archive.filename,previousPin.archive.sha256+'.json.gz')
const previousFiles=unpackFrontendArchive(fs.readFileSync(path.join(productRoot,'tests/fixtures/frontend-rollback',previousPin.archive.filename)),previousPin.archive.sha256,'distribution')
assert.deepEqual([...previousFiles.keys()],Object.keys(previousPin.files).map(n=>'dist/'+n))
const previous=path.join(out,'previous-source');fs.mkdirSync(previous)
for(const [name,bytes]of previousFiles){const relative=name.slice(5),expected=previousPin.files[relative];assert.equal(sha256(bytes),expected.sha256);assert.equal(bytes.length,expected.bytes);const target=path.join(previous,relative);fs.mkdirSync(path.dirname(target),{recursive:true});fs.writeFileSync(target,bytes,{flag:'wx'})}
assert.equal(sha256(fs.readFileSync(path.join(previous,'.vite/manifest.json'))),previousPin.viteManifestSHA256)
assert.equal(verifyDist(previous).size,Object.keys(previousPin.files).length)
// Selection reverses the source/artifact pin, even when unchanged UI bytes are equal.
const selections=[{pin:previousPin.source,archiveSHA256:previousPin.archive.sha256,directory:previous},{pin:pin.source,archiveSHA256:pin.profiles.full.archiveSHA256,directory:path.join(out,'full')}]
for(const [index,selection]of [...selections,...selections].entries()){
 const target=path.join(productRoot,'web/html');publishAssets(selection.directory,target)
 const files=verifyDist(target),expected=verifyDist(selection.directory)
 assert.deepEqual([...files].map(([n,v])=>[n,v.sha256]),[...expected].map(([n,v])=>[n,v.sha256]))
 const result=spawnSync('go',['test','-count=1','-v','./web','-run','TestEmbeddedFrontendMatchesDeclaredDistribution|TestFrontendInput|TestAssetsFS|TestComponentFrontendAssetDirs'],{cwd:productRoot,env:{...process.env,CGO_ENABLED:'1',SOLOVEY_FRONTEND_TEST_DIST:selection.directory},encoding:'utf8',maxBuffer:16*1024*1024,windowsHide:true})
 fs.writeFileSync(path.join(out,'go-embed-rollback-'+index+'.log'),(result.stdout||'')+(result.stderr||''))
 if(result.error)throw result.error;assert.equal(result.status,0,'actual Product embed rollback check failed; inspect retained log')
 report.rollback.push({source:selection.pin,archiveSHA256:selection.archiveSHA256,manifestSHA256:sha256(fs.readFileSync(path.join(target,'.vite/manifest.json'))),files:files.size,allFileHashesEqual:true,actualProductGoEmbed:'PASS'})
}
assert.notDeepEqual(report.rollback[0].source,report.rollback[1].source)
assert.deepEqual(report.rollback[0],report.rollback[2]);assert.deepEqual(report.rollback[1],report.rollback[3])
assert.equal(sha256(fs.readFileSync(path.join(source,'package-lock.json'))),pin.toolchain.packageLockSHA256)
report.result='PASS';fs.writeFileSync(path.join(out,'proof.json'),JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify({result:'PASS',version:pin.version,profiles:Object.keys(report.profiles),rollback:report.rollback}))
