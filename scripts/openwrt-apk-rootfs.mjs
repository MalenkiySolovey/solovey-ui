#!/usr/bin/env node

import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'
import { canonical, sha256 } from './openwrt-package-source-fingerprint.mjs'
import { stageManifestSchema } from './openwrt-stage-manifest.mjs'

export const apkRootfsProofSchema = 'solovey-ui/openwrt-apk-rootfs-proof/v1'

const metadataEntries = [
  { path: 'lib/apk', type: 'directory', mode: '0755' },
  { path: 'lib/apk/packages', type: 'directory', mode: '0755' },
  { path: 'lib/apk/packages/solovey-ui.list', type: 'file', mode: '0644' },
  { path: 'lib/apk/packages/solovey-ui.rusers', type: 'file', mode: '0644' },
]

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const args = parseArgs(process.argv.slice(2))
    const manifest = JSON.parse(fs.readFileSync(path.resolve(required(args, 'stage-manifest')), 'utf8'))
    const proof = verifyApkRootfs(manifest, path.resolve(required(args, 'rootfs')), args.get('apk-sha256'))
    writeJSON(path.resolve(required(args, 'out')), proof)
    console.log(`[openwrt-apk-rootfs] verified ${proof.rootfsIdentity}`)
  } catch (error) {
    console.error(`[openwrt-apk-rootfs] ERROR: ${error instanceof Error ? error.message : String(error)}`)
    process.exit(1)
  }
}

export function verifyApkRootfs(manifest, rootfs, apkSha256) {
  if (manifest?.schema !== stageManifestSchema || !Array.isArray(manifest.payload?.entries)) {
    throw new Error('canonical stage manifest is unavailable or unsupported')
  }
  if (apkSha256 !== undefined && !/^[0-9a-f]{64}$/.test(apkSha256)) throw new Error('APK SHA-256 is invalid')
  const expectedStageEntries = manifest.payload.entries.map(comparableRecord)
  const actualEntries = enumerateRootfs(rootfs)
  const expectedAll = [
    ...expectedStageEntries.map(entry => ({ path: entry.path, type: entry.type })),
    ...metadataEntries.map(entry => ({ path: entry.path, type: entry.type })),
  ].sort(compareEntries)
  const actualPathTypes = actualEntries.map(entry => ({ path: entry.path, type: entry.type }))
  assertSameEntries(expectedAll, actualPathTypes)

  const metadataPaths = new Set(metadataEntries.map(entry => entry.path))
  const actualStageEntries = actualEntries.filter(entry => !metadataPaths.has(entry.path)).map(comparableRecord)
  if (canonical(expectedStageEntries) !== canonical(actualStageEntries)) {
    throw new Error('APK installed rootfs differs from the canonical stage')
  }

  verifyPackageMetadata(rootfs, expectedStageEntries)
  const rootfsIdentity = sha256(canonical(actualStageEntries))
  const material = {
    schema: apkRootfsProofSchema,
    stageIdentity: manifest.stageIdentity,
    stageTreeDigest: manifest.payload.treeDigest,
    apkSha256: apkSha256 ?? null,
    rootfsIdentity,
    fileCount: actualStageEntries.filter(entry => entry.type === 'file').length,
    directoryCount: actualStageEntries.filter(entry => entry.type === 'directory').length,
    packageMetadata: {
      model: 'apk-tools-v3-generated-package-registration',
      paths: metadataEntries.map(entry => entry.path),
      listSha256: sha256(fs.readFileSync(path.join(rootfs, 'lib/apk/packages/solovey-ui.list'))),
      runtimeUsersSha256: sha256(fs.readFileSync(path.join(rootfs, 'lib/apk/packages/solovey-ui.rusers'))),
    },
  }
  return { ...material, proofIdentity: sha256(canonical(material)) }
}

function enumerateRootfs(rootfs) {
  const rootStat = fs.lstatSync(rootfs)
  if (!rootStat.isDirectory() || rootStat.isSymbolicLink()) throw new Error('APK rootfs extraction root is unavailable')
  const entries = []
  walk(rootfs, rootfs, entries)
  entries.sort(compareEntries)
  const seen = new Set()
  const folded = new Set()
  for (const entry of entries) {
    if (!isSafeRelative(entry.path) || seen.has(entry.path)) throw new Error(`APK rootfs contains an invalid or duplicate path: ${entry.path}`)
    const lower = entry.path.toLowerCase()
    if (folded.has(lower)) throw new Error(`APK rootfs contains a case-colliding path: ${entry.path}`)
    seen.add(entry.path)
    folded.add(lower)
  }
  return entries
}

