import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'
import test from 'node:test'
import {frontendFixtureDirectory} from '../frontend-source.mjs'
import './aggregate.test.mjs'
import { downloadGoAuditArtifacts, selectLatestAuditArtifacts } from './download-go-artifacts.mjs'

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const YAML = createRequire(path.join(await frontendFixtureDirectory(), 'package.json'))('yaml')
const workflow = YAML.parse(fs.readFileSync(path.join(repo, '.github/workflows/audit-go.yml'), 'utf8'))
const jobs = workflow.jobs
const shards = ['test-go-windows-shard', 'test-go-race-linux-shard', 'test-go-race-windows-shard']
const gates = [
  ['test-go-windows', 'test-go (windows-latest)', shards[0]],
  ['test-go-race-linux', 'test-go-race (ubuntu-latest)', shards[1]],
  ['test-go-race-windows', 'test-go-race (windows-latest)', shards[2]],
]
const bash = process.platform === 'win32' ? 'C:/Program Files/Git/bin/bash.exe' : 'bash'

test('only slow suites become four independent runner shards; normal Ubuntu remains single', () => {
  assert.deepEqual(jobs['test-go'].strategy.matrix.os, ['ubuntu-latest'])
  assert.equal(jobs['test-go'].strategy.matrix.shard, undefined)
  assert.equal(jobs['test-go'].name, 'test-go (${{ matrix.os }})')
  for (const id of shards) {
    const job = jobs[id]
    assert.deepEqual(job.strategy.matrix.shard, [1, 2, 3, 4])
    assert.deepEqual(job.strategy.matrix.os, [id.includes('linux') ? 'ubuntu-latest' : 'windows-latest'])
    assert.equal(job['runs-on'], '${{ matrix.os }}')
    assert.equal(job.strategy['fail-fast'], false)
    assert.equal(job['continue-on-error'], undefined)
    assert.equal(job.concurrency, undefined)
    assert.equal(job.env.SHARD_INDEX, '${{ matrix.shard }}')
    if (id.includes('race')) assert.equal(job['timeout-minutes'], 60)
    const run = job.steps.find(step => step.name?.startsWith('make audit:')).run
    assert.match(run, /GO_TEST_ARGS="--shard-index \$\{SHARD_INDEX\} --shard-total 4"/)
    assert.match(run, /GO_TEST_NAME_SUFFIX="-shard-\$\{SHARD_INDEX\}-of-4"/)
    assert.ok(run.includes(`make audit:${id.includes('race') ? 'test-go-race' : 'test-go'} `))
    assert.equal(job.steps.filter(step => step.name === 'Materialize pinned OpenWrt test references').length, 1)
    assert.equal(job.steps.filter(step => step.name === 'Install privileged fixture tools').length, 1)
    assert.equal(job.steps.find(step => step.name === 'Install privileged fixture tools').if, "runner.os == 'Linux'")
  }
  const make = fs.readFileSync(path.join(repo, 'Makefile'), 'utf8')
  assert.match(make, /go-test\.mjs \$\(GO_TEST_ARGS\) -count=1 -p 1 -timeout 30m -- \.\/\.\.\./)
  assert.match(make, /go-test\.mjs \$\(GO_TEST_ARGS\) -race -count=1 -p 1 -timeout 30m -- \.\/\.\.\./)
})

test('only statement coverage is scoped away from PRs; Go audit and required checks remain', () => {
  for (const event of ['pull_request', 'push', 'schedule', 'workflow_dispatch']) {
    assert.ok(Object.hasOwn(workflow.on, event), event)
  }
  assert.equal(workflow.on.pull_request, null)
  assert.deepEqual(workflow.on.push.branches, ['main'])
  assert.equal(jobs.coverage.name, 'coverage (ubuntu-latest)')
  assert.equal(jobs.coverage.if, "github.event_name != 'pull_request'")
  for (const id of ['build', 'vet', 'test-go', ...shards, 'lint-go', 'staticcheck', 'gosec', 'govulncheck']) {
    assert.equal(jobs[id].if, undefined, id)
    assert.equal(jobs[id]['continue-on-error'], undefined, id)
  }
  for (const id of ['build', 'gosec', 'govulncheck']) {
    assert.deepEqual(jobs[id].strategy.matrix.os, ['ubuntu-latest', 'windows-latest'], id)
  }
  assert.equal(jobs.dashboard.if, 'always()')
})

