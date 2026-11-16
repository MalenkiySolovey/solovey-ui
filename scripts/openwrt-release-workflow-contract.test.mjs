import assert from 'node:assert/strict'
import fs from 'node:fs'
import test from 'node:test'
import { verifyLinuxReleaseBuilder } from './openwrt-release-workflow-contract.mjs'

const workflow = fs.readFileSync(new URL('../.github/workflows/release.yml', import.meta.url), 'utf8')

test('actual Linux target builds coexist with the separately built release inspector', () => {
  assert.match(workflow, /go build[^\n]+solovey-artifact-info/)
  assert.doesNotThrow(() => verifyLinuxReleaseBuilder(workflow))
})

test('the same release workflow is qualified with Windows line endings', () => {
  assert.doesNotThrow(() => verifyLinuxReleaseBuilder(workflow.replace(/\r?\n/g, '\r\n')))
})

test('a direct Product build inside the Linux release job is rejected', () => {
  const bypass = workflow.replace(
    'bash scripts/build-linux-target.sh --mode release --profile full',
    'go build -o solovey-ui main.go\n          bash scripts/build-linux-target.sh --mode release --profile full',
  )
  assert.notEqual(bypass, workflow)
  assert.throws(() => verifyLinuxReleaseBuilder(bypass), /must use the target builder/)
})

test('target builder calls from another job cannot qualify a missing Linux builder', () => {
  const missing = workflow.replace('  build-linux:', '  other-build:')
  assert.notEqual(missing, workflow)
  assert.throws(() => verifyLinuxReleaseBuilder(missing), /Linux release build job is missing/)
})

test('a target builder missing either profile is rejected', () => {
  for (const profile of ['full', 'core']) {
    const missing = workflow.replace('bash scripts/build-linux-target.sh --mode release --profile ' + profile, 'echo omitted-product-build-' + profile)
    assert.notEqual(missing, workflow)
    assert.throws(() => verifyLinuxReleaseBuilder(missing))
  }
})
