#!/usr/bin/env node

import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'
import { canonical, sha256 } from './openwrt-package-source-fingerprint.mjs'
import { stageManifestSchema } from './openwrt-stage-manifest.mjs'
import { packageHostToolProofSchema } from './openwrt-package-host-tools.mjs'
import { assertUnixLF } from './openwrt-unix-text-assets.mjs'

export const apkMetadataProofSchema = 'solovey-ui/openwrt-apk-metadata-proof/v1'

const expectedMetadata = Object.freeze({
  name: 'solovey-ui',
  license: 'GPL-3.0-only',
  origin: 'feeds/base/solovey-ui',
  maintainer: 'MalenkiySolovey',
  url: 'https://github.com/MalenkiySolovey/solovey-ui',
  description: 'Solovey UI management panel packaged for the OpenWrt 25.12 apk backend. The package owns its fixed procd topology and exact logical sysupgrade preservation integration. Runtime self-replacement remains package-managed.',
  dependencies: ['dropbear', 'libc', 'logd', 'nftables', 'procd', 'ubus', 'uci'],
  provides: ['solovey-ui-any'],
})

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    runCLI()
  } catch (error) {
    console.error(`[openwrt-apk-metadata] ERROR: ${error instanceof Error ? error.message : String(error)}`)
    process.exit(1)
  }
}

function runCLI() {
  const args = parseArgs(process.argv.slice(2))
  const sourceRoot = path.resolve(required(args, 'source-root'))
  const version = fs.readFileSync(path.join(sourceRoot, 'config/identity/version'), 'utf8').trim()
  if (!/^[0-9]+\.[0-9]+\.[0-9]+$/.test(version)) throw new Error('source package version is invalid')
  const release = readPackageRelease(sourceRoot)
  const stageManifest = JSON.parse(fs.readFileSync(path.resolve(required(args, 'stage-manifest')), 'utf8'))
  const packageToolProof = JSON.parse(fs.readFileSync(path.resolve(required(args, 'package-tool-proof')), 'utf8'))
  if (stageManifest?.schema !== stageManifestSchema) throw new Error('stage manifest is unavailable or unsupported')
  if (packageToolProof?.schema !== packageHostToolProofSchema) throw new Error('package host-tool proof is unavailable or unsupported')
  const packageArchitecture = stageManifest?.target?.packageArchitecture
  if (!/^[a-z0-9_]+$/.test(packageArchitecture ?? '')) throw new Error('stage manifest package architecture is invalid')
  const proof = createApkMetadataProof(
    fs.readFileSync(path.resolve(required(args, 'adbdump')), 'utf8'),
    {
      version,
      sourceFingerprint: stageManifest.sourceFingerprint,
      stageIdentity: stageManifest.stageIdentity,
      packageHostToolIdentity: packageToolProof.packageHostToolIdentity,
      apkSha256: required(args, 'apk-sha256'),
      packageArchitecture,
      release,
    },
  )
  writeJSON(path.resolve(required(args, 'out')), proof)
  console.log(`[openwrt-apk-metadata] verified ${proof.metadataIdentity}`)
}

export function createApkMetadataProof(adbDump, context) {
  if (!context || !/^[0-9]+\.[0-9]+\.[0-9]+$/.test(context.version ?? '')) throw new Error('expected package version is invalid')
  for (const [name, value] of [
    ['source fingerprint', context.sourceFingerprint],
    ['stage identity', context.stageIdentity],
    ['package host-tool identity', context.packageHostToolIdentity],
    ['APK SHA-256', context.apkSha256],
  ]) {
    if (!/^[0-9a-f]{64}$/.test(value ?? '')) throw new Error(`${name} is invalid`)
  }
  const metadata = parseApkMetadata(adbDump)
  const lifecycleScripts = proveApkLifecycleScripts(adbDump)
  const packageArchitecture = context.packageArchitecture ?? 'x86_64'
  if (!/^[a-z0-9_]+$/.test(packageArchitecture)) throw new Error('expected package architecture is invalid')
  const release = parseRelease(context.release)
  verifyExpectedMetadata(metadata, context.version, packageArchitecture, release)
  const metadataIdentity = sha256(canonical({ schema: apkMetadataProofSchema, metadata }))
  const proofMaterial = {
    schema: apkMetadataProofSchema,
    sourceFingerprint: context.sourceFingerprint,
    stageIdentity: context.stageIdentity,
    packageHostToolIdentity: context.packageHostToolIdentity,
    apkSha256: context.apkSha256,
    metadataIdentity,
    lifecycleScripts,
  }
  return {
    schema: apkMetadataProofSchema,
    generatedBy: 'scripts/openwrt-apk-metadata.mjs',
    sourceFingerprint: context.sourceFingerprint,
    stageIdentity: context.stageIdentity,
    packageHostToolIdentity: context.packageHostToolIdentity,
    apkSha256: context.apkSha256,
    metadata,
    metadataIdentity,
    lifecycleScripts,
    proofIdentity: sha256(canonical(proofMaterial)),
  }
}

