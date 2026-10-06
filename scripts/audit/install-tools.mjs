#!/usr/bin/env node

import fs from 'node:fs'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const tools = JSON.parse(fs.readFileSync(new URL('./tools.json',import.meta.url),'utf8'))
const names = process.argv.slice(2)
if (!names.length || names.some(name => !Object.hasOwn(tools,name))) throw Error('Specify declared audit tool names')
const repo = fileURLToPath(new URL('../../',import.meta.url))
const go = args => execFileSync('go',args,{cwd:repo,encoding:'utf8',maxBuffer:8*1024*1024})
const bin = go(['env','GOBIN']).trim() || path.join(go(['env','GOPATH']).trim().split(path.delimiter)[0],'bin')
for (const name of names) {
  const tool=tools[name]
  execFileSync('go',['install',`${tool.package}@${tool.version}`],{cwd:repo,stdio:'inherit'})
  const executable=path.join(bin,name+(process.platform==='win32'?'.exe':''))
  const metadata=go(['version','-m',executable])
  if (!metadata.includes(`\tmod\t${tool.module}\t${tool.version}\t`)) throw Error(`Installed audit tool identity mismatch: ${name}`)
  console.log(`[audit-tools] ${name} ${tool.version}`)
}
if (process.env.GITHUB_PATH) fs.appendFileSync(process.env.GITHUB_PATH,bin+'\n')
