#!/usr/bin/env node
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { verifySeriesMetadata, verifyGitSeries } from './core-source-provenance.mjs'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const command = process.argv[2] ?? 'check'
const lock = JSON.parse(fs.readFileSync(path.join(root, 'deploy/dependencies/sing-quic-integration.json'), 'utf8'))
const goMod = fs.readFileSync(path.join(root, 'go.mod'), 'utf8')
const goSum = fs.readFileSync(path.join(root, 'go.sum'), 'utf8')
function requireFact(condition, message) { if (!condition) throw new Error(message) }
requireFact(lock.schema === 'solovey.quic-integration/v2' && lock.patched && !lock.officialByteIdentical, 'patched source identity missing')
verifySeriesMetadata(lock)
requireFact(goMod.includes(`replace ${lock.original.module} => ${lock.replacement.module} ${lock.replacement.version}`), 'QUIC replacement differs from provenance')
for (const dependency of [lock.semanticBaseline, lock.original, lock.transport]) {
  requireFact(goMod.includes(`${dependency.module} ${dependency.version}`), `unexpected baseline: ${dependency.module}`)
}
requireFact(lock.replacement.version.endsWith(`-${lock.replacement.commit.slice(0, 12)}`), 'replacement commit mismatch')
requireFact(goSum.includes(`${lock.replacement.module} ${lock.replacement.version} ${lock.replacement.sum}\n`), 'replacement module checksum missing')
requireFact(goSum.includes(`${lock.replacement.module} ${lock.replacement.version}/go.mod ${lock.replacement.goModSum}\n`), 'replacement Go module checksum missing')

if (command === 'manifest') {
  console.log(JSON.stringify(lock, null, 2))
} else if (command === 'build-info') {
  console.log([
    'quic_integration=patched',
    `quic_semantic_baseline=${lock.semanticBaseline.module}@${lock.semanticBaseline.version}`,
    `quic_original=${lock.original.module}@${lock.original.version}`,
    `quic_replacement=${lock.replacement.module}@${lock.replacement.version}`,
    `quic_source_commit=${lock.replacement.commit}`,
    `quic_patch_series=${lock.patchSeries.identity}`,
    `quic_module_sum=${lock.replacement.sum}`,
    `quic_license=${lock.license}`,
  ].join('\n'))
} else if (command === 'verify-git') {
  requireFact(Boolean(process.argv[3]), 'verify-git requires an existing source clone')
  console.log(JSON.stringify(verifyGitSeries(lock, path.resolve(process.argv[3]))))
} else if (command === 'check') {
  console.log(`QUIC_PROVENANCE=PASS; replacement=${lock.replacement.commit}; series=${lock.patchSeries.identity}`)
} else {
  throw new Error('expected check, manifest, build-info or verify-git <existing-source-clone>')
}