export function proveApkLifecycleScripts(adbDump) {
  if (typeof adbDump !== 'string') throw new Error('APK adbdump input is unavailable')
  assertUnixLF(Buffer.from(adbDump, 'utf8'), 'APK adbdump and lifecycle scripts')
  const lines = adbDump.split('\n')
  const scriptsIndex = lines.indexOf('scripts:')
  if (scriptsIndex < 0) throw new Error('APK adbdump omits lifecycle scripts')
  const scripts = []
  for (let index = scriptsIndex + 1; index < lines.length;) {
    const header = /^  ([a-z][a-z-]*): \|$/.exec(lines[index])
    if (!header) break
    const name = header[1]
    const body = []
    index += 1
    while (index < lines.length && lines[index].startsWith('    ')) {
      body.push(lines[index].slice(4))
      index += 1
    }
    const bytes = Buffer.from(`${body.join('\n')}\n`, 'utf8')
    assertUnixLF(bytes, `APK lifecycle script ${name}`)
    scripts.push({ name, size: bytes.length, sha256: sha256(bytes), lineEndings: 'LF_ONLY', firstLineTerminatorHex: '0a' })
  }
  const expected = ['post-install', 'post-upgrade', 'pre-deinstall', 'pre-install', 'pre-upgrade']
  const actual = scripts.map(script => script.name).sort()
  if (canonical(actual) !== canonical(expected)) throw new Error(`APK lifecycle script set mismatch: ${actual.join(', ')}`)
  return { scripts: scripts.sort((left, right) => left.name.localeCompare(right.name)), scriptCount: scripts.length }
}

