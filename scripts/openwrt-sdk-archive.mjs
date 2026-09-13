#!/usr/bin/env node

import crypto from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'

export const sdkDerivationSchema = 'solovey-ui/openwrt-sdk-derivation/v3'

// The pinned official SDK contains these exact host-tool links. They are
// archive-authoritative rather than caller-selected: every other absolute
// symlink remains forbidden, and the verified archive SHA binds this list to
// the extracted tree that is executed.
const officialAbsoluteSymlinks = new Map([
  ['staging_dir/host/bin/awk', '/usr/bin/gawk'],
  ['staging_dir/host/bin/bash', '/bin/bash'],
  ['staging_dir/host/bin/diff', '/usr/bin/diff'],
  ['staging_dir/host/bin/egrep', '/bin/egrep'],
  ['staging_dir/host/bin/file', '/usr/bin/file'],
  ['staging_dir/host/bin/g++', '/builder/openwrt-25_12_x86_64/ccache_cxx.sh'],
  ['staging_dir/host/bin/gcc', '/builder/openwrt-25_12_x86_64/ccache_cc.sh'],
  ['staging_dir/host/bin/getopt', '/usr/bin/getopt'],
  ['staging_dir/host/bin/git', '/usr/bin/git'],
  ['staging_dir/host/bin/grep', '/bin/grep'],
  ['staging_dir/host/bin/gzip', '/bin/gzip'],
  ['staging_dir/host/bin/ldconfig', '/builder/shared-workdir/build/scripts/noop.sh'],
  ['staging_dir/host/bin/perl', '/usr/bin/perl'],
  ['staging_dir/host/bin/python', '/opt/venv/bin/python3.9'],
  ['staging_dir/host/bin/python3', '/opt/venv/bin/python3.9'],
  ['staging_dir/host/bin/rsync', '/usr/bin/rsync'],
  ['staging_dir/host/bin/unzip', '/usr/bin/unzip'],
  ['staging_dir/host/bin/wget', '/usr/bin/wget'],
  ['staging_dir/host/bin/which', '/usr/bin/which'],
  ['staging_dir/host/bin/xxd', '/builder/shared-workdir/build/scripts/xxdi.pl'],
])

function officialAbsoluteSymlinkTarget(sdkPath, archiveFilename) {
  if (sdkPath === 'staging_dir/host/bin/g++' || sdkPath === 'staging_dir/host/bin/gcc') {
    const toolchainRoot = archiveFilename.includes('rockchip-armv8')
      ? '/builder/openwrt-25_12_rockchip_armv8'
      : '/builder/openwrt-25_12_x86_64'
    return `${toolchainRoot}/${sdkPath.endsWith('/g++') ? 'ccache_cxx.sh' : 'ccache_cc.sh'}`
  }
  return officialAbsoluteSymlinks.get(sdkPath)
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    await main()
  } catch (error) {
    console.error(`[openwrt-sdk-archive] ERROR: ${error instanceof Error ? error.message : String(error)}`)
    process.exit(1)
  }
}

