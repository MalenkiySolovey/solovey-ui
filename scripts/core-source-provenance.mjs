// Panel consumption checks only. Core Git commits own engine source and tests.
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const sha = value => createHash('sha256').update(value).digest('hex')
const commit = value => typeof value === 'string' && /^[a-f0-9]{40}$/.test(value)
const topologies = new Map([
  ['https://github.com/SagerNet/sing-box', 'https://github.com/MalenkiySolovey/sing-box'],
  ['https://github.com/shtorm-7/sing-box-extended', 'https://github.com/MalenkiySolovey/sing-box-extended'],
  ['https://github.com/SagerNet/sing-quic', 'https://github.com/MalenkiySolovey/sing-quic'],
  ['https://github.com/anytls/sing-anytls', 'https://github.com/MalenkiySolovey/sing-anytls'],
])
const git = (cwd, ...args) => execFileSync('git', ['-c', `safe.directory=${path.resolve(cwd)}`, '-C', cwd, ...args], {
  encoding: 'utf8', windowsHide: true, maxBuffer: 16 << 20, stdio: ['ignore', 'pipe', 'pipe'],
  timeout: 180000, env: { ...process.env, GIT_TERMINAL_PROMPT: '0' },
}).trim()

export function patchSeriesIdentity(series) {
  return sha(`solovey.core-patch-series/v1\n${series.base}\n${series.commits.join('\n')}\n${series.patchedTree}\n`)
}

export function verifySeriesMetadata(source) {
  const { original, replacement, patchSeries: series } = source
  assert.ok(!Object.hasOwn(source, 'patch'), 'Panel patch payload metadata is forbidden')
  assert.equal(source.copyRole, 'mirror/cache/development', 'core copy role differs')
  assert.ok(topologies.has(original.repository), 'core source authority differs')
  assert.equal(replacement.repository, topologies.get(original.repository), 'core copy responsibility differs')
  assert.equal(replacement.module, replacement.repository.replace('https://', ''), 'copy module differs')
  assert.ok(commit(original.commit) && commit(replacement.commit), 'core commit differs')
  assert.ok(replacement.version.endsWith(`-${replacement.commit.slice(0, 12)}`), 'copy commit/version mismatch')
  assert.equal(series?.format, 'linear-git-commits/v1', 'canonical core series missing')
  assert.equal(series.base, original.commit, 'patch-series upstream base differs')
  assert.ok(commit(series.upstreamTree) && commit(series.patchedTree), 'core tree missing')
  assert.match(series.pristineRef, /^refs\/heads\/mirror\/[A-Za-z0-9._/-]+$/, 'pristine ref differs')
  assert.match(series.branch, /^(?:solovey|stage2)\/[A-Za-z0-9._/-]+$/, 'patch branch differs')
  assert.ok(Array.isArray(series.commits) && series.commits.every(commit), 'ordered core commits missing')
  assert.equal(new Set(series.commits).size, series.commits.length, 'duplicate core commit')
  assert.ok(Array.isArray(series.patchCommits) && series.patchCommits.every(commit), 'engine patch commits missing')
  assert.deepEqual(series.commits.filter(value => series.patchCommits.includes(value)), series.patchCommits, 'engine patch order differs')
  assert.equal(series.patchCount, series.patchCommits.length, 'engine patch count differs')
  assert.equal(series.commits.at(-1) ?? series.base, replacement.commit, 'built core head differs')
  assert.equal(series.identity, patchSeriesIdentity(series), 'patch-series identity mismatch')
  return source
}

export function verifyGitSeries(source, clone) {
  verifySeriesMetadata(source)
  const series = source.patchSeries
  assert.equal(git(clone, 'rev-parse', `${series.base}^{tree}`), series.upstreamTree, 'authority tree differs')
  assert.equal(git(clone, 'rev-parse', `${source.replacement.commit}^{tree}`), series.patchedTree, 'build tree differs')
  git(clone, 'merge-base', '--is-ancestor', series.base, source.replacement.commit)
  const text = git(clone, 'rev-list', '--reverse', `${series.base}..${source.replacement.commit}`)
  assert.deepEqual(text ? text.split('\n') : [], series.commits, 'canonical source queue differs')
  let parent = series.base
  for (const revision of series.commits) {
    assert.deepEqual(git(clone, 'rev-list', '--parents', '-n', '1', revision).split(' '), [revision, parent], 'nonlinear core queue')
    parent = revision
  }
  return { status: 'PASS', authority: source.original.repository, copy: source.replacement.repository,
    base: series.base, head: source.replacement.commit, tree: series.patchedTree,
    patchCount: series.patchCount, seriesIdentity: series.identity }
}

export function verifyRemoteSeries(source, parent = os.tmpdir()) {
  verifySeriesMetadata(source)
  const owner = fs.realpathSync(parent)
  const scratch = fs.mkdtempSync(path.join(owner, 'solovey-core-consumption-'))
  try {
    git(scratch, 'init', '--quiet')
    // Fetch Git objects without checking out or applying engine modifications.
    git(scratch, 'fetch', '--quiet', '--no-tags', '--filter=blob:none', `--depth=${source.patchSeries.commits.length + 1}`,
      source.replacement.repository, source.replacement.commit)
    const pristine = git(scratch, 'ls-remote', source.replacement.repository, source.patchSeries.pristineRef)
    assert.equal(pristine, `${source.original.commit}\t${source.patchSeries.pristineRef}`, 'pristine copy differs')
    git(scratch, 'fetch', '--quiet', '--no-tags', '--filter=blob:none', '--depth=1', source.original.repository, source.original.commit)
    return verifyGitSeries(source, scratch)
  } finally {
    const actual = fs.realpathSync(scratch)
    assert.equal(path.dirname(actual), owner)
    assert.ok(path.basename(actual).startsWith('solovey-core-consumption-'))
    fs.rmSync(actual, { recursive: true, force: true })
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  assert.equal(process.argv[2], 'verify-remote', 'expected verify-remote')
  const root = fileURLToPath(new URL('../', import.meta.url))
  const sources = ['sing-box-ssm', 'sing-quic', 'sing-anytls'].map(name =>
    JSON.parse(fs.readFileSync(path.join(root, `deploy/dependencies/${name}-integration.json`), 'utf8')))
  sources.push(...JSON.parse(fs.readFileSync(path.join(root, 'deploy/dependencies/sing-box-extended-integration.json'), 'utf8')).sources)
  for (const source of sources) console.log(JSON.stringify(verifyRemoteSeries(source)))
}
