#!/usr/bin/env node
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { verifySeriesMetadata, verifyGitSeries } from './core-source-provenance.mjs'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const command = process.argv[2] ?? 'check'
const lock = JSON.parse(fs.readFileSync(path.join(root, 'deploy/dependencies/sing-anytls-integration.json'), 'utf8'))
const goMod = fs.readFileSync(path.join(root, 'go.mod'), 'utf8')
const goSum = fs.readFileSync(path.join(root, 'go.sum'), 'utf8')
function requireFact(condition, message) { if (!condition) throw new Error(message) }
requireFact(lock.schema === 'solovey.anytls-integration/v2' && lock.patched && !lock.officialByteIdentical, 'patched source identity missing')
verifySeriesMetadata(lock)
requireFact(goMod.includes(`replace ${lock.original.module} => ${lock.replacement.module} ${lock.replacement.version}`), 'ANYTLS replacement differs from provenance')
for (const dependency of [lock.semanticBaseline, lock.original]) {
  requireFact(goMod.includes(`${dependency.module} ${dependency.version}`), `unexpected baseline: ${dependency.module}`)
}
requireFact(lock.replacement.version.endsWith(`-${lock.replacement.commit.slice(0, 12)}`), 'replacement commit mismatch')
requireFact(goSum.includes(`${lock.replacement.module} ${lock.replacement.version} ${lock.replacement.sum}\n`), 'replacement module checksum missing')
requireFact(goSum.includes(`${lock.replacement.module} ${lock.replacement.version}/go.mod ${lock.replacement.goModSum}\n`), 'replacement Go module checksum missing')
requireFact(lock.original.module === 'github.com/anytls/sing-anytls' && lock.original.version === 'v0.0.11' && lock.original.commit === '130d2e61b8895727bfed4942c535e91b246a9603', 'ANYTLS baseline differs')
requireFact(lock.semanticBaseline.module === 'github.com/sagernet/sing-box' && lock.semanticBaseline.version === 'v1.14.2' && lock.semanticBaseline.commit === 'af6e64c3b69e6132ebaee0e1a3d24e93903f6709', 'official core semantic baseline differs')
const allowed = ["SOLOVEY_INTEGRATION.md","session/concurrency_test.go","session/session.go","session/stream.go"].sort()
requireFact(JSON.stringify([...lock.sourceFiles].sort()) === JSON.stringify(allowed), 'ANYTLS source scope differs')

if (command === 'manifest') {
  console.log(JSON.stringify(lock, null, 2))
} else if (command === 'build-info') {
  console.log([
    'anytls_integration=patched',
    `anytls_core_semantic_baseline=${lock.semanticBaseline.module}@${lock.semanticBaseline.version}`,
    `anytls_original=${lock.original.module}@${lock.original.version}`,
    `anytls_replacement=${lock.replacement.module}@${lock.replacement.version}`,
    `anytls_source_commit=${lock.replacement.commit}`,
    `anytls_patch_series=${lock.patchSeries.identity}`,
    `anytls_module_sum=${lock.replacement.sum}`,
    `anytls_license=${lock.license}`,
  ].join('\n'))
} else if (command === 'verify-git') {
  requireFact(Boolean(process.argv[3]), 'verify-git requires an existing source clone')
  console.log(JSON.stringify(verifyGitSeries(lock, path.resolve(process.argv[3]))))
} else if (command === 'check') {
  console.log(`ANYTLS_PROVENANCE=PASS; replacement=${lock.replacement.commit}; series=${lock.patchSeries.identity}`)
} else {
  throw new Error('expected check, manifest, build-info or verify-git <existing-source-clone>')
}
