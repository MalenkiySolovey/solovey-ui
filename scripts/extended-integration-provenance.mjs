#!/usr/bin/env node
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { resolveBuildPlan } from './core-provider-build.mjs'
import { verifySeriesMetadata, verifyGitSeries } from './core-source-provenance.mjs'

const root=fileURLToPath(new URL('../',import.meta.url))
const requireFact=(condition,message) => { if(!condition)throw Error(message) }

export function verifyExtendedProvenance(lock,{plan,mod,sums}) {
  requireFact(lock.schema==='solovey.extended-integration/v2' && lock.provider==='shtorm-extended' && lock.patched===true && lock.upstreamByteIdentical===false,'Extended patched identity missing')
  requireFact(lock.modFile===plan.modFile && lock.runtimeSemanticVersion===plan.source.runtimeSemanticVersion,'Extended build flavor differs')
  requireFact(lock.sources?.length===2 && lock.sources[0].id==='engine' && lock.sources[1].id==='quic','Extended source inventory differs')
  for(const source of lock.sources) {
    verifySeriesMetadata(source)
    const {original,replacement}=source
    requireFact(source.license==='GPL-3.0-or-later' && source.scopes?.length>0,'Extended source notice missing')
    const required=mod.Require?.filter(item => item.Path===original.module) ?? []
    const replaced=mod.Replace?.filter(item => item.Old.Path===original.module) ?? []
    requireFact(required.length===1 && required[0].Version===original.version && replaced.length===1 && (!replaced[0].Old.Version || replaced[0].Old.Version===original.version) && replaced[0].New.Path===replacement.module && replaced[0].New.Version===replacement.version,'Extended module replacement differs')
    for(const [suffix,sum] of [['',replacement.sum],['/go.mod',replacement.goModSum]]) {
      requireFact(sums.split(/\r?\n/).includes(`${replacement.module} ${replacement.version}${suffix} ${sum}`),'Extended module checksum missing')
    }
  }
  const engine=lock.sources[0],quic=lock.sources[1]
  requireFact(engine.original.module==='github.com/sagernet/sing-box' && engine.original.version===`v${lock.runtimeSemanticVersion}` && engine.original.commit===plan.source.upstreamRevision && engine.original.repository===plan.source.upstreamRepository,'Extended native baseline differs')
  requireFact(engine.replacement.module===plan.source.module && engine.replacement.version===plan.source.providerVersion && engine.replacement.commit===plan.source.sourceRevision && engine.replacement.sum===plan.source.moduleSum,'Extended observed source differs')
  requireFact(quic.original.module==='github.com/sagernet/sing-quic' && quic.original.repository==='https://github.com/SagerNet/sing-quic','Extended QUIC authority differs')
  requireFact(mod.Require?.some(item => item.Path===lock.transport.module && item.Version===lock.transport.version),'Extended QUIC transport differs')
  return lock
}

export function readExtendedProvenance(repoRoot=root) {
  const plan=resolveBuildPlan({provider:'shtorm-extended',repoRoot})
  const lock=JSON.parse(fs.readFileSync(path.join(repoRoot,'deploy/dependencies/sing-box-extended-integration.json'),'utf8'))
  const mod=JSON.parse(execFileSync('go',['mod','edit','-json',path.join(repoRoot,plan.modFile)],{encoding:'utf8',windowsHide:true}))
  const sums=fs.readFileSync(path.join(repoRoot,'go.extended.sum'),'utf8')
  return verifyExtendedProvenance(lock,{plan,mod,sums})
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
        `extended_${source.id}_source_tree=${source.patchSeries.patchedTree}`,
        `extended_${source.id}_patch_series=${source.patchSeries.identity}`,
        `extended_${source.id}_patch_count=${source.patchSeries.patchCount}`,
        `extended_${source.id}_module_sum=${source.replacement.sum}`,
        `extended_${source.id}_license=${source.license}`,
      ]),
    ].join('\n'))
    else if(command==='verify-git') {
      requireFact(process.argv[3] && process.argv[4],'verify-git requires existing engine and QUIC clones')
      for(const [index,source] of lock.sources.entries())console.log(JSON.stringify(verifyGitSeries(source,path.resolve(process.argv[index+3]))))
    }
    else if(command==='check')console.log(`EXTENDED_PROVENANCE=PASS; engine=${lock.sources[0].replacement.commit}; quic=${lock.sources[1].replacement.commit}`)
    else throw Error('Expected check, manifest, build-info or verify-git <existing-engine-clone> <existing-quic-clone>')
  } catch(error) { console.error(error.message);process.exitCode=1 }
}
