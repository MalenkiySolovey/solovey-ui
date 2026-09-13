#!/usr/bin/env node

import crypto from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'

export const goToolchainSchema = 'solovey-ui/go-toolchain-authority/v1'
export const goDependencySchema = 'solovey-ui/go-dependency-authority/v1'

const supportedLinuxGoarch = new Set(['amd64', 'arm64'])

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    await main()
  } catch (error) {
    console.error(`[openwrt-go-authority] ERROR: ${error instanceof Error ? error.message : String(error)}`)
    process.exit(1)
  }
}

async function main() {
  const [command, ...values] = process.argv.slice(2)
  const args = parseArgs(values)
  if (command === 'pin-archive') {
    const digest = pinArchive(path.resolve(required(args, 'source')), path.resolve(required(args, 'out')), required(args, 'sha256'))
    console.log(`[openwrt-go-authority] pinned ${digest}`)
    return
  }
  if (command === 'validate-list') {
    const entries = (await readStdin()).split('\n').filter(Boolean)
    validateArchiveEntries(entries, required(args, 'expected-top'))
    console.log(`[openwrt-go-authority] validated ${entries.length} archive entries`)
    return
  }
  if (command === 'create-toolchain') {
    const proof = createToolchainProof(path.resolve(required(args, 'root')), toolchainMetadata(args))
    writeJSON(path.resolve(required(args, 'out')), proof)
    console.log(`[openwrt-go-authority] recorded ${proof.toolchainIdentity}`)
    return
  }
  if (command === 'verify-toolchain') {
    const proof = readJSON(path.resolve(required(args, 'proof')))
    verifyToolchainProof(proof, path.resolve(required(args, 'root')))
    console.log(`[openwrt-go-authority] reverified ${proof.toolchainIdentity}`)
    return
  }
  if (command === 'create-dependencies') {
    const graph = args.get('graph') ?? 'module'
    const modules = dependencyModules(parseJSONStream(await readStdin()), graph)
    const proof = createDependencyProof(modules, path.resolve(required(args, 'go-mod')), path.resolve(required(args, 'go-sum')), graph)
    writeJSON(path.resolve(required(args, 'out')), proof)
    console.log(`[openwrt-go-authority] recorded ${proof.dependencyIdentity}`)
    return
  }
  if (command === 'verify-dependencies') {
    const graph = args.get('graph') ?? 'module'
    const modules = dependencyModules(parseJSONStream(await readStdin()), graph)
    const proof = readJSON(path.resolve(required(args, 'proof')))
    verifyDependencyProof(proof, modules, path.resolve(required(args, 'go-mod')), path.resolve(required(args, 'go-sum')), graph)
    console.log(`[openwrt-go-authority] reverified ${proof.dependencyIdentity}`)
    return
  }
  usage()
}

export function pinArchive(source, out, expectedSha256) {
  if (!/^[0-9a-f]{64}$/.test(expectedSha256 ?? '')) throw new Error('archive digest is invalid')
  if (fs.existsSync(out)) throw new Error('private archive output already exists')
  const sourceStat = fs.lstatSync(source)
  if (!sourceStat.isFile()) throw new Error('caller archive is not a file')
  fs.copyFileSync(source, out, fs.constants.COPYFILE_EXCL)
  try {
    fs.chmodSync(out, 0o400)
    const digest = hashFile(out)
    if (digest !== expectedSha256) throw new Error('private archive checksum mismatch')
    return digest
  } catch (error) {
    fs.rmSync(out, { force: true })
    throw error
  }
}

function toolchainMetadata(args) {
  return {
    archiveFilename: required(args, 'archive-filename'),
    archiveSha256: required(args, 'archive-sha256'),
    version: required(args, 'version'),
    goos: required(args, 'goos'),
    goarch: required(args, 'goarch'),
  }
}