test('Go audit build publishes the pinned full frontend without installing npm dependencies', () => {
  const commands = jobs.build.steps.flatMap(step => step.run ? step.run.trim().split(/\r?\n/).map(line => line.trim()) : [])
  const build = commands.indexOf('SOLOVEY_UI_PROFILE=full node scripts/frontend.mjs build')
  assert.ok(build >= 0)
  assert.deepEqual(commands.slice(build + 1, build + 3), [
    'node scripts/frontend-assets.mjs publish --dist .artifacts/frontend-dist --destination web/html',
    'make audit:build PS="${MAKE_PS}"',
  ])
  assert.ok(!commands.some(command => /(?:scripts\/frontend\.mjs install|npm (?:ci|install))\b/.test(command)))
})

test('legacy gates always run and only all-success passes, including cancellation and missing results', () => {
  for (const [id, name, dependency] of gates) {
    const gate = jobs[id]
    assert.equal(gate.name, name)
    assert.equal(gate.if, 'always()')
    assert.deepEqual(gate.needs, [dependency])
    assert.equal(gate['continue-on-error'], undefined)
    const step = gate.steps.find(step => step.name === 'Require every shard to succeed')
    assert.equal(step.env.SHARD_RESULT, `\${{ needs['${dependency}'].result }}`)
    assert.equal(step.shell, 'pwsh')
    const predicate = step.run.match(/-CommandLine '([^']+)'/)[1]
    for (const result of ['success', 'failure', 'cancelled', 'skipped', '']) {
      const actual = spawnSync(bash, ['-c', predicate], { env: { ...process.env, SHARD_RESULT: result } })
      assert.equal(actual.status, result === 'success' ? 0 : 1)
    }
    const upload = gate.steps.find(step => step.uses?.startsWith('actions/upload-artifact@'))
    assert.equal(upload.if, 'always()')
    assert.equal(upload.with['if-no-files-found'], 'error')
  }
})

test('artifact and evidence names are unique; dashboard waits for all shards and gates without merging files', () => {
  const names = []
  for (const id of ['test-go', ...shards, ...gates.map(([id]) => id)]) {
    const job = jobs[id]
    const upload = job.steps.find(step => step.uses?.startsWith('actions/upload-artifact@'))
    assert.equal(upload.if, 'always()')
    assert.ok(upload.with.path.includes('.txt'))
    assert.ok(upload.with.path.includes('.junit.xml'))
    for (const os of job.strategy?.matrix.os ?? ['ubuntu-latest']) {
      for (const shard of job.strategy?.matrix.shard ?? [0]) {
        names.push(upload.with.name.replace(/\$\{\{ matrix.os \}\}/g, os).replace(/\$\{\{ matrix.shard \}\}/g, shard))
      }
    }
  }
  assert.equal(names.length, 16)
  assert.equal(new Set(names).size, names.length)
  const dashboard = jobs.dashboard
  assert.equal(dashboard.if, 'always()')
  for (const id of [...shards, ...gates.map(([id]) => id), 'test-go', 'build', 'vet', 'coverage', 'lint-go', 'staticcheck', 'gosec', 'govulncheck']) {
    assert.ok(dashboard.needs.includes(id), id)
  }
  const download = dashboard.steps.find(step => step.name === 'Download audit job artifacts')
  assert.equal(download.run, 'node scripts/audit/download-go-artifacts.mjs')
  assert.equal(download.env.GH_TOKEN, '${{ github.token }}')
  assert.deepEqual(dashboard.permissions, { contents: 'read', actions: 'read' })
  const upload = dashboard.steps.find(step => step.uses?.startsWith('actions/upload-artifact@'))
  assert.ok(upload.with.path.includes('tests/baseline/audit-artifact-selection.json'))
})

