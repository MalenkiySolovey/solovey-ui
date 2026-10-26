#!/usr/bin/env node
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { verifySeriesMetadata, verifyGitSeries } from './core-source-provenance.mjs'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const command = process.argv[2] ?? 'check'
const lock = JSON.parse(fs.readFileSync(path.join(root, 'deploy/dependencies/sing-box-ssm-integration.json'), 'utf8'))
const goMod = fs.readFileSync(path.join(root, 'go.mod'), 'utf8')
const goSum = fs.readFileSync(path.join(root, 'go.sum'), 'utf8')
function requireFact(condition, message) { if (!condition) throw new Error(message) }
requireFact(lock.schema === 'solovey.ssm-integration/v2' && lock.patched && !lock.officialByteIdentical, 'patched source identity missing')
verifySeriesMetadata(lock)
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
requireFact(JSON.stringify([...lock.sourceFiles].sort()) === JSON.stringify(allowed), 'SSM source scope differs')
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
    `ssm_patch_series=${lock.patchSeries.identity}`,
    `ssm_module_sum=${lock.replacement.sum}`,
    `ssm_license=${lock.license}`,
    `sing_box_integration_scopes=${lock.scopes.join(',')}`,
    'websocket_integration=patched',
    'websocket_early_data_maximum=before-handler',
    'naive_h3_integration=patched',
    `naive_h3_parent_control=${naive.parentControl}`,
    `naive_h3_companion_commit=${naive.companion.commit}`,
  ].join('\n'))
} else if (command === 'verify-git') {
  requireFact(Boolean(process.argv[3]), 'verify-git requires an existing source clone')
  console.log(JSON.stringify(verifyGitSeries(lock, path.resolve(process.argv[3]))))
} else if (command === 'check') {
  console.log(`SSM_PROVENANCE=PASS; replacement=${lock.replacement.commit}; series=${lock.patchSeries.identity}`)
} else {
  throw new Error('expected check, manifest, build-info or verify-git <existing-source-clone>')
}