export function validateArchiveEntries(entries, expectedTop) {
  if (!Array.isArray(entries) || entries.length === 0) throw new Error('Go toolchain archive is empty')
  if (!safeSegment(expectedTop)) throw new Error('Go toolchain top-level name is invalid')
  const seen = new Set()
  for (const original of entries) {
    const entry = original.endsWith('/') ? original.slice(0, -1) : original
    if (!safeRelative(entry) || entry.split('/')[0] !== expectedTop) throw new Error(`Go toolchain archive path is unsafe: ${JSON.stringify(original)}`)
    if (seen.has(entry)) throw new Error(`Go toolchain archive contains a duplicate path: ${entry}`)
    seen.add(entry)
  }
  if (!seen.has(expectedTop)) throw new Error('Go toolchain archive omits its top-level directory')
  return true
}

export function createToolchainProof(toolchainRoot, metadata) {
  toolchainRoot = path.resolve(toolchainRoot)
  if (path.basename(toolchainRoot) !== 'go') throw new Error('Go toolchain root is not the canonical go directory')
  if (!safeSegment(metadata.archiveFilename) || !/^[0-9a-f]{64}$/.test(metadata.archiveSha256 ?? '')) throw new Error('Go archive identity is invalid')
  if (!/^go[0-9]+\.[0-9]+(?:\.[0-9]+)?$/.test(metadata.version ?? '') || metadata.goos !== 'linux' || !supportedLinuxGoarch.has(metadata.goarch)) {
    throw new Error('Go toolchain coordinates are invalid')
  }
  const rootStat = fs.lstatSync(toolchainRoot)
  if (!rootStat.isDirectory() || rootStat.isSymbolicLink()) throw new Error('Go toolchain root is not a non-symlink directory')
  const records = enumerateTree(toolchainRoot, { allowAbsoluteSymlinks: false })
  for (const requiredPath of ['bin/go', 'bin/gofmt', 'VERSION']) {
    const record = records.find(item => item.path === requiredPath)
    if (!record || record.type !== 'file') throw new Error(`Go toolchain omits required file: ${requiredPath}`)
  }
  const treeDigest = sha256(canonical(records))
  const material = {
    schema: goToolchainSchema,
    archiveFilename: metadata.archiveFilename,
    archiveSha256: metadata.archiveSha256,
    version: metadata.version,
    goos: metadata.goos,
    goarch: metadata.goarch,
    authentication: 'official-go-download-sha256-private-copy',
    extraction: 'fresh-private-direct-from-verified-private-archive',
    rootMode: mode(rootStat),
    tree: {
      records,
      entryCount: records.length,
      fileCount: records.filter(item => item.type === 'file').length,
      directoryCount: records.filter(item => item.type === 'directory').length,
      symlinkCount: records.filter(item => item.type === 'symlink').length,
      treeDigest,
    },
  }
  return { ...material, toolchainIdentity: sha256(canonical(material)) }
}

export function verifyToolchainProof(proof, toolchainRoot) {
  if (proof?.schema !== goToolchainSchema) throw new Error(`unsupported Go toolchain schema: ${proof?.schema ?? 'missing'}`)
  const expected = createToolchainProof(toolchainRoot, proof)
  if (canonical(proof) !== canonical(expected)) throw new Error('Go toolchain proof differs from the tree that will be executed')
  return true
}

export function createDependencyProof(modules, goMod, goSum, graph = 'module') {
  if (!['module', 'package'].includes(graph)) throw new Error('Go dependency graph kind is invalid')
  if (!Array.isArray(modules) || modules.length === 0) throw new Error('Go module graph is empty')
  const records = modules.map(moduleRecord).sort((left, right) => bytewise(moduleKey(left), moduleKey(right)))
  const main = records.filter(record => record.main)
  if (main.length !== 1 || main[0].path !== 'github.com/MalenkiySolovey/solovey-ui') throw new Error('Go module graph has the wrong main module')
  const external = records.filter(record => !record.main)
  if (external.length === 0) throw new Error('Go module graph omits external dependencies')
  for (let index = 1; index < records.length; index += 1) {
    if (moduleKey(records[index - 1]) === moduleKey(records[index])) throw new Error(`Go module graph contains a duplicate: ${moduleKey(records[index])}`)
  }
  const source = { goMod: fileRecord(goMod), goSum: fileRecord(goSum) }
  const moduleGraphDigest = sha256(canonical(records))
  const material = {
    schema: goDependencySchema,
    authentication: {
      model: 'fresh-private-gomodcache-go-sumdb',
      proxy: 'https://proxy.golang.org',
      checksumDatabase: 'sum.golang.org',
      privateExemptions: false,
      verification: 'go mod verify immediately before build',
      graph: graph === 'package' ? 'exact-openwrt-target-package-dependencies' : 'complete-selected-module-list',
    },
    source,
    modules: { records, count: records.length, externalCount: external.length, moduleGraphDigest },
  }
  return { ...material, dependencyIdentity: sha256(canonical(material)) }
}

