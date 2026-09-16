#!/usr/bin/env node

import crypto from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'

export const sourceFingerprintSchema = 'solovey-ui/openwrt-package-source-fingerprint/v4'

const productRoots = [
  'api', 'app', 'cmd', 'componenthost', 'componentkit', 'components',
  'config', 'core', 'cronjob', 'database', 'deploy', 'frontend', 'internal',
  'ipmonitor', 'logger', 'middleware', 'network', 'realtime', 'service',
  'sub', 'util', 'web',
]

const topLevelInputs = new Set(['LICENSE', 'go.mod', 'go.sum', 'main.go'])

const sourceHiddenFiles = new Set([
  'frontend/.browserslistrc',
  'frontend/.npmrc',
])

const targetBuildScripts = new Set([
  'scripts/build-linux-target.sh',
  'scripts/check-frontend-profile.mjs',
  'scripts/component-frontend-manifest.mjs',
  'scripts/extract-component-frontend.mjs',
  'scripts/frontend-runtime-closure.mjs',
  'scripts/generate-component-imports.mjs',
  'scripts/openwrt-go-authority.mjs',
  'scripts/openwrt-apk-metadata.mjs',
  'scripts/openwrt-apk-rootfs.mjs',
  'scripts/openwrt-package-build.sh',
  'scripts/openwrt-package-host-tools.mjs',
  'scripts/openwrt-package-process.sh',
  'scripts/openwrt-package-source-fingerprint.mjs',
  'scripts/openwrt-persistence-authority.mjs',
  'scripts/openwrt-sdk-archive.mjs',
  'scripts/openwrt-stage-build.sh',
  'scripts/openwrt-stage-manifest.mjs',
  'scripts/openwrt-target-profile.sh',
  'scripts/openwrt-unix-text-assets.mjs',
  'scripts/write-component-installed-metadata.mjs',
])

const generatedGo = new Set([
  'app/components_generated.go',
  'cmd/optional_commands_generated.go',
  'cmd/solovey-privileged-broker/components_generated.go',
  'cmd/solovey-openwrt-preservation/components_generated.go',
  'cmd/solovey-openwrt-lifecycle/components_generated.go',
])

const commands = new Set(['create', 'materialize', 'verify'])

if (isMain()) {
  try {
    await runCLI()
  } catch (error) {
    fail(error instanceof Error ? error.message : String(error))
  }
}

async function runCLI() {
  const [command, ...values] = process.argv.slice(2)
  if (!commands.has(command)) usage()
  const args = parseArgs(values)
  const sourceRoot = path.resolve(required(args, 'source-root'))

  if (command === 'materialize') {
    const out = path.resolve(required(args, 'out'))
    materializeSource(sourceRoot, out)
    console.log(`[openwrt-source-fingerprint] materialized ${enumerateSourcePaths(sourceRoot).length} source input(s)`)
    return
  }

  if (command === 'create') {
    const buildFacts = readJSON(path.resolve(required(args, 'build-facts')))
    const out = path.resolve(required(args, 'out'))
    const provenance = createSourceFingerprint(sourceRoot, buildFacts)
    writeJSON(out, provenance, 0o644)
    console.log(`[openwrt-source-fingerprint] wrote ${relative(sourceRoot, out)} ${provenance.sourceFingerprint}`)
    return
  }

  const manifest = readJSON(path.resolve(required(args, 'manifest')))
  verifySourceFingerprint(manifest, sourceRoot, { verifyGenerated: args.has('generated') })
  console.log(`[openwrt-source-fingerprint] verified ${manifest.sourceFingerprint}`)
}

