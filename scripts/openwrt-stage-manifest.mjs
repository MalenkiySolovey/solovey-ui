#!/usr/bin/env node

import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'
import {
  canonical,
  sha256,
  verifySourceFingerprint,
} from './openwrt-package-source-fingerprint.mjs'
import { proveInstalledUnixTextAssets } from './openwrt-unix-text-assets.mjs'

export const stageManifestSchema = 'solovey-ui/openwrt-target-stage/v2'

export const executablePaths = [
  'usr/lib/solovey-ui/solovey-ui',
  'usr/lib/solovey-ui/solovey-privileged-broker',
  'usr/lib/solovey-ui/solovey-ssh-proof',
  'usr/lib/solovey-ui/solovey-evidence',
  'usr/lib/solovey-ui/solovey-broker-readiness',
  'usr/lib/solovey-ui/solovey-openwrt-preservation',
  'usr/lib/solovey-ui/solovey-openwrt-owner-manifest',
  'usr/lib/solovey-ui/solovey-openwrt-broker-manifest',
  'usr/lib/solovey-ui/solovey-openwrt-durability',
  'usr/lib/solovey-ui/solovey-openwrt-lifecycle',
]

const fixedPayloadPaths = [
  ...executablePaths,
  'usr/lib/solovey-ui/BUILD_INFO.txt',
  'usr/lib/solovey-ui/SOURCE_FINGERPRINT.json',
  'usr/lib/solovey-ui/GO_TOOLCHAIN_AUTHORITY.json',
  'usr/lib/solovey-ui/GO_DEPENDENCY_AUTHORITY.json',
  'usr/lib/solovey-ui/solovey-openwrt-prepare',
  'usr/lib/solovey-ui/components/installed.json',
  'usr/lib/solovey-ui/components/runtime-closure.json',
  'etc/init.d/solovey-ui',
  'lib/upgrade/solovey-ui.sh',
  'lib/upgrade/keep.d/solovey-ui',
  'usr/share/licenses/solovey-ui/LICENSE',
]

if (isMain()) {
  try {
    runCLI()
  } catch (error) {
    fail(error instanceof Error ? error.message : String(error))
  }
}

function runCLI() {
  const [command, ...values] = process.argv.slice(2)
  if (!['create', 'verify'].includes(command)) usage()
  const args = parseArgs(values)
  const stageRoot = path.resolve(required(args, 'stage'))
  const sourceRoot = args.get('source-root') ? path.resolve(args.get('source-root')) : undefined

  if (command === 'create') {
    const manifest = createStageManifest(stageRoot, sourceRoot)
    writeJSON(path.join(stageRoot, 'STAGE_MANIFEST.json'), manifest)
    console.log(`[openwrt-stage-manifest] wrote ${manifest.stageIdentity}`)
    return
  }

  const manifestPath = path.join(stageRoot, 'STAGE_MANIFEST.json')
  const manifest = readJSON(manifestPath)
  verifyStageManifest(manifest, stageRoot, sourceRoot)
  const canonicalBytes = `${JSON.stringify(manifest, null, 2)}\n`
  if (!fs.readFileSync(manifestPath, 'utf8').split('\r\n').join('\n').endsWith('\n') ||
      fs.readFileSync(manifestPath, 'utf8').split('\r\n').join('\n') !== canonicalBytes) {
    throw new Error('stage manifest encoding is not canonical')
  }
  console.log(`[openwrt-stage-manifest] verified ${manifest.stageIdentity}`)
}

