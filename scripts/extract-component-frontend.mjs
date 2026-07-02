#!/usr/bin/env node

import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'
import { flattenComponentFrontendEntries, readComponentFrontendEntries } from './component-frontend-manifest.mjs'

const args = parseArgs()
const dist = path.resolve(args.get('dist') ?? 'frontend/dist')
const componentsDir = path.resolve(args.get('components-dir') ?? 'components')
const outDir = path.resolve(args.get('out-dir') ?? '.release/components')
const pruneDist = args.has('prune-dist')
const manifestPath = path.join(dist, '.vite', 'manifest.json')

if (!fs.existsSync(manifestPath)) {
  fail(`frontend manifest not found: ${manifestPath}`)
}
if (!fs.existsSync(componentsDir)) {
  fail(`components directory not found: ${componentsDir}`)
}

const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'))
const componentEntries = readComponentFrontendEntries(componentsDir)
const optionalRoots = new Set(flattenComponentFrontendEntries(manifestEntriesByComponent(componentEntries)))
if (optionalRoots.size === 0) {
  fail(`no component frontend entries declared in ${componentsDir}`)
}
const coreFiles = collectCoreFiles()
const copiedByComponent = new Map()
const prunableFiles = new Set()

fs.rmSync(outDir, { recursive: true, force: true })
fs.mkdirSync(outDir, { recursive: true })

for (const [componentId, roots] of Object.entries(componentEntries)) {
  const componentSource = path.join(componentsDir, componentId)
  const componentJson = path.join(componentSource, 'component.json')
  if (!fs.existsSync(componentJson)) {
    fail(`component manifest not found: ${componentJson}`)
  }

  const componentTarget = path.join(outDir, componentId)
  fs.mkdirSync(componentTarget, { recursive: true })
  fs.copyFileSync(componentJson, path.join(componentTarget, 'component.json'))

  const files = new Set()
  const rootKeys = roots.map(root => manifestEntryKey(componentId, root))
  for (const root of rootKeys) {
    collectFiles(root, files, { includeDynamicImports: true })
  }

  const componentFiles = [...files]
    .filter(file => isAssetFile(file))
    .filter(file => !coreFiles.has(file))
    .sort()
  if (componentFiles.length === 0) {
    fail(`component ${componentId} did not produce standalone frontend assets`)
  }

  for (const file of componentFiles) {
    copyAsset(file, componentTarget)
    prunableFiles.add(file)
  }

  const frontendManifest = {
    schemaVersion: 1,
    component: componentId,
    entries: roots,
    files: componentFiles,
  }
  const frontendDir = path.join(componentTarget, 'frontend')
  fs.mkdirSync(frontendDir, { recursive: true })
  fs.writeFileSync(
    path.join(frontendDir, 'assets.json'),
    `${JSON.stringify(frontendManifest, null, 2)}\n`,
  )
  copiedByComponent.set(componentId, componentFiles)
}

if (pruneDist) {
  for (const file of prunableFiles) {
    fs.rmSync(path.join(dist, file), { force: true })
  }
}

for (const [componentId, files] of copiedByComponent.entries()) {
  console.log(`[component-frontend] ${componentId}: ${files.length} asset(s)`)
}
if (pruneDist) {
  console.log(`[component-frontend] pruned ${prunableFiles.size} asset(s) from ${dist}`)
}

function collectCoreFiles() {
  const files = new Set()
  const index = manifest['index.html']
  if (!index) {
    fail('index.html entry is missing from frontend manifest')
  }

  collectEntryFiles(index, files)
  for (const key of index.imports ?? []) {
    collectFiles(key, files, { includeDynamicImports: false })
  }
  for (const key of index.dynamicImports ?? []) {
    if (!optionalRoots.has(key)) {
      collectFiles(key, files, { includeDynamicImports: true })
    }
  }
  return files
}

function manifestEntriesByComponent(entriesByComponent) {
  const resolved = {}
  for (const [componentId, entries] of Object.entries(entriesByComponent)) {
    resolved[componentId] = entries.map(entry => manifestEntryKey(componentId, entry))
  }
  return resolved
}

function manifestEntryKey(componentId, entry) {
  if (entry.startsWith('frontend/')) {
    return `../components/${componentId}/${entry}`
  }
  return entry
}

function collectFiles(key, files, options, seen = new Set()) {
  if (seen.has(key)) return
  seen.add(key)

  const entry = manifest[key]
  if (!entry) {
    fail(`manifest entry not found: ${key}`)
  }

  collectEntryFiles(entry, files)
  for (const imported of entry.imports ?? []) {
    collectFiles(imported, files, options, seen)
  }
  if (options.includeDynamicImports && key !== 'index.html') {
    for (const imported of entry.dynamicImports ?? []) {
      if (optionalRoots.has(imported)) continue
      collectFiles(imported, files, options, seen)
    }
  }
}

function collectEntryFiles(entry, files) {
  addIfPresent(files, entry.file)
  for (const file of entry.css ?? []) addIfPresent(files, file)
  for (const file of entry.assets ?? []) addIfPresent(files, file)
}

function addIfPresent(files, file) {
  if (typeof file === 'string' && file !== '') {
    files.add(file)
  }
}

function isAssetFile(file) {
  return file.startsWith('assets/') && !file.endsWith('/')
}

function copyAsset(file, componentTarget) {
  const source = path.join(dist, file)
  if (!fs.existsSync(source)) {
    fail(`frontend asset not found: ${source}`)
  }

  const relativeAssetPath = path.relative('assets', file)
  const target = path.join(componentTarget, 'frontend', 'assets', relativeAssetPath)
  fs.mkdirSync(path.dirname(target), { recursive: true })
  fs.copyFileSync(source, target)
}

function parseArgs() {
  const parsed = new Map()
  for (let index = 2; index < process.argv.length; index += 1) {
    const key = process.argv[index]
    if (!key.startsWith('--')) {
      usage()
    }
    if (key === '--prune-dist') {
      parsed.set('prune-dist', 'true')
      continue
    }
    const value = process.argv[index + 1]
    if (!value || value.startsWith('--')) {
      usage()
    }
    parsed.set(key.slice(2), value)
    index += 1
  }
  return parsed
}

function usage() {
  console.error('Usage: node scripts/extract-component-frontend.mjs [--dist frontend/dist] [--components-dir components] [--out-dir .release/components] [--prune-dist]')
  process.exit(2)
}

function fail(message) {
  console.error(`[component-frontend] ERROR: ${message}`)
  process.exit(1)
}
