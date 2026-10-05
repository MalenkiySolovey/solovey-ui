import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'
import test from 'node:test'
import './aggregate.test.mjs'

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..')
const YAML = createRequire(import.meta.url)(path.join(repo, 'frontend/node_modules/yaml'))
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
  const download = dashboard.steps.find(step => step.uses?.startsWith('actions/download-artifact@'))
  assert.equal(download.with['merge-multiple'], false)
  assert.equal(download.with.pattern, 'audit-go-*')
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
