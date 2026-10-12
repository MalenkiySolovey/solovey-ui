#!/usr/bin/env node
import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const command = process.argv[2] ?? 'check'
const lock = JSON.parse(fs.readFileSync(path.join(root, 'deploy/dependencies/sing-box-ssm-integration.json'), 'utf8'))
const patch = fs.readFileSync(path.join(root, lock.patch.file))
const goMod = fs.readFileSync(path.join(root, 'go.mod'), 'utf8')
const goSum = fs.readFileSync(path.join(root, 'go.sum'), 'utf8')
function requireFact(condition, message) { if (!condition) throw new Error(message) }
function sha256(bytes) { return createHash('sha256').update(bytes).digest('hex') }
requireFact(lock.schema === 'solovey.ssm-integration/v1' && lock.patched && !lock.officialByteIdentical, 'patched source identity missing')
requireFact(sha256(patch) === lock.patch.sha256, 'SSM patch checksum mismatch')
requireFact(goMod.includes(`replace ${lock.original.module} => ${lock.replacement.module} ${lock.replacement.version}`), 'SSM replacement differs from provenance')
for (const dependency of [lock.semanticBaseline, lock.original]) {
  requireFact(goMod.includes(`${dependency.module} ${dependency.version}`), `unexpected baseline: ${dependency.module}`)
}
requireFact(lock.replacement.version.endsWith(`-${lock.replacement.commit.slice(0, 12)}`), 'replacement commit mismatch')
requireFact(goSum.includes(`${lock.replacement.module} ${lock.replacement.version} ${lock.replacement.sum}\n`), 'replacement module checksum missing')
requireFact(goSum.includes(`${lock.replacement.module} ${lock.replacement.version}/go.mod ${lock.replacement.goModSum}\n`), 'replacement Go module checksum missing')
requireFact(lock.original.version === 'v1.14.2' && lock.original.commit === 'af6e64c3b69e6132ebaee0e1a3d24e93903f6709', 'SSM baseline differs')
requireFact(lock.original.commit === lock.semanticBaseline.commit, 'SSM source baseline mismatch')
const allowed = ['SOLOVEY_INTEGRATION.md', 'protocol/naive/inbound.go', 'protocol/naive/quic/inbound_init.go', 'protocol/naive/quic/inbound_lifecycle.go', 'service/ssmapi/cache.go', 'service/ssmapi/cache_store.go', 'service/ssmapi/cache_test.go', 'service/ssmapi/server.go', 'transport/v2raywebsocket/server.go', 'transport/v2raywebsocket/server_path_test.go'].sort()
requireFact(JSON.stringify([...lock.patch.files].sort()) === JSON.stringify(allowed), 'SSM source scope differs')
const changedPaths = [...patch.toString('utf8').matchAll(/^diff --git a\/(\S+) b\/\1$/gm)].map(match => match[1]).sort()
requireFact(JSON.stringify(changedPaths) === JSON.stringify([...lock.patch.files].sort()), 'patch inventory mismatch')
const quicLock = JSON.parse(fs.readFileSync(path.join(root, 'deploy/dependencies/sing-quic-integration.json'), 'utf8'))
const websocket = lock.additionalIntegrations?.websocket
requireFact(websocket?.patched && websocket.disabledEarlyDataDecoded === false && websocket.pathSuffixBasis === 'decoded-URL.Path' && websocket.configuredMaximumEnforcedBeforeHandler === true, 'WebSocket transport contract differs')
requireFact(JSON.stringify([...websocket.files].sort()) === JSON.stringify(allowed.filter(file => file.startsWith('transport/v2raywebsocket/'))), 'WebSocket source inventory mismatch')
const naive = lock.additionalIntegrations?.naiveHttp3
requireFact(JSON.stringify(lock.scopes) === JSON.stringify(['ssm-cache', 'naive-http3-lifecycle', 'websocket-path-early-data']), 'sing-box integration scopes differ')
requireFact(naive?.patched && naive.parentControl === 'inbound-lifecycle-only' && naive.selectivePrincipalParentControl === false, 'Naive lifecycle capability differs')
requireFact(JSON.stringify([...naive.files].sort()) === JSON.stringify(allowed.filter(file => file.startsWith('protocol/naive/'))), 'Naive source inventory mismatch')
requireFact(JSON.stringify(naive.companion) === JSON.stringify({ module: quicLock.replacement.module, version: quicLock.replacement.version, commit: quicLock.replacement.commit }), 'Naive companion source mismatch')
requireFact(JSON.stringify(naive.transport) === JSON.stringify(quicLock.transport), 'Naive transport source mismatch')
requireFact(goMod.includes(`replace ${quicLock.original.module} => ${naive.companion.module} ${naive.companion.version}`), 'Naive companion module differs from provenance')
requireFact(goMod.includes(`${naive.transport.module} ${naive.transport.version}`), 'Naive transport module differs from provenance')