test('dashboard only admits the intentional PR coverage skip', () => {
  const step = jobs.dashboard.steps.at(-1)
  assert.equal(step.name, 'Require expected audit job results')
  assert.equal(step.if, 'always()')
  assert.equal(step.env.AUDIT_NEEDS, '${{ toJSON(needs) }}')
  function check(event, job, result, expectedStatus) {
    const needs = Object.fromEntries(jobs.dashboard.needs.map(name => [name, { result: 'success' }]))
    if (event === 'pull_request') needs.coverage.result = 'skipped'
    if (job) needs[job].result = result
    const actual = spawnSync(bash, ['-c', step.run], {
      encoding: 'utf8',
      env: { ...process.env, AUDIT_NEEDS: JSON.stringify(needs), GITHUB_EVENT_NAME: event },
    })
    assert.equal(actual.status, expectedStatus, actual.stderr || actual.error?.message)
    if (expectedStatus) assert.ok(actual.stderr.includes(`${job}: expected`))
  }
  check('pull_request', null, null, 0)
  check('push', null, null, 0)
  check('pull_request', 'govulncheck', 'skipped', 1)
  check('pull_request', 'gosec', 'failure', 1)
  check('pull_request', 'test-go-race-windows', 'cancelled', 1)
  check('schedule', 'coverage', 'skipped', 1)
})

test('rerun artifacts follow creation time even when the older failure has a larger numeric ID', () => {
  const artifact = (id, name, created_at, extra = {}) => ({ id, name, created_at, expired: false, digest: `sha256:${'a'.repeat(64)}`, ...extra })
  const older = artifact(12016560069, 'audit-go-race-windows-latest-gate', '2026-10-16T00:11:58Z')
  const current = artifact(12016364959, older.name, '2026-10-16T00:47:31Z')
  const unchanged = artifact(12013194738, 'audit-go-govulncheck-ubuntu-latest', '2026-10-15T23:30:44Z')
  const priorDashboard = artifact(12017248450, 'audit-go-dashboard', '2026-10-16T00:47:46Z')
  for (const input of [[older, current, unchanged, priorDashboard], [priorDashboard, unchanged, current, older]]) {
    assert.deepEqual(selectLatestAuditArtifacts(input).map(value => value.id), [unchanged.id, current.id])
  }
  // A later failure must replace an earlier success as well; selection does
  // not inspect or waive test results.
  assert.equal(selectLatestAuditArtifacts([current, older]).find(value => value.name === older.name).id, current.id)
})

test('artifact selection rejects missing, ambiguous, expired or unverifiable current evidence', () => {
  const good = { id: 1, name: 'audit-go-test', created_at: '2026-10-16T00:11:58Z', expired: false, digest: `sha256:${'a'.repeat(64)}` }
  for (const input of [[], [good, { ...good }], [{ ...good, created_at: '' }], [good, { ...good, id: 2 }], [{ ...good, expired: true }], [{ ...good, digest: undefined }], [{ ...good, name: 'audit-go-../escape' }]]) {
    assert.throws(() => selectLatestAuditArtifacts(input))
  }
})

test('artifact download retries the same ID on transient HTTP errors and still fails closed', async t => {
  const zip = Buffer.from('verified ZIP bytes')
  const artifact = { id: 17, name: 'audit-go-test', created_at: '2026-10-16T00:11:58Z', expired: false,
    digest: `sha256:${createHash('sha256').update(zip).digest('hex')}`, workflow_run: { id: 42 } }
  for (const [name, responses, delays, failure] of [
    ['recovers', [503, 502, zip], [2000, 4000]],
    ['exhausts the retry budget', [503, 503, 503, 503, 503], [2000, 4000, 8000, 16000], /HTTP 503/],
    ['does not retry authorization failures', [403], [], /HTTP 403/],
    ['does not retry or extract corrupt bytes', [503, Buffer.from('corrupt ZIP')], [2000], /archive digest mismatch/],
  ]) {
    await t.test(name, async t => {
      const destination = fs.mkdtempSync(path.join(os.tmpdir(), 'go-audit-retry-test-'))
      t.after(() => fs.rmSync(destination, { recursive: true, force: true }))
      const observedDelays = []
      let calls = 0
      let unzipCalls = 0
      const execute = (command, args) => {
        if (command === 'unzip') {
          assert.equal(failure, undefined, 'must not inspect or extract an unverified archive')
          unzipCalls++
          return Buffer.from('report.txt\n')
        }
        assert.equal(command, 'gh')
        if (args.includes('--paginate')) return Buffer.from(JSON.stringify([{ total_count: 1, artifacts: [artifact] }]))
        assert.equal(args.at(-1), 'repos/owner/repo/actions/artifacts/17/zip')
        const response = responses[calls++]
        assert.notEqual(response, undefined, 'unexpected extra download attempt')
        if (Buffer.isBuffer(response)) return response
        throw Object.assign(new Error(`HTTP ${response}`), { stderr: Buffer.from(`gh: HTTP ${response}\n`) })
      }
      const download = downloadGoAuditArtifacts({ repository: 'owner/repo', runId: 42, destination, execute,
        wait: async ms => { observedDelays.push(ms) } })
      if (failure) {
        await assert.rejects(download, failure)
        assert.deepEqual(fs.readdirSync(destination), [])
      } else {
        assert.equal((await download).selected[0].digest, artifact.digest)
        assert.equal(unzipCalls, 2)
      }
      assert.deepEqual(observedDelays, delays)
      assert.equal(calls, responses.length)
    })
  }
})

