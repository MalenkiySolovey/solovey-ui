#!/usr/bin/env node

import crypto from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'
import { readComponentFrontendEntries } from './component-frontend-manifest.mjs'

const schema = 'solovey-ui/frontend-runtime-closure/v1'
const args = parseArgs()
const dist = path.resolve(args.get('dist') ?? 'frontend/dist')
const componentsDir = path.resolve(args.get('components-dir') ?? '.release/components')
const out = path.resolve(args.get('out') ?? path.join(componentsDir, 'runtime-closure.json'))
const manifestPath = path.join(dist, '.vite', 'manifest.json')
const installedPath = path.join(componentsDir, 'installed.json')

try {
  const manifestText = fs.readFileSync(manifestPath, 'utf8')
  const manifest = JSON.parse(manifestText)
  const entriesByComponent = readComponentFrontendEntries(componentsDir)
  const installed = readInstalledMetadata(installedPath)
  const rootKeysByComponent = new Map(
    Object.entries(entriesByComponent).map(([componentID, entries]) => [
      componentID,
      entries.map(entry => manifestEntryKey(componentID, entry)),
    ]),
  )
  const optionalRoots = new Set([...rootKeysByComponent.values()].flat())

  assertSameList(
    'installed component packs',
    installed.components.map(component => component.id).sort(),
    [...rootKeysByComponent.keys()].sort(),
  )

  const documentAssets = readDocumentAssets(path.join(dist, 'index.html'))
  const coreFiles = collectClosure(manifest, ['index.html'], optionalRoots)
  for (const file of documentAssets) coreFiles.add(file)
  const actualCoreFiles = enumerateAssets(dist)
  assertSameList('core frontend assets', actualCoreFiles, [...coreFiles].sort())

  const componentProofs = []
  const providers = new Map(actualCoreFiles.map(file => [file, [{ kind: 'core', path: path.join(dist, ...file.split('/')) }]]))

  for (const componentID of [...rootKeysByComponent.keys()].sort()) {
    const roots = rootKeysByComponent.get(componentID)
    const expected = new Set()
    for (const root of roots) {
      const stopAtOtherComponents = new Set([...optionalRoots].filter(key => key !== root))
      for (const file of collectClosure(manifest, [root], stopAtOtherComponents)) {
        if (!coreFiles.has(file)) expected.add(file)
      }
    }

    const componentDir = path.join(componentsDir, componentID)
    const assetsManifest = readJSON(path.join(componentDir, 'frontend', 'assets.json'))
    if (assetsManifest?.schemaVersion !== 1 || assetsManifest.component !== componentID) {
      fail(`component ${componentID} has invalid frontend asset metadata`)
    }
    assertSameList(`component ${componentID} entries`, [...assetsManifest.entries].sort(), [...entriesByComponent[componentID]].sort())
    assertSameList(`component ${componentID} declared closure`, [...assetsManifest.files].sort(), [...expected].sort())

    const actualFiles = enumerateAssets(path.join(componentDir, 'frontend'))
    assertSameList(`component ${componentID} packaged closure`, actualFiles, [...expected].sort())
    for (const file of actualFiles) {
      const provider = {
        kind: `component:${componentID}`,
        path: path.join(componentDir, 'frontend', ...file.split('/')),
      }
      providers.set(file, [...(providers.get(file) ?? []), provider])
    }
    componentProofs.push({
      id: componentID,
      entries: [...entriesByComponent[componentID]].sort(),
      manifestEntries: [...roots].sort(),
      files: actualFiles,
    })
  }

  const requiredFiles = new Set(coreFiles)
  for (const component of componentProofs) {
    for (const file of component.files) requiredFiles.add(file)
  }
  assertSameList('runtime asset providers', [...providers.keys()].sort(), [...requiredFiles].sort())

  const emittedFiles = new Set()
  for (const entry of Object.values(manifest)) collectEntryFiles(entry, emittedFiles)
  for (const file of documentAssets) emittedFiles.add(file)
  assertSameList('Vite emitted runtime assets', [...emittedFiles].sort(), [...requiredFiles].sort())

  const runtimeAssets = [...requiredFiles].sort().map(file => {
    const fileProviders = providers.get(file) ?? []
    if (fileProviders.length === 0) fail(`runtime asset has no provider: ${file}`)
    const digests = new Set(fileProviders.map(provider => sha256File(provider.path)))
    if (digests.size !== 1) fail(`runtime asset providers disagree: ${file}`)
    if (fileProviders.some(provider => provider.kind === 'core') && fileProviders.length !== 1) {
      fail(`runtime asset is mixed between core and component generations: ${file}`)
    }
    return {
      file,
      sha256: [...digests][0],
      providers: fileProviders.map(provider => provider.kind).sort(),
    }
  })

  const proof = {
    schema,
    viteManifestSHA256: sha256(Buffer.from(manifestText)),
    installedMetadata: 'installed.json',
    runtimeLayout: {
      core: 'embedded web/html/assets',
      components: '<installed-metadata-directory>/<component-id>/frontend/assets',
    },
    counts: {
      emitted: emittedFiles.size,
      core: actualCoreFiles.length,
      componentUnion: runtimeAssets.filter(asset => !asset.providers.includes('core')).length,
      runtime: runtimeAssets.length,
    },
    coreFiles: actualCoreFiles,
    components: componentProofs,
    runtimeAssets,
  }
  fs.mkdirSync(path.dirname(out), { recursive: true })
  fs.writeFileSync(out, `${JSON.stringify(proof, null, 2)}\n`)
  console.log(`[frontend-runtime-closure] verified ${runtimeAssets.length} runtime asset(s): ${actualCoreFiles.length} core, ${proof.counts.componentUnion} component`)
} catch (error) {
  fail(error instanceof Error ? error.message : String(error))
}