export function createStageManifest(stageRoot, sourceRoot) {
  stageRoot = path.resolve(stageRoot)
  const payloadRoot = path.join(stageRoot, 'payload')
  assertRootEnvelope(stageRoot)
  const sourceFingerprintDocument = readJSON(path.join(payloadRoot, 'usr/lib/solovey-ui/SOURCE_FINGERPRINT.json'))
  verifySourceFingerprint(sourceFingerprintDocument, sourceRoot)
  const expectedElfArchitecture = expectedELFArchitecture(sourceFingerprintDocument)
  const expectedEntries = expectedPayloadEntries(payloadRoot)
  const actualEntries = enumeratePayload(payloadRoot)
  assertSameEntries(expectedEntries, actualEntries)
  const entries = actualEntries.map(entry => payloadRecord(payloadRoot, entry.path, expectedElfArchitecture))
  const files = entries.filter(entry => entry.type === 'file')
  const directories = entries.filter(entry => entry.type === 'directory')
  const payloadFileDigest = sha256(canonical(files))
  const payloadTreeDigest = sha256(canonical(entries))
  const unixTextAssets = proveInstalledUnixTextAssets(payloadRoot)
  const unixTextAssetDigest = sha256(canonical(unixTextAssets))
  assertGoAuthorityBinding(payloadRoot, sourceFingerprintDocument)
  const buildInfoPath = path.join(payloadRoot, 'usr/lib/solovey-ui/BUILD_INFO.txt')
  const buildInfo = parseBuildInfo(fs.readFileSync(buildInfoPath, 'utf8'))
  assertBuildInfoBinding(buildInfo, sourceFingerprintDocument)
  const target = sourceFingerprintDocument.build.facts.target
  const identityMaterial = {
    schema: stageManifestSchema,
    sourceFingerprint: sourceFingerprintDocument.sourceFingerprint,
    generatedOutputDigest: sourceFingerprintDocument.generated.outputDigest,
    buildIdentity: sourceFingerprintDocument.build.identity,
    buildInfoSha256: sha256(fs.readFileSync(buildInfoPath)),
    payloadTreeDigest,
    unixTextAssetDigest,
    target,
  }
  return {
    schema: stageManifestSchema,
    generatedBy: 'scripts/openwrt-stage-manifest.mjs',
    sourceFingerprint: sourceFingerprintDocument.sourceFingerprint,
    generatedOutputDigest: sourceFingerprintDocument.generated.outputDigest,
    buildIdentity: sourceFingerprintDocument.build.identity,
    buildInfoSha256: identityMaterial.buildInfoSha256,
    target,
    payload: {
      entries,
      entryCount: entries.length,
      fileCount: files.length,
      directoryCount: directories.length,
      fileDigest: payloadFileDigest,
      treeDigest: payloadTreeDigest,
    },
    unixTextAssets: {
      assets: unixTextAssets,
      assetCount: unixTextAssets.length,
      digest: unixTextAssetDigest,
    },
    stageIdentity: sha256(canonical(identityMaterial)),
  }
}

export function verifyStageManifest(manifest, stageRoot, sourceRoot) {
  stageRoot = path.resolve(stageRoot)
  if (manifest?.schema !== stageManifestSchema) throw new Error(`unsupported stage manifest schema: ${manifest?.schema ?? 'missing'}`)
  assertRootEnvelope(stageRoot, true)
  const payloadRoot = path.join(stageRoot, 'payload')
  const fingerprint = readJSON(path.join(payloadRoot, 'usr/lib/solovey-ui/SOURCE_FINGERPRINT.json'))
  verifySourceFingerprint(fingerprint, sourceRoot)
  const expectedElfArchitecture = expectedELFArchitecture(fingerprint)
  const expectedEntries = expectedPayloadEntries(payloadRoot)
  const actualEntries = enumeratePayload(payloadRoot)
  assertSameEntries(expectedEntries, actualEntries)
  const records = validateRecords(manifest.payload?.entries)
  const files = records.filter(record => record.type === 'file')
  const directories = records.filter(record => record.type === 'directory')
  if (manifest.payload.entryCount !== records.length || manifest.payload.fileCount !== files.length || manifest.payload.directoryCount !== directories.length) {
    throw new Error('stage payload entry count mismatch')
  }
  if (canonical(records.map(record => ({ path: record.path, type: record.type }))) !== canonical(actualEntries)) {
    throw new Error('stage manifest path/type set mismatch')
  }
  for (const record of records) verifyPayloadRecord(payloadRoot, record, expectedElfArchitecture)
  const payloadFileDigest = sha256(canonical(files))
  const payloadTreeDigest = sha256(canonical(records))
  if (manifest.payload.fileDigest !== payloadFileDigest) throw new Error('stage payload file digest mismatch')
  if (manifest.payload.treeDigest !== payloadTreeDigest) throw new Error('stage payload tree digest mismatch')
  const unixTextAssets = proveInstalledUnixTextAssets(payloadRoot)
  const unixTextAssetDigest = sha256(canonical(unixTextAssets))
  if (manifest.unixTextAssets?.assetCount !== unixTextAssets.length ||
      manifest.unixTextAssets?.digest !== unixTextAssetDigest ||
      canonical(manifest.unixTextAssets?.assets) !== canonical(unixTextAssets)) {
    throw new Error('stage Unix text asset proof mismatch')
  }

  assertGoAuthorityBinding(payloadRoot, fingerprint)
  if (manifest.sourceFingerprint !== fingerprint.sourceFingerprint) throw new Error('stage/source fingerprint mismatch')
  if (manifest.generatedOutputDigest !== fingerprint.generated.outputDigest) throw new Error('stage/generated output mismatch')
  if (manifest.buildIdentity !== fingerprint.build.identity) throw new Error('stage/build identity mismatch')
  if (canonical(manifest.target) !== canonical(fingerprint.build.facts.target)) throw new Error('stage target mismatch')

  const buildInfoPath = path.join(payloadRoot, 'usr/lib/solovey-ui/BUILD_INFO.txt')
  const buildInfoBytes = fs.readFileSync(buildInfoPath)
  if (manifest.buildInfoSha256 !== sha256(buildInfoBytes)) throw new Error('stage BUILD_INFO digest mismatch')
  assertBuildInfoBinding(parseBuildInfo(buildInfoBytes.toString('utf8')), fingerprint)
  const stageIdentity = sha256(canonical({
    schema: stageManifestSchema,
    sourceFingerprint: manifest.sourceFingerprint,
    generatedOutputDigest: manifest.generatedOutputDigest,
    buildIdentity: manifest.buildIdentity,
    buildInfoSha256: manifest.buildInfoSha256,
    payloadTreeDigest,
    unixTextAssetDigest,
    target: manifest.target,
  }))
  if (manifest.stageIdentity !== stageIdentity) throw new Error('stage identity mismatch')
  return true
}

