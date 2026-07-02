#!/usr/bin/env node

import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'
import { flattenComponentFrontendEntries, readComponentFrontendEntries } from './component-frontend-manifest.mjs'

const args = new Map()
for (let index = 2; index < process.argv.length; index += 2) {
  const key = process.argv[index]
  const value = process.argv[index + 1]
  if (!key?.startsWith('--') || !value) {
    usage()
  }
  args.set(key.slice(2), value)
}

const profile = args.get('profile')
const dist = args.get('dist') ?? 'frontend/dist'
const componentsDir = args.get('components-dir') ?? 'components'
const selectedIDs = parseSelectedIDs(args.get('selected-ids'))
if (profile !== 'full' && profile !== 'core') {
  usage()
}

const manifestPath = path.join(dist, '.vite', 'manifest.json')
if (!fs.existsSync(manifestPath)) {
  fail(`frontend manifest not found: ${manifestPath}`)
}

const manifestText = fs.readFileSync(manifestPath, 'utf8')
const manifest = JSON.parse(manifestText)
const haystack = JSON.stringify(manifest)
const entriesByComponent = readComponentFrontendEntries(componentsDir)
const componentIDs = readComponentIDs(componentsDir)
const optionalModuleNeedles = flattenComponentFrontendEntries(entriesByComponent)
if (optionalModuleNeedles.length === 0) {
  fail(`no component frontend entries declared in ${componentsDir}`)
}
const present = optionalModuleNeedles.filter(needle => haystack.includes(needle))

if (profile === 'core') {
  if (present.length > 0) {
    fail(`core frontend contains optional modules:\n${present.map(item => `  - ${item}`).join('\n')}`)
  }
  console.log(`[frontend-profile] core OK: optional modules absent from ${manifestPath}`)
} else {
  const expectedModules = selectedIDs
    ? flattenComponentFrontendEntries(Object.fromEntries(
      selectedIDs.map(id => [id, entriesByComponent[id] ?? []]),
    ))
    : optionalModuleNeedles
  const forbiddenModules = selectedIDs
    ? optionalModuleNeedles.filter(needle => !expectedModules.includes(needle))
    : []
  const unknownSelected = selectedIDs?.filter(id => !componentIDs.has(id)) ?? []
  if (unknownSelected.length > 0) {
    fail(`selected component not found:\n${unknownSelected.map(item => `  - ${item}`).join('\n')}`)
  }
  const missingModules = expectedModules.filter(needle => !haystack.includes(needle))
  if (missingModules.length > 0) {
    fail(`full frontend misses optional component chunks:\n${missingModules.map(item => `  - ${item}`).join('\n')}`)
  }
  const unexpectedModules = forbiddenModules.filter(needle => haystack.includes(needle))
  if (unexpectedModules.length > 0) {
    fail(`full frontend contains unselected optional chunks:\n${unexpectedModules.map(item => `  - ${item}`).join('\n')}`)
  }
  console.log(`[frontend-profile] full OK: expected optional component chunks present in ${manifestPath}`)
}

function parseSelectedIDs(value) {
  if (value === undefined) return undefined
  const ids = value.split(',').map(item => item.trim()).filter(Boolean)
  for (const id of ids) {
    if (!/^[a-z0-9-]+$/.test(id)) {
      fail(`invalid selected component id: ${id}`)
    }
  }
  return [...new Set(ids)].sort((a, b) => a.localeCompare(b))
}

function readComponentIDs(componentsDir) {
  const ids = new Set()
  for (const entry of fs.readdirSync(componentsDir, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue
    if (fs.existsSync(path.join(componentsDir, entry.name, 'component.json'))) {
      ids.add(entry.name)
    }
  }
  return ids
}

function usage() {
  console.error('Usage: node scripts/check-frontend-profile.mjs --profile <full|core> [--dist frontend/dist] [--components-dir components] [--selected-ids id,id]')
  process.exit(2)
}

function fail(message) {
  console.error(`[frontend-profile] ERROR: ${message}`)
  process.exit(1)
}