export function createSourceFingerprint(sourceRoot, buildFacts) {
  sourceRoot = path.resolve(sourceRoot)
  assertPortableBuildFacts(buildFacts)
  const sourceInputs = enumerateSourcePaths(sourceRoot).map(file => fileRecord(sourceRoot, file))
  const generatedOutputs = enumerateGeneratedPaths(sourceRoot).map(file => fileRecord(sourceRoot, file))
  const sourceInputDigest = sha256(canonical(sourceInputs))
  const generatedOutputDigest = sha256(canonical(generatedOutputs))
  const buildIdentity = sha256(canonical(buildFacts))
  const identityMaterial = {
    schema: sourceFingerprintSchema,
    sourceInputDigest,
    generatedOutputDigest,
    buildIdentity,
    target: buildFacts.target,
  }
  return {
    schema: sourceFingerprintSchema,
    generatedBy: 'scripts/openwrt-package-source-fingerprint.mjs',
    source: { inputs: sourceInputs, inputCount: sourceInputs.length, inputDigest: sourceInputDigest },
    generated: { outputs: generatedOutputs, outputCount: generatedOutputs.length, outputDigest: generatedOutputDigest },
    build: { facts: buildFacts, identity: buildIdentity },
    sourceFingerprint: sha256(canonical(identityMaterial)),
  }
}

export function verifySourceFingerprint(manifest, sourceRoot, options = {}) {
  if (manifest?.schema !== sourceFingerprintSchema) {
    throw new Error(`unsupported source fingerprint schema: ${manifest?.schema ?? 'missing'}`)
  }
  assertPortableBuildFacts(manifest.build?.facts)
  const expectedBuildIdentity = sha256(canonical(manifest.build.facts))
  if (manifest.build.identity !== expectedBuildIdentity) throw new Error('build identity mismatch')

  const sourceInputs = validateRecords(manifest.source?.inputs, 'source input')
  if (manifest.source.inputCount !== sourceInputs.length) throw new Error('source input count mismatch')
  const sourceInputDigest = sha256(canonical(sourceInputs))
  if (manifest.source.inputDigest !== sourceInputDigest) throw new Error('source input digest mismatch')

  const generatedOutputs = validateRecords(manifest.generated?.outputs, 'generated output')
  if (manifest.generated.outputCount !== generatedOutputs.length) throw new Error('generated output count mismatch')
  const generatedOutputDigest = sha256(canonical(generatedOutputs))
  if (manifest.generated.outputDigest !== generatedOutputDigest) throw new Error('generated output digest mismatch')

  const expectedFingerprint = sha256(canonical({
    schema: sourceFingerprintSchema,
    sourceInputDigest,
    generatedOutputDigest,
    buildIdentity: expectedBuildIdentity,
    target: manifest.build.facts.target,
  }))
  if (manifest.sourceFingerprint !== expectedFingerprint) throw new Error('source fingerprint mismatch')

  if (sourceRoot) {
    sourceRoot = path.resolve(sourceRoot)
    const actualPaths = enumerateSourcePaths(sourceRoot)
    const expectedPaths = sourceInputs.map(item => item.path)
    if (canonical(actualPaths) !== canonical(expectedPaths)) throw new Error('source input path set mismatch')
    verifyRecordsAgainstRoot(sourceInputs, sourceRoot, 'source input')
    if (options.verifyGenerated) {
      const generatedPaths = enumerateGeneratedPaths(sourceRoot)
      const expectedGeneratedPaths = generatedOutputs.map(item => item.path)
      if (canonical(generatedPaths) !== canonical(expectedGeneratedPaths)) throw new Error('generated output path set mismatch')
      verifyRecordsAgainstRoot(generatedOutputs, sourceRoot, 'generated output')
    }
  }
  return true
}

export function enumerateSourcePaths(sourceRoot) {
  sourceRoot = path.resolve(sourceRoot)
  const files = []
  for (const name of [...topLevelInputs].sort(bytewise)) {
    if (fs.existsSync(path.join(sourceRoot, name))) files.push(name)
  }
  for (const root of productRoots) {
    const directory = path.join(sourceRoot, root)
    if (fs.existsSync(directory)) walk(directory, sourceRoot, files, isSourceInput)
  }
  for (const script of [...targetBuildScripts].sort(bytewise)) {
    if (fs.existsSync(path.join(sourceRoot, script))) files.push(script)
  }
  return [...new Set(files)].sort(bytewise)
}

