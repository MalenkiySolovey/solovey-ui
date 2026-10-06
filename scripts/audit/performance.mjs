#!/usr/bin/env node

import fs from 'node:fs'
import path from 'node:path'
import os from 'node:os'
import { execFileSync, spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const repo=fileURLToPath(new URL('../../',import.meta.url))
const config=JSON.parse(fs.readFileSync(new URL('./performance.json',import.meta.url),'utf8'))
const tool=JSON.parse(fs.readFileSync(new URL('./tools.json',import.meta.url),'utf8')).benchstat

export function normalizeBenchmarks(source) {
  const lines=[]
  let owner='',pending=''
  for(const line of source.split(/\r?\n/)) {
    if(/^(?:goos|goarch|pkg|cpu): /.test(line)) {
      if(pending)throw Error(`Missing benchmark observation: ${owner}/${pending}`)
      if(line.startsWith('pkg: '))owner=line.slice(5).trim()
      lines.push(line)
      continue
    }
    const prefix=line.match(/^(Benchmark\S+)(?:\s+(.*))?$/)
    let observation=line.trim()
    if(prefix) {
      if(pending)throw Error(`Missing benchmark observation: ${owner}/${pending}`)
      pending=prefix[1]
      observation=(prefix[2]??'').trim()
    }
    // Go writes the benchmark name before calibration. Fixture/application
    // logging can separate it from the final numeric result on stdout.
    const match=observation.match(/^([1-9]\d*)\s+([\d.eE+-]+)\s+ns\/op(?:\s.*)?$/)
    if(match) {
      if(!owner || !pending || !Number.isFinite(Number(match[2])) || Number(match[2])<=0)throw Error('Invalid benchmark observation')
      lines.push(pending+'\t'+observation)
      pending=''
    } else if(pending && /^--- (?:SKIP|FAIL): Benchmark/.test(line)) {
      throw Error(`Missing benchmark observation: ${owner}/${pending}`)
    }
  }
  if(pending)throw Error(`Missing benchmark observation: ${owner}/${pending}`)
  return lines.join('\n')+'\n'
}

export function parseBenchmarks(source) {
  const rows=new Map()
  let owner=''
  for(const line of normalizeBenchmarks(source).split('\n')) {
    if(line.startsWith('pkg: '))owner=line.slice(5).trim()
    const match=line.match(/^(Benchmark\S+)\s+[1-9]\d*\s+([\d.eE+-]+)\s+ns\/op(?:\s|$)/)
    if(!match)continue
    const ns=Number(match[2])
    if(!owner || !Number.isFinite(ns) || ns<=0)throw Error('Invalid benchmark observation')
    const key=owner+'/'+match[1]
    rows.set(key,[...(rows.get(key)??[]),ns])
  }
  if(!rows.size)throw Error('Missing benchmark observations')
  return rows
}
const median=values => {
  const sorted=[...values].sort((a,b)=>a-b), mid=Math.floor(sorted.length/2)
  return sorted.length%2 ? sorted[mid] : (sorted[mid-1]+sorted[mid])/2
}
export function compareBenchmarks(baseline,candidate,{samples=config.samples,threshold=config.regressionThreshold}={}) {
  const base=parseBenchmarks(baseline),head=parseBenchmarks(candidate)
  if(base.size!==head.size || [...base.keys()].some(key=>!head.has(key)))throw Error('Baseline/candidate benchmark sets differ')
  const comparisons=[...base].map(([name,values])=>{
    const newer=head.get(name)
    if(values.length!==samples || newer.length!==samples)throw Error(`Incomplete sample set: ${name}`)
    const oldNs=median(values),newNs=median(newer),ratio=newNs/oldNs
    return {name,baselineSamples:values,candidateSamples:newer,oldNs,newNs,ratio,regression:ratio>1+threshold}
  })
  return {status:'PASS',policy:'ADVISORY',threshold,samples,comparisons,regressions:comparisons.filter(row=>row.regression)}
}

function run(program,args,{cwd=repo,env=process.env,file}={}) {
  const result=spawnSync(program,args,{cwd,env,encoding:'utf8',maxBuffer:16*1024*1024})
  if(file)fs.writeFileSync(file,(result.stdout??'')+(result.stderr??''))
  if(result.error)throw result.error
  if(result.status!==0)throw Error(`${program} failed (${result.status}); ${file??'see setup output'}: ${(result.stderr??'').slice(-1500)}`)
  return result.stdout??''
}

async function main() {
  const args=new Map(),input=process.argv.slice(2)
  for(let index=0;index<input.length;index+=2) {
    if(!['--baseline','--candidate','--out'].includes(input[index]) || !input[index+1] || args.has(input[index]))throw Error('Invalid performance arguments')
    args.set(input[index],input[index+1])
  }
  const baseline=args.get('--baseline'),candidate=args.get('--candidate'),out=path.resolve(args.get('--out')??'tests/baseline/current')
  if(!/^[a-f0-9]{40}$/.test(baseline??'') || !/^[a-f0-9]{40}$/.test(candidate??''))throw Error('Performance comparison requires exact baseline and candidate commits')
  fs.mkdirSync(out,{recursive:true})
  const summary=path.join(out,'perf-comparison.json')
  const temporary=fs.mkdtempSync(path.join(os.tmpdir(),'performance-comparison-'))
  const env={...process.env,GOTOOLCHAIN:'local',CGO_ENABLED:'1',GOFLAGS:'-mod=readonly',GOMAXPROCS:String(config.gomaxprocs),SOLOVEY_UI_PROFILE:'full'}
  const identity={baseline,candidate,config,tool,go:run('go',['version']).trim(),node:process.version,platform:process.platform,architecture:process.arch,startedAt:new Date().toISOString()}
  fs.writeFileSync(path.join(out,'perf-identity.json'),JSON.stringify(identity,null,2)+'\n')
  const worktrees=[]
  try {
    const goBin=run('go',['env','GOBIN']).trim() || path.join(run('go',['env','GOPATH']).trim().split(path.delimiter)[0],'bin')
    const benchstat=path.join(goBin,'benchstat'+(process.platform==='win32'?'.exe':''))
    const installed=run('go',['version','-m',benchstat])
    if(!installed.includes(`\tmod\t${tool.module}\t${tool.version}\t`))throw Error('Comparison tool identity mismatch')
    for(const [label,reference] of [['baseline',baseline],['candidate',candidate]]) {
      const directory=path.join(temporary,label)
      run('git',['worktree','add','--detach',directory,reference])
      worktrees.push(directory)
      const group=path.join(out,'perf-'+label)
      fs.mkdirSync(group,{recursive:true})
      run('npm',['ci'],{cwd:path.join(directory,'frontend'),env,file:path.join(group,'install.txt')})
      run('npm',['run','build'],{cwd:path.join(directory,'frontend'),env,file:path.join(group,'frontend.txt')})
      run(process.execPath,[path.join(directory,'scripts/generate-component-imports.mjs'),'--profile','full'],{cwd:directory,env,file:path.join(group,'composition.txt')})
      // The current publication owner validates both revisions, including an
      // older baseline that predates this helper. No checkout is reused.
      run(process.execPath,[path.join(repo,'scripts/frontend-assets.mjs'),'publish','--dist',path.join(directory,'frontend/dist'),'--destination',path.join(directory,'web/html')],{cwd:directory,env,file:path.join(group,'publication.txt')})
      run('go',['test','-run=^$','-bench=.','-benchmem','-benchtime='+config.benchtime,'-count='+config.samples,'-p=1','-timeout=25m',...(config.tags?['-tags',config.tags]:[]),...config.packages],{cwd:directory,env,file:path.join(group,'benchmarks.txt')})
    }
    const rawBase=fs.readFileSync(path.join(out,'perf-baseline/benchmarks.txt'),'utf8'),rawHead=fs.readFileSync(path.join(out,'perf-candidate/benchmarks.txt'),'utf8')
    const baseFile=path.join(out,'perf-baseline/benchmarks-normalized.txt'),headFile=path.join(out,'perf-candidate/benchmarks-normalized.txt')
    fs.writeFileSync(baseFile,normalizeBenchmarks(rawBase))
    fs.writeFileSync(headFile,normalizeBenchmarks(rawHead))
    const comparison=compareBenchmarks(rawBase,rawHead)
    run(benchstat,[baseFile,headFile],{env,file:path.join(out,'benchstat.txt')})
    fs.writeFileSync(summary,JSON.stringify({...comparison,baseline,candidate},null,2)+'\n')
    // Preserve the dashboard's existing warning projection, with raw samples
    // and complete comparison evidence alongside it.
    fs.writeFileSync(path.join(out,'perf-regressions.json'),JSON.stringify(comparison.regressions,null,2)+'\n')
    if(process.env.GITHUB_OUTPUT)fs.appendFileSync(process.env.GITHUB_OUTPUT,`regression=${comparison.regressions.length>0}\n`)
    for(const row of comparison.regressions)console.log(`::warning title=Performance comparison::${row.name}: ${row.ratio.toFixed(2)}x median ns/op; advisory, inspect samples and benchstat`)
    console.log(`[performance] ${comparison.comparisons.length} benchmarks, ${config.samples} samples per revision, ${comparison.regressions.length} advisory warnings`)
  } catch(error) {
    fs.writeFileSync(summary,JSON.stringify({status:'HARNESS_FAILURE',policy:'ADVISORY',baseline,candidate,error:error.message},null,2)+'\n')
    throw error
  } finally {
    for(const directory of worktrees) {
      if(path.dirname(directory)!==temporary)throw Error('Unsafe performance worktree cleanup path')
      execFileSync('git',['worktree','remove','--force',directory],{cwd:repo,stdio:'pipe'})
    }
    fs.rmSync(temporary,{recursive:true,force:true})
  }
}
if(process.argv[1] && path.resolve(process.argv[1])===fileURLToPath(import.meta.url)) {
  main().catch(error=>{console.error(error.message);process.exitCode=1})
}