if (command === 'manifest') {
  console.log(JSON.stringify(lock, null, 2))
} else if (command === 'build-info') {
  console.log([
    'ssm_integration=patched',
    `ssm_semantic_baseline=${lock.semanticBaseline.module}@${lock.semanticBaseline.version}`,
    `ssm_original=${lock.original.module}@${lock.original.version}`,
    `ssm_replacement=${lock.replacement.module}@${lock.replacement.version}`,
    `ssm_source_commit=${lock.replacement.commit}`,
    `ssm_patch_sha256=${lock.patch.sha256}`,
    `ssm_module_sum=${lock.replacement.sum}`,
    `ssm_license=${lock.license}`,
    `sing_box_integration_scopes=${lock.scopes.join(',')}`,
    'websocket_integration=patched',
    'websocket_early_data_maximum=before-handler',
    'naive_h3_integration=patched',
    `naive_h3_parent_control=${naive.parentControl}`,
    `naive_h3_companion_commit=${naive.companion.commit}`,
  ].join('\n'))
} else if (command === 'replay') {
  const source = path.resolve(process.argv[3] ?? '')
  requireFact(Boolean(process.argv[3]), 'replay requires existing source clone path')
  const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'solovey-ssm-replay-'))
  try {
    execFileSync('git', ['init', '--quiet', scratch], { stdio: 'pipe' })
    execFileSync('git', ['-C', scratch, 'config', 'core.autocrlf', 'false'], { stdio: 'pipe' })
    execFileSync('git', ['-C', scratch, 'config', 'core.eol', 'lf'], { stdio: 'pipe' })
    const git = (...args) => execFileSync('git', ['-C', source, ...args], { maxBuffer: 2 * 1024 * 1024, stdio: ['ignore', 'pipe', 'pipe'] })
    const baselinePaths = new Set(git('ls-tree', '-r', '--name-only', lock.original.commit).toString('utf8').trim().split('\n'))
    for (const file of lock.patch.files) {
      requireFact(!path.isAbsolute(file) && !file.split('/').includes('..'), 'invalid replay path')
      if (!baselinePaths.has(file)) continue
      const destination = path.join(scratch, file)
      fs.mkdirSync(path.dirname(destination), { recursive: true })
      fs.writeFileSync(destination, git('show', `${lock.original.commit}:${file}`))
    }
    execFileSync('git', ['-C', scratch, 'apply', '--check', path.join(root, lock.patch.file)], { stdio: 'pipe' })
    execFileSync('git', ['-C', scratch, 'apply', path.join(root, lock.patch.file)], { stdio: 'pipe' })
    for (const file of lock.patch.files) {
      requireFact(sha256(fs.readFileSync(path.join(scratch, file))) === sha256(git('show', `${lock.replacement.commit}:${file}`)), `replay differs: ${file}`)
    }
    console.log(`SSM_PATCH_REPLAY=PASS; baseline=${lock.original.commit}; replacement=${lock.replacement.commit}; files=${lock.patch.files.length}`)
  } finally {
    // mkdtemp creates this one owned scratch tree; no cache or source is removed.
    const actual = fs.realpathSync(scratch)
    requireFact(path.dirname(actual) === fs.realpathSync(os.tmpdir()) && path.basename(actual).startsWith('solovey-ssm-replay-'), 'scratch cleanup escaped temporary root')
    fs.rmSync(actual, { recursive: true, force: true })
  }
} else if (command === 'check') {
  console.log(`SSM_PROVENANCE=PASS; replacement=${lock.replacement.commit}; patch=${lock.patch.sha256}`)
} else {
  throw new Error('expected check, manifest, build-info or replay <existing-source-clone>')
}
