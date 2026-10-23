#!/usr/bin/env node
import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { resolveBuildPlan } from './core-provider-build.mjs'

const root=fileURLToPath(new URL('../',import.meta.url))
const sha256=bytes => createHash('sha256').update(bytes).digest('hex')
const requireFact=(condition,message) => { if(!condition)throw Error(message) }
const safePath=value => typeof value==='string' && !path.isAbsolute(value) && !value.includes('\\') && !value.split('/').some(part => part==='..' || part==='')

export function verifyExtendedProvenance(lock,{plan,mod,sums,readPatch}) {
  requireFact(lock.schema==='solovey.extended-integration/v1' && lock.provider==='shtorm-extended' && lock.patched===true && lock.upstreamByteIdentical===false,'Extended patched identity missing')
  requireFact(lock.modFile===plan.modFile && lock.runtimeSemanticVersion===plan.source.runtimeSemanticVersion,'Extended build flavor differs')
  requireFact(lock.sources?.length===2 && lock.sources[0].id==='engine' && lock.sources[1].id==='quic','Extended source inventory differs')
  for(const source of lock.sources) {
    const {original,replacement,patch}=source
    requireFact(source.license==='GPL-3.0-or-later' && source.scopes?.length>0,'Extended source notice missing')
    requireFact(/^[a-f0-9]{40}$/.test(original.commit) && /^[a-f0-9]{40}$/.test(replacement.commit) && replacement.version.endsWith(`-${replacement.commit.slice(0,12)}`),'Extended source commit differs')
    requireFact(safePath(patch.file) && patch.file.startsWith('deploy/dependencies/') && patch.file.endsWith('.patch'),'Invalid Extended patch path')
    const bytes=readPatch(patch.file)
    requireFact(sha256(bytes)===patch.sha256,'Extended patch checksum differs')
    const files=[...bytes.toString('utf8').matchAll(/^diff --git a\/(\S+) b\/\1$/gm)].map(match => match[1]).sort()
    requireFact(files.length>0 && files.every(safePath) && new Set(files).size===files.length && JSON.stringify(files)===JSON.stringify([...patch.files].sort()),'Extended patch inventory differs')
    const required=mod.Require?.filter(item => item.Path===original.module) ?? []
    const replaced=mod.Replace?.filter(item => item.Old.Path===original.module) ?? []
    requireFact(required.length===1 && required[0].Version===original.version && replaced.length===1 && (!replaced[0].Old.Version || replaced[0].Old.Version===original.version) && replaced[0].New.Path===replacement.module && replaced[0].New.Version===replacement.version,'Extended module replacement differs')
    for(const [suffix,sum] of [['',replacement.sum],['/go.mod',replacement.goModSum]]) {
      requireFact(sums.split(/\r?\n/).includes(`${replacement.module} ${replacement.version}${suffix} ${sum}`),'Extended module checksum missing')
    }
  }
  const engine=lock.sources[0]
  requireFact(engine.original.module==='github.com/sagernet/sing-box' && engine.original.version===`v${lock.runtimeSemanticVersion}` && engine.original.commit===plan.source.upstreamRevision && engine.original.repository===plan.source.upstreamRepository,'Extended native baseline differs')
  requireFact(engine.replacement.module===plan.source.module && engine.replacement.version===plan.source.providerVersion && engine.replacement.commit===plan.source.sourceRevision && engine.replacement.sum===plan.source.moduleSum,'Extended observed source differs')
  requireFact(mod.Require?.some(item => item.Path===lock.transport.module && item.Version===lock.transport.version),'Extended QUIC transport differs')
  return lock
}

export function readExtendedProvenance(repoRoot=root) {
  const plan=resolveBuildPlan({provider:'shtorm-extended',repoRoot})
  const lock=JSON.parse(fs.readFileSync(path.join(repoRoot,'deploy/dependencies/sing-box-extended-integration.json'),'utf8'))
  const mod=JSON.parse(execFileSync('go',['mod','edit','-json',path.join(repoRoot,plan.modFile)],{encoding:'utf8',windowsHide:true}))
  const sums=fs.readFileSync(path.join(repoRoot,'go.extended.sum'),'utf8')
  return verifyExtendedProvenance(lock,{plan,mod,sums,readPatch:name => fs.readFileSync(path.join(repoRoot,name))})
}