async function main() {
  const [command, ...values] = process.argv.slice(2)
  const args = parseArgs(values)
  if (command === 'pin-archive') {
    const digest = pinArchive(path.resolve(required(args, 'source')), path.resolve(required(args, 'out')), required(args, 'sha256'))
    console.log(`[openwrt-sdk-archive] pinned ${digest}`)
  } else if (command === 'validate-list') {
    const expectedTop = required(args, 'expected-top')
    const input = await readStdin()
    const entries = input.split('\n').filter(line => line !== '')
    validateArchiveEntries(entries, expectedTop)
    console.log(`[openwrt-sdk-archive] validated ${entries.length} archive entries`)
  } else if (command === 'verify-extracted') {
    const proof = verifyExtractedSDK(
      path.resolve(required(args, 'root')),
      required(args, 'expected-top'),
      required(args, 'archive-filename'),
      required(args, 'archive-sha256'),
    )
    writeJSON(path.resolve(required(args, 'out')), proof)
    console.log(`[openwrt-sdk-archive] verified ${proof.sdkIdentity}`)
  } else if (command === 'create-tree') {
    const proof = createSDKTreeProof(path.resolve(required(args, 'root')), {
      archiveFilename: required(args, 'archive-filename'),
      archiveSha256: required(args, 'archive-sha256'),
      expectedTopLevel: required(args, 'expected-top'),
      phase: required(args, 'phase'),
    })
    writeJSON(path.resolve(required(args, 'out')), proof)
    console.log(`[openwrt-sdk-archive] recorded ${proof.sdkIdentity}`)
  } else if (command === 'verify-tree') {
    const proof = JSON.parse(fs.readFileSync(path.resolve(required(args, 'proof')), 'utf8'))
    verifySDKTreeProof(proof, path.resolve(required(args, 'root')))
    console.log(`[openwrt-sdk-archive] reverified ${proof.sdkIdentity}`)
  } else {
    usage()
  }
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

export function validateArchiveEntries(entries, expectedTop) {
  if (!Array.isArray(entries) || entries.length === 0) throw new Error('SDK archive is empty')
  if (!isSafeSegment(expectedTop)) throw new Error('expected SDK top-level name is invalid')
  const seen = new Set()
  for (const original of entries) {
    if (typeof original !== 'string' || original === '') throw new Error('SDK archive contains an empty path')
    const entry = original.endsWith('/') ? original.slice(0, -1) : original
    if (entry === '' || entry.startsWith('/') || entry.includes('\\') || /[\u0000-\u001f\u007f]/.test(entry)) {
      throw new Error(`SDK archive contains an unsafe path: ${JSON.stringify(original)}`)
    }
    const segments = entry.split('/')
    if (segments[0] !== expectedTop || segments.some(segment => !isSafeSegment(segment))) {
      throw new Error(`SDK archive escapes the expected top level: ${JSON.stringify(original)}`)
    }
    if (path.posix.normalize(entry) !== entry) throw new Error(`SDK archive path is not canonical: ${JSON.stringify(original)}`)
    if (seen.has(entry)) throw new Error(`SDK archive contains a duplicate path: ${entry}`)
    seen.add(entry)
  }
  if (!seen.has(expectedTop)) throw new Error('SDK archive omits its expected top-level directory')
  return true
}

export function verifyExtractedSDK(root, expectedTop, archiveFilename, archiveSha256) {
  if (!/^[0-9a-f]{64}$/.test(archiveSha256)) throw new Error('SDK archive digest is invalid')
  const rootEntries = fs.readdirSync(root, { withFileTypes: true })
  if (rootEntries.length !== 1 || rootEntries[0].name !== expectedTop || !rootEntries[0].isDirectory() || rootEntries[0].isSymbolicLink()) {
    throw new Error('SDK extraction root does not contain exactly the expected SDK directory')
  }
  const sdkRoot = path.join(root, expectedTop)
  for (const requiredPath of ['include/package.mk', 'include/package-pack.mk', 'include/version.mk', 'scripts/feeds']) {
    const candidate = path.join(sdkRoot, ...requiredPath.split('/'))
    const stat = fs.lstatSync(candidate)
    if (!stat.isFile() || stat.isSymbolicLink()) throw new Error(`SDK extraction omits required regular file: ${requiredPath}`)
  }
  return createSDKTreeProof(sdkRoot, {
    archiveFilename,
    archiveSha256,
    expectedTopLevel: expectedTop,
    phase: 'extracted',
  })
}

export function createSDKTreeProof(sdkRoot, metadata) {
  sdkRoot = path.resolve(sdkRoot)
  const rootStat = fs.lstatSync(sdkRoot)
  if (!rootStat.isDirectory() || rootStat.isSymbolicLink()) throw new Error('SDK root is not a non-symlink directory')
  if (!metadata || !isSafeSegment(metadata.expectedTopLevel) || path.basename(sdkRoot) !== metadata.expectedTopLevel) {
    throw new Error('SDK top-level identity is invalid')
  }
  if (!isSafeSegment(metadata.archiveFilename) || !/^[0-9a-f]{64}$/.test(metadata.archiveSha256 ?? '')) {
    throw new Error('SDK archive identity is invalid')
  }
  if (!['extracted', 'prepared-for-package'].includes(metadata.phase)) throw new Error('SDK tree phase is invalid')
  const records = []
  walkExtracted(sdkRoot, sdkRoot, records, metadata.phase, metadata.archiveFilename)
  records.sort((left, right) => bytewise(left.path, right.path))
  validateTreeRecords(records)
  const counts = {
    files: records.filter(record => record.type === 'file').length,
    directories: records.filter(record => record.type === 'directory').length,
    symlinks: records.filter(record => record.type === 'symlink').length,
  }
  const treeDigest = sha256(canonical(records))
  const material = {
    schema: sdkDerivationSchema,
    archiveFilename: metadata.archiveFilename,
    archiveSha256: metadata.archiveSha256,
    expectedTopLevel: metadata.expectedTopLevel,
    extraction: 'fresh-private-direct-from-verified-private-archive',
    phase: metadata.phase,
    preparedTreeNormalization: metadata.phase === 'prepared-for-package' ? 'remove-openwrt-25.12.5-defconfig-pid-scratch-v1' : 'none',
    sdkInternalAbsoluteSymlinkEncoding: '<sdk-root>/<relative-path>',
    rootMode: mode(rootStat),
    tree: { records, entryCount: records.length, ...counts, treeDigest },
  }
  return { ...material, sdkIdentity: sha256(canonical(material)) }
}

export function verifySDKTreeProof(proof, sdkRoot) {
  if (proof?.schema !== sdkDerivationSchema) throw new Error(`unsupported SDK derivation schema: ${proof?.schema ?? 'missing'}`)
  const expected = createSDKTreeProof(sdkRoot, {
    archiveFilename: proof.archiveFilename,
    archiveSha256: proof.archiveSha256,
    expectedTopLevel: proof.expectedTopLevel,
    phase: proof.phase,
  })
  if (canonical(proof) !== canonical(expected)) throw new Error('SDK tree proof differs from the tree that will be executed')
  return true
}

function walkExtracted(directory, sdkRoot, records, phase, archiveFilename) {
  for (const entry of fs.readdirSync(directory, { withFileTypes: true }).sort((a, b) => bytewise(a.name, b.name))) {
    const candidate = path.join(directory, entry.name)
    const stat = fs.lstatSync(candidate)
    const sdkPath = relative(sdkRoot, candidate)
    if (stat.isDirectory()) {
      records.push({ path: sdkPath, type: 'directory', mode: mode(stat) })
      walkExtracted(candidate, sdkRoot, records, phase, archiveFilename)
    } else if (stat.isFile()) {
      records.push({ path: sdkPath, type: 'file', mode: mode(stat), size: stat.size, sha256: hashFile(candidate) })
    } else if (stat.isSymbolicLink()) {
      const target = fs.readlinkSync(candidate)
      if (path.isAbsolute(target)) {
        const targetInsideSDK = path.resolve(target) === target && (target === sdkRoot || target.startsWith(`${sdkRoot}${path.sep}`))
        if (officialAbsoluteSymlinkTarget(sdkPath, archiveFilename) !== target &&
            !(phase === 'prepared-for-package' && (targetInsideSDK || preparedHostToolTarget(target)))) {
          throw new Error(`SDK symlink has an unrecognized absolute target: ${sdkPath} -> ${target}`)
        }
        const recordedTarget = targetInsideSDK ? encodeSDKInternalTarget(sdkRoot, target) : target
        records.push({ path: sdkPath, type: 'symlink', mode: mode(stat), target: recordedTarget })
        continue
      }
      const resolved = path.resolve(path.dirname(candidate), target)
      if (resolved !== sdkRoot && !resolved.startsWith(`${sdkRoot}${path.sep}`)) {
        throw new Error(`SDK symlink escapes the extraction root: ${sdkPath}`)
      }
      records.push({ path: sdkPath, type: 'symlink', mode: mode(stat), target })
    } else {
      throw new Error(`SDK extraction contains an unsupported entry type: ${relative(sdkRoot, candidate)}`)
    }
  }
}

function encodeSDKInternalTarget(sdkRoot, target) {
  const suffix = relative(sdkRoot, target)
  return suffix === '' ? '<sdk-root>' : `<sdk-root>/${suffix}`
}

function preparedHostToolTarget(target) {
  return /^\/(?:bin|sbin|usr\/bin|usr\/sbin|usr\/local\/bin)\/[A-Za-z0-9._+-]+$/.test(target) && path.posix.normalize(target) === target
}

function validateTreeRecords(records) {
  if (!Array.isArray(records) || records.length === 0) throw new Error('SDK tree is empty')
  let previous = ''
  const seen = new Set()
  for (const record of records) {
    if (!record || !isSafeRelative(record.path) || seen.has(record.path) || (previous && bytewise(previous, record.path) >= 0)) {
      throw new Error('SDK tree records are not safe, unique and bytewise sorted')
    }
    if (!/^0[0-7]{3}$/.test(record.mode ?? '')) throw new Error(`SDK tree mode is invalid: ${record.path}`)
    if (record.type === 'file') {
      if (!Number.isSafeInteger(record.size) || record.size < 0 || !/^[0-9a-f]{64}$/.test(record.sha256 ?? '')) throw new Error(`SDK file record is invalid: ${record.path}`)
    } else if (record.type === 'symlink') {
      if (typeof record.target !== 'string' || record.target === '' || record.target.includes('\0')) throw new Error(`SDK symlink record is invalid: ${record.path}`)
    } else if (record.type !== 'directory') throw new Error(`SDK tree record has unsupported type: ${record.path}`)
    seen.add(record.path)
    previous = record.path
  }
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

function mode(stat) {
  return (stat.mode & 0o7777).toString(8).padStart(4, '0')
}

function isSafeRelative(value) {
  if (typeof value !== 'string' || value === '' || value.startsWith('/') || value.includes('\\') || value.includes('\0')) return false
  return value.split('/').every(segment => isSafeSegment(segment)) && path.posix.normalize(value) === value
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

function isSafeSegment(value) {
  return value !== '' && value !== '.' && value !== '..' && !/[\\/\u0000-\u001f\u007f]/.test(value)
}

function relative(root, candidate) {
  return path.relative(root, candidate).split(path.sep).join('/')
}

function canonical(value) {
  if (Array.isArray(value)) return `[${value.map(canonical).join(',')}]`
  if (value && typeof value === 'object') {
    return `{${Object.keys(value).sort(bytewise).map(key => `${JSON.stringify(key)}:${canonical(value[key])}`).join(',')}}`
  }
  return JSON.stringify(value)
}

function sha256(value) {
  return crypto.createHash('sha256').update(value).digest('hex')
}

function bytewise(left, right) {
  return Buffer.from(left).compare(Buffer.from(right))
}

function writeJSON(file, value) {
  fs.writeFileSync(file, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o644 })
  fs.chmodSync(file, 0o644)
}

async function readStdin() {
  const chunks = []
  for await (const chunk of process.stdin) chunks.push(chunk)
  return Buffer.concat(chunks).toString('utf8')
}

function usage() {
  throw new Error('Usage: node scripts/openwrt-sdk-archive.mjs <pin-archive|validate-list|verify-extracted|create-tree|verify-tree> ...')
}
