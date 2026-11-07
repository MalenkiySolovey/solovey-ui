import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import test from 'node:test'
import { candidatePanelInventory, requireExternalCandidate, verifyArchiveBuildInfo, verifyCandidate, verifyRecoveryBinding } from './release-candidate.mjs'

test('Windows archive admission binds its emitted commit and fingerprint without a Linux-only tree field', () => {
  const identity = { commit: 'a'.repeat(40), tree: 'b'.repeat(40), fingerprint: 'c'.repeat(64), tag: 'v2026.5.0' }
  const target = { platform: 'windows', arch: 'arm64', profile: 'core', provider: 'shtorm-extended' }
  const fields = { app: 'solovey-ui', version: identity.tag, commit: identity.commit, source_fingerprint: identity.fingerprint,
    platform: 'windows/arm64', profile: 'core', core_provider: 'shtorm-extended' }
  const text = fields => Object.entries(fields).map(([key, value]) => `${key}=${value}`).join('\n') + '\n'
  verifyArchiveBuildInfo(text(fields), target, identity)
  for (const name of ['version', 'commit', 'source_fingerprint', 'platform', 'profile', 'core_provider']) {
    const missing = { ...fields }; delete missing[name]
    assert.throws(() => verifyArchiveBuildInfo(text(missing), target, identity), name)
    assert.throws(() => verifyArchiveBuildInfo(text({ ...fields, [name]: 'different' }), target, identity), name)
  }
  verifyArchiveBuildInfo(text({ ...fields, tree: identity.tree }), target, identity)
  assert.throws(() => verifyArchiveBuildInfo(text({ ...fields, tree: 'd'.repeat(40) }), target, identity), /tree/)
})

test('Linux archive admission retains the additional required tree binding', () => {
  const identity = { commit: 'a'.repeat(40), tree: 'b'.repeat(40), fingerprint: 'c'.repeat(64), tag: 'v2026.5.0' }
  const target = { platform: 'linux', arch: 'amd64', profile: 'full', provider: 'sing-box' }
  const fields = { version: identity.tag, commit: identity.commit, source_fingerprint: identity.fingerprint,
    platform: 'linux/amd64', profile: 'full', core_provider: 'sing-box' }
  const text = values => Object.entries(values).map(([key, value]) => `${key}=${value}`).join('\n') + '\n'
  assert.throws(() => verifyArchiveBuildInfo(text(fields), target, identity), /tree/)
  verifyArchiveBuildInfo(text({ ...fields, tree: identity.tree }), target, identity)
  assert.throws(() => verifyArchiveBuildInfo(text({ ...fields, tree: 'd'.repeat(40) }), target, identity), /tree/)
})

test('new recovery orchestration retains old Official-only products and uses v2 product policy when available', async () => {
  const targets = { linux: [{ platform: 'amd64' }, { platform: 'armv7' }] }
  const unavailable = () => { throw Error('old product has no provider policy module') }
  const legacy = await candidatePanelInventory({}, targets, unavailable)
  assert.equal(legacy.providerAware, false)
  assert.deepEqual(legacy.panels.map(item => item.name), [
    'solovey-ui-linux-amd64.tar.gz', 'solovey-ui-core-linux-amd64.tar.gz',
    'solovey-ui-linux-armv7.tar.gz', 'solovey-ui-core-linux-armv7.tar.gz',
  ])
  assert.deepEqual(await candidatePanelInventory({ schema: 'solovey.release/v1' }, targets, unavailable), legacy)
  const policy = await import('./release-providers.mjs')
  const current = await candidatePanelInventory({ schema: policy.providerSchema }, targets, () => policy)
  assert.equal(current.providerAware, true)
  assert.deepEqual(current.panels, policy.linuxPanelArtifacts())
  await assert.rejects(candidatePanelInventory({ schema: 'unknown' }, targets, () => policy), /unsupported product release schema/)
})

test('candidate staging cannot pollute the immutable source inventory or hide unknown source inputs', t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'release-candidate-'))
  t.after(() => { assert.equal(path.dirname(fs.realpathSync(root)), fs.realpathSync(os.tmpdir())); fs.rmSync(root, { recursive: true, force: true }) })
  const checkout = path.join(root, 'product'), outside = path.join(root, 'candidate')
  fs.mkdirSync(checkout); fs.mkdirSync(outside)
  execFileSync('git', ['init', '--quiet', checkout])
  fs.writeFileSync(path.join(checkout, 'source.txt'), 'immutable source\n')
  execFileSync('git', ['-C', checkout, '-c', 'core.autocrlf=false', 'add', '.'])
  const script = fileURLToPath(new URL('./release-source.mjs', import.meta.url))
  const fingerprint = () => execFileSync(process.execPath, [script, checkout], { encoding: 'utf8' }).trim()
  const before = fingerprint()
  fs.writeFileSync(path.join(outside, 'solovey-ui-provenance.json'), '{"source":"candidate"}\n')
  requireExternalCandidate(checkout, outside)
  assert.equal(fingerprint(), before)
  const contaminated = path.join(checkout, 'metadata-assets'); fs.mkdirSync(contaminated)
  fs.copyFileSync(path.join(outside, 'solovey-ui-provenance.json'), path.join(contaminated, 'solovey-ui-provenance.json'))
  assert.notEqual(fingerprint(), before, 'retain the strong untracked-source inventory')
  assert.throws(() => requireExternalCandidate(checkout, contaminated), /outside/)
  assert.throws(() => requireExternalCandidate(checkout, checkout), /outside/)
})

function original() {
  const actual = { commit: 'a'.repeat(40), tag: 'v2026.4.0', fingerprint: 'b'.repeat(64), envelopeSHA256: 'c'.repeat(64), sequence: 59001,
    assets: [{ name: 'payload', size: 12, sha256: 'd'.repeat(64) }, { name: 'payload.sha256', size: 74, sha256: 'e'.repeat(64) }] }
  return { actual, receipt: { schema: 'solovey.release-recovery/v1', sourceRun: { id: 123, number: 59, attempt: 1 }, ...structuredClone(actual) } }
}

test('recovery preserves source, signature issuance and every original transferred byte', () => {
  const { actual, receipt } = original(); verifyRecoveryBinding(receipt, actual, '123')
  for (const field of ['commit', 'tag', 'fingerprint', 'envelopeSHA256', 'sequence']) {
    const changed = structuredClone(actual); changed[field] = field === 'sequence' ? 59002 : 'changed'
    assert.throws(() => verifyRecoveryBinding(receipt, changed, '123'), field)
  }
  for (const change of [a => { a[0].sha256 = 'f'.repeat(64) }, a => { a[0].size++ }, a => { a.pop() }, a => { a.push({ name: 'extra' }) }]) {
    const changed = structuredClone(actual); change(changed.assets)
    assert.throws(() => verifyRecoveryBinding(receipt, changed, '123'), /original candidate byte/)
  }
  assert.throws(() => verifyRecoveryBinding(receipt, actual, '124'))
  const reissued = structuredClone(receipt); reissued.sourceRun.attempt = 2
  assert.throws(() => verifyRecoveryBinding(reissued, actual, '123'))
})

test('a recovery cannot bypass the original-byte receipt', async () => {
  await assert.rejects(verifyCandidate('unused', { commit: 'a'.repeat(40), fingerprint: 'b'.repeat(64), receiptRequired: true }), /original-byte receipt/)
})