test('existing wrapper and dashboard retain every shard log/JUnit and report a failed or cancelled gate', () => {
  const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'go-audit-artifact-test-'))
  try {
    const baseline = path.join(scratch, 'tests/baseline')
    fs.mkdirSync(baseline, { recursive: true })
    const wrapper = path.join(baseline, 'run-command.ps1')
    fs.copyFileSync(path.join(repo, 'tests/baseline/run-command.ps1'), wrapper)
    const ps = process.platform === 'win32' ? 'powershell.exe' : 'pwsh'
    const logs = []
    for (let shard = 1; shard <= 4; shard++) {
      const name = `go-test-shard-${shard}-of-4`
      const command = `node -e "console.log('shard-${shard}')"`
      const result = spawnSync(ps, ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', wrapper, '-Group', 'core', '-Name', name, '-CommandLine', command], { cwd: scratch, encoding: 'utf8' })
      assert.equal(result.status, 0, result.stdout + result.stderr)
      const target = path.join(baseline, `audit-go-test-windows-latest-shard-${shard}-of-4/core`)
      fs.mkdirSync(target, { recursive: true })
      for (const extension of ['txt', 'junit.xml']) fs.renameSync(path.join(baseline, 'core', `${name}.${extension}`), path.join(target, `${name}.${extension}`))
      logs.push(path.join(target, `${name}.txt`))
    }
    // Execute the existing dashboard's embedded Node program verbatim. This
    // works on Windows too, without altering the production shell driver.
    const aggregate = fs.readFileSync(path.join(repo, 'scripts/audit/aggregate.sh'), 'utf8').replace(/\r\n/g, '\n').split("<<'NODE'\n")[1].replace(/\nNODE\s*$/, '')
    assert.ok(aggregate?.startsWith('const fs'))
    const summarize = () => spawnSync(process.execPath, ['-', scratch, 'tests/baseline/current'], { input: aggregate, encoding: 'utf8' })
    assert.equal(summarize().status, 0)
    let summary = JSON.parse(fs.readFileSync(path.join(baseline, 'current/summary.json')))
    assert.equal(summary.totals.green, 4)
    assert.equal(summary.files.length, 4)
    assert.equal(summary.groups.length, 4)
    assert.ok(logs.every((file, index) => fs.readFileSync(file, 'utf8').includes(`shard-${index + 1}`)))
    const failed = spawnSync(ps, ['-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', wrapper, '-Group', 'core', '-Name', 'go-test-windows-gate', '-CommandLine', 'node -e "process.exit(1)"'], { cwd: scratch })
    assert.equal(failed.status, 1)
    const failedSummary = summarize()
    assert.equal(failedSummary.status, 1, failedSummary.stdout + failedSummary.stderr)
    summary = JSON.parse(fs.readFileSync(path.join(baseline, 'current/summary.json')))
    assert.equal(summary.totals.green, 4)
    assert.equal(summary.totals.red, 1)
    assert.equal(summary.files.length, 5)
    assert.match(fs.readFileSync(path.join(baseline, 'current/aggregate.junit.xml'), 'utf8'), /failures="1"/)
    assert.match(fs.readFileSync(path.join(baseline, 'current/summary.html'), 'utf8'), /audit-go-test-windows-latest-shard-4-of-4/)
  } finally {
    fs.rmSync(scratch, { recursive: true, force: true })
  }
})
