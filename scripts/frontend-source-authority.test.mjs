import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import {execFileSync} from 'node:child_process'
import {readFrontendPin,resolveFrontendSource} from './frontend-artifacts.mjs'
import {readSourceLock} from './component-source-inputs.mjs'
const root=path.resolve(import.meta.dirname,'..')
function implicitGoFrontendCaller(text) {
 const code=text.replace(/\/\/[^\r\n]*|\/\*[\s\S]*?\*\//g,'')
 const joined=code.replace(/(["'])\s*,\s*\1/g,'/').replace(/\\+/g,'/')
 return /["'](?:\.\.?\/)*frontend\/(?:src|scripts|package(?:-lock)?\.json|vite\.config|tsconfig|go\.mod)(?:\/|[."'])/.test(joined)
}
const tracked=execFileSync('git',['ls-files','-z'],{cwd:root,encoding:'utf8'}).split('\0').filter(Boolean)
test('Product has one immutable external Frontend pin and no second editable source',async()=>{
 const pin=readFrontendPin(),source=await resolveFrontendSource()
 assert.ok(!tracked.some(n=>n.startsWith('frontend/')||n.startsWith('components/panel-update-ui/frontend/')),'retired editable Frontend source returned')
 assert.ok(!tracked.includes('scripts/frontend-standalone.mjs'),'Product cannot keep a second standalone producer')
 assert.ok(fs.existsSync(path.join(source,'src/main.ts')))
 assert.equal(fs.readFileSync(path.join(root,'.node-version'),'utf8').trim(),pin.toolchain.node)
 for(const n of tracked.filter(n=>n.endsWith('.go'))) assert.equal(implicitGoFrontendCaller(fs.readFileSync(path.join(root,n),'utf8')),false,'implicit Product frontend caller: '+n)
 for(const n of tracked.filter(n=>['Makefile','Dockerfile','build.sh'].includes(n)||/^(?:scripts|windows|tests\/e2e|\.github\/workflows)\//.test(n)&&/\.(?:m?js|ps1|sh|ya?ml)$/.test(n)&&!n.endsWith('.test.mjs'))){
  const text=fs.readFileSync(path.join(root,n),'utf8')
  assert.doesNotMatch(text,/(?:\.\.\/frontend\/(?:src|scripts)|working-directory:\s*frontend|node-version-file:\s*frontend\/|npm ci --prefix frontend)/,'implicit Product frontend caller: '+n)
 }
 const lock=JSON.parse(fs.readFileSync(path.join(root,'components/sources.lock.json')))
 assert.equal(lock.schema,'solovey.component-source-inputs/v1')
 for(const {id} of lock.sources) {
  const prefix=`components/${id}/frontend/`
  assert.ok(!tracked.some(name=>name.startsWith(prefix)),`Product retains editable Frontend source for pinned ${id}`)
 }
 assert.ok(!tracked.includes('frontend/components/sources.lock.json'))
})

test('Product has no editable UI for pinned component owners',()=>{
 const {sources}=readSourceLock(root)
 const duplicates=sources.flatMap(({id})=>tracked.filter(name=>name.startsWith(`components/${id}/frontend/`)))
 assert.deepEqual(duplicates,[],'editable Product UI shadows a pinned component owner')
})

test('Go source caller ratchet rejects literal and filepath.Join references to retired Product source',()=>{
 for(const code of [
  "os.ReadFile(\"../frontend/src/features/settingsPayload.ts\")",
  "filepath.Join(\"..\", \"frontend\", \"src\", \"features\", \"settingsPayload.ts\")",
  "filepath.Join(root, \"frontend\", \"scripts\", \"component-build-inputs.mjs\")",
  "os.ReadFile(\"frontend/package-lock.json\")",
  "os.ReadFile(\"..\\\\frontend\\\\src\\\\main.ts\")"
]) assert.equal(implicitGoFrontendCaller(code),true,code)
 for(const code of [
  "// editor owned by frontend/src/types/dns.ts\nos.ReadFile(pinnedSource)",
  "filepath.Join(packDir, \"frontend\", \"assets\")",
  "filepath.Join(frontendSource, \"src\", \"features\", \"settingsPayload.ts\")"
]) assert.equal(implicitGoFrontendCaller(code),false,code)
})
