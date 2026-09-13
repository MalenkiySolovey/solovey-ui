#!/usr/bin/env node

import crypto from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import {
  canonical,
  sha256,
  sourceFingerprintSchema,
} from './openwrt-package-source-fingerprint.mjs'

export const packageHostToolProofSchema = 'solovey-ui/openwrt-package-host-tools/v1'

export const packageExecutionEnvironment = Object.freeze({
  model: 'explicit-empty-package-environment-v1',
  launcher: '/usr/bin/env',
  topLevelPath: [
    '/usr/local/sbin',
    '/usr/local/bin',
    '/usr/sbin',
    '/usr/bin',
    '/sbin',
    '/bin',
  ],
  openWrtPackagePathPrefix: [
    '<sdk-root>/staging_dir/host/bin',
    '<sdk-root>/staging_dir/hostpkg/bin',
  ],
  openWrtPathOwnership: 'rules.mk TARGET_PATH plus package.mk TARGET_PATH_PKG',
  callerPathInherited: false,
  privateRoots: ['HOME', 'XDG_CONFIG_HOME', 'XDG_CACHE_HOME', 'TMPDIR'],
  canonicalValues: { SHELL: '/usr/bin/bash', TZ: 'UTC', LC_ALL: 'C', LANG: 'C' },
  rejectedCallerMakeControls: [
    'MAKE', 'MAKEFLAGS', 'GNUMAKEFLAGS', 'MFLAGS', 'MAKELEVEL', 'MAKEFILES',
    'MAKE_RESTARTS', 'MAKE_TERMOUT', 'MAKE_TERMERR',
  ],
  clearedCallerSelectors: [
    'BASH_ENV', 'ENV', 'CDPATH', 'CONFIG_SITE', 'HOSTCC', 'HOSTCXX', 'CC', 'CXX',
    'LD', 'AR', 'AS', 'STRIP', 'OBJCOPY', 'RANLIB', 'PKG_CONFIG', 'STAGING_DIR',
    'BUILD_DIR', 'TOPDIR', 'LD_PRELOAD', 'LD_LIBRARY_PATH',
  ],
  recursiveMake: 'GNU Make $(MAKE) derives from the absolute /usr/bin/make parent invocation',
  sdkVariables: 'OpenWrt-owned after top-level Make starts',
  trustedBase: 'openwrt-builder base OS absolute executables and fixed directories',
})

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    runCLI()
  } catch (error) {
    console.error(`[openwrt-package-host-tools] ERROR: ${error instanceof Error ? error.message : String(error)}`)
    process.exit(1)
  }
}

function runCLI() {
  const [command, ...values] = process.argv.slice(2)
  const args = parseArgs(values)
  const makeProgram = path.resolve(required(args, 'make'))
  const nodeProgram = path.resolve(required(args, 'node'))
  if (command === 'create') {
    const proof = createPackageHostToolProof(makeProgram, nodeProgram)
    writeJSON(path.resolve(required(args, 'out')), proof)
    console.log(`[openwrt-package-host-tools] recorded ${proof.packageHostToolIdentity}`)
    return
  }
  if (command === 'verify') {
    const proof = JSON.parse(fs.readFileSync(path.resolve(required(args, 'proof')), 'utf8'))
    const sourceFingerprint = args.get('source-fingerprint')
      ? JSON.parse(fs.readFileSync(path.resolve(args.get('source-fingerprint')), 'utf8'))
      : undefined
    verifyPackageHostToolProof(proof, makeProgram, nodeProgram, sourceFingerprint)
    console.log(`[openwrt-package-host-tools] verified ${proof.packageHostToolIdentity}`)
    return
  }
  usage()
}

export function createPackageHostToolProof(makeProgram, nodeProgram) {
  const make = inspectMake(makeProgram)
  const node = inspectNode(nodeProgram)
  const material = {
    schema: packageHostToolProofSchema,
    authority: 'fixed-absolute-make-plus-stage-bound-node',
    make,
    node,
    environment: packageExecutionEnvironment,
  }
  return { ...material, packageHostToolIdentity: sha256(canonical(material)) }
}