function assertRootEnvelope(stageRoot, allowManifest = false) {
  const entries = fs.readdirSync(stageRoot, { withFileTypes: true }).sort((a, b) => bytewise(a.name, b.name))
  const allowed = allowManifest ? ['STAGE_MANIFEST.json', 'payload'] : ['payload']
  const actual = entries.map(entry => entry.name)
  if (canonical(actual) !== canonical(allowed.sort(bytewise))) throw new Error(`stage envelope is not closed: ${actual.join(', ')}`)
  for (const entry of entries) {
    if (entry.isSymbolicLink()) throw new Error(`stage envelope contains symlink: ${entry.name}`)
    if (entry.name === 'payload' && !entry.isDirectory()) throw new Error('stage payload has wrong type')
    if (entry.name === 'STAGE_MANIFEST.json' && !entry.isFile()) throw new Error('stage manifest has wrong type')
  }
}

function expectedPayloadEntries(payloadRoot) {
  const installedPath = path.join(payloadRoot, 'usr/lib/solovey-ui/components/installed.json')
  if (!fs.existsSync(installedPath)) throw new Error('component installation metadata is missing')
  const installed = readJSON(installedPath)
  if (installed?.version !== 1 || installed.profile !== 'full' || installed.binary !== 'full' || !Array.isArray(installed.components)) {
    throw new Error('component installation metadata is invalid or not the full profile')
  }
  const componentIDs = []
  const paths = [...fixedPayloadPaths]
  for (const item of installed.components) {
    if (!item || !/^[a-z0-9-]+$/.test(item.id ?? '') || item.delivery !== 'in-process' || item.installed !== true) {
      throw new Error('component installation metadata contains an invalid component')
    }
    if (componentIDs.includes(item.id)) throw new Error(`duplicate installed component: ${item.id}`)
    componentIDs.push(item.id)
    const prefix = `usr/lib/solovey-ui/components/${item.id}`
    const componentPath = path.join(payloadRoot, ...`${prefix}/component.json`.split('/'))
    const assetsPath = path.join(payloadRoot, ...`${prefix}/frontend/assets.json`.split('/'))
    const component = readJSON(componentPath)
    const assets = readJSON(assetsPath)
    if (component.id !== item.id || component.delivery !== item.delivery) throw new Error(`stale component manifest: ${item.id}`)
    if (assets?.schemaVersion !== 1 || assets.component !== item.id || !Array.isArray(assets.entries) || !Array.isArray(assets.files) || assets.files.length === 0) {
      throw new Error(`stale component frontend metadata: ${item.id}`)
    }
    paths.push(`${prefix}/component.json`, `${prefix}/frontend/assets.json`)
    const seenAssets = new Set()
    for (const asset of assets.files) {
      if (typeof asset !== 'string' || !/^assets\/[A-Za-z0-9_.\/-]+$/.test(asset) || asset.includes('..')) {
        throw new Error(`component ${item.id} declares invalid frontend asset`)
      }
      const stagedAsset = `${prefix}/frontend/${asset}`
      if (seenAssets.has(stagedAsset)) throw new Error(`component ${item.id} declares duplicate frontend asset`)
      seenAssets.add(stagedAsset)
      paths.push(stagedAsset)
    }
  }
  const sortedIDs = [...componentIDs].sort(bytewise)
  if (canonical(componentIDs) !== canonical(sortedIDs) || componentIDs.length === 0) throw new Error('installed component list is not non-empty and bytewise sorted')
  const entries = new Map(paths.map(file => [file, 'file']))
  for (const file of paths) {
    let directory = path.posix.dirname(file)
    while (directory !== '.') {
      const existing = entries.get(directory)
      if (existing && existing !== 'directory') throw new Error(`payload path is both file and directory: ${directory}`)
      entries.set(directory, 'directory')
      directory = path.posix.dirname(directory)
    }
  }
  return [...entries]
    .map(([entryPath, type]) => ({ path: entryPath, type }))
    .sort((left, right) => bytewise(left.path, right.path))
}