function collectClosure(manifest, roots, stopKeys) {
  const files = new Set()
  const seen = new Set()
  const visit = key => {
    if (seen.has(key) || stopKeys.has(key)) return
    seen.add(key)
    const entry = manifest[key]
    if (!entry) fail(`Vite manifest entry not found: ${key}`)
    collectEntryFiles(entry, files)
    for (const imported of entry.imports ?? []) visit(imported)
    for (const imported of entry.dynamicImports ?? []) visit(imported)
  }
  for (const root of roots) visit(root)
  return files
}

function collectEntryFiles(entry, files) {
  addAsset(entry?.file, files)
  for (const file of entry?.css ?? []) addAsset(file, files)
  for (const file of entry?.assets ?? []) addAsset(file, files)
}

function addAsset(file, files) {
  if (typeof file !== 'string' || !file.startsWith('assets/') || file.endsWith('/')) return
  files.add(file)
}

function enumerateAssets(root) {
  const assetsRoot = path.join(root, 'assets')
  if (!fs.existsSync(assetsRoot)) return []
  const files = []
  const visit = (directory, prefix) => {
    for (const entry of fs.readdirSync(directory, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
      const relative = prefix ? `${prefix}/${entry.name}` : entry.name
      const absolute = path.join(directory, entry.name)
      if (entry.isDirectory()) visit(absolute, relative)
      else if (entry.isFile()) files.push(`assets/${relative}`)
      else fail(`frontend asset has unsupported type: ${absolute}`)
    }
  }
  visit(assetsRoot, '')
  return files.sort()
}

function readDocumentAssets(file) {
  const html = fs.readFileSync(file, 'utf8')
  const assets = new Set()
  const attribute = /\b(?:src|href)=["'](?:\.\/)?(assets\/[A-Za-z0-9_./-]+)["']/g
  for (const match of html.matchAll(attribute)) assets.add(match[1])
  return assets
}

function readInstalledMetadata(file) {
  const installed = readJSON(file)
  if (installed?.version !== 1 || installed.profile !== 'full' || installed.binary !== 'full' || !Array.isArray(installed.components)) {
    fail('installed component metadata is invalid or not the full production profile')
  }
  for (const component of installed.components) {
    if (!component || !/^[a-z0-9-]+$/.test(component.id ?? '') || component.delivery !== 'in-process' || component.installed !== true) {
      fail('installed component metadata contains an invalid component')
    }
  }
  return installed
}

function manifestEntryKey(componentID, entry) {
  return entry.startsWith('frontend/') ? `../components/${componentID}/${entry}` : entry
}

function assertSameList(label, actual, expected) {
  if (actual.length === expected.length && actual.every((value, index) => value === expected[index])) return
  const missing = expected.filter(value => !actual.includes(value))
  const extra = actual.filter(value => !expected.includes(value))
  fail(`${label} mismatch; missing=[${missing.join(', ')}] extra=[${extra.join(', ')}]`)
}

function readJSON(file) {
  return JSON.parse(fs.readFileSync(file, 'utf8'))
}

function sha256File(file) {
  return sha256(fs.readFileSync(file))
}

function sha256(data) {
  return crypto.createHash('sha256').update(data).digest('hex')
}

function parseArgs() {
  const parsed = new Map()
  for (let index = 2; index < process.argv.length; index += 2) {
    const key = process.argv[index]
    const value = process.argv[index + 1]
    if (!key?.startsWith('--') || !value || value.startsWith('--')) usage()
    parsed.set(key.slice(2), value)
  }
  return parsed
}

function usage() {
  console.error('Usage: node scripts/frontend-runtime-closure.mjs [--dist frontend/dist] [--components-dir .release/components] [--out .release/components/runtime-closure.json]')
  process.exit(2)
}

function fail(message) {
  console.error(`[frontend-runtime-closure] ERROR: ${message}`)
  process.exit(1)
}
