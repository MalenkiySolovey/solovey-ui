import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import fs from 'node:fs'
import test from 'node:test'

const targets = JSON.parse(fs.readFileSync(new URL('./release-targets.json', import.meta.url), 'utf8')).linux
function validate(target, changes = {}, naive = target.naive) {
  return spawnSync(process.execPath, ['scripts/release-target.mjs', target.platform, String(naive)], {
    env: { ...process.env, GOOS: 'linux', GOARCH: target.arch, GOARM: target.goarm ?? '', ...changes }, encoding: 'utf8',
  }).status
}
test('all seven supported Linux targets have exact architecture and feature identities', () => {
  assert.deepEqual(targets.map(target => target.platform).sort(), ['386', 'amd64', 'arm64', 'armv5', 'armv6', 'armv7', 's390x'])
  for (const target of targets) {
    assert.equal(validate(target), 0)
    assert.notEqual(validate(target, { GOOS: 'windows' }), 0)
    assert.notEqual(validate(target, { GOARCH: 'invalid' }), 0)
    assert.notEqual(validate(target, {}, !target.naive), 0)
    if (target.goarm) assert.notEqual(validate(target, { GOARM: '8' }), 0)
  }
})
test('ARMv6 cannot link the ARMv7-only Cronet static library', () => {
  const target = targets.find(value => value.platform === 'armv6')
  assert.equal(target.naive, false)
  assert.match(target.bootlin_archive, /^armv6-eabihf--musl--/)
  assert.match(target.bootlin_sha256, /^[a-f0-9]{64}$/)
})
