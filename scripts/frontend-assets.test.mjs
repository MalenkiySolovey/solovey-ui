import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { spawnSync, execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { verifyDist, publishAssets } from './frontend-assets.mjs'

function fixture(t) {
  const root=fs.mkdtempSync(path.join(os.tmpdir(),'frontend assets '))
  t.after(() => fs.rmSync(root,{recursive:true,force:true}))
  const dist=path.join(root,'dist'), target=path.join(root,'web','html')
  fs.mkdirSync(path.join(dist,'assets'),{recursive:true})
  fs.mkdirSync(path.join(dist,'.vite'))
  fs.mkdirSync(target,{recursive:true})
  fs.writeFileSync(path.join(target,'previous.html'),'previous complete asset set')
  fs.writeFileSync(path.join(dist,'index.html'),'<script type="module" src="./assets/app.js"></script><link href="./assets/style.css">')
  fs.writeFileSync(path.join(dist,'assets','app.js'),'import "./chunk.js"; import("./lazy.js"); new URL("./image.svg", import.meta.url)')
  fs.writeFileSync(path.join(dist,'assets','chunk.js'),'export const ready=true')
  fs.writeFileSync(path.join(dist,'assets','lazy.js'),'export default 1')
  fs.writeFileSync(path.join(dist,'assets','style.css'),'body{background:url(./image.svg)}')
  fs.writeFileSync(path.join(dist,'assets','image.svg'),'<svg/>')
  fs.writeFileSync(path.join(dist,'.vite','manifest.json'),JSON.stringify({'index.html':{isEntry:true,file:'assets/app.js',css:['assets/style.css'],assets:['assets/image.svg'],imports:['chunk'],dynamicImports:['lazy']},chunk:{file:'assets/chunk.js'},lazy:{file:'assets/lazy.js'}}))
  return {root,dist,target}
}
function previousIntact(target) {
  assert.deepEqual(fs.readdirSync(target),['previous.html'])
  assert.equal(fs.readFileSync(path.join(target,'previous.html'),'utf8'),'previous complete asset set')
}

test('complete dist replaces the whole generation and leaves no staged assets in web',t => {
  const {root,dist,target}=fixture(t)
  assert.equal(publishAssets(dist,target).published,true)
  assert.equal(verifyDist(target).size,7)
  assert.deepEqual(fs.readdirSync(path.join(root,'web')),['html'])
  assert.deepEqual(fs.readdirSync(root).sort(),['dist','web'])
})

for (const name of ['index.html','.vite/manifest.json','assets/chunk.js','assets/style.css','assets/lazy.js','assets/image.svg']) {
  test(`missing ${name} preserves published assets`,t => {
    const {dist,target}=fixture(t)
    fs.unlinkSync(path.join(dist,name))
    assert.throws(() => publishAssets(dist,target))
    previousIntact(target)
  })
}

test('empty output, unsafe manifest paths and Go-excluded directories fail',t => {
  const {dist,target}=fixture(t)
  fs.writeFileSync(path.join(dist,'assets/app.js'),'')
  assert.throws(() => publishAssets(dist,target),/Empty asset/)
  fs.writeFileSync(path.join(dist,'assets/app.js'),'export default 1')
  const manifest=path.join(dist,'.vite/manifest.json')
  const original=fs.readFileSync(manifest)
  fs.writeFileSync(manifest,JSON.stringify({'index.html':{isEntry:true,file:'../outside.js'}}))
  assert.throws(() => publishAssets(dist,target),/cannot be embedded/)
  fs.writeFileSync(manifest,original)
  fs.mkdirSync(path.join(dist,'.git'))
  fs.writeFileSync(path.join(dist,'.git/config'),'unsafe')
  assert.throws(() => publishAssets(dist,target),/cannot be embedded/)
  previousIntact(target)
})

test('native symlinks or junctions are rejected',t => {
  const {dist,target}=fixture(t)
  fs.symlinkSync(path.join(dist,'assets'),path.join(dist,'linked-assets'),process.platform==='win32'?'junction':'dir')
  assert.throws(() => publishAssets(dist,target),/symlink/)
  previousIntact(target)
})

test('copy failure and a changed staging copy preserve the previous generation',t => {
  const {dist,target}=fixture(t)
  assert.throws(() => publishAssets(dist,target,{operations:{cpSync(){throw Error('copy failed')}}}),/copy failed/)
  previousIntact(target)
  assert.throws(() => publishAssets(dist,target,{operations:{cpSync(source,stage,options){fs.cpSync(source,stage,options);fs.writeFileSync(path.join(stage,'assets/chunk.js'),'export const changed=true')}}}),/integrity mismatch/)
  previousIntact(target)
})

test('a failed final rename rolls back the complete previous tree',t => {
  const {dist,target}=fixture(t)
  let renames=0
  assert.throws(() => publishAssets(dist,target,{operations:{renameSync(from,to){renames++;if(renames===2)throw Error('replacement failed');fs.renameSync(from,to)}}}),/replacement failed/)
  assert.equal(renames,3)
  previousIntact(target)
})

test('failure preserving previous assets never publishes the candidate',t => {
  const {dist,target}=fixture(t)
  assert.throws(() => publishAssets(dist,target,{operations:{renameSync(){throw Error('previous rename failed')}}}),/previous rename failed/)
  previousIntact(target)
})

test('cleanup failure retains previous complete tree outside embed root and reports successful publication',t => {
  const {dist,target}=fixture(t)
  const result=publishAssets(dist,target,{operations:{rmSync(){throw Error('cleanup failed')}}})
  assert.equal(result.published,true)
  assert.equal(verifyDist(target).size,7)
  previousIntact(result.retainedBackup)
  assert.equal(path.dirname(path.dirname(result.retainedBackup)),fs.realpathSync.native(path.dirname(path.dirname(target))))
})

test('concurrent publication fails closed without touching the destination',t => {
  const {root,dist,target}=fixture(t)
  fs.mkdirSync(path.join(root,'.web-html-publication.lock'))
  assert.throws(() => publishAssets(dist,target),/EEXIST/)
  previousIntact(target)
})

test('a failed npm build leaves the previous embedded tree untouched',{skip:process.platform==='win32'},t => {
  const {root,target}=fixture(t)
  const entry=path.join(root,'build.sh'),bin=path.join(root,'bin')
  fs.copyFileSync(fileURLToPath(new URL('../build.sh',import.meta.url)),entry)
  fs.mkdirSync(path.join(root,'frontend'))
  fs.mkdirSync(bin)
  fs.writeFileSync(path.join(bin,'npm'),'#!/bin/sh\n[ "$1" = ci ] && exit 0\nexit 8\n',{mode:0o755})
  const result=spawnSync('bash',[entry],{cwd:os.tmpdir(),encoding:'utf8',env:{...process.env,PATH:bin+path.delimiter+process.env.PATH}})
  assert.equal(result.status,8,result.stderr)
  previousIntact(target)
})

test('non-regular generated files cannot enter the published tree',{skip:process.platform==='win32'},t => {
  const {dist,target}=fixture(t)
  execFileSync('mkfifo',[path.join(dist,'assets','pipe')])
  assert.throws(()=>publishAssets(dist,target),/Non-regular asset/)
  previousIntact(target)
})

test('the real embed contract includes underscore assets and Vite metadata',t => {
  const {root,dist}=fixture(t)
  const directive=fs.readFileSync(fileURLToPath(new URL('../web/web.go',import.meta.url)),'utf8').match(/^\/\/go:embed (.+)$/m)?.[1]
  assert.ok(directive)
  fs.cpSync(dist,path.join(root,'html'),{recursive:true})
  fs.writeFileSync(path.join(root,'html/assets/_chunk.js'),'export default 1')
  fs.writeFileSync(path.join(root,'go.mod'),'module example/assets\n\ngo 1.26.6\n')
  fs.writeFileSync(path.join(root,'main.go'),`package main\nimport ("embed";"fmt")\n//go:embed ${directive}\nvar assets embed.FS\nfunc main(){ for _,name:=range []string{"html/assets/_chunk.js","html/.vite/manifest.json"}{if _,err:=assets.ReadFile(name);err!=nil{panic(err)}};fmt.Println("PASS") }\n`)
  const result=spawnSync('go',['run','.'],{cwd:root,encoding:'utf8',maxBuffer:4*1024*1024})
  assert.equal(result.status,0,result.stderr)
  assert.match(result.stdout,/PASS/)
})