export function enumerateGeneratedPaths(sourceRoot) {
  sourceRoot = path.resolve(sourceRoot)
  const files = []
  for (const file of [...generatedGo].sort(bytewise)) {
    if (fs.existsSync(path.join(sourceRoot, file))) files.push(file)
  }
  for (const root of ['web/html', '.release/components']) {
    const directory = path.join(sourceRoot, root)
    if (fs.existsSync(directory)) walk(directory, sourceRoot, files, () => true)
  }
  return [...new Set(files)].sort(bytewise)
}

export function materializeSource(sourceRoot, out) {
  sourceRoot = path.resolve(sourceRoot)
  out = path.resolve(out)
  if (fs.existsSync(out)) throw new Error(`materialized source output already exists: ${out}`)
  const paths = enumerateSourcePaths(sourceRoot)
  fs.mkdirSync(out, { recursive: true, mode: 0o755 })
  for (const file of paths) {
    const source = path.join(sourceRoot, ...file.split('/'))
    const target = path.join(out, ...file.split('/'))
    const stat = fs.lstatSync(source)
    if (!stat.isFile() || stat.isSymbolicLink()) throw new Error(`source input must be a regular file: ${file}`)
    fs.mkdirSync(path.dirname(target), { recursive: true, mode: 0o755 })
    fs.copyFileSync(source, target)
    fs.chmodSync(target, 0o644)
  }
}

function isSourceInput(file) {
  if (sourceHiddenFiles.has(file)) return true
  if (generatedGo.has(file)) return false
  if (file.startsWith('web/html/') || file.startsWith('frontend/dist/') || file.startsWith('frontend/node_modules/')) return false
  if (file.includes('/node_modules/') || file.includes('/testdata/') || file.includes('/tests/')) return false
  if (file.startsWith('tests/') || file.startsWith('testsupport/')) return false
  const name = path.posix.basename(file)
  if (name.startsWith('.') || name === 'coverage') return false
  if (name.endsWith('_test.go') || name.endsWith('.test.ts') || name.endsWith('.spec.ts') || name.endsWith('.a11y.ts')) return false
  if (name.endsWith('.md') || name.endsWith('.log') || name.endsWith('.golden')) return false
  return true
}

function walk(directory, sourceRoot, files, include) {
  const entries = fs.readdirSync(directory, { withFileTypes: true }).sort((a, b) => bytewise(a.name, b.name))
  for (const entry of entries) {
    const candidate = path.join(directory, entry.name)
    const relativePath = relative(sourceRoot, candidate)
    if (entry.isSymbolicLink()) throw new Error(`source-owned path must not be a symlink: ${relativePath}`)
    if (entry.isDirectory()) {
      if (isExcludedDirectory(relativePath)) continue
      walk(candidate, sourceRoot, files, include)
    } else if (entry.isFile() && include(relativePath)) {
      files.push(relativePath)
    } else if (!entry.isFile()) {
      throw new Error(`source-owned path has unsupported type: ${relativePath}`)
    }
  }
}

function isExcludedDirectory(file) {
  return file === 'frontend/node_modules' || file === 'frontend/dist' || file === 'web/html' ||
    file.includes('/node_modules') || file.includes('/testdata') || file.includes('/tests') ||
    file.includes('/coverage') || file.includes('/.cache') || file.includes('/.vite')
}

function validateRecords(records, label) {
  if (!Array.isArray(records)) throw new Error(`${label} records are unavailable`)
  const paths = new Set()
  let previous = ''
  for (const record of records) {
    if (!record || typeof record.path !== 'string' || !isSafeRelative(record.path)) throw new Error(`${label} has invalid path`)
    if (!/^[0-9a-f]{64}$/.test(record.sha256 ?? '')) throw new Error(`${label} has invalid digest: ${record.path}`)
    if (!Number.isSafeInteger(record.size) || record.size < 0) throw new Error(`${label} has invalid size: ${record.path}`)
    if (paths.has(record.path) || (previous && bytewise(previous, record.path) >= 0)) throw new Error(`${label} records are not uniquely sorted`)
    paths.add(record.path)
    previous = record.path
  }
  return records
}

