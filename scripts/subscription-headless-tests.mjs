import path from 'node:path'
import assert from 'node:assert/strict'
import {execFileSync,spawnSync} from 'node:child_process'
import {fileURLToPath} from 'node:url'
import {readSourceLock,validateModuleResolution} from './component-source-inputs.mjs'

const root=fileURLToPath(new URL('../',import.meta.url))
const source=readSourceLock(root).sources.find(item=>item.id==='remote-outbound-subscriptions')
assert.ok(source,'immutable Remote Subscriptions source input is required')
const directory=path.join(root,'tests/headless/remote-outbound-subscriptions')
const env={...process.env,GOWORK:'off',GOFLAGS:'-mod=readonly'}
const go=args=>JSON.parse(execFileSync('go',args,{cwd:directory,env,encoding:'utf8'}))
const mod=go(['mod','edit','-json'])
assert.equal(mod.Module.Path,'github.com/MalenkiySolovey/solovey-node/qualification/remote-outbound-subscriptions')
assert.ok(!(mod.Replace??[]).length,'headless consumer cannot replace source')
assert.equal(mod.Require.find(item=>item.Path===source.module)?.Version,source.version,'headless feature version differs from Product')
const selected=go(['mod','download','-json',source.module+'@'+source.version])
validateModuleResolution(selected,source)
const actual=go(['list','-m','-json',source.module])
validateModuleResolution({...actual,Origin:selected.Origin},source)
assert.equal(path.resolve(actual.Dir),path.resolve(selected.Dir),'headless consumer selected another implementation')
const product=JSON.parse(execFileSync('go',['mod','edit','-json'],{cwd:root,env,encoding:'utf8'}))
const host='github.com/MalenkiySolovey/solovey-host'
assert.equal(mod.Require.find(item=>item.Path===host)?.Version,product.Require.find(item=>item.Path===host)?.Version,'headless Host contract version differs from Product')
const packageJSON=execFileSync('go',['list','-deps','-test','-json','./...'],{cwd:directory,env,encoding:'utf8',maxBuffer:12*1024*1024})
const packages=(packageJSON.match(/^{[\s\S]*?^}/gm)??[]).map(JSON.parse)
assert.ok(packages.length>0)
const allowed=new Set([mod.Module.Path,source.module,host,'golang.org/x/sys'])
for(const pkg of packages)if(pkg.Module){
  assert.ok(allowed.has(pkg.Module.Path),'headless fixture depends on Product-private or unrelated source: '+pkg.ImportPath)
  assert.ok(!pkg.Module.Replace,'headless fixture selected substituted source')
}
const result=spawnSync('go',['test','-race','-count=1','./...'],{cwd:directory,env,stdio:'inherit'})
if(result.error)throw result.error
assert.equal(result.status,0,'headless consumer qualification failed')
console.log(JSON.stringify({status:'PASS',module:source.module,version:source.version,sourceCommit:source.sourceCommit,checkedPackages:packages.length,productPrivateImports:0,productionNodeChanged:false}))
