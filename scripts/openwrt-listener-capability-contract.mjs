#!/usr/bin/env node

import assert from 'node:assert/strict'
import crypto from 'node:crypto'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'
import { fileURLToPath } from 'node:url'
import { enumerateSourcePaths } from './openwrt-package-source-fingerprint.mjs'

const schema = 'solovey-ui/openwrt-listener-capability-contract/v1'
const pinnedOpenWrtRevision = 'f0a60eee2fe051741c643ea6118718aae1ef17fb'

try {
  const scriptRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
  const sourceRoot = path.resolve(argumentValue('--source-root') ?? scriptRoot)
  const workspaceRoot = path.resolve(sourceRoot, '..', '..')
  const openwrtRoot = path.resolve(argumentValue('--openwrt-root') ?? path.join(workspaceRoot, 'upstreams', 'openwrt-openwrt-25.12.5'))
  const lockPath = path.resolve(argumentValue('--lock') ?? path.join(workspaceRoot, 'upstreams', 'OPENWRT_REFERENCE_LOCK.md'))
  const evidenceOut = argumentValue('--evidence-out')

  const lock = readFile(lockPath)
  assert.ok(lock.includes('**Directory**: `upstreams/openwrt-openwrt-25.12.5`'))
  assert.ok(lock.includes(`**HEAD SHA**: \`${pinnedOpenWrtRevision}\``))
  assert.ok(lock.includes('**Requested Ref**: `v25.12.5`'))

  const actualRevision = git(openwrtRoot, ['rev-parse', 'HEAD']).trim()
  assert.equal(actualRevision, pinnedOpenWrtRevision, 'local OpenWrt source is not the locked v25.12.5 revision')
  assert.ok(gitTreeMatchesHead(openwrtRoot), 'local pinned OpenWrt tracked source differs from HEAD')

  const rockchipMakefile = readRelative(openwrtRoot, 'target/linux/rockchip/Makefile')
  const kernelMatch = rockchipMakefile.match(/^KERNEL_PATCHVER:=(\d+)\.(\d+)$/m)
  assert.ok(kernelMatch, 'rockchip kernel patch version is unavailable')
  const kernelVersion = `${kernelMatch[1]}.${kernelMatch[2]}`
  assert.ok(versionAtLeast(kernelVersion, '5.3'), 'rockchip kernel predates pidfd_open')

  const genericKernelConfig = readRelative(openwrtRoot, `target/linux/generic/config-${kernelVersion}`)
  assert.match(genericKernelConfig, /^CONFIG_PROC_FS=y$/m, 'pinned kernel does not provide procfs')
  assert.match(genericKernelConfig, /^# CONFIG_INET_DIAG is not set$/m, 'inet_diag assumption changed and must be reviewed')
  assert.match(genericKernelConfig, /^# CONFIG_INET_TCP_DIAG is not set$/m, 'inet_tcp_diag assumption changed and must be reviewed')

  const dropbearInit = readRelative(openwrtRoot, 'package/network/services/dropbear/files/dropbear.init')
  const dropbearRecipe = readRelative(openwrtRoot, 'package/network/services/dropbear/Makefile')
  assert.match(dropbearRecipe, /^PKG_VERSION:=2025\.89$/m)
  assert.match(dropbearRecipe, /^PKG_HASH:=0d1f7ca711cfc336dc8a85e672cab9cfd8223a02fe2da0a4a7aeb58c9e113634$/m)
  assert.match(dropbearInit, /local pid_file="\/var\/run\/\$\{NAME\}\.\$\{1\}\.pid"/)
  assert.match(dropbearInit, /procd_open_instance[\s\S]*procd_set_param command "\$PROG" -F -P "\$pid_file"/)
  assert.match(dropbearInit, /procd_append_param command -p "\$\{Port\}"/)
  assert.match(dropbearInit, /config_foreach validate_section_dropbear dropbear dropbear_instance/)

  const listenerPath = path.join(sourceRoot, 'internal', 'ops', 'listenerevidence', 'listeners_linux.go')
  const listenerSource = readFile(listenerPath)
  const descriptorSource = readRelative(sourceRoot, 'internal/ops/processevidence/descriptors_linux.go')
  assert.match(listenerSource, /pidfdOpen:\s+unix\.PidfdOpen/, 'production listener proof lacks its process liveness fence binding')
  assert.match(listenerSource, /pidfdGetfd:\s+unix\.PidfdGetfd/, 'production optional descriptor proof binding disappeared')
  assert.match(listenerSource, /environment\.pidfdOpen\(pid, 0\)/, 'listener proof does not consume the production liveness fence')
  assert.match(listenerSource, /environment\.pidfdGetfd\(pidfd, targetFD, 0\)/, 'listener proof does not consume optional descriptor enrichment')
  assert.match(listenerSource, /if duplicateErr != nil \{[\s\S]*?continue\s*\}/, 'pidfd_getfd failure is no longer capability-fenced')
  assert.match(listenerSource, /ListenerProofProcFSV1/, 'procfs exact fallback proof disappeared')
  assert.match(listenerSource, /environment\.readDescriptorSnapshot\(pid\)[\s\S]*environment\.readSocketTables\(pid,[\s\S]*environment\.readSocketTables\(pid,[\s\S]*environment\.readDescriptorSnapshot\(pid\)/,
    'listener proof no longer brackets socket-table evidence with stable descriptor snapshots')
  assert.doesNotMatch(listenerSource, /NETLINK_SOCK_DIAG|INET_DIAG|INET_TCP_DIAG/i,
    'production acquired an undeclared inet_diag dependency')
  assert.match(descriptorSource, /const maxProcessDescriptors = 4096/,
    'shared process descriptor evidence lost its explicit production bound')
  assert.match(descriptorSource, /Readdirnames\(maxProcessDescriptors \+ 1\)/,
    'shared process descriptor evidence no longer detects a bound overflow')
  assert.match(descriptorSource, /len\(names\) > maxProcessDescriptors[\s\S]*ErrDescriptorInventoryBound/,
    'descriptor overflow no longer fails closed through its typed reason')

  const getfdOwners = productionGoFiles(sourceRoot)
    .filter(file => readFile(file).includes('PidfdGetfd'))
    .map(file => slash(path.relative(sourceRoot, file)))
  assert.deepEqual(getfdOwners, ['internal/ops/listenerevidence/listeners_linux.go'],
    'pidfd_getfd escaped the shared low-level capability owner')

  const recipe = readRelative(sourceRoot, 'deploy/openwrt/package/solovey-ui/Makefile')
  const dependencyLine = recipe.match(/^\s*DEPENDS:=(.+)$/m)?.[1]?.trim()
  assert.equal(dependencyLine, '+procd +dropbear +ubus +uci +logd +nftables')
  assert.doesNotMatch(recipe, /\+(?:kmod-inet-diag|kmod-inet-tcp-diag)\b/,
    'package unexpectedly declares an inet_diag dependency that production does not use')

  const material = {
    schema,
    classifications: ['SOURCE_VERIFIED', 'PHYSICAL_TARGET_REQUIRED_OPENWRT'],
    pinnedPlatform: {
      release: 'OpenWrt 25.12.5',
      revision: actualRevision,
      target: 'rockchip/armv8',
      kernelPatchVersion: kernelVersion,
    },
    requiredCapabilities: {
      procfs: 'CONFIG_PROC_FS=y',
      pidfdOpen: `kernel-${kernelVersion}-baseline`,
      pidfdGetfd: 'optional-enrichment-only',
      inetDiag: 'not-required-and-not-enabled-in-generic-config',
    },
    semanticContracts: {
      processFence: 'pidfd_open-plus-stable-proc-start',
      socketOwnership: 'stable-process-fd-inode-set',
      descriptorInventory: 'shared-fail-closed-4096-entry-bound',
      acceptingTuple: 'stable-proc-network-namespace-socket-table',
      descriptorEnrichment: 'SO_ACCEPTCONN-plus-SO_COOKIE-plus-IPV6_V6ONLY-when-pidfd_getfd-works',
      dropbearShape: 'uci-section-to-procd-command-pidfile-and-port; runtime-map-key-remains-observed-not-inferred',
      dropbearSource: '2025.89 sha256:0d1f7ca711cfc336dc8a85e672cab9cfd8223a02fe2da0a4a7aeb58c9e113634',
    },
    packaging: { dependencies: dependencyLine },
    productionGetfdOwners: getfdOwners,
    physicalClaim: 'NOT_PHYSICAL_TARGET_VERIFIED',
  }
  const proof = { ...material, contractIdentity: sha256(canonical(material)) }
  if (evidenceOut) writeJSON(path.resolve(evidenceOut), proof)
  console.log(`[openwrt-listener-capability-contract] verified ${proof.contractIdentity}`)
  console.log('[openwrt-listener-capability-contract] SOURCE_VERIFIED; physical OpenWrt remains required')
} catch (error) {
  console.error(`[openwrt-listener-capability-contract] ERROR: ${error instanceof Error ? error.message : String(error)}`)
  process.exit(1)
}

function argumentValue(name) {
  const index = process.argv.indexOf(name)
  if (index === -1) return undefined
  const value = process.argv[index + 1]
  if (!value || value.startsWith('--')) throw new Error(`${name} requires a value`)
  return value
}

function readRelative(root, relative) {
  return readFile(path.join(root, ...relative.split('/')))
}

function readFile(file) {
  return fs.readFileSync(file, 'utf8')
}

function git(root, args) {
  return execFileSync('git', ['-C', root, ...args], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trimEnd()
}

function gitTreeMatchesHead(root) {
  try {
    execFileSync('git', ['-C', root, 'diff', '--quiet', '--ignore-submodules', 'HEAD', '--'], { stdio: 'ignore' })
    return true
  } catch {
    return false
  }
}

function productionGoFiles(root) {
  // Use the package owner's existing source boundary. Downloaded sources and
  // hidden build caches are not production inputs to the shipped artifact.
  return enumerateSourcePaths(root)
    .filter(file => file.endsWith('.go'))
    .map(file => path.join(root, ...file.split('/')))
    .sort(bytewise)
}

function versionAtLeast(actual, minimum) {
  const left = actual.split('.').map(Number)
  const right = minimum.split('.').map(Number)
  return left[0] > right[0] || left[0] === right[0] && left[1] >= right[1]
}

function writeJSON(file, value) {
  fs.mkdirSync(path.dirname(file), { recursive: true })
  fs.writeFileSync(file, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o644 })
}

function canonical(value) {
  if (Array.isArray(value)) return `[${value.map(canonical).join(',')}]`
  if (value && typeof value === 'object') return `{${Object.keys(value).sort(bytewise).map(key => `${JSON.stringify(key)}:${canonical(value[key])}`).join(',')}}`
  return JSON.stringify(value)
}

function sha256(value) {
  return crypto.createHash('sha256').update(value).digest('hex')
}

function slash(value) {
  return value.split(path.sep).join('/')
}

function bytewise(left, right) {
  return Buffer.from(left).compare(Buffer.from(right))
}
