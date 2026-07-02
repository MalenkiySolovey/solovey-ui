#!/usr/bin/env node

import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'

const args = parseArgs()
const componentsDir = path.resolve(args.get('components-dir') ?? 'components')
const out = path.resolve(args.get('out') ?? path.join(componentsDir, 'installed.json'))
const profile = args.get('profile') ?? 'full'
const binary = args.get('binary') ?? 'full'

if (!fs.existsSync(componentsDir)) {
  fail(`components directory not found: ${componentsDir}`)
}
if (!/^[a-z0-9-]+$/.test(profile)) {
  fail(`invalid profile: ${profile}`)
}
if (!/^[a-z0-9-]+$/.test(binary)) {
  fail(`invalid binary: ${binary}`)
}

const components = []
for (const entry of fs.readdirSync(componentsDir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
  if (!entry.isDirectory()) continue

  const manifestPath = path.join(componentsDir, entry.name, 'component.json')
  if (!fs.existsSync(manifestPath)) continue

  const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'))
  if (manifest.id !== entry.name) {
    fail(`component manifest id ${manifest.id} does not match directory ${entry.name}`)
  }
  if (!/^[a-z0-9-]+$/.test(manifest.id ?? '')) {
    fail(`component manifest has invalid id: ${manifestPath}`)
  }
  if (manifest.delivery !== 'in-process') {
    fail(`unsupported component delivery for installed metadata: ${manifest.id} ${manifest.delivery}`)
  }

  components.push({
    id: manifest.id,
    delivery: manifest.delivery,
    installed: true,
  })
}

if (components.length === 0) {
  fail(`no component packs found in ${componentsDir}`)
}

const metadata = {
  version: 1,
  profile,
  binary,
  components,
}
fs.mkdirSync(path.dirname(out), { recursive: true })
fs.writeFileSync(out, `${JSON.stringify(metadata, null, 2)}\n`)
console.log(`[component-installed] wrote ${out}`)

function parseArgs() {
  const parsed = new Map()
  for (let index = 2; index < process.argv.length; index += 1) {
    const key = process.argv[index]
    if (!key.startsWith('--')) {
      usage()
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
  console.error('Usage: node scripts/write-component-installed-metadata.mjs [--components-dir components] [--out components/installed.json] [--profile full] [--binary full]')
  process.exit(2)
}

function fail(message) {
  console.error(`[component-installed] ERROR: ${message}`)
  process.exit(1)
}
