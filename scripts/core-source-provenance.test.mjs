import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { patchSeriesIdentity, verifyGitSeries, verifySeriesMetadata } from './core-source-provenance.mjs'

test('Panel contains metadata without editable engine patch payloads', () => {
  const files=execFileSync('git',['ls-files','-z','--','*.patch'],{encoding:'utf8'}).split('\0').filter(Boolean)
  assert.deepEqual(files.filter(file=>fs.existsSync(file)),[])
  const root=new URL('../deploy/dependencies/',import.meta.url)
  const sources=['sing-box-ssm','sing-quic','sing-anytls'].map(name=>JSON.parse(fs.readFileSync(new URL(`${name}-integration.json`,root),'utf8')))
  sources.push(...JSON.parse(fs.readFileSync(new URL('sing-box-extended-integration.json',root),'utf8')).sources)
  for(const source of sources)verifySeriesMetadata(source)
})

test('Docker frontend provenance runs using its declared metadata and script inputs', t => {
  const parent=fs.realpathSync(os.tmpdir()),directory=fs.mkdtempSync(path.join(parent,'solovey-core-docker-test-'))
  t.after(()=>{
    const actual=fs.realpathSync(directory)
    assert.equal(path.dirname(actual),parent)
    assert.ok(path.basename(actual).startsWith('solovey-core-docker-test-'))
    fs.rmSync(actual,{recursive:true,force:true})
  })
  const root=new URL('../',import.meta.url)
  const stage=fs.readFileSync(new URL('Dockerfile',root),'utf8').split(/^FROM /m).find(stage=>stage.split(/\r?\n/)[0].endsWith(' AS front-builder'))
  assert.ok(stage, 'Docker frontend builder must remain explicitly named')
  const tracked=execFileSync('git',['ls-files','-z'],{cwd:root,encoding:'utf8',windowsHide:true}).split('\0').filter(Boolean)
  for(const line of stage.split(/\r?\n/)) {
    if(!line.startsWith('COPY ') || line.startsWith('COPY --from='))continue
    const inputs=line.slice(5).split(/\s+/),destination=inputs.pop()
    assert.ok(destination.startsWith('./'), 'frontend input must use the build context')
    for(const input of inputs) {
      const source=new URL(input,root)
      const directoryInput=fs.statSync(source).isDirectory()
      const prefix=input.replace(/\/$/,'')+'/'
      const files=directoryInput ? tracked.filter(name=>name.startsWith(prefix)) : [input]
      assert.ok(files.length, 'declared Docker input must contain tracked files: '+input)
      for(const file of files) {
        const target=path.join(directory,destination,directoryInput ? file.slice(prefix.length) : path.basename(file))
        fs.mkdirSync(path.dirname(target),{recursive:true})
        fs.copyFileSync(new URL(file,root),target)
      }
    }
  }
  for(const name of ['ssm','quic','anytls']) {
    const output=execFileSync(process.execPath,[path.join(directory,`scripts/${name}-integration-provenance.mjs`),'manifest'],{encoding:'utf8',windowsHide:true})
    verifySeriesMetadata(JSON.parse(output))
  }
  const generator=path.join(directory,'scripts/generate-component-imports.mjs')
  const generate=()=>execFileSync(process.execPath,[generator,'--profile','full','--dry-run'],{
    cwd:directory,encoding:'utf8',windowsHide:true,env:{...process.env,GOWORK:'off',GOFLAGS:'-mod=readonly'},stdio:['ignore','pipe','pipe'],
  })
  assert.match(generate(), /in-process component import\(s\)/)
  fs.unlinkSync(path.join(directory,'deploy/dependencies/solovey-backend-source.json'))
  assert.throws(generate, error=>error.status!==0 && /solovey-backend-source[.]json/.test(error.stderr.toString()))
})

test('Git verification rejects forged trees, omitted commits and changed authority even with a recomputed series identity', t => {
  const root=fs.realpathSync(os.tmpdir()),directory=fs.mkdtempSync(path.join(root,'solovey-core-series-test-'))
  t.after(()=>{
    const actual=fs.realpathSync(directory)
    assert.equal(path.dirname(actual),root)
    assert.ok(path.basename(actual).startsWith('solovey-core-series-test-'))
    fs.rmSync(actual,{recursive:true,force:true})
  })
  const git=(...args)=>execFileSync('git',['-C',directory,...args],{encoding:'utf8',windowsHide:true,stdio:['ignore','pipe','pipe']}).trim()
  git('init','--quiet');git('config','user.email','fixture@example.invalid');git('config','user.name','Fixture');git('config','core.autocrlf','false')
  const make=(filename,text)=>{fs.writeFileSync(path.join(directory,filename),text);git('add',filename);git('commit','--quiet','-m',filename);return git('rev-parse','HEAD')}
  const base=make('source.txt','upstream\n'),upstreamTree=git('rev-parse','HEAD^{tree}')
  const correction=make('source.txt','upstream\nrequired correction\n')
  const head=make('policy.txt','core owns this correction\n'),patchedTree=git('rev-parse','HEAD^{tree}')
  const source={original:{repository:'https://github.com/shtorm-7/sing-box-extended',commit:base},replacement:{repository:'https://github.com/MalenkiySolovey/sing-box-extended',module:'github.com/MalenkiySolovey/sing-box-extended',commit:head,version:'v0.0.0-20261025185415-'+head.slice(0,12)},copyRole:'mirror/cache/development',patchSeries:{format:'linear-git-commits/v1',base,upstreamTree,patchedTree,pristineRef:'refs/heads/mirror/extended',branch:'solovey/extended-patches',commits:[correction,head],patchCommits:[correction],patchCount:1}}
  source.patchSeries.identity=patchSeriesIdentity(source.patchSeries)
  assert.equal(verifyGitSeries(source,directory).status,'PASS')
  for(const mutate of [
    value=>{value.patchSeries.upstreamTree=patchedTree},
    value=>{value.patchSeries.patchedTree=upstreamTree},
    value=>{value.patchSeries.commits=[head];value.patchSeries.patchCommits=[];value.patchSeries.patchCount=0},
    value=>{value.original.repository='https://github.com/deposist/sing-box-extended'},
    value=>{value.replacement.repository='https://github.com/MalenkiySolovey/sing-box'},
    value=>{value.patch={file:'duplicated-engine.patch'}},
  ]){
    const changed=structuredClone(source);mutate(changed)
    changed.patchSeries.identity=patchSeriesIdentity(changed.patchSeries)
    assert.throws(()=>verifyGitSeries(changed,directory))
  }
})
