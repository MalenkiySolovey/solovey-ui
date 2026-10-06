#!/usr/bin/env node

import fs from 'node:fs'
import path from 'node:path'
import { execFileSync, spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

export function validateUnixText(bytes, name) {
  if (bytes.includes(13)) throw Error(`LF required: ${name}`)
  if (bytes.subarray(0,3).equals(Buffer.from([0xef,0xbb,0xbf]))) throw Error(`UTF-8 BOM is unsupported: ${name}`)
}

export function validateGoSource(source, name) {
  // Git normalizes LF for Go. Ignore an older Windows checkout's CRLF when
  // comparing formatting, while clean CI checks all tracked text directly.
  const normalized=source.replaceAll('\r\n','\n')
  const result=spawnSync('gofmt',[],{input:normalized,encoding:'utf8'})
  if (result.error) throw result.error
  if (result.status!==0) throw Error(`Invalid Go source: ${name}: ${result.stderr}`)
  if (result.stdout!==normalized) throw Error(`gofmt required: ${name}`)
}

export function inspectSource(root, {shell=true} = {}) {
  const files=execFileSync('git',['ls-files','-z'],{cwd:root,encoding:'utf8',maxBuffer:4*1024*1024}).split('\0').filter(Boolean)
  const goFiles=files.filter(name => name.endsWith('.go'))
  const problems=[]
  let shellCount=0, textCount=0
  for (const name of files) {
    const absolute=path.join(root,name)
    if (!fs.existsSync(absolute) || !fs.lstatSync(absolute).isFile()) continue
    const bytes=fs.readFileSync(absolute)
    const interpreter=bytes.toString('utf8').split(/\r?\n/,1)[0].match(/^#!.*\b(bash|sh)\b/)?.[1]
    const unixText=interpreter || /\.(?:sh|service|socket|timer)$/.test(name) || /^\.github\/workflows\/.*\.ya?ml$/.test(name) || name==='.gitattributes'
    if (unixText) {
      textCount++
      try { validateUnixText(bytes,name) } catch(error) { problems.push(error.message) }
    }
    if (shell && (interpreter || name.endsWith('.sh'))) {
      shellCount++
      const result=spawnSync(interpreter ?? 'bash',['-n',absolute],{cwd:root,encoding:'utf8'})
      if (result.error) throw result.error
      if (result.status!==0) problems.push(`Shell syntax: ${name}: ${result.stderr.trim()}`)
    }
  }
  // Batch bounded argument lists; Windows has a native command-line limit.
  for (let index=0;index<goFiles.length;index+=80) {
    const batch=goFiles.slice(index,index+80)
    const listed=execFileSync('gofmt',['-l',...batch],{cwd:root,encoding:'utf8',maxBuffer:4*1024*1024}).trim().split(/\r?\n/).filter(Boolean)
    for (const name of listed) {
      try { validateGoSource(fs.readFileSync(path.join(root,name),'utf8'),name) } catch(error) { problems.push(error.message) }
    }
  }
  if (problems.length) throw Error(problems.join('\n'))
  return {go:goFiles.length,unixText:textCount,shell:shellCount}
}

if (process.argv[1] && path.resolve(process.argv[1])===fileURLToPath(import.meta.url)) {
  try {
    const args=process.argv.slice(2)
    if(args.some(arg => arg!=='--no-shell'))throw Error('Unknown source hygiene argument')
    const root=fileURLToPath(new URL('../../',import.meta.url))
    console.log(`[source-hygiene] ${JSON.stringify(inspectSource(root,{shell:!args.includes('--no-shell')}))}`)
  } catch(error) { console.error(error.message);process.exitCode=1 }
}