function walk(directory, root, records) {
  const entries = fs.readdirSync(directory, { withFileTypes: true }).sort((left, right) => bytewise(left.name, right.name))
  for (const entry of entries) {
    const candidate = path.join(directory, entry.name)
    const relative = path.relative(root, candidate).split(path.sep).join('/')
    const stat = fs.lstatSync(candidate)
    const mode = (stat.mode & 0o777).toString(8).padStart(4, '0')
    if (stat.isSymbolicLink()) throw new Error(`APK rootfs contains an unexpected symlink: ${relative}`)
    if (stat.isDirectory()) {
      records.push({ path: relative, type: 'directory', mode })
      walk(candidate, root, records)
    } else if (stat.isFile()) {
      records.push({ path: relative, type: 'file', mode, sha256: sha256(fs.readFileSync(candidate)), size: stat.size })
    } else {
      throw new Error(`APK rootfs contains an unsupported entry type: ${relative}`)
    }
  }
}

function comparableRecord(record) {
  if (record.type === 'directory') return { path: record.path, type: 'directory', mode: record.mode }
  return { path: record.path, type: 'file', mode: record.mode, sha256: record.sha256, size: record.size }
}

function verifyPackageMetadata(rootfs, stageEntries) {
  for (const expected of metadataEntries) {
    const candidate = path.join(rootfs, ...expected.path.split('/'))
    const stat = fs.lstatSync(candidate)
    const mode = (stat.mode & 0o777).toString(8).padStart(4, '0')
    if (mode !== expected.mode || (expected.type === 'file' ? !stat.isFile() : !stat.isDirectory()) || stat.isSymbolicLink()) {
      throw new Error(`APK package metadata contract mismatch: ${expected.path}`)
    }
  }
  const expectedList = [
    ...stageEntries.filter(entry => entry.type === 'file').map(entry => `/${entry.path}`),
    '/lib/apk/packages/solovey-ui.rusers',
  ].sort(bytewise)
  const listValue = fs.readFileSync(path.join(rootfs, 'lib/apk/packages/solovey-ui.list'), 'utf8')
  if (listValue !== `${expectedList.join('\n')}\n`) throw new Error('APK package path registration does not match the canonical stage')
  const runtimeUsers = fs.readFileSync(path.join(rootfs, 'lib/apk/packages/solovey-ui.rusers'), 'utf8')
  if (runtimeUsers !== 'solovey-ui:solovey-ui\n') throw new Error('APK package runtime-user metadata is not canonical')
}

function assertSameEntries(expected, actual) {
  if (canonical(expected) === canonical(actual)) return
  const key = entry => `${entry.type}:${entry.path}`
  const expectedSet = new Set(expected.map(key))
  const actualSet = new Set(actual.map(key))
  const missing = expected.filter(entry => !actualSet.has(key(entry))).map(key)
  const extra = actual.filter(entry => !expectedSet.has(key(entry))).map(key)
  throw new Error(`APK rootfs path/type mismatch; missing=[${missing.join(', ')}] extra=[${extra.join(', ')}]`)
}

function compareEntries(left, right) {
  return bytewise(left.path, right.path)
}

function parseArgs(values) {
  const result = new Map()
  for (let index = 0; index < values.length; index += 2) {
    const key = values[index]
    const value = values[index + 1]
    if (!key?.startsWith('--') || !value || value.startsWith('--')) usage()
    result.set(key.slice(2), value)
  }
  return result
}

function required(args, name) {
  const value = args.get(name)
  if (!value) usage()
  return value
}

function isSafeRelative(value) {
  if (value === '' || value.startsWith('/') || value.includes('\\') || value.includes('\0')) return false
  const segments = value.split('/')
  return segments.every(segment => segment !== '' && segment !== '.' && segment !== '..') && path.posix.normalize(value) === value
}

function bytewise(left, right) {
  return Buffer.from(left).compare(Buffer.from(right))
}

function writeJSON(file, value) {
  fs.writeFileSync(file, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o644 })
  fs.chmodSync(file, 0o644)
}

function usage() {
  throw new Error('Usage: node scripts/openwrt-apk-rootfs.mjs --stage-manifest <json> --rootfs <directory> --out <json> [--apk-sha256 <hex>]')
}