function enumeratePayload(payloadRoot) {
  if (!fs.existsSync(payloadRoot) || !fs.lstatSync(payloadRoot).isDirectory()) throw new Error('stage payload directory is unavailable')
  const entries = []
  walk(payloadRoot, payloadRoot, entries)
  entries.sort((left, right) => bytewise(left.path, right.path))
  const caseFolded = new Set()
  for (const entry of entries) {
    const folded = entry.path.toLowerCase()
    if (caseFolded.has(folded)) throw new Error(`stage payload has a case-colliding path: ${entry.path}`)
    caseFolded.add(folded)
  }
  return entries
}

function walk(directory, root, entries) {
  const directoryEntries = fs.readdirSync(directory, { withFileTypes: true }).sort((a, b) => bytewise(a.name, b.name))
  for (const entry of directoryEntries) {
    const candidate = path.join(directory, entry.name)
    const relative = path.relative(root, candidate).split(path.sep).join('/')
    const stat = fs.lstatSync(candidate)
    if (stat.isSymbolicLink()) throw new Error(`stage payload contains unexpected symlink: ${relative}`)
    if (stat.isDirectory()) {
      entries.push({ path: relative, type: 'directory' })
      walk(candidate, root, entries)
    } else if (stat.isFile()) entries.push({ path: relative, type: 'file' })
    else throw new Error(`stage payload contains unsupported type: ${relative}`)
  }
}

function payloadRecord(root, entryPath, expectedElfArchitecture) {
  const candidate = path.join(root, ...entryPath.split('/'))
  const stat = fs.lstatSync(candidate)
  if (stat.isSymbolicLink()) throw new Error(`stage payload contains unexpected symlink: ${entryPath}`)
  const mode = (stat.mode & 0o777).toString(8).padStart(4, '0')
  if (stat.isDirectory()) {
    if (mode !== '0755') throw new Error(`wrong directory mode for ${entryPath}: expected 0755, got ${mode}`)
    return { path: entryPath, type: 'directory', mode, kind: 'directory' }
  }
  if (!stat.isFile()) throw new Error(`stage payload contains unsupported type: ${entryPath}`)
  const expectedMode = executablePaths.includes(entryPath) || entryPath === 'etc/init.d/solovey-ui' || entryPath === 'lib/upgrade/solovey-ui.sh' || entryPath === 'usr/lib/solovey-ui/solovey-openwrt-prepare' ? '0755' : '0644'
  if (mode !== expectedMode) throw new Error(`wrong file mode for ${entryPath}: expected ${expectedMode}, got ${mode}`)
  const architecture = executablePaths.includes(entryPath) ? readELFArchitecture(candidate, expectedElfArchitecture) : undefined
  return {
    path: entryPath,
    type: 'file',
    sha256: sha256(fs.readFileSync(candidate)),
    size: stat.size,
    mode,
    kind: payloadKind(entryPath),
    ...(architecture ? { architecture } : {}),
  }
}

