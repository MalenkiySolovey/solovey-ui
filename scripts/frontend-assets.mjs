#!/usr/bin/env node

import fs from 'node:fs'
import path from 'node:path'
import os from 'node:os'
import crypto from 'node:crypto'
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

const scriptDirectory = path.dirname(fileURLToPath(import.meta.url))
const digest = bytes => crypto.createHash('sha256').update(bytes).digest('hex')

function safePath(value) {
  if (typeof value !== 'string' || !value || value.startsWith('/') || /[\\\x00-\x1f<>:"|?*]/.test(value)) throw Error(`Unsafe asset path: ${value}`)
  if (value.split('/').some(part => !part || part === '.' || part === '..' || ['.git','.hg','.svn','.bzr'].includes(part))) throw Error(`Asset path cannot be embedded: ${value}`)
  return value
}

function inventory(root) {
  const files = new Map()
  const walk = (directory, prefix = '') => {
    const stat = fs.lstatSync(directory)
    if (!stat.isDirectory() || stat.isSymbolicLink()) throw Error(`Asset directory must be a regular directory: ${directory}`)
    for (const name of fs.readdirSync(directory).sort()) {
      const relative = safePath(prefix ? `${prefix}/${name}` : name)
      const absolute = path.join(directory,name)
      const entry = fs.lstatSync(absolute)
      if (entry.isSymbolicLink()) throw Error(`Asset symlink is unsupported: ${relative}`)
      if (entry.isDirectory()) walk(absolute,relative)
      else if (entry.isFile()) {
        const bytes = fs.readFileSync(absolute)
        if (bytes.length === 0) throw Error(`Empty asset: ${relative}`)
        files.set(relative,{path:absolute,sha256:digest(bytes)})
      } else throw Error(`Non-regular asset: ${relative}`)
    }
  }
  walk(root)
  return files
}

function componentProviders(dist, componentsDirectory) {
  if (!componentsDirectory) return new Map()
  // Optional packs retain their manifest/closure owner. Its proof identifies
  // every provider; publication never redefines component membership.
  const temporary = fs.mkdtempSync(path.join(os.tmpdir(),'frontend-closure-'))
  try {
    const proofPath = path.join(temporary,'closure.json')
    execFileSync(process.execPath,[path.join(scriptDirectory,'frontend-runtime-closure.mjs'),'--dist',dist,'--components-dir',path.resolve(componentsDirectory),'--out',proofPath],{stdio:'pipe',maxBuffer:4*1024*1024})
    const proof = JSON.parse(fs.readFileSync(proofPath,'utf8'))
    const providers = new Map(), inventories = new Map()
    for (const asset of proof.runtimeAssets) {
      safePath(asset.file)
      for (const owner of asset.providers.filter(owner => owner.startsWith('component:'))) {
        const id = owner.slice('component:'.length)
        if (!/^[a-z0-9-]+$/.test(id)) throw Error('Invalid component asset owner')
        if (!inventories.has(id)) inventories.set(id,inventory(path.join(path.resolve(componentsDirectory),id,'frontend')))
        const entry = inventories.get(id).get(asset.file)
        if (!entry || entry.sha256 !== asset.sha256) throw Error(`Component asset changed during verification: ${asset.file}`)
        providers.set(asset.file,entry)
      }
    }
    return providers
  } finally { fs.rmSync(temporary,{recursive:true,force:true}) }
}

function references(name, source) {
  const result = []
  const collect = pattern => { for (const match of source.matchAll(pattern)) result.push(match[1]) }
  if (name.endsWith('.html')) collect(/\b(?:src|href)\s*=\s*["']([^"']+)["']/gi)
  if (/\.m?js$/.test(name)) {
    collect(/\b(?:import|export)\s+(?:[^;"']*?\s+from\s*)?["']([^"']+)["']/g)
    collect(/\bimport\s*\(\s*["']([^"']+)["']\s*\)/g)
    collect(/\bnew\s+URL\(\s*["']([^"']+)["']\s*,\s*import\.meta\.url/g)
    collect(/["']((?:\.\/)?assets\/[^"'\s]+\.(?:m?js|css))["']/g)
  }
  if (name.endsWith('.css')) collect(/\burl\(\s*["']?([^"')\s]+)["']?\s*\)/gi)
  return result
}

export function verifyDist(directory, {componentsDirectory} = {}) {
  const dist = path.resolve(directory), files = inventory(dist)
  if (!files.has('index.html') || !files.has('.vite/manifest.json')) throw Error('Production dist requires index.html and .vite/manifest.json')
  const manifest = JSON.parse(fs.readFileSync(files.get('.vite/manifest.json').path,'utf8'))
  if (!manifest['index.html']?.isEntry || !manifest['index.html']?.file) throw Error('Production manifest has no index entry')
  const provided = new Map([...files,...componentProviders(dist,componentsDirectory)])
  const requireFile = value => {
    safePath(value)
    if (!provided.has(value)) throw Error(`Missing generated asset: ${value}`)
  }
  for (const [key,entry] of Object.entries(manifest)) {
    if (!entry || typeof entry !== 'object') throw Error(`Invalid manifest entry: ${key}`)
    requireFile(entry.file)
    for (const value of [...(entry.css ?? []),...(entry.assets ?? [])]) requireFile(value)
    for (const imported of [...(entry.imports ?? []),...(entry.dynamicImports ?? [])]) {
      if (!Object.hasOwn(manifest,imported)) throw Error(`Missing manifest dependency: ${imported}`)
    }
  }
  if (![...files.keys()].some(name => /^assets\/.+\.m?js$/.test(name))) throw Error('Production dist has no JavaScript assets')
  for (const [name,entry] of provided) {
    if (!/\.(?:html|css|m?js)$/.test(name)) continue
    for (const reference of references(name,fs.readFileSync(entry.path,'utf8'))) {
      if (!reference || reference.startsWith('#') || reference.startsWith('//') || /^[a-z][a-z0-9+.-]*:/i.test(reference)) continue
      const decoded = decodeURIComponent(reference.split(/[?#]/,1)[0])
      if (!decoded) continue
      const resolved = decoded.startsWith('/') ? decoded.slice(1) : /^(?:\.\/)?assets\//.test(decoded) && /\.m?js$/.test(name) ? decoded.replace(/^\.\//,'') : path.posix.join(path.posix.dirname(name),decoded)
      requireFile(resolved)
    }
  }
  return files
}

export function publishAssets(directory, destination, options = {}) {
  let target = path.resolve(destination)
  const dist = path.resolve(directory)
  if (target === dist || target.startsWith(dist+path.sep) || dist.startsWith(target+path.sep)) throw Error('Dist and published assets must be separate trees')
  const source = verifyDist(dist,options)
  fs.mkdirSync(path.dirname(target),{recursive:true})
  if (fs.lstatSync(path.dirname(target)).isSymbolicLink()) throw Error('Published asset parent must not be a symlink')
  const parent = fs.realpathSync.native(path.dirname(target))
  target = path.join(parent,path.basename(target))
  const targetStat = fs.lstatSync(target,{throwIfNoEntry:false})
  if (targetStat && (!targetStat.isDirectory() || targetStat.isSymbolicLink())) throw Error('Published asset destination must be a regular directory')
  // Staging and backup stay outside web, so Go cannot embed them. Concurrent
  // publishers fail before touching the destination. Both swaps use renames.
  const workspace = path.dirname(parent)
  const lock = path.join(workspace,`.${path.basename(parent)}-${path.basename(target)}-publication.lock`)
  fs.mkdirSync(lock)
  const operations = {...fs,...options.operations}
  let transaction, previous, preserved = false, published = false
  try {
    transaction = fs.mkdtempSync(path.join(workspace,'.frontend-assets-'))
    const candidate = path.join(transaction,'candidate')
    previous = path.join(transaction,'previous')
    operations.cpSync(dist,candidate,{recursive:true,errorOnExist:true,force:false})
    const staged = verifyDist(candidate,options)
    if (staged.size !== source.size || [...source].some(([name,entry]) => staged.get(name)?.sha256 !== entry.sha256)) throw Error('Staged asset integrity mismatch')
    if (fs.existsSync(target)) { operations.renameSync(target,previous); preserved = true }
    try { operations.renameSync(candidate,target); published = true }
    catch (error) {
      if (preserved) {
        try { operations.renameSync(previous,target); preserved = false }
        catch (restoreError) { throw new AggregateError([error,restoreError],`Publication failed; previous complete assets retained at ${previous}`) }
      }
      throw error
    }
    try {
      operations.rmSync(transaction,{recursive:true,force:true})
      transaction = undefined
      preserved = false
      return {published:true,files:staged.size}
    } catch {
      return {published:true,files:staged.size,retainedBackup:previous}
    }
  } finally {
    if (transaction && !preserved && !published) fs.rmSync(transaction,{recursive:true,force:true})
    fs.rmdirSync(lock)
  }
}

function main() {
  const [command,...args] = process.argv.slice(2), values = new Map()
  for (let index=0;index<args.length;index+=2) {
    const key=args[index], value=args[index+1]
    if (!['--dist','--destination','--components-dir'].includes(key) || !value || value.startsWith('--') || values.has(key)) throw Error('Invalid frontend asset arguments')
    values.set(key,value)
  }
  if (!['verify','publish'].includes(command) || !values.has('--dist') || (command==='publish' && !values.has('--destination')) || (command==='verify' && values.has('--destination'))) throw Error('Usage: frontend-assets.mjs <verify|publish> --dist <dist> [--destination <web/html>] [--components-dir <installed-packs>]')
  const options={componentsDirectory:values.get('--components-dir')}
  const result=command==='verify' ? {verified:true,files:verifyDist(values.get('--dist'),options).size} : publishAssets(values.get('--dist'),values.get('--destination'),options)
  console.log(`[frontend-assets] ${JSON.stringify(result)}`)
}

if (process.argv[1] && path.resolve(process.argv[1])===fileURLToPath(import.meta.url)) {
  try { main() } catch (error) { console.error(`[frontend-assets] ${error.message}`); process.exitCode=1 }
}
