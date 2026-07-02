#!/usr/bin/env node

import crypto from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'

const args = new Map()
for (let index = 2; index < process.argv.length; index += 2) {
  const key = process.argv[index]
  const value = process.argv[index + 1]
  if (!key?.startsWith('--') || !value) {
    usage()
  }
  args.set(key.slice(2), value)
}

const assetsDir = args.get('assets-dir') ?? 'release-assets'
const componentsDir = args.get('components-dir') ?? 'components'
const version = args.get('version')
const out = args.get('out') ?? path.join(assetsDir, 'solovey-ui-release.json')

if (!version) usage()
if (!fs.existsSync(assetsDir)) fail(`assets directory not found: ${assetsDir}`)
if (!fs.existsSync(componentsDir)) fail(`components directory not found: ${componentsDir}`)

const linux = {}
let componentsBundle = null

for (const name of fs.readdirSync(assetsDir).sort()) {
  const fullPath = path.join(assetsDir, name)
  if (!fs.statSync(fullPath).isFile() || !name.endsWith('.tar.gz')) continue

  if (name === 'solovey-ui-components.tar.gz') {
    componentsBundle = artifactInfo(fullPath)
    continue
  }

  let match = /^solovey-ui-linux-(.+)\.tar\.gz$/.exec(name)
  if (match) {
    const platform = match[1]
    linux[platform] ??= {}
    linux[platform].full = artifactInfo(fullPath)
    continue
  }

  match = /^solovey-ui-core-linux-(.+)\.tar\.gz$/.exec(name)
  if (match) {
    const platform = match[1]
    linux[platform] ??= {}
    linux[platform].core = artifactInfo(fullPath)
  }
}

if (Object.keys(linux).length === 0) {
  fail(`no linux release archives found in ${assetsDir}`)
}
if (!componentsBundle) {
  fail(`component bundle not found in ${assetsDir}`)
}

for (const [platform, artifacts] of Object.entries(linux)) {
  if (!artifacts.full || !artifacts.core) {
    fail(`platform ${platform} must have both full and core artifacts`)
  }
  artifacts.components = componentsBundle
}

const components = {}
for (const entry of fs.readdirSync(componentsDir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
  if (!entry.isDirectory()) continue

  const manifestPath = path.join(componentsDir, entry.name, 'component.json')
  if (!fs.existsSync(manifestPath)) continue

  const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'))
  const frontendManifestPath = path.join(componentsDir, entry.name, 'frontend', 'assets.json')
  const frontend = fs.existsSync(frontendManifestPath)
    ? JSON.parse(fs.readFileSync(frontendManifestPath, 'utf8'))
    : null
  components[manifest.id] = {
    name: manifest.name,
    version: manifest.version,
    ...(manifest.since ? { since: manifest.since } : {}),
    delivery: manifest.delivery,
    defaultEnabled: Boolean(manifest.defaultEnabled),
    ...((manifest.tokenScopes ?? []).length > 0 ? { tokenScopes: manifest.tokenScopes } : {}),
    ...((manifest.frontend || frontend)
      ? {
          frontend: {
            entries: manifest.frontend?.entries ?? [],
            files: frontend?.files ?? [],
          },
        }
      : {}),
  }
}

const manifest = {
  schemaVersion: 1,
  app: 'solovey-ui',
  version,
  generatedAt: new Date().toISOString(),
  linux,
  components,
}

fs.mkdirSync(path.dirname(out), { recursive: true })
const json = `${JSON.stringify(manifest, null, 2)}\n`
fs.writeFileSync(out, json)
fs.writeFileSync(`${out}.sha256`, `${sha256(Buffer.from(json))}  ${path.basename(out)}\n`)

console.log(`[release-manifest] wrote ${out}`)
console.log(`[release-manifest] wrote ${out}.sha256`)

function artifactInfo(filePath) {
  return {
    name: path.basename(filePath),
    sha256: sha256(fs.readFileSync(filePath)),
  }
}

function sha256(data) {
  return crypto.createHash('sha256').update(data).digest('hex')
}

function usage() {
  console.error('Usage: node scripts/release-manifest.mjs --version <version> [--assets-dir release-assets] [--components-dir components] [--out release-assets/solovey-ui-release.json]')
  process.exit(2)
}

function fail(message) {
  console.error(`[release-manifest] ERROR: ${message}`)
  process.exit(1)
}
