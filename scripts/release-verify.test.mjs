import assert from 'node:assert/strict'
import crypto from 'node:crypto'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import test from 'node:test'
import { linuxAssetNames, verifyReleaseSet, verifyWindowsSet } from './release-verify.mjs'

function fixture(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'solovey-release-contract-'))
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }))
  // Digest/signature fixtures only; these are never installation candidates.
  for (const name of linuxAssetNames()) {
    fs.writeFileSync(path.join(dir, name), `contract fixture for ${name}\n`)
    checksum(dir, name)
  }
  const { privateKey, publicKey } = crypto.generateKeyPairSync('ed25519')
  const now = Math.floor(Date.now() / 1000)
  const version = fs.readFileSync('config/identity/version', 'utf8').trim()
  const roots = [{ keyId: 'unit-test-only', publicKey: Buffer.from(publicKey.export({ format: 'jwk' }).x, 'base64url').toString('base64'), state: 'ACTIVE', notBefore: now - 60, notAfter: now + 86400, minSequence: 1 }]
  execFileSync(process.execPath, ['scripts/release-manifest.mjs', '--assets-dir', dir, '--version', version, '--sequence', '10', '--issued-at', String(now), '--channel', 'main', '--key-id', roots[0].keyId], {
    env: { ...process.env, SUI_RELEASE_SIGNING_PRIVATE_KEY_B64: privateKey.export({ format: 'der', type: 'pkcs8' }).toString('base64') }, stdio: 'pipe',
  })
  const options = { version, trustRootsBase64: Buffer.from(JSON.stringify(roots)).toString('base64'), now }
  const editManifest = change => {
    const file = path.join(dir, 'solovey-ui-release.json')
    const envelope = JSON.parse(fs.readFileSync(file, 'utf8'))
    change(envelope)
    envelope.signature = crypto.sign(null, Buffer.from(JSON.stringify(envelope.manifest)), privateKey).toString('base64')
    fs.writeFileSync(file, `${JSON.stringify(envelope)}\n`)
    checksum(dir, 'solovey-ui-release.json')
  }
  return { dir, options, editManifest }
}
function checksum(dir, name) {
  const digest = crypto.createHash('sha256').update(fs.readFileSync(path.join(dir, name))).digest('hex')
  fs.writeFileSync(path.join(dir, `${name}.sha256`), `${digest}  ${name}\n`)
}

test('complete canonical set is bound to signed roles, targets, bytes and checksums', async t => {
  const { dir, options } = fixture(t)
  const result = await verifyReleaseSet(dir, options)
  assert.equal(Object.keys(result.artifacts).length, 16)
  assert.ok(result.artifacts['solovey-ui-linux-arm64.tar.gz'])
  assert.ok(result.artifacts['solovey-ui-core-linux-arm64.tar.gz'])
})
test('symmetric but incomplete platform inventory cannot be published', async t => {
  const { dir, options } = fixture(t)
  for (const name of ['solovey-ui-linux-arm64.tar.gz', 'solovey-ui-core-linux-arm64.tar.gz']) {
    fs.unlinkSync(path.join(dir, name)); fs.unlinkSync(path.join(dir, `${name}.sha256`))
  }
  await assert.rejects(verifyReleaseSet(dir, options), /complete canonical/)
})
test('checksum sidecar cannot bind a different file', async t => {
  const { dir, options } = fixture(t)
  const name = linuxAssetNames()[0]
  fs.writeFileSync(path.join(dir, `${name}.sha256`), fs.readFileSync(path.join(dir, `${name}.sha256`), 'utf8').replace(name, 'unrelated.tar.gz'))
  await assert.rejects(verifyReleaseSet(dir, options), /checksum binding/)
})
test('matching new checksum cannot conceal bytes differing from signed manifest', async t => {
  const { dir, options } = fixture(t)
  const name = linuxAssetNames()[0]
  fs.appendFileSync(path.join(dir, name), 'tamper')
  checksum(dir, name)
  await assert.rejects(verifyReleaseSet(dir, options), /artifact identity/)
})
test('signature and trust failures remain closed', async t => {
  const { dir, options } = fixture(t)
  const file = path.join(dir, 'solovey-ui-release.json')
  const envelope = JSON.parse(fs.readFileSync(file, 'utf8'))
  envelope.signature = Buffer.alloc(64).toString('base64')
  fs.writeFileSync(file, JSON.stringify(envelope))
  checksum(dir, 'solovey-ui-release.json')
  await assert.rejects(verifyReleaseSet(dir, options), /signature mismatch/)
  await assert.rejects(verifyReleaseSet(dir, { ...options, trustRootsBase64: Buffer.from('[]').toString('base64') }), /trust roots/)
})
test('even a signed wrong role, wrong version or expired envelope is rejected', async t => {
  const { dir, options, editManifest } = fixture(t)
  await assert.rejects(verifyReleaseSet(dir, { ...options, version: '1900.0.1' }), /version or validity/)
  await assert.rejects(verifyReleaseSet(dir, { ...options, now: options.now + 8 * 86400 }), /version or validity/)
  editManifest(envelope => { envelope.manifest.artifacts[0].role = 'unknown' })
  await assert.rejects(verifyReleaseSet(dir, options), /role\/target/)
})

test('both Windows archives and their exact checksum bindings are mandatory', async t => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'solovey-windows-inventory-'))
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }))
  // Inventory/digest fixtures, never executable release candidates.
  const names = ['solovey-ui-windows-amd64.zip', 'solovey-ui-windows-arm64.zip']
  for (const name of names) { fs.writeFileSync(path.join(directory, name), Buffer.alloc(24, 1)); checksum(directory, name) }
  assert.deepEqual(await verifyWindowsSet(directory), names)
  fs.appendFileSync(path.join(directory, names[1]), 'tampered')
  await assert.rejects(verifyWindowsSet(directory), /checksum mismatch/)
  fs.unlinkSync(path.join(directory, names[1]))
  fs.unlinkSync(path.join(directory, `${names[1]}.sha256`))
  await assert.rejects(verifyWindowsSet(directory), /incomplete/)
})
