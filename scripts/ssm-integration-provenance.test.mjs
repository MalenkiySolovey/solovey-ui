import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const inputs = ['go.mod', 'go.sum', 'scripts/ssm-integration-provenance.mjs', 'deploy/dependencies/sing-box-ssm-integration.json', 'deploy/dependencies/sing-box-ssm-cache.patch']
function fixture(t) {
  const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'solovey-ssm-proof-test-'))
  for (const file of inputs) {
    fs.mkdirSync(path.dirname(path.join(scratch, file)), { recursive: true })
    fs.copyFileSync(path.join(root, file), path.join(scratch, file))
  }
  t.after(() => {
    const actual = fs.realpathSync(scratch)
    assert.equal(path.dirname(actual), fs.realpathSync(os.tmpdir()))
    assert.ok(path.basename(actual).startsWith('solovey-ssm-proof-test-'))
    fs.rmSync(actual, { recursive: true, force: true })
  })
  return scratch
}
function run(source, command = 'check') {
  return spawnSync(process.execPath, [path.join(source, 'scripts/ssm-integration-provenance.mjs'), command], { encoding: 'utf8' })
}

test('package metadata identifies the actual patched dependency', t => {
  const source = fixture(t)
  const result = run(source, 'manifest')
  assert.equal(result.status, 0, result.stderr)
  const manifest = JSON.parse(result.stdout)
  assert.equal(manifest.patched, true)
  assert.equal(manifest.officialByteIdentical, false)
  assert.equal(manifest.semanticBaseline.version, 'v1.14.2')
  const buildInfo = run(source, 'build-info')
  assert.equal(buildInfo.status, 0, buildInfo.stderr)
  assert.ok(buildInfo.stdout.includes(`ssm_source_commit=${manifest.replacement.commit}`))
  assert.ok(buildInfo.stdout.includes(`ssm_module_sum=${manifest.replacement.sum}`))
})

for (const scenario of ['patch', 'module pin', 'checksum']) {
  test(`source provenance fails closed after ${scenario} tampering`, t => {
    const source = fixture(t)
    if (scenario === 'patch') fs.appendFileSync(path.join(source, inputs[4]), '\nchanged\n')
    if (scenario === 'module pin') {
      const mod = path.join(source, 'go.mod')
      fs.writeFileSync(mod, fs.readFileSync(mod, 'utf8').replace('github.com/MalenkiySolovey/sing-box', 'github.com/other/sing-box'))
    }
    if (scenario === 'checksum') fs.writeFileSync(path.join(source, 'go.sum'), '')
    const result = run(source)
    assert.notEqual(result.status, 0)
    assert.match(result.stderr, /mismatch|differs|missing/)
  })
}
