#!/usr/bin/env node

import fs from 'node:fs'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const root=fileURLToPath(new URL('../',import.meta.url))
const engineModule='github.com/sagernet/sing-box'

export function verifySourcePin(source, mod, sums) {
  const required=mod.Require?.filter(item => item.Path===engineModule) ?? []
  const replacements=mod.Replace?.filter(item => item.Old.Path===engineModule) ?? []
  if(required.length!==1 || required[0].Version!==`v${source.runtimeSemanticVersion}` || replacements.length!==1) {
    throw Error('Provider engine requirement or replacement differs from its source pin')
  }
  const replacement=replacements[0]
  if((replacement.Old.Version && replacement.Old.Version!==required[0].Version) ||
    replacement.New.Path!==source.module || replacement.New.Version!==source.providerVersion) {
    throw Error('Provider replacement differs from its source pin')
  }
  if(!sums.split(/\r?\n/).includes(`${source.module} ${source.providerVersion} ${source.moduleSum}`)) {
    throw Error('Provider checksum differs from its source pin')
  }
}

export function resolveBuildPlan({provider,baseTags='',repoRoot=root}={}) {
  const catalog=JSON.parse(fs.readFileSync(path.join(repoRoot,'core/provider/pins.json'),'utf8'))
  provider ??= catalog.defaultProvider
  const flavors=catalog.buildFlavors.filter(item => item.id===provider)
  if(flavors.length!==1)throw Error('Unknown core provider build flavor')
  const flavor=flavors[0]
  if(!['go.mod','go.extended.mod'].includes(flavor.modFile))throw Error('Unrecognized provider module lock')
  const sources=catalog.sources.filter(item => item.id===provider && item.sourceRevision===flavor.sourceRevision)
  if(sources.length!==1)throw Error('Provider build source is not pinned uniquely')
  const source=sources[0]
  const tags=baseTags ? baseTags.split(',') : []
  if(tags.some(tag => !/^[a-zA-Z0-9_]+$/.test(tag)))throw Error('Invalid base build tag')
  if(tags.includes('shtorm_extended') && !flavor.buildTags.includes('shtorm_extended')) {
    throw Error('Native ABI tag contradicts the selected provider')
  }
  const modPath=path.join(repoRoot,flavor.modFile)
  const mod=JSON.parse(execFileSync('go',['mod','edit','-json',modPath],{cwd:repoRoot,encoding:'utf8',windowsHide:true}))
  const sums=fs.readFileSync(modPath.replace(/\.mod$/,'.sum'),'utf8')
  verifySourcePin(source,mod,sums)
  return {
    schema:'solovey.core-provider-build/v1',
    provider,modFile:flavor.modFile,source,
    tags:[...new Set([...tags,...flavor.buildTags])].join(','),
    goArgs:['-mod=readonly',...(flavor.modFile==='go.mod' ? [] : [`-modfile=${flavor.modFile}`])],
  }
}

if(process.argv[1] && path.resolve(process.argv[1])===fileURLToPath(import.meta.url)) {
  try {
    const args=process.argv.slice(2)
    const command=args.shift() ?? 'plan'
    if(!['plan','go-args','tags'].includes(command))throw Error('Unknown provider build command')
    const options={}
    while(args.length) {
      const name=args.shift(), value=args.shift()
      if(!value || !['--provider','--tags'].includes(name))throw Error('Invalid provider build argument')
      const key=name==='--provider' ? 'provider' : 'baseTags'
      if(Object.hasOwn(options,key))throw Error('Duplicate provider build argument')
      options[key]=value
    }
    const plan=resolveBuildPlan(options)
    console.log(command==='go-args' ? plan.goArgs.join('\n') : command==='tags' ? plan.tags : JSON.stringify(plan))
  } catch(error) { console.error(error.message);process.exitCode=1 }
}