export function verifyPackageHostToolProof(proof, makeProgram, nodeProgram, sourceFingerprint) {
  if (proof?.schema !== packageHostToolProofSchema) {
    throw new Error(`unsupported package host-tool proof schema: ${proof?.schema ?? 'missing'}`)
  }
  const expected = createPackageHostToolProof(makeProgram, nodeProgram)
  if (canonical(proof) !== canonical(expected)) {
    throw new Error('package host-tool proof differs from the executable bytes that will be used')
  }
  if (sourceFingerprint !== undefined) assertStageNodeBinding(proof.node, sourceFingerprint)
  return true
}

function inspectMake(program) {
  const executable = inspectExecutable(program, 'Make')
  const output = runVersion(executable.resolvedProgram, ['--version'], 'Make')
  const lines = output.split(/\r?\n/).filter(Boolean)
  if (!/^GNU Make [0-9][^\s]*$/.test(lines[0] ?? '') || !/^Built for \S+$/.test(lines[1] ?? '')) {
    throw new Error('canonical Make does not expose the expected GNU Make identity')
  }
  return {
    authority: 'trusted-build-runner-fixed-absolute-path-and-sha256',
    requestedProgram: executable.requestedProgram,
    resolvedProgram: executable.resolvedProgram,
    resolution: 'realpath-of-fixed-absolute-path',
    executableSha256: executable.executableSha256,
    size: executable.size,
    mode: executable.mode,
    version: lines[0],
    buildTarget: lines[1].slice('Built for '.length),
  }
}

function inspectNode(program) {
  const executable = inspectExecutable(program, 'Node')
  const version = runVersion(executable.resolvedProgram, ['--version'], 'Node').trim()
  if (!/^v[0-9]+\.[0-9]+\.[0-9]+$/.test(version)) throw new Error('package Node version is invalid')
  return {
    authority: 'same-executable-as-stage-build-facts',
    executableSha256: executable.executableSha256,
    size: executable.size,
    mode: executable.mode,
    version,
  }
}

function inspectExecutable(program, label) {
  if (!path.isAbsolute(program)) throw new Error(`${label} program is not absolute`)
  const requestedStat = fs.lstatSync(program)
  if (!requestedStat.isFile() && !requestedStat.isSymbolicLink()) throw new Error(`${label} program path is not a file or symlink`)
  const resolvedProgram = fs.realpathSync.native(program)
  const stat = fs.lstatSync(resolvedProgram)
  if (!stat.isFile() || stat.isSymbolicLink()) throw new Error(`${label} resolved program is not a regular file`)
  fs.accessSync(resolvedProgram, fs.constants.X_OK)
  return {
    requestedProgram: program,
    resolvedProgram,
    executableSha256: hashFile(resolvedProgram),
    size: stat.size,
    mode: (stat.mode & 0o7777).toString(8).padStart(4, '0'),
  }
}

function assertStageNodeBinding(node, fingerprint) {
  if (fingerprint?.schema !== sourceFingerprintSchema) throw new Error('stage source fingerprint is unavailable or unsupported')
  const frontend = fingerprint.build?.facts?.frontend
  if (frontend?.node !== node.version || frontend?.nodeExecutableSha256 !== node.executableSha256) {
    throw new Error('package Node executable differs from the stage build facts')
  }
}

function runVersion(program, args, label) {
  const result = spawnSync(program, args, {
    encoding: 'utf8',
    env: { PATH: '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin', HOME: '/nonexistent', TZ: 'UTC', LC_ALL: 'C', LANG: 'C' },
    timeout: 10_000,
    maxBuffer: 1024 * 1024,
  })
  if (result.error) throw result.error
  if (result.status !== 0) throw new Error(`${label} identity command failed: ${result.stderr.trim()}`)
  return result.stdout
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

function writeJSON(file, value) {
  fs.writeFileSync(file, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o644 })
  fs.chmodSync(file, 0o644)
}

function usage() {
  throw new Error('Usage: node scripts/openwrt-package-host-tools.mjs <create|verify> --make <absolute-make> --node <absolute-node> [--source-fingerprint <json>] (--out <json> | --proof <json>)')
}
