import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const docs = new Set(['README.md', 'CONTRIBUTING.md', 'docs/build-reliability.md', 'scripts/audit/README.md'])
const weights = 'scripts/go-test-shard-weights.json'
const sha = /^[0-9a-f]{40}$/

// Inspect the merge tree, not a truncated PR file list or a head-only diff.
export function nativeScope(env = process.env, cwd = process.cwd()) {
  const { GITHUB_EVENT_NAME: event, GITHUB_REF: ref, GITHUB_SHA: merge, BASE_SHA: base, PR_SHA: head } = env
  if (event !== 'pull_request' || !/^refs\/pull\/[1-9][0-9]*\/merge$/.test(ref) ||
      ![merge, base, head].every(value => sha.test(value)) || new Set([merge, base, head]).size !== 3) return 'full'
  const git = (...args) => execFileSync('git', ['--no-replace-objects', ...args], {
    cwd, encoding: 'latin1', stdio: ['ignore', 'pipe', 'pipe'], maxBuffer: 4 * 1024 * 1024,
  })
  try {
    if (git('rev-parse', '--verify', 'HEAD').trim() !== merge ||
        git('show', '-s', '--format=%P', merge).trim() !== `${base} ${head}` ||
        git('cat-file', '-t', base).trim() !== 'commit' || git('cat-file', '-t', head).trim() !== 'commit' ||
        git('status', '--porcelain=v1', '--untracked-files=all', '--ignore-submodules=none', '-z')) return 'full'
    // No rename detection: both sides are represented, and only modifications
    // of existing non-executable regular files are eligible for a fast path.
    const entries = git('diff', '--raw', '--no-renames', '--no-ext-diff', '--ignore-submodules=none', '-z', base, merge, '--').split('\0')
    if (entries.pop() !== '' || !entries.length || entries.length % 2) return 'full'
    const classes = new Set()
    for (let i = 0; i < entries.length; i += 2) {
      if (!/^:100644 100644 [a-f0-9]+ [a-f0-9]+ M$/.test(entries[i])) return 'full'
      const name = entries[i + 1]
      if (docs.has(name)) classes.add('docs-only')
      // Weights change allocation, never the package universe or test flags.
      else if (name === weights) classes.add('audit-go-only')
      else return 'full'
    }
    return classes.size === 1 ? [...classes][0] : 'full'
  } catch (error) {
    console.warn(`Unable to verify the merge tree; using full qualification (${error.code ?? error.status ?? 'git error'})`)
    return 'full'
  }
}

const ciJobs = ['build-tooling', 'extended-runtime', 'docker-native-cronet', 'backend', 'quic-integration',
  'ssm-integration', 'anytls-integration', 'frontend', 'frontend-embedded-browser']
const platforms = name => ['ubuntu-latest', 'windows-latest'].map(os => `${name}-${os}`)
const shards = name => [1, 2, 3, 4].map(i => `${name}-shard-${i}-of-4`)
const auditJobs = {
  build: platforms('build'),
  vet: platforms('vet'),
  'test-go': ['test-ubuntu-latest'],
  'test-go-windows': ['test-windows-latest-gate'],
  'test-go-windows-shard': shards('test-windows-latest'),
  'test-go-race-linux': ['race-ubuntu-latest-gate'],
  'test-go-race-linux-shard': shards('race-ubuntu-latest'),
  'test-go-race-windows': ['race-windows-latest-gate'],
  'test-go-race-windows-shard': shards('race-windows-latest'),
  coverage: ['coverage'],
  'lint-go': platforms('lint'),
  staticcheck: platforms('staticcheck'),
  gosec: platforms('gosec'),
  govulncheck: platforms('govulncheck'),
}

function requireScope(scope, event) {
  assert.ok(scope === 'full' || (event === 'pull_request' && ['docs-only', 'audit-go-only'].includes(scope)), 'Invalid native scope')
}

function auditSelected(job, scope, event) {
  return scope !== 'docs-only' && (job !== 'coverage' || event !== 'pull_request')
}

export function expectedGoArtifacts(scope, event) {
  requireScope(scope, event)
  return Object.entries(auditJobs).filter(([job]) => auditSelected(job, scope, event))
    .flatMap(([, names]) => names.map(name => `audit-go-${name}`))
}

export function requireJobResults(workflow, needs, event, qualifyMetadata = false) {
  const scope = needs['native-scope']?.outputs?.scope
  requireScope(scope, event)
  const expected = { 'native-scope': 'success' }
  const result = selected => selected ? 'success' : 'skipped'
  if (workflow === 'ci') {
    expected.installer = 'success'
    expected['metadata-qualification'] = result(event === 'workflow_dispatch' && qualifyMetadata)
    for (const name of ciJobs) expected[name] = result(scope === 'full')
  } else {
    assert.equal(workflow, 'audit-go')
    for (const name of Object.keys(auditJobs)) expected[name] = result(auditSelected(name, scope, event))
  }
  assert.deepEqual(Object.keys(needs).sort(), Object.keys(expected).sort(), 'Unexpected or missing qualification jobs')
  for (const [name, expectedResult] of Object.entries(expected)) {
    assert.equal(needs[name]?.result, expectedResult, `${name}: expected ${expectedResult}`)
  }
  return scope
}

export function verifyScopeClaim(scope, env = process.env, cwd = process.cwd()) {
  if (scope !== 'full') assert.equal(nativeScope(env, cwd), scope, 'Scoped qualification must match the tested merge tree')
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const command = process.argv[2]
  if (command === 'classify') console.log(nativeScope())
  else {
    const scope = requireJobResults(command, JSON.parse(process.env.QUALIFICATION_NEEDS), process.env.GITHUB_EVENT_NAME,
      process.env.QUALIFY_METADATA === 'true')
    verifyScopeClaim(scope)
    console.log(`Verified ${command} job results for ${scope}`)
  }
}
