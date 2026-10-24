import assert from 'node:assert/strict'
import crypto from 'node:crypto'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import test from 'node:test'
import { resolveProviderSource } from './core-provider-build.mjs'
import { linuxPanelArtifacts, providerSchema, withArtifactInspector, verifyReleaseEnvelope } from './release-providers.mjs'
import { verifyReleaseSet } from './release-verify.mjs'

const full = process.env.SOLOVEY_RELEASE_TEST_FULL
const core = process.env.SOLOVEY_RELEASE_TEST_CORE
const provider = process.env.SOLOVEY_RELEASE_TEST_PROVIDER

test('v2 producer binds both actual Panel profiles to runtime-verifiable source and package bytes', {
  skip: !full && !core && !provider ? 'actual Linux full/core binaries are supplied after the provider CI builds' : false,
}, async t => {
  assert.equal(process.platform, 'linux')
  assert.ok(full && core && ['sing-box', 'shtorm-extended'].includes(provider))
  const source = resolveProviderSource({ provider }).source
  const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'solovey-release-provider-test-'))
  t.after(() => {
    assert.equal(path.dirname(fs.realpathSync(scratch)), fs.realpathSync(os.tmpdir()))
    assert.ok(path.basename(scratch).startsWith('solovey-release-provider-test-'))
    fs.rmSync(scratch, { recursive: true, force: true })
  })
  const assets = path.join(scratch, 'assets'), input = path.join(scratch, 'input'), components = path.join(scratch, 'components')
  fs.mkdirSync(assets); fs.mkdirSync(input); fs.mkdirSync(path.join(components, 'telegram'), { recursive: true })
  fs.writeFileSync(path.join(components, 'telegram', 'component.json'), '{"id":"telegram","version":"1","delivery":"in-process"}\n')
  // Only the Panel binaries are real here. Inert helper fixtures exercise the
  // packaging/signing contract; installed Root/runtime proof has separate tests.
  for (const name of ['solovey-privileged-broker', 'solovey-ssh-proof', 'solovey-broker-manifest', 'solovey-owner-manifest']) {
    fs.writeFileSync(path.join(input, name), '#!/bin/sh\nexit 89\n', { mode: 0o755 })
  }
  for (const [profile, binary] of [['full', full], ['core', core]]) {
    const metadata = path.join(input, `BUILD_INFO.${profile}.txt`)
    fs.writeFileSync(metadata, `app=solovey-ui\nprofile=${profile}\nversion=packaging-test\ncommit=packaging-test\nplatform=linux/amd64\ngo=packaging-test\nsing_box=v${source.runtimeSemanticVersion}\ncore_provider=${provider}\n`)
    execFileSync('bash', ['scripts/release-package-linux.sh', '--target', 'linux-amd64', '--profile', profile, '--core-provider', provider,
      '--binary', path.resolve(binary), '--broker', path.join(input, 'solovey-privileged-broker'), '--proof', path.join(input, 'solovey-ssh-proof'),
      '--manifest-writer', path.join(input, 'solovey-broker-manifest'), '--owner-manifest-writer', path.join(input, 'solovey-owner-manifest'),
      '--build-info', metadata, '--out-dir', assets], { stdio: 'pipe' })
  }
  execFileSync('bash', ['scripts/release-package-components.sh', '--components-dir', components, '--out-dir', assets], { stdio: 'pipe' })
  const { privateKey, publicKey } = crypto.generateKeyPairSync('ed25519')
  const now = Math.floor(Date.now() / 1000)
  const roots = [{ keyId: 'packaging-test', publicKey: Buffer.from(publicKey.export({ format: 'jwk' }).x, 'base64url').toString('base64'),
    state: 'ACTIVE', notBefore: now - 1, notAfter: now + 86400, minSequence: 1 }]
  const version = fs.readFileSync('config/identity/version', 'utf8').trim()
  await withArtifactInspector(process.env.SOLOVEY_ARTIFACT_INFO, async inspector => {
    const generate = (...extra) => execFileSync(process.execPath, ['scripts/release-manifest.mjs', '--schema', providerSchema,
      '--artifact-info', inspector, '--assets-dir', assets, '--components-dir', components, '--version', version, '--sequence', '101',
      '--issued-at', String(now), '--channel', version.includes('-') ? 'beta' : 'main', '--key-id', 'packaging-test', ...extra], {
      env: { ...process.env, SUI_RELEASE_SIGNING_PRIVATE_KEY_B64: privateKey.export({ format: 'der', type: 'pkcs8' }).toString('base64') }, stdio: 'pipe',
    })
    generate()
    const envelopePath = path.join(assets, 'solovey-ui-release.json')
    const envelope = JSON.parse(fs.readFileSync(envelopePath, 'utf8'))
    assert.equal(envelope.schema, providerSchema)
    assert.deepEqual(envelope.manifest.coreProviders, [source])
    assert.deepEqual(envelope.manifest.artifacts.filter(item => item.coreProvider).map(item => item.role).sort(), ['panel-core', 'panel-full'])
    assert.ok(crypto.verify(null, Buffer.from(JSON.stringify(envelope.manifest)), publicKey, Buffer.from(envelope.signature, 'base64')))
    const observed = verifyReleaseEnvelope(inspector, envelopePath, { trustRoots: roots, now, channel: envelope.manifest.channel })
    assert.deepEqual(observed.manifest, envelope.manifest)
    await assert.rejects(verifyReleaseSet(assets, { version, now, artifactInfo: inspector,
      trustRootsBase64: Buffer.from(JSON.stringify(roots)).toString('base64') }), /complete canonical/, 'a valid subset cannot pass publication completeness')

    const selected = linuxPanelArtifacts().filter(item => item.provider === provider && item.arch === 'amd64')
    const fullFile = path.join(assets, selected.find(item => item.profile === 'full').name)
    const coreFile = path.join(assets, selected.find(item => item.profile === 'core').name)
    const original = fs.readFileSync(fullFile)
    fs.copyFileSync(coreFile, fullFile)
    assert.throws(() => generate(), /Observed package profile differs/, 'a valid source cannot conceal a mislabeled component profile')
    fs.writeFileSync(fullFile, original)
    const corrupt = Buffer.from(original); corrupt[corrupt.length - 8] ^= 1
    fs.writeFileSync(fullFile, corrupt)
    assert.throws(() => generate(), /static source inspection failed/, 'a valid Panel cannot conceal a corrupt archive trailer')
    fs.writeFileSync(fullFile, original)
    const other = provider === 'sing-box' ? 'shtorm-extended' : 'sing-box'
    const renamed = linuxPanelArtifacts().find(item => item.provider === other && item.arch === 'amd64' && item.profile === 'full')
    const misleading = path.join(assets, renamed.name)
    fs.renameSync(fullFile, misleading)
    const otherCore = linuxPanelArtifacts().find(item => item.provider === other && item.arch === 'amd64' && item.profile === 'core')
    fs.renameSync(coreFile, path.join(assets, otherCore.name))
    assert.throws(() => generate(), /static source inspection failed/, 'a filename cannot relabel the actual provider')
    console.log(`PROVIDER_RELEASE_ARTIFACT_CONTRACT=PASS provider=${provider} source=${source.sourceRevision} profiles=full,core`)
  })
})