export function parseApkMetadata(adbDump) {
  if (typeof adbDump !== 'string') throw new Error('APK adbdump input is unavailable')
  const lines = adbDump.replaceAll('\r\n', '\n').split('\n')
  const infoIndex = lines.indexOf('info:')
  const pathsIndex = lines.findIndex((line, index) => index > infoIndex && /^paths: # [0-9]+ items$/.test(line))
  if (infoIndex < 0 || pathsIndex < 0) throw new Error('APK adbdump omits info or paths metadata')
  const values = new Map()
  for (let index = infoIndex + 1; index < pathsIndex;) {
    const match = /^  ([a-z][a-z0-9-]*):(?: (.*))?$/.exec(lines[index])
    if (!match) throw new Error(`APK info contains an unsupported line: ${lines[index]}`)
    const [, key, raw = ''] = match
    if (values.has(key)) throw new Error(`APK info contains duplicate field: ${key}`)
    if (raw === '|') {
      const parts = []
      index += 1
      while (index < pathsIndex && lines[index].startsWith('    ') && !lines[index].startsWith('    - ')) {
        parts.push(lines[index].slice(4).trim())
        index += 1
      }
      values.set(key, parts.join(' ').trim())
      continue
    }
    if (/^# [0-9]+ items$/.test(raw)) {
      const items = []
      index += 1
      while (index < pathsIndex && lines[index].startsWith('    - ')) {
        items.push(lines[index].slice(6))
        index += 1
      }
      values.set(key, items)
      continue
    }
    values.set(key, raw)
    index += 1
  }
  const expectedFields = ['arch', 'depends', 'description', 'hashes', 'installed-size', 'license', 'maintainer', 'name', 'origin', 'provides', 'url', 'version']
  const actualFields = [...values.keys()].sort(bytewise)
  if (canonical(actualFields) !== canonical(expectedFields.sort(bytewise))) {
    throw new Error(`APK info field set is not canonical: ${actualFields.join(', ')}`)
  }
  const installedSize = parseInteger(values.get('installed-size'), 'installed-size')
  let pathFileSizeTotal = 0
  let pathFileCount = 0
  for (const line of lines.slice(pathsIndex + 1)) {
    const match = /^\s+size: ([0-9]+)$/.exec(line)
    if (!match) continue
    pathFileSizeTotal += parseInteger(match[1], 'path file size')
    pathFileCount += 1
    if (!Number.isSafeInteger(pathFileSizeTotal)) throw new Error('APK path file-size total is unsafe')
  }
  if (pathFileCount === 0 || installedSize !== pathFileSizeTotal) {
    throw new Error(`APK installed-size does not equal the ADB path file-size sum: ${installedSize} != ${pathFileSizeTotal}`)
  }
  const fullVersion = values.get('version')
  const versionMatch = /^(.*)-r([1-9][0-9]*)$/.exec(fullVersion)
  if (!versionMatch) throw new Error('APK version/release field is invalid')
  const contentHash = values.get('hashes')
  if (!/^[0-9a-f]{40}$/.test(contentHash)) throw new Error('APK content hash field is invalid')
  const dependencies = values.get('depends')
  const provides = values.get('provides')
  if (!Array.isArray(dependencies) || !Array.isArray(provides)) throw new Error('APK dependency/provides metadata is invalid')
  return {
    name: values.get('name'),
    version: versionMatch[1],
    release: Number(versionMatch[2]),
    fullVersion,
    architecture: values.get('arch'),
    license: values.get('license'),
    origin: values.get('origin'),
    maintainer: values.get('maintainer'),
    url: values.get('url'),
    description: values.get('description'),
    dependencies,
    provides,
    installedSize,
    installedSizeModel: 'sum-of-adb-path-file-sizes',
    pathFileCount,
    contentHash,
  }
}

function verifyExpectedMetadata(actual, version, packageArchitecture, release) {
  const expected = { ...expectedMetadata, architecture: packageArchitecture, version, release, fullVersion: `${version}-r${release}` }
  for (const field of ['name', 'version', 'release', 'fullVersion', 'architecture', 'license', 'origin', 'maintainer', 'url', 'description']) {
    if (actual[field] !== expected[field]) throw new Error(`APK package metadata mismatch: ${field}`)
  }
  if (canonical(actual.dependencies) !== canonical(expected.dependencies)) throw new Error('APK package metadata mismatch: dependencies')
  if (canonical(actual.provides) !== canonical(expected.provides)) throw new Error('APK package metadata mismatch: provides')
}

function readPackageRelease(sourceRoot) {
  const recipePath = path.join(sourceRoot, 'deploy/openwrt/package/solovey-ui/Makefile')
  const recipe = fs.readFileSync(recipePath, 'utf8')
  const match = /^PKG_RELEASE:=([1-9][0-9]*)$/m.exec(recipe)
  if (!match) throw new Error('OpenWrt package recipe release is unavailable or invalid')
  return parseRelease(match[1])
}

function parseRelease(value) {
  const text = String(value ?? '')
  if (!/^[1-9][0-9]*$/.test(text)) throw new Error('expected package release is invalid')
  const release = Number(text)
  if (!Number.isSafeInteger(release)) throw new Error('expected package release is unsafe')
  return release
}

function parseInteger(value, label) {
  if (!/^(?:0|[1-9][0-9]*)$/.test(value ?? '')) throw new Error(`APK ${label} is invalid`)
  const number = Number(value)
  if (!Number.isSafeInteger(number)) throw new Error(`APK ${label} is unsafe`)
  return number
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

function bytewise(left, right) {
  return Buffer.from(left).compare(Buffer.from(right))
}

function writeJSON(file, value) {
  fs.writeFileSync(file, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o644 })
  fs.chmodSync(file, 0o644)
}

function usage() {
  throw new Error('Usage: node scripts/openwrt-apk-metadata.mjs --adbdump <text> --source-root <source> --stage-manifest <json> --package-tool-proof <json> --apk-sha256 <hex> --out <json>')
}
