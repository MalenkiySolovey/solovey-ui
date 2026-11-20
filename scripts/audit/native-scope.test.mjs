import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { nativeScope, requireJobResults, verifyScopeClaim } from './native-scope.mjs'

test('only exact, clean two-parent PR merges of existing allowlisted blobs can select a fast path', t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'native-scope-test-'))
  t.after(() => fs.rmSync(root, { recursive: true, force: true }))
  const git = (...args) => execFileSync('git', ['-c', 'user.name=Scope fixture', '-c', 'user.email=fixture@example.invalid',
    '-c', 'commit.gpgsign=false', ...args], { cwd: root, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim()
  const weight = 'scripts/go-test-shard-weights.json'
  const policy = 'scripts/audit/native-scope.mjs'
  const provider = 'core/provider/pins.json'
  const audit = '.github/workflows/audit-go.yml'
  const write = (name, text) => {
    const file = path.join(root, name)
    fs.mkdirSync(path.dirname(file), { recursive: true })
    fs.writeFileSync(file, text)
  }
  git('init', '-b', 'base')
  for (const name of ['README.md', 'unreviewed.md', weight, policy, provider, audit]) write(name, 'original\n')
  git('add', '--all')
  git('commit', '-m', 'Base fixtures')
  const base = git('rev-parse', 'HEAD')
  for (const [i, [change, expected]] of [
    [() => write('README.md', 'updated\n'), 'docs-only'],
    [() => write(weight, 'updated\n'), 'audit-go-only'],
    [() => git('mv', 'unreviewed.md', 'new.md'), 'full'],
    [() => git('rm', 'unreviewed.md'), 'full'],
    [() => write(policy, 'policy change\n'), 'full'],
    [() => write(audit, 'audit trust change\n'), 'full'],
    [() => write(provider, 'pin change\n'), 'full'],
    [() => { write('README.md', 'updated\n'); write('main.go', 'package main\n') }, 'full'],
    [() => { write('README.md', 'updated\n'); write(weight, 'updated\n') }, 'full'],
  ].entries()) {
    git('switch', '-c', `candidate-${i}`, base)
    change()
    git('add', '--all')
    git('commit', '-m', `Change ${i}`)
    const head = git('rev-parse', 'HEAD')
    git('switch', '--detach', base)
    git('merge', '--no-ff', '--no-edit', head)
    const merge = git('rev-parse', 'HEAD')
    const env = { GITHUB_EVENT_NAME: 'pull_request', GITHUB_REF: 'refs/pull/123/merge', GITHUB_SHA: merge, BASE_SHA: base, PR_SHA: head }
    assert.equal(nativeScope(env, root), expected, `candidate ${i}`)
    if (expected !== 'full') {
      verifyScopeClaim(expected, env, root)
      assert.throws(() => verifyScopeClaim(expected === 'docs-only' ? 'audit-go-only' : 'docs-only', env, root), /must match/)
      for (const override of [{ GITHUB_REF: '' }, { GITHUB_SHA: head }, { PR_SHA: '' }, { BASE_SHA: head, PR_SHA: base }, { GITHUB_EVENT_NAME: 'push' }]) {
        assert.equal(nativeScope({ ...env, ...override }, root), 'full')
      }
      const original = fs.readFileSync(path.join(root, 'README.md'))
      write('README.md', 'dirty checkout\n')
      assert.equal(nativeScope(env, root), 'full')
      write('README.md', original)
      assert.equal(git('status', '--porcelain'), '')
    } else assert.throws(() => verifyScopeClaim('docs-only', env, root), /must match/)
  }
})

test('CI gate requires selected jobs and rejects unexpected skips, incomplete or invalid scope', () => {
  const names = ['installer', 'metadata-qualification', 'build-tooling', 'extended-runtime', 'docker-native-cronet', 'backend',
    'quic-integration', 'ssm-integration', 'anytls-integration', 'frontend', 'frontend-embedded-browser']
  const needsFor = (scope, scopedResult) => ({
    ...Object.fromEntries(names.map(name => [name, { result: name === 'metadata-qualification' ? 'skipped' : name === 'installer' ? 'success' : scopedResult }])),
    'native-scope': { result: 'success', outputs: { scope } },
  })
  for (const scope of ['docs-only', 'audit-go-only']) {
    const needs = needsFor(scope, 'skipped')
    assert.equal(requireJobResults('ci', needs, 'pull_request'), scope)
    assert.throws(() => requireJobResults('ci', needs, 'push'), /Invalid native scope/)
    needs['extended-runtime'].result = 'cancelled'
    assert.throws(() => requireJobResults('ci', needs, 'pull_request'), /extended-runtime: expected skipped/)
  }
  for (const scope of [undefined, '', 'unknown']) assert.throws(() => requireJobResults('ci', needsFor(scope, 'skipped'), 'pull_request'), /Invalid native scope/)
  const full = needsFor('full', 'success')
  assert.equal(requireJobResults('ci', full, 'pull_request'), 'full')
  full['metadata-qualification'].result = 'success'
  assert.equal(requireJobResults('ci', full, 'workflow_dispatch', true), 'full')
  full['extended-runtime'].result = 'skipped'
  assert.throws(() => requireJobResults('ci', full, 'workflow_dispatch', true), /extended-runtime: expected success/)
  delete full.backend
  assert.throws(() => requireJobResults('ci', full, 'workflow_dispatch', true), /missing qualification jobs/)
})
