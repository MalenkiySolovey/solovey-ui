import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const source = fileURLToPath(new URL('./aggregate.sh', import.meta.url))
const program = readFileSync(source, 'utf8').replace(/\r\n/g, '\n')
  .split("<<'NODE'\n")[1].replace(/\nNODE\s*$/, '')

for (const genuineFailure of [false, true]) {
  test(`prior dashboard cannot become test input; source failure=${genuineFailure}`, () => {
    const root = mkdtempSync(path.join(tmpdir(), 'audit-aggregation-'))
    try {
      const baseline = path.join(root, 'tests/baseline')
      const unit = path.join(baseline, 'audit-go-test-fixture')
      const prior = path.join(baseline, 'audit-go-dashboard')
      mkdirSync(unit, { recursive: true })
      mkdirSync(prior, { recursive: true })
      writeFileSync(path.join(unit, 'unit.junit.xml'), `<testsuite tests="3" failures="${genuineFailure ? 1 : 0}" errors="0" skipped="0"><testcase name="actual source"/></testsuite>`)
      writeFileSync(path.join(prior, 'aggregate.junit.xml'), '<testsuite tests="100" failures="1" errors="0" skipped="0"><testcase name="previous aggregate"><failure>prior failure</failure></testcase></testsuite>')
      // Execute the dashboard's own program, including on Windows CI.
      assert.ok(program.startsWith('const fs'))
      const result = spawnSync(process.execPath, ['-', root, 'tests/baseline/current'], { input: program, encoding: 'utf8' })
      assert.ifError(result.error)
      assert.equal(result.status, genuineFailure ? 1 : 0, result.stderr || result.stdout)
      const report = JSON.parse(readFileSync(path.join(baseline, 'current/summary.json'), 'utf8'))
      assert.equal(report.totals.tests, 3)
      assert.equal(report.totals.red, genuineFailure ? 1 : 0)
      assert.equal(report.totals.green, genuineFailure ? 2 : 3)
      assert.equal(report.groups.some((group) => group.group === 'audit-go-dashboard'), false)
    } finally {
      rmSync(root, { recursive: true, force: true })
    }
  })
}