function verifyRecordsAgainstRoot(records, root, label) {
  for (const record of records) {
    const candidate = path.join(root, ...record.path.split('/'))
    if (!fs.existsSync(candidate)) throw new Error(`${label} is missing: ${record.path}`)
    const stat = fs.lstatSync(candidate)
    if (!stat.isFile() || stat.isSymbolicLink()) throw new Error(`${label} has wrong type: ${record.path}`)
    if (stat.size !== record.size || sha256(fs.readFileSync(candidate)) !== record.sha256) {
      throw new Error(`${label} content mismatch: ${record.path}`)
    }
  }
}

function assertPortableBuildFacts(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('build facts must be an object')
  if (!value.target || typeof value.target !== 'object') throw new Error('build target facts are unavailable')
  visit(value, (item, key) => {
    if (typeof item !== 'string') return
    if (/((?:^|[\s"'(])[A-Za-z]:[\\/]|\/home\/|\/Users\/|\\Users\\)/i.test(item)) {
      throw new Error(`build facts contain host/process identity at ${key}`)
    }
  })
}

function visit(value, callback, key = '$') {
  callback(value, key)
  if (Array.isArray(value)) value.forEach((item, index) => visit(item, callback, `${key}[${index}]`))
  else if (value && typeof value === 'object') {
    for (const [name, item] of Object.entries(value)) visit(item, callback, `${key}.${name}`)
  }
}

function fileRecord(root, file) {
  const candidate = path.join(root, ...file.split('/'))
  const stat = fs.lstatSync(candidate)
  if (!stat.isFile() || stat.isSymbolicLink()) throw new Error(`identity input must be a regular file: ${file}`)
  return { path: file, sha256: sha256(fs.readFileSync(candidate)), size: stat.size }
}

export function canonical(value) {
  if (Array.isArray(value)) return `[${value.map(canonical).join(',')}]`
  if (value && typeof value === 'object') {
    return `{${Object.keys(value).sort(bytewise).map(key => `${JSON.stringify(key)}:${canonical(value[key])}`).join(',')}}`
  }
  return JSON.stringify(value)
}

export function sha256(value) {
  return crypto.createHash('sha256').update(value).digest('hex')
}

function parseArgs(values) {
  const result = new Map()
  for (let index = 0; index < values.length; index += 1) {
    const key = values[index]
    if (!key?.startsWith('--')) usage()
    if (key === '--generated') {
      result.set('generated', 'true')
      continue
    }
    const value = values[index + 1]
    if (!value || value.startsWith('--')) usage()
    result.set(key.slice(2), value)
    index += 1
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

function writeJSON(file, value, mode) {
  fs.mkdirSync(path.dirname(file), { recursive: true, mode: 0o755 })
  fs.writeFileSync(file, `${JSON.stringify(value, null, 2)}\n`, { mode })
  fs.chmodSync(file, mode)
}

function relative(root, candidate) {
  return path.relative(root, candidate).split(path.sep).join('/')
}

function isSafeRelative(value) {
  return value !== '' && !value.startsWith('/') && !value.startsWith('../') && !value.includes('/../') && !value.includes('\\')
}

function bytewise(left, right) {
  return Buffer.from(left).compare(Buffer.from(right))
}

function isMain() {
  return process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
}

function usage() {
  throw new Error('Usage: node scripts/openwrt-package-source-fingerprint.mjs <create|materialize|verify> --source-root <path> [--build-facts <json> --out <json> | --manifest <json> [--generated]]')
}

function fail(message) {
  console.error(`[openwrt-source-fingerprint] ERROR: ${message}`)
  process.exit(1)
}