function replay(lock,clones,scratchParent) {
  requireFact(clones.every(Boolean) && Boolean(scratchParent),'Replay requires existing engine and QUIC clones and an owned scratch parent')
  const parent=fs.realpathSync(path.resolve(scratchParent))
  for(const [index,source] of lock.sources.entries()) {
    const clone=path.resolve(clones[index])
    const scratch=fs.mkdtempSync(path.join(parent,'solovey-extended-replay-'))
    try {
      execFileSync('git',['init','--quiet',scratch],{stdio:'pipe'})
      execFileSync('git',['-C',scratch,'config','core.autocrlf','false'],{stdio:'pipe'})
      const git=(...args) => execFileSync('git',['-C',clone,...args],{maxBuffer:4*1024*1024,stdio:['ignore','pipe','pipe']})
      const originalPaths=new Set(git('ls-tree','-r','--name-only',source.original.commit).toString('utf8').trim().split('\n'))
      for(const name of source.patch.files) {
        if(!originalPaths.has(name))continue
        const destination=path.join(scratch,name)
        fs.mkdirSync(path.dirname(destination),{recursive:true})
        fs.writeFileSync(destination,git('show',`${source.original.commit}:${name}`))
      }
      execFileSync('git',['-C',scratch,'apply','--check',path.join(root,source.patch.file)],{stdio:'pipe'})
      execFileSync('git',['-C',scratch,'apply',path.join(root,source.patch.file)],{stdio:'pipe'})
      for(const name of source.patch.files) {
        requireFact(sha256(fs.readFileSync(path.join(scratch,name)))===sha256(git('show',`${source.replacement.commit}:${name}`)),`Extended patch replay differs: ${name}`)
      }
      console.log(`EXTENDED_PATCH_REPLAY=PASS; source=${source.id}; baseline=${source.original.commit}; replacement=${source.replacement.commit}; files=${source.patch.files.length}`)
    } finally {
      const actual=fs.realpathSync(scratch)
      requireFact(path.dirname(actual)===parent && path.basename(actual).startsWith('solovey-extended-replay-'),'Extended replay cleanup escaped owned parent')
      fs.rmSync(actual,{recursive:true,force:true})
    }
  }
}

if(process.argv[1] && path.resolve(process.argv[1])===fileURLToPath(import.meta.url)) {
  try {
    const lock=readExtendedProvenance(), command=process.argv[2] ?? 'check'
    if(command==='manifest')console.log(JSON.stringify(lock,null,2))
    else if(command==='build-info')console.log([
      'core_provider=shtorm-extended',`core_runtime_semantic=${lock.runtimeSemanticVersion}`,'core_upstream_byte_identical=false',
      ...lock.sources.flatMap(source => [
        `extended_${source.id}_original=${source.original.repository}@${source.original.commit}`,
        `extended_${source.id}_replacement=${source.replacement.module}@${source.replacement.version}`,
        `extended_${source.id}_source_commit=${source.replacement.commit}`,
        `extended_${source.id}_patch_sha256=${source.patch.sha256}`,
        `extended_${source.id}_module_sum=${source.replacement.sum}`,
        `extended_${source.id}_license=${source.license}`,
      ]),
    ].join('\n'))
    else if(command==='replay')replay(lock,[process.argv[3],process.argv[4]],process.argv[5])
    else if(command==='check')console.log(`EXTENDED_PROVENANCE=PASS; engine=${lock.sources[0].replacement.commit}; quic=${lock.sources[1].replacement.commit}`)
    else throw Error('Expected check, manifest, build-info or replay <existing-engine-clone> <existing-quic-clone> <owned-scratch-parent>')
  } catch(error) { console.error(error.message);process.exitCode=1 }
}