export function verifyDependencyProof(proof, modules, goMod, goSum, graph = 'module') {
  if (proof?.schema !== goDependencySchema) throw new Error(`unsupported Go dependency schema: ${proof?.schema ?? 'missing'}`)
  const expected = createDependencyProof(modules, goMod, goSum, graph)
  if (canonical(proof) !== canonical(expected)) throw new Error('Go dependency proof differs from the authenticated module graph that will be built')
  return true
}

function dependencyModules(values, graph) {
  if (graph === 'module') return values
  if (graph !== 'package') throw new Error('Go dependency graph kind is invalid')
  const unique = new Map()
  for (const value of values) {
    if (!value || value.Standard === true || !value.Module) continue
    const record = moduleRecord(value.Module)
    const key = moduleKey(record)
    const encoded = canonical(record)
    if (unique.has(key) && canonical(unique.get(key)) !== encoded) throw new Error(`Go package graph disagrees about module: ${record.path}`)
    unique.set(key, record)
  }
  return [...unique.values()].map(record => record.main ? { Path: record.path, Main: true } : denormalizeModule(record))
}

function denormalizeModule(record) {
  if (record.replacement) {
    return {
      Path: record.path, Version: record.version,
      Replace: { Path: record.replacement.path, Version: record.replacement.version, Sum: record.replacement.sum, GoModSum: record.replacement.goModSum },
    }
  }
  return { Path: record.path, Version: record.version, Sum: record.sum, GoModSum: record.goModSum }
}

function moduleRecord(module) {
  if (!module || typeof module.Path !== 'string' || module.Path === '' || module.Path.includes('\0')) throw new Error('Go module record has an invalid path')
  if (module.Main === true) {
    if (module.Replace || module.Version || module.Sum || module.GoModSum) throw new Error('main Go module record is not canonical')
    return { path: module.Path, main: true }
  }
  if (typeof module.Version !== 'string' || module.Version === '') throw new Error(`Go module has no version: ${module.Path}`)
  const record = { path: module.Path, version: module.Version, main: false }
  if (module.Replace) {
    record.replacement = authenticatedModule(module.Replace, `replacement for ${module.Path}`)
  } else {
    Object.assign(record, authenticatedModule(module, module.Path))
  }
  return record
}

function authenticatedModule(module, label) {
  if (!module || typeof module.Path !== 'string' || module.Path === '' || typeof module.Version !== 'string' || module.Version === '') {
    throw new Error(`Go ${label} is a local or unversioned replacement`)
  }
  if (!/^h1:[A-Za-z0-9+/]{43}=$/.test(module.Sum ?? '') || !/^h1:[A-Za-z0-9+/]{43}=$/.test(module.GoModSum ?? '')) {
    throw new Error(`Go ${label} lacks authenticated content and go.mod sums`)
  }
  return { path: module.Path, version: module.Version, sum: module.Sum, goModSum: module.GoModSum }
}

function enumerateTree(root, options) {
  const records = []
  walk(root, root, records, options)
  records.sort((left, right) => bytewise(left.path, right.path))
  return records
}