function validateRecords(records) {
  if (!Array.isArray(records)) throw new Error('stage payload records are unavailable')
  let previous = ''
  const seen = new Set()
  const caseFolded = new Map()
  for (const record of records) {
    if (!record || typeof record.path !== 'string' || !isSafeRelative(record.path)) throw new Error('stage payload record has invalid path')
    if (seen.has(record.path) || (previous && bytewise(previous, record.path) >= 0)) throw new Error('stage payload records are not uniquely sorted')
    const folded = record.path.toLocaleLowerCase('en-US')
    if (caseFolded.has(folded)) throw new Error(`stage payload has a case-colliding path: ${record.path}`)
    caseFolded.set(folded, record.path)
    if (!/^0[0-7]{3}$/.test(record.mode ?? '') || typeof record.kind !== 'string') throw new Error(`stage payload contract is invalid: ${record.path}`)
    if (record.type === 'directory') {
      if (canonical(Object.keys(record).sort(bytewise)) !== canonical(['kind', 'mode', 'path', 'type'])) {
        throw new Error(`stage directory record is invalid: ${record.path}`)
      }
    } else if (record.type === 'file') {
      if (!/^[0-9a-f]{64}$/.test(record.sha256 ?? '') || !Number.isSafeInteger(record.size) || record.size < 0) {
        throw new Error(`stage file record is invalid: ${record.path}`)
      }
      const expectedKeys = executablePaths.includes(record.path)
        ? ['architecture', 'kind', 'mode', 'path', 'sha256', 'size', 'type']
        : ['kind', 'mode', 'path', 'sha256', 'size', 'type']
      if (canonical(Object.keys(record).sort(bytewise)) !== canonical(expectedKeys)) throw new Error(`stage file record is invalid: ${record.path}`)
    } else {
      throw new Error(`stage payload record has unsupported type: ${record.path}`)
    }
    seen.add(record.path)
    previous = record.path
  }
  return records
}

function verifyPayloadRecord(root, record, expectedElfArchitecture) {
  const candidate = path.join(root, ...record.path.split('/'))
  try {
    fs.lstatSync(candidate)
  } catch (error) {
    if (error?.code === 'ENOENT') throw new Error(`stage payload is missing: ${record.path}`)
    throw error
  }
  const actual = payloadRecord(root, record.path, expectedElfArchitecture)
  if (canonical(actual) !== canonical(record)) throw new Error(`stage payload record mismatch: ${record.path}`)
}

function assertSameEntries(expected, actual) {
  if (canonical(expected) === canonical(actual)) return
  const key = entry => `${entry.type}:${entry.path}`
  const expectedSet = new Set(expected.map(key))
  const actualSet = new Set(actual.map(key))
  const missing = expected.filter(entry => !actualSet.has(key(entry))).map(key)
  const extra = actual.filter(entry => !expectedSet.has(key(entry))).map(key)
  throw new Error(`stage payload path/type set mismatch; missing=[${missing.join(', ')}] extra=[${extra.join(', ')}]`)
}

function expectedELFArchitecture(fingerprint) {
  const goarch = fingerprint?.build?.facts?.go?.goarch
  if (goarch === 'amd64') return { machine: 62, name: 'elf64-x86-64' }
  if (goarch === 'arm64') return { machine: 183, name: 'elf64-aarch64' }
  throw new Error(`unsupported packaged ELF Go architecture: ${goarch ?? 'missing'}`)
}

function readELFArchitecture(file, expected) {
  const header = fs.readFileSync(file).subarray(0, 20)
  if (header.length < 20 || header[0] !== 0x7f || header.toString('ascii', 1, 4) !== 'ELF') throw new Error(`required executable is not ELF: ${path.basename(file)}`)
  if (header[4] !== 2 || header[5] !== 1) throw new Error(`required executable is not 64-bit little-endian ELF: ${path.basename(file)}`)
  if (header.readUInt16LE(18) !== expected.machine) throw new Error(`wrong ELF architecture: ${path.basename(file)}`)
  return expected.name
}

function payloadKind(file) {
  if (executablePaths.includes(file)) return 'executable'
  if (file.endsWith('/component.json')) return 'component-manifest'
  if (file.endsWith('/frontend/assets.json')) return 'component-asset-manifest'
  if (file.includes('/components/') && file.includes('/frontend/assets/')) return 'component-frontend-asset'
  if (file.endsWith('/installed.json')) return 'component-installation-metadata'
  if (file.endsWith('/SOURCE_FINGERPRINT.json')) return 'source-provenance'
  if (file.endsWith('/GO_TOOLCHAIN_AUTHORITY.json')) return 'go-toolchain-provenance'
  if (file.endsWith('/GO_DEPENDENCY_AUTHORITY.json')) return 'go-dependency-provenance'
  if (file.endsWith('/BUILD_INFO.txt')) return 'build-metadata'
  if (file === 'etc/init.d/solovey-ui') return 'openwrt-init'
  if (file.startsWith('lib/upgrade/')) return 'openwrt-upgrade'
  if (file.endsWith('/LICENSE')) return 'license'
  return 'data'
}

