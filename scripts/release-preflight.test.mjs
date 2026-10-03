import assert from 'node:assert/strict'
import { generateKeyPairSync } from 'node:crypto'
import test from 'node:test'

import { validateReleaseSigning, validateReleaseTrain } from './release-preflight.mjs'

test('qualification train cannot accidentally publish a stable patch', () => {
  const train = { stableBaseline: '2026.3.1', candidateBase: '2026.3.2', stage: 'qualification' }
  for (const version of ['2026.3.2-beta.1', '2026.3.2-beta.2', '2026.3.2-beta.10']) {
    assert.doesNotThrow(() => validateReleaseTrain(version, train))
  }
  for (const version of ['2026.3.1', '2026.3.2', '2026.3.3', '2026.3.2-beta.01', '2026.3.1.patch1']) {
    assert.throws(() => validateReleaseTrain(version, train), /acceptance is pending/)
  }
})

test('stable train admits only its accepted stable identity', () => {
  const train = {
    stableBaseline: '2026.3.2', candidateBase: '2026.3.2', stage: 'stable',
    previousStable: '2026.3.1', acceptedPrerelease: '2026.3.2-beta.10', pendingAcceptance: [],
  }
  assert.doesNotThrow(() => validateReleaseTrain('2026.3.2', train))
  for (const version of ['2026.3.1', '2026.3.3', '2026.3.2-beta.10']) {
    assert.throws(() => validateReleaseTrain(version, train), /coherent accepted release train/)
  }
  for (const mutation of [
    { stableBaseline: '2026.3.1' }, { previousStable: '2026.3.2' },
    { acceptedPrerelease: '2026.3.2-beta.9' }, { pendingAcceptance: ['platform'] },
    { pendingAcceptance: undefined },
  ]) {
    assert.throws(() => validateReleaseTrain('2026.3.2', { ...train, ...mutation }), /coherent accepted release train/)
  }
  assert.throws(() => validateReleaseTrain('2026.3.2', { ...train, stage: 'unknown' }), /reviewed policy transition/)
})

function fixture(overrides = {}) {
  const { privateKey, publicKey } = generateKeyPairSync('ed25519')
  const now = new Date('2026-08-15T00:00:00Z')
  const keyID = 'release-2026-01'
  const root = {
    keyId: keyID,
    publicKey: publicKey.export({ format: 'jwk' }).x,
    state: 'ACTIVE',
    notBefore: Math.floor(now.getTime() / 1000) - 60,
    notAfter: Math.floor(now.getTime() / 1000) + 86400,
    minSequence: 1,
  }
  root.publicKey = Buffer.from(root.publicKey, 'base64url').toString('base64')
  return {
    trustRootsBase64: Buffer.from(JSON.stringify([root])).toString('base64'),
    signingKeyID: keyID,
    signingPrivateKeyBase64: privateKey.export({ format: 'der', type: 'pkcs8' }).toString('base64'),
    sequence: 2,
    now,
    ...overrides,
  }
}

test('accepts one consistent Ed25519 release authority', () => {
  assert.deepEqual(validateReleaseSigning(fixture()), { keyID: 'release-2026-01', rootCount: 1 })
})

test('rejects missing release configuration by public name', () => {
  assert.throws(
    () => validateReleaseSigning(fixture({ trustRootsBase64: '', signingPrivateKeyBase64: '' })),
    /SUI_RELEASE_TRUST_ROOTS_B64 repository variable.*SUI_RELEASE_SIGNING_PRIVATE_KEY_B64 repository secret/,
  )
})

test('rejects a private key that does not match the public trust root', () => {
  const other = generateKeyPairSync('ed25519').privateKey.export({ format: 'der', type: 'pkcs8' }).toString('base64')
  assert.throws(() => validateReleaseSigning(fixture({ signingPrivateKeyBase64: other })), /does not match/)
})

test('rejects malformed, retired, expired, and sequence-ineligible roots', () => {
  assert.throws(() => validateReleaseSigning(fixture({ trustRootsBase64: 'not-base64' })), /encoding is invalid/)

  for (const mutation of [
    (root) => { root.state = 'RETIRED' },
    (root) => { root.notAfter = root.notBefore + 1 },
    (root) => { root.minSequence = 3 },
  ]) {
    const values = fixture()
    const roots = JSON.parse(Buffer.from(values.trustRootsBase64, 'base64').toString('utf8'))
    mutation(roots[0])
    values.trustRootsBase64 = Buffer.from(JSON.stringify(roots)).toString('base64')
    assert.throws(() => validateReleaseSigning(values))
  }
})
