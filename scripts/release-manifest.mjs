#!/usr/bin/env node

import crypto from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'

const args = new Map()
for (let index = 2; index < process.argv.length; index += 2) {
  const key = process.argv[index]
  const value = process.argv[index + 1]
  if (!key?.startsWith('--') || !value || value.startsWith('--')) usage()
  args.set(key.slice(2), value)
}

const assetsDir = args.get('assets-dir') ?? 'release-assets'
const componentsDir = args.get('components-dir') ?? 'components'
const out = args.get('out') ?? path.join(assetsDir, 'solovey-ui-release.json')
const version = normalizeVersion(args.get('version'))
const sequence = exactPositiveInteger(args.get('sequence'), 'sequence')
const issuedAt = exactPositiveInteger(args.get('issued-at'), 'issued-at')
const expiresAt = exactPositiveInteger(args.get('expires-at') ?? String(issuedAt + 7 * 24 * 60 * 60), 'expires-at')
const channel = args.get('channel')
const keyId = args.get('key-id')
const privateKeyBase64 = process.env.SUI_RELEASE_SIGNING_PRIVATE_KEY_B64 ?? ''
const minimumPanelVersion = normalizeVersion(fs.readFileSync('config/identity/version', 'utf8').trim())
const releaseId = `solovey-ui-${channel}-${sequence}`

if (!['main', 'beta'].includes(channel)) fail('channel must be main or beta')
if (!/^[a-z0-9][a-z0-9._+-]{0,95}$/.test(keyId ?? '')) fail('key-id is invalid')
if (expiresAt <= issuedAt || expiresAt - issuedAt > 14 * 24 * 60 * 60) fail('release validity window is invalid')
if (compareVersions(minimumPanelVersion, version) > 0) fail('release version is below the minimum compatible panel version')
if (!fs.existsSync(assetsDir) || !fs.existsSync(componentsDir)) fail('release input directory is unavailable')
if (!privateKeyBase64) fail('SUI_RELEASE_SIGNING_PRIVATE_KEY_B64 is required')

const artifacts = []
let componentCatalog = null
const profilesByTarget = new Map()
for (const name of fs.readdirSync(assetsDir).sort()) {
  const filePath = path.join(assetsDir, name)
  if (!fs.statSync(filePath).isFile() || !name.endsWith('.tar.gz')) continue
  if (name === 'solovey-ui-components.tar.gz') {
    componentCatalog = await artifact(filePath, 'component-catalog', 'any', 'any')
    artifacts.push(componentCatalog)
    continue
  }
  let match = /^solovey-ui-linux-([a-z0-9._+-]+)\.tar\.gz$/.exec(name)
  let profile = 'full'
  if (!match) {
    match = /^solovey-ui-core-linux-([a-z0-9._+-]+)\.tar\.gz$/.exec(name)
    profile = 'core'
  }
  if (!match) continue
  const target = `linux/${match[1]}`
  const seen = profilesByTarget.get(target) ?? new Set()
  if (seen.has(profile)) fail(`duplicate ${profile} artifact for ${target}`)
  seen.add(profile)
  profilesByTarget.set(target, seen)
  artifacts.push(await artifact(filePath, `panel-${profile}`, 'linux', match[1]))
}
if (profilesByTarget.size === 0 || !componentCatalog) fail('coherent Linux archives and component catalog are required')
for (const [target, profiles] of profilesByTarget) {
  if (!profiles.has('full') || !profiles.has('core')) fail(`${target} lacks full/core release-set symmetry`)
}
artifacts.sort((left, right) => artifactIdentity(left).localeCompare(artifactIdentity(right)))
const totalArtifactBytes = artifacts.reduce((total, item) => total + item.size, 0)
if (!Number.isSafeInteger(totalArtifactBytes) || totalArtifactBytes > 2 * 1024 * 1024 * 1024) {
  fail('release artifact set exceeds the 2 GiB total limit')
}

