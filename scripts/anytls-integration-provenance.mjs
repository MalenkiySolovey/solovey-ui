#!/usr/bin/env node
import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const command = process.argv[2] ?? 'check'
const lock = JSON.parse(fs.readFileSync(path.join(root, 'deploy/dependencies/sing-anytls-integration.json'), 'utf8'))
const patch = fs.readFileSync(path.join(root, lock.patch.file))
const goMod = fs.readFileSync(path.join(root, 'go.mod'), 'utf8')
const goSum = fs.readFileSync(path.join(root, 'go.sum'), 'utf8')
function requireFact(condition, message) { if (!condition) throw new Error(message) }
function sha256(bytes) { return createHash('sha256').update(bytes).digest('hex') }
requireFact(lock.schema === 'solovey.anytls-integration/v1' && lock.patched && !lock.officialByteIdentical, 'patched source identity missing')
requireFact(sha256(patch) === lock.patch.sha256, 'ANYTLS patch checksum mismatch')
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
requireFact(JSON.stringify([...lock.patch.files].sort()) === JSON.stringify(allowed), 'ANYTLS source scope differs')
const changedPaths = [...patch.toString('utf8').matchAll(/^diff --git a\/(\S+) b\/\1$/gm)].map(match => match[1]).sort()
requireFact(JSON.stringify(changedPaths) === JSON.stringify([...lock.patch.files].sort()), 'patch inventory mismatch')

if (command === 'manifest') {
  console.log(JSON.stringify(lock, null, 2))
} else if (command === 'build-info') {
  console.log([
    'anytls_integration=patched',
    `anytls_core_semantic_baseline=${lock.semanticBaseline.module}@${lock.semanticBaseline.version}`,
    `anytls_original=${lock.original.module}@${lock.original.version}`,
    `anytls_replacement=${lock.replacement.module}@${lock.replacement.version}`,
    `anytls_source_commit=${lock.replacement.commit}`,
    `anytls_patch_sha256=${lock.patch.sha256}`,
    `anytls_module_sum=${lock.replacement.sum}`,
    `anytls_license=${lock.license}`,
  ].join('\n'))
} else if (command === 'replay') {
  const source = path.resolve(process.argv[3] ?? '')
  requireFact(Boolean(process.argv[3]), 'replay requires existing source clone path')
  const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'solovey-anytls-replay-'))
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
    console.log(`ANYTLS_PATCH_REPLAY=PASS; baseline=${lock.original.commit}; replacement=${lock.replacement.commit}; files=${lock.patch.files.length}`)
  } finally {
    // mkdtemp creates this one owned scratch tree; no cache or source is removed.
    const actual = fs.realpathSync(scratch)
    requireFact(path.dirname(actual) === fs.realpathSync(os.tmpdir()) && path.basename(actual).startsWith('solovey-anytls-replay-'), 'scratch cleanup escaped temporary root')
    fs.rmSync(actual, { recursive: true, force: true })
  }
} else if (command === 'check') {
  console.log(`ANYTLS_PROVENANCE=PASS; replacement=${lock.replacement.commit}; patch=${lock.patch.sha256}`)
} else {
  throw new Error('expected check, manifest, build-info or replay <existing-source-clone>')
}