function walk(directory, root, records, options) {
  const entries = fs.readdirSync(directory, { withFileTypes: true }).sort((a, b) => bytewise(a.name, b.name))
  for (const entry of entries) {
    const candidate = path.join(directory, entry.name)
    const stat = fs.lstatSync(candidate)
    const relative = path.relative(root, candidate).split(path.sep).join('/')
    if (!safeRelative(relative)) throw new Error(`tree contains unsafe path: ${relative}`)
    if (stat.isDirectory()) {
      records.push({ path: relative, type: 'directory', mode: mode(stat) })
      walk(candidate, root, records, options)
    } else if (stat.isFile()) {
      records.push({ path: relative, type: 'file', mode: mode(stat), size: stat.size, sha256: hashFile(candidate) })
    } else if (stat.isSymbolicLink()) {
      const target = fs.readlinkSync(candidate)
      if (target === '' || target.includes('\0') || (path.isAbsolute(target) && !options.allowAbsoluteSymlinks)) throw new Error(`tree symlink target is unsafe: ${relative}`)
      const resolved = path.resolve(path.dirname(candidate), target)
      if (!path.isAbsolute(target) && resolved !== root && !resolved.startsWith(`${root}${path.sep}`)) throw new Error(`tree symlink escapes its root: ${relative}`)
      records.push({ path: relative, type: 'symlink', mode: mode(stat), target })
    } else throw new Error(`tree contains unsupported entry type: ${relative}`)
  }
}

function fileRecord(file) {
  const stat = fs.lstatSync(file)
  if (!stat.isFile() || stat.isSymbolicLink()) throw new Error(`Go source authority is not a regular file: ${path.basename(file)}`)
  return { filename: path.basename(file), size: stat.size, sha256: hashFile(file) }
}

function hashFile(file) {
  const descriptor = fs.openSync(file, 'r')
  const hash = crypto.createHash('sha256')
  const buffer = Buffer.allocUnsafe(1024 * 1024)
  try {
    for (;;) {
      const bytes = fs.readSync(descriptor, buffer, 0, buffer.length, null)
      if (bytes === 0) break
      hash.update(buffer.subarray(0, bytes))
    }
  } finally {
    fs.closeSync(descriptor)
  }
  return hash.digest('hex')
}

function parseJSONStream(input) {
  const values = []
  let start = -1
  let depth = 0
  let string = false
  let escaped = false
  for (let index = 0; index < input.length; index += 1) {
    const character = input[index]
    if (start === -1) {
      if (/\s/.test(character)) continue
      if (character !== '{') throw new Error('Go module JSON stream is invalid')
      start = index
      depth = 1
      continue
    }
    if (string) {
      if (escaped) escaped = false
      else if (character === '\\') escaped = true
      else if (character === '"') string = false
      continue
    }
    if (character === '"') string = true
    else if (character === '{') depth += 1
    else if (character === '}') {
      depth -= 1
      if (depth === 0) {
        values.push(JSON.parse(input.slice(start, index + 1)))
        start = -1
      }
    }
  }
  if (start !== -1 || string || depth !== 0) throw new Error('Go module JSON stream is truncated')
  return values
}

function mode(stat) {
  return (stat.mode & 0o7777).toString(8).padStart(4, '0')
}

function safeSegment(value) {
  return value !== '' && value !== '.' && value !== '..' && !/[\\/\u0000-\u001f\u007f]/.test(value)
}

function safeRelative(value) {
  if (typeof value !== 'string' || value === '' || value.startsWith('/') || value.includes('\\') || value.includes('\0')) return false
  return value.split('/').every(safeSegment) && path.posix.normalize(value) === value
}

function moduleKey(record) {
  return `${record.path}\u0000${record.version ?? ''}`
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

function canonical(value) {
  if (Array.isArray(value)) return `[${value.map(canonical).join(',')}]`
  if (value && typeof value === 'object') return `{${Object.keys(value).sort(bytewise).map(key => `${JSON.stringify(key)}:${canonical(value[key])}`).join(',')}}`
  return JSON.stringify(value)
}

function sha256(value) {
  return crypto.createHash('sha256').update(value).digest('hex')
}

function bytewise(left, right) {
  return Buffer.from(left).compare(Buffer.from(right))
}

async function readStdin() {
  const chunks = []
  for await (const chunk of process.stdin) chunks.push(chunk)
  return Buffer.concat(chunks).toString('utf8')
}

function usage() {
  throw new Error('Usage: node scripts/openwrt-go-authority.mjs <pin-archive|validate-list|create-toolchain|verify-toolchain|create-dependencies|verify-dependencies> ...')
}