const components = []
for (const entry of fs.readdirSync(componentsDir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
  if (!entry.isDirectory()) continue
  const manifestPath = path.join(componentsDir, entry.name, 'component.json')
  if (!fs.existsSync(manifestPath)) continue
  const item = JSON.parse(fs.readFileSync(manifestPath, 'utf8'))
  if (item.id !== entry.name || !/^[a-z0-9-]+$/.test(item.id ?? '')) fail(`component identity is invalid: ${manifestPath}`)
  components.push({
    id: item.id,
    version: normalizeComponentVersion(item.version),
    artifactSha256: componentCatalog.sha256,
    minimumCoreSchema: '1.11',
    maximumCoreSchema: '1.11',
  })
}
components.sort((left, right) => left.id.localeCompare(right.id))

const manifest = {
  schema: 'solovey.release/v1',
  releaseId,
  sequence,
  version,
  channel,
  issuedAt,
  expiresAt,
  deploymentRevision: digestSourceSet(['deploy/systemd', 'solovey-ui.service']),
  minimumPanelVersion,
  maximumPanelVersion: version,
  minimumCoreSchema: '1.11',
  maximumCoreSchema: '1.11',
  targetCoreSchema: '1.11',
  brokerCapability: 'broker-capabilities-1.2',
  migrationSetDigest: digestSourceSet(['database/migration/steps']),
  releaseNotesDigest: sha256(fs.readFileSync('CHANGELOG.md')),
  restartClass: 'stack',
  rebootClass: 'operator-advisory',
  rollbackClass: 'automatic',
  artifacts,
  components,
}
const canonical = Buffer.from(JSON.stringify(manifest))
let privateKey
try {
  privateKey = crypto.createPrivateKey({ key: Buffer.from(privateKeyBase64, 'base64'), format: 'der', type: 'pkcs8' })
} catch {
  fail('release signing private key encoding is invalid')
}
if (privateKey.asymmetricKeyType !== 'ed25519') fail('release signing key must be Ed25519')
const signature = crypto.sign(null, canonical, privateKey).toString('base64')
const envelope = `${JSON.stringify({
  schema: 'solovey.release/v1',
  keyId,
  algorithm: 'Ed25519',
  manifest,
  signature,
})}\n`

fs.mkdirSync(path.dirname(out), { recursive: true })
fs.writeFileSync(out, envelope, { mode: 0o600 })
fs.writeFileSync(`${out}.sha256`, `${sha256(Buffer.from(envelope))}  ${path.basename(out)}\n`, { mode: 0o600 })
console.log(`[release-manifest] wrote signed ${out} (sequence ${sequence}, channel ${channel}, key ${keyId})`)

async function artifact(filePath, role, platform, arch) {
  const size = fs.statSync(filePath).size
  if (size <= 0 || size > 1024 * 1024 * 1024) fail(`artifact size is invalid: ${filePath}`)
  return {
    name: path.basename(filePath),
    role,
    platform,
    arch,
    mediaType: 'application/vnd.solovey.release-set+gzip',
    size,
    sha256: await sha256File(filePath),
    provenance: 'github-actions',
  }
}

function sha256File(filePath) {
  return new Promise((resolve, reject) => {
    const hash = crypto.createHash('sha256')
    const input = fs.createReadStream(filePath, { highWaterMark: 1024 * 1024 })
    input.on('data', chunk => hash.update(chunk))
    input.on('error', reject)
    input.on('end', () => resolve(hash.digest('hex')))
  })
}

function artifactIdentity(value) {
  return `${value.platform}\0${value.arch}\0${value.role}\0${value.name}`
}

function digestSourceSet(roots) {
  const files = []
  for (const root of roots) collectFiles(root, files)
  if (files.length === 0) fail(`source digest inventory is empty: ${roots.join(',')}`)
  const hash = crypto.createHash('sha256')
  for (const file of files.sort()) {
    const relative = file.split(path.sep).join('/')
    const contents = fs.readFileSync(file)
    hash.update(`${Buffer.byteLength(relative)}:`).update(relative)
    hash.update(`${contents.length}:`).update(contents)
  }
  return hash.digest('hex')
}

function collectFiles(candidate, result) {
  if (!fs.existsSync(candidate)) fail(`release contract source is missing: ${candidate}`)
  const stat = fs.statSync(candidate)
  if (stat.isFile()) {
    result.push(candidate)
    return
  }
  for (const entry of fs.readdirSync(candidate, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
    const child = path.join(candidate, entry.name)
    if (entry.isDirectory()) collectFiles(child, result)
    else if (entry.isFile() && !entry.name.endsWith('_test.go')) result.push(child)
  }
}

function normalizeVersion(value) {
  const normalized = String(value ?? '').replace(/^v/, '')
  if (!/^[0-9]{1,6}\.[0-9]{1,6}\.[0-9]{1,6}(?:-[0-9A-Za-z.-]{1,64})?$/.test(normalized)) fail('version must be semantic')
  return normalized
}

function normalizeComponentVersion(value) {
  const raw = String(value ?? '')
  if (/^[0-9]{1,6}$/.test(raw)) return `${raw}.0.0`
  return normalizeVersion(raw)
}

function compareVersions(left, right) {
  const parse = value => {
    const separator = value.indexOf('-')
    const core = separator === -1 ? value : value.slice(0, separator)
    const prerelease = separator === -1 ? '' : value.slice(separator + 1)
    return { core: core.split('.').map(Number), prerelease: prerelease ? prerelease.split('.') : [] }
  }
  const a = parse(left)
  const b = parse(right)
  for (let index = 0; index < 3; index++) {
    if (a.core[index] !== b.core[index]) return a.core[index] < b.core[index] ? -1 : 1
  }
  if (a.prerelease.length === 0 || b.prerelease.length === 0) {
    return a.prerelease.length === b.prerelease.length ? 0 : a.prerelease.length === 0 ? 1 : -1
  }
  const limit = Math.max(a.prerelease.length, b.prerelease.length)
  for (let index = 0; index < limit; index++) {
    const l = a.prerelease[index]
    const r = b.prerelease[index]
    if (l === undefined || r === undefined) return l === r ? 0 : l === undefined ? -1 : 1
    if (l === r) continue
    const lNumeric = /^[0-9]+$/.test(l)
    const rNumeric = /^[0-9]+$/.test(r)
    if (lNumeric && rNumeric) return Number(l) < Number(r) ? -1 : 1
    if (lNumeric !== rNumeric) return lNumeric ? -1 : 1
    return l < r ? -1 : 1
  }
  return 0
}

function exactPositiveInteger(value, label) {
  if (!/^[1-9][0-9]{0,15}$/.test(String(value ?? ''))) fail(`${label} must be a positive integer`)
  const parsed = Number(value)
  if (!Number.isSafeInteger(parsed)) fail(`${label} exceeds the safe range`)
  return parsed
}

function sha256(data) {
  return crypto.createHash('sha256').update(data).digest('hex')
}

function usage() {
  console.error('Usage: node scripts/release-manifest.mjs --version <semver> --sequence <n> --channel <main|beta> --issued-at <unix> --key-id <id> [--expires-at <unix>] [--assets-dir <dir>] [--components-dir <dir>] [--out <file>]')
  process.exit(2)
}

function fail(message) {
  console.error(`[release-manifest] ERROR: ${message}`)
  process.exit(1)
}
