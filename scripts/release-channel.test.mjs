import assert from 'node:assert/strict'
import { createHash, generateKeyPairSync, sign } from 'node:crypto'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { resolveProviderSource } from './core-provider-build.mjs'
import { canonicalReleaseManifest, providerSchema, verifyReleaseEnvelope, withArtifactInspector } from './release-providers.mjs'
import { comparePublished, inspectPublished, renewEnvelope } from './release-channel.mjs'

test('renewal reuses only the authenticated stable payload and still fails closed', async t => {
  const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'solovey-channel-test-'))
  t.after(() => {
    assert.equal(path.dirname(fs.realpathSync(scratch)), fs.realpathSync(os.tmpdir()))
    fs.rmSync(scratch, { recursive: true, force: true })
  })
  const file = path.join(scratch, 'solovey-ui-release.json')
  const { privateKey, publicKey } = generateKeyPairSync('ed25519')
  const signingKeyID = 'test-key'
  const signingPrivateKeyBase64 = privateKey.export({ format: 'der', type: 'pkcs8' }).toString('base64')
  const now = 1_800_000_100
  const trustRoots = [{ keyId: signingKeyID, publicKey: Buffer.from(publicKey.export({ format: 'jwk' }).x, 'base64url').toString('base64'),
    state: 'ACTIVE', notBefore: now - 101, notAfter: now + 8 * 86400, minSequence: 1, maxSequence: 12 }]
  const trustRootsBase64 = Buffer.from(JSON.stringify(trustRoots)).toString('base64')
  const settings = { signingKeyID, signingPrivateKeyBase64, trustRootsBase64, now, sequence: 11 }
  const version = '2026.5.1'
  const manifest = { schema: providerSchema, releaseId: 'solovey-ui-main-10', sequence: 10, version, channel: 'main',
    issuedAt: now - 100, expiresAt: now - 1, deploymentRevision: 'a'.repeat(64), minimumPanelVersion: version,
    maximumPanelVersion: version, minimumCoreSchema: '1.13', maximumCoreSchema: '1.13', targetCoreSchema: '1.13',
    brokerCapability: 'broker-capabilities-1.3', migrationSetDigest: 'b'.repeat(64), releaseNotesDigest: 'c'.repeat(64),
    restartClass: 'stack', rebootClass: 'operator-advisory', rollbackClass: 'manual-recovery', components: [],
    coreProviders: [resolveProviderSource().source],
    artifacts: ['core', 'full'].map(role => ({ name: `fixture-${role}.tar.gz`, role: `panel-${role}`, platform: 'linux', arch: 'amd64',
      mediaType: 'application/gzip', size: 1, sha256: 'd'.repeat(64), provenance: 'github-actions', coreProvider: 'sing-box' })),
  }
  const registry = (bytes, tag_name) => ({ tag_name, assets: [
    { name: path.basename(file), size: bytes.length, digest: 'sha256:' + createHash('sha256').update(bytes).digest('hex'), state: 'uploaded' },
    ...manifest.artifacts.map(({ name, size, sha256 }) => ({ name, size, digest: `sha256:${sha256}`, state: 'uploaded' })),
  ] })
  await withArtifactInspector(process.env.SOLOVEY_ARTIFACT_INFO, inspector => {
    const canonical = canonicalReleaseManifest(inspector, manifest)
    const envelope = { schema: providerSchema, keyId: signingKeyID, algorithm: 'Ed25519', manifest: JSON.parse(canonical),
      signature: sign(null, canonical, privateKey).toString('base64') }
    const tag = `v${version}`
    const original = Buffer.from(JSON.stringify(envelope))
    fs.writeFileSync(file, original)
    const accepted = inspectPublished(inspector, file, registry(original, tag), trustRoots, now)
    assert.throws(() => verifyReleaseEnvelope(inspector, file, { trustRoots, now, channel: 'main' }), /authenticated runtime/)
    const damaged = registry(original, tag)
    damaged.assets[1].digest = 'sha256:' + 'e'.repeat(64)
    assert.throws(() => inspectPublished(inspector, file, damaged, trustRoots, now), /differs from signed release/)

    const forged = Buffer.from(JSON.stringify({ ...envelope, signature: Buffer.alloc(64).toString('base64') }))
    fs.writeFileSync(file, forged)
    assert.throws(() => inspectPublished(inspector, file, registry(forged, tag), trustRoots, now), /authenticated runtime/)

    assert.throws(() => renewEnvelope(inspector, accepted, { ...settings, sequence: 10 }), /sequence must increase/)
    assert.throws(() => renewEnvelope(inspector, accepted, { ...settings, sequence: 13 }), /does not authorize/)
    assert.throws(() => renewEnvelope(inspector, accepted, { ...settings, signingKeyID: 'other' }), /existing stable signer/)
    const renewed = renewEnvelope(inspector, accepted, settings)
    fs.writeFileSync(file, renewed)
    const observed = inspectPublished(inspector, file, registry(renewed, 'channel-main'), trustRoots, now)
    comparePublished(accepted, observed, tag)
    assert.equal(observed.manifest.sequence, 11)
    assert.equal(observed.manifest.expiresAt, now + 7 * 86400)
    assert.notEqual(observed.envelopeDigest, accepted.envelopeDigest)
    verifyReleaseEnvelope(inspector, file, { trustRoots, now, channel: 'main' })
    assert.throws(() => comparePublished(observed, accepted, tag), /predates the immutable release/)
    assert.throws(() => comparePublished(accepted, { ...accepted, envelopeDigest: observed.envelopeDigest }, tag), /sequence was reused/)
    assert.throws(() => comparePublished(accepted, { ...observed, manifest: { ...observed.manifest, rollbackClass: 'automatic' } }, tag), /payload differs/)
  })
})
