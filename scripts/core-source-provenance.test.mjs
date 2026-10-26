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
  const stage=fs.readFileSync(new URL('Dockerfile',root),'utf8').split(/^FROM /m)[1]
  for(const line of stage.split(/\r?\n/)) {
    if(!line.startsWith('COPY ') || !/\.\/(?:scripts\/|deploy\/dependencies\/)?$/.test(line))continue
    const inputs=line.slice(5).split(/\s+/)
    inputs.pop()
    for(const input of inputs) {
      const target=path.join(directory,input)
      fs.mkdirSync(path.dirname(target),{recursive:true})
      fs.copyFileSync(new URL(input,root),target)
    }
  }
  for(const name of ['ssm','quic','anytls']) {
    const output=execFileSync(process.execPath,[path.join(directory,`scripts/${name}-integration-provenance.mjs`),'manifest'],{encoding:'utf8',windowsHide:true})
    verifySeriesMetadata(JSON.parse(output))
  }
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