function parseBuildInfo(value) {
  const result = {}
  for (const line of value.split(/\r?\n/)) {
    if (line === '') continue
    const separator = line.indexOf('=')
    if (separator <= 0 || Object.hasOwn(result, line.slice(0, separator))) throw new Error('BUILD_INFO is not canonical key/value metadata')
    result[line.slice(0, separator)] = line.slice(separator + 1)
  }
  return result
}

function assertBuildInfoBinding(info, fingerprint) {
  if (info.app !== 'solovey-ui' || info.profile !== 'full') throw new Error('BUILD_INFO product/profile mismatch')
  if (info.source_fingerprint !== fingerprint.sourceFingerprint) throw new Error('BUILD_INFO source fingerprint mismatch')
  if (info.build_identity !== fingerprint.build.identity) throw new Error('BUILD_INFO build identity mismatch')
  if (info.go_toolchain_identity !== fingerprint.build.facts.go?.toolchain?.identity) throw new Error('BUILD_INFO Go toolchain identity mismatch')
  if (info.go_dependency_identity !== fingerprint.build.facts.go?.dependencies?.identity) throw new Error('BUILD_INFO Go dependency identity mismatch')
  if (fingerprint.build.facts.revision && info.commit !== fingerprint.build.facts.revision.commit) throw new Error('BUILD_INFO source revision mismatch')
  const target = fingerprint.build.facts.target
  if (info.target !== `${target.release}-${target.target}-${target.subtarget}-${target.packageArchitecture}`) throw new Error('BUILD_INFO target mismatch')
}

function assertGoAuthorityBinding(payloadRoot, fingerprint) {
  const toolchain = readJSON(path.join(payloadRoot, 'usr/lib/solovey-ui/GO_TOOLCHAIN_AUTHORITY.json'))
  const dependencies = readJSON(path.join(payloadRoot, 'usr/lib/solovey-ui/GO_DEPENDENCY_AUTHORITY.json'))
  const goFacts = fingerprint.build?.facts?.go
  if (toolchain.schema !== 'solovey-ui/go-toolchain-authority/v1' || dependencies.schema !== 'solovey-ui/go-dependency-authority/v1') {
    throw new Error('Go authority document schema is invalid')
  }
  if (toolchain.toolchainIdentity !== goFacts?.toolchain?.identity || toolchain.tree?.treeDigest !== goFacts?.toolchain?.treeDigest ||
      toolchain.archiveSha256 !== goFacts?.toolchain?.archiveSha256) throw new Error('Go toolchain authority differs from build facts')
  if (dependencies.dependencyIdentity !== goFacts?.dependencies?.identity || dependencies.modules?.moduleGraphDigest !== goFacts?.dependencies?.moduleGraphDigest ||
      dependencies.source?.goMod?.sha256 !== goFacts?.dependencies?.goModSha256 || dependencies.source?.goSum?.sha256 !== goFacts?.dependencies?.goSumSha256) {
    throw new Error('Go dependency authority differs from build facts')
  }
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

function readJSON(file) {
  return JSON.parse(fs.readFileSync(file, 'utf8'))
}

function writeJSON(file, value) {
  fs.writeFileSync(file, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o644 })
  fs.chmodSync(file, 0o644)
}

function isSafeRelative(value) {
  if (value === '' || value.startsWith('/') || value.includes('\\') || value.includes('\0')) return false
  const segments = value.split('/')
  return segments.every(segment => segment !== '' && segment !== '.' && segment !== '..') && path.posix.normalize(value) === value
}

function bytewise(left, right) {
  return Buffer.from(left).compare(Buffer.from(right))
}

function isMain() {
  return process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
}

function usage() {
  throw new Error('Usage: node scripts/openwrt-stage-manifest.mjs <create|verify> --stage <stage-root> [--source-root <source-root>]')
}

function fail(message) {
  console.error(`[openwrt-stage-manifest] ERROR: ${message}`)
  process.exit(1)
}
