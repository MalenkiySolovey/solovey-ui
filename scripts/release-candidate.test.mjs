import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import test from 'node:test'
import { requireExternalCandidate, verifyCandidate, verifyRecoveryBinding } from './release-candidate.mjs'

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
