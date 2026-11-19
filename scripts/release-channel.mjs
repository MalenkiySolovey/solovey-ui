import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { createHash, createPrivateKey, sign } from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import { pathToFileURL } from 'node:url'
import { validateReleaseSigning } from './release-preflight.mjs'
import { canonicalReleaseManifest, providerSchema, verifyReleaseEnvelope } from './release-providers.mjs'

const repository = 'MalenkiySolovey/solovey-ui'
const channel = 'main'
const channelTag = 'channel-main'
const envelopeName = 'solovey-ui-release.json'
const checksumName = `${envelopeName}.sha256`
const day = 86400
const maxEnvelopeBytes = 2 << 20
const stableTag = /^v[0-9]+\.[0-9]+\.[0-9]+$/
const digest = bytes => createHash('sha256').update(bytes).digest('hex')
const checksum = bytes => `${digest(bytes)}  ${envelopeName}\n`
const payload = ({ releaseId, issuedAt, expiresAt, sequence, ...rest }) => rest

function asset(release, name) {
  const matches = (release.assets ?? []).filter(item => item.name === name)
  assert.equal(matches.length, 1, `${release.tag_name}: expected exactly one ${name}`)
  assert.equal(matches[0].state, 'uploaded', `${release.tag_name}: ${name} is not uploaded`)
  return matches[0]
}

function checkBytes(release, name, bytes) {
  const identity = asset(release, name)
  assert.equal(identity.size, bytes.length, `${release.tag_name}: ${name} size changed`)
  assert.equal(identity.digest, `sha256:${digest(bytes)}`, `${release.tag_name}: ${name} digest changed`)
}

// Historical inputs are authenticated at issuance. This does not qualify
// them for live use; the replacement must verify at the current time.
export function inspectPublished(inspector, file, release, trustRoots, now) {
  const size = fs.statSync(file).size
  assert.ok(size > 0 && size <= maxEnvelopeBytes, 'Published envelope is not a bounded file')
  const bytes = fs.readFileSync(file)
  checkBytes(release, envelopeName, bytes)
  const { manifest: { issuedAt } } = JSON.parse(bytes)
  assert.ok(Number.isSafeInteger(issuedAt) && issuedAt > 0 && issuedAt <= now, 'Invalid published issuance time')
  const observed = verifyReleaseEnvelope(inspector, file, { trustRoots, now: issuedAt, channel })
  assert.equal(observed.manifest.schema, providerSchema, 'Channel maintenance requires a signed v2 release')
  assert.ok(Number.isSafeInteger(observed.manifest.sequence), 'Invalid published sequence')
  assert.equal(observed.manifest.releaseId, `solovey-ui-main-${observed.manifest.sequence}`, 'Unexpected release identity')
  for (const item of observed.manifest.artifacts) {
    const published = asset(release, item.name)
    assert.equal(item.provenance, 'github-actions', 'Unexpected signed artifact provenance')
    assert.equal(published.size, item.size, `${release.tag_name}: ${item.name} size differs from signed release`)
    assert.equal(published.digest, `sha256:${item.sha256}`, `${release.tag_name}: ${item.name} differs from signed release`)
  }
  return { ...observed, envelopeDigest: digest(bytes) }
}

export function comparePublished(version, current, tag) {
  assert.ok(stableTag.test(tag), 'Renewal requires an exact stable version tag')
  assert.equal(`v${version.manifest.version}`, tag, 'Immutable envelope version differs from the tag')
  assert.equal(current.keyId, version.keyId, 'Signer differs from the supported stable release')
  assert.deepEqual(payload(current.manifest), payload(version.manifest), 'Live channel payload differs from the immutable release')
  assert.ok(current.manifest.sequence >= version.manifest.sequence, 'Live sequence predates the immutable release')
  assert.ok(current.manifest.issuedAt >= version.manifest.issuedAt, 'Live issuance predates the immutable release')
  if (current.manifest.sequence === version.manifest.sequence) {
    assert.equal(current.envelopeDigest, version.envelopeDigest, 'The immutable sequence was reused with different bytes')
  }
}

export function requireRenewalLeadTime(expiresAt, now) {
  assert.ok(expiresAt > now + 3 * day, 'channel-main expires within three days or has expired; dispatch Release Solovey UI with renew_main=true')
}

export function renewEnvelope(inspector, current, { trustRootsBase64, signingKeyID, signingPrivateKeyBase64, sequence, now }) {
  assert.ok(Number.isSafeInteger(sequence) && sequence > current.manifest.sequence, 'Renewal sequence must increase')
  assert.ok(now >= current.manifest.issuedAt, 'Renewal cannot precede the latest issuance')
  assert.equal(signingKeyID, current.keyId, 'Renewal must use the existing stable signer')
  validateReleaseSigning({ trustRootsBase64, signingKeyID, signingPrivateKeyBase64, sequence, now: new Date(now * 1000) })
  const roots = JSON.parse(Buffer.from(trustRootsBase64, 'base64'))
  const root = roots.find(candidate => candidate.keyId === signingKeyID)
  const expiresAt = now + 7 * day
  assert.ok(root.state === 'ACTIVE' && expiresAt <= root.notAfter, 'Stable signer must remain active for the entire renewed window')
  const canonical = canonicalReleaseManifest(inspector, {
    ...current.manifest, releaseId: `solovey-ui-main-${sequence}`, sequence, issuedAt: now, expiresAt,
  })
  const key = createPrivateKey({ key: Buffer.from(signingPrivateKeyBase64, 'base64'), format: 'der', type: 'pkcs8' })
  return Buffer.from(`${JSON.stringify({
    schema: providerSchema, keyId: signingKeyID, algorithm: 'Ed25519', manifest: JSON.parse(canonical),
    signature: sign(null, canonical, key).toString('base64'),
  })}\n`)
}

function gh(args, options = {}) {
  return execFileSync('gh', args, { encoding: 'utf8', timeout: 30000, maxBuffer: 4 << 20, ...options })
}

function releaseAt(endpoint) {
  return JSON.parse(gh(['api', '--hostname', 'github.com', `repos/${repository}/releases/${endpoint}`]))
}

function fetchPublic(tag, name, file) {
  assert.ok(tag === channelTag || stableTag.test(tag), 'Unexpected public release tag')
  assert.ok(name === envelopeName || name === checksumName, 'Only channel metadata may be downloaded here')
  execFileSync('curl', ['--disable', '--silent', '--show-error', '--fail', '--location', '--max-redirs', '3',
    '--proto', '=https', '--proto-redir', '=https', '--max-time', '25', '--max-filesize', String(maxEnvelopeBytes),
    '--output', file, `https://github.com/${repository}/releases/download/${tag}/${name}`], { timeout: 30000 })
}

async function run() {
  const [mode, directory, tag] = process.argv.slice(2)
  const inspector = process.env.SOLOVEY_ARTIFACT_INFO
  const trustRootsBase64 = process.env.SUI_RELEASE_TRUST_ROOTS_B64
  const trustRoots = JSON.parse(Buffer.from(trustRootsBase64 ?? '', 'base64'))
  const signingKeyID = process.env.SUI_RELEASE_SIGNING_KEY_ID
  const signingPrivateKeyBase64 = process.env.SUI_RELEASE_SIGNING_PRIVATE_KEY_B64
  delete process.env.SUI_RELEASE_SIGNING_PRIVATE_KEY_B64
  assert.ok(inspector && directory && ['check', 'renew'].includes(mode), 'Usage: release-channel.mjs check|renew WORKDIR [STABLE_TAG]')

  const immutable = releaseAt('latest')
  const live = releaseAt(`tags/${channelTag}`)
  assert.ok(!immutable.draft && !immutable.prerelease && stableTag.test(immutable.tag_name), 'Latest release is not a published stable version')
  assert.ok(live.tag_name === channelTag && !live.draft && !live.prerelease && !live.immutable, 'Main channel is not an existing mutable publication')
  assert.ok(mode === 'check' || tag === immutable.tag_name, 'Requested tag is not the latest published stable version')
  fs.mkdirSync(directory, { recursive: true })
  const priorFile = path.join(directory, 'live-before.json')
  const originalFile = path.join(directory, 'version.json')
  fetchPublic(channelTag, envelopeName, priorFile)
  fetchPublic(immutable.tag_name, envelopeName, originalFile)
  const now = Math.floor(Date.now() / 1000)
  const original = inspectPublished(inspector, originalFile, immutable, trustRoots, now)
  const prior = inspectPublished(inspector, priorFile, live, trustRoots, now)
  comparePublished(original, prior, immutable.tag_name)
  describe(prior)

  if (mode === 'check') {
    requireRenewalLeadTime(prior.manifest.expiresAt, now)
    verifyReleaseEnvelope(inspector, priorFile, { trustRoots, now, channel })
    return
  }

  assert.ok(process.env.GITHUB_EVENT_NAME === 'workflow_dispatch' && process.env.GITHUB_REPOSITORY === repository &&
    process.env.GITHUB_REF === 'refs/heads/main', 'Renewal is restricted to manual dispatch from canonical main')
  const runNumber = Number(process.env.GITHUB_RUN_NUMBER)
  const attempt = Number(process.env.GITHUB_RUN_ATTEMPT)
  assert.ok(Number.isSafeInteger(runNumber) && runNumber > 0 && Number.isSafeInteger(attempt) && attempt > 0 && attempt < 1000,
    'Invalid canonical release run sequence')
  const sequence = runNumber * 1000 + attempt
  const bytes = renewEnvelope(inspector, prior, { trustRootsBase64, signingKeyID, signingPrivateKeyBase64, sequence, now })
  const pending = path.join(directory, 'candidate.json')
  fs.writeFileSync(pending, bytes, { mode: 0o600 })
  const verified = verifyReleaseEnvelope(inspector, pending, { trustRoots, now, channel })
  assert.equal(verified.manifest.sequence, sequence)
  assert.deepEqual(payload(verified.manifest), payload(original.manifest))
  const candidate = path.join(directory, envelopeName)
  const sidecar = path.join(directory, checksumName)
  fs.renameSync(pending, candidate)
  fs.writeFileSync(sidecar, checksum(bytes), { mode: 0o600 })

  // Ordinary publication and renewal share the release workflow lock.
  const latest = releaseAt('latest')
  const beforeWrite = releaseAt(`tags/${channelTag}`)
  assert.equal(latest.id, immutable.id, 'Latest stable release changed during renewal')
  assert.equal(beforeWrite.id, live.id, 'Channel release changed during renewal')
  inspectPublished(inspector, originalFile, latest, trustRoots, now)
  inspectPublished(inspector, priorFile, beforeWrite, trustRoots, now)
  gh(['release', 'upload', channelTag, sidecar, candidate, '--clobber', '--repo', repository], { stdio: 'inherit' })

  const readBack = path.join(directory, 'live-after.json')
  const readBackSidecar = path.join(directory, 'live-after.sha256')
  for (let attempt = 0; ; attempt++) {
    try {
      fetchPublic(channelTag, envelopeName, readBack)
      fetchPublic(channelTag, checksumName, readBackSidecar)
      assert.equal(digest(fs.readFileSync(readBack)), digest(bytes), 'Public channel did not return the renewed envelope')
      assert.equal(fs.readFileSync(readBackSidecar, 'utf8'), checksum(bytes), 'Public channel checksum does not match the renewal')
      break
    } catch (error) {
      if (attempt === 4) throw error
      await new Promise(resolve => setTimeout(resolve, 3000))
    }
  }
  const after = releaseAt(`tags/${channelTag}`)
  assert.equal(after.id, live.id, 'Channel release changed during publication')
  checkBytes(after, checksumName, fs.readFileSync(readBackSidecar))
  const renewed = inspectPublished(inspector, readBack, after, trustRoots, Math.floor(Date.now() / 1000))
  verifyReleaseEnvelope(inspector, readBack, { trustRoots, now: Date.now() / 1000, channel })
  comparePublished(original, renewed, immutable.tag_name)
  assert.equal(renewed.manifest.sequence, sequence)
  describe(renewed)
}

function describe(verified) {
  const { keyId, envelopeDigest, manifest: { version, sequence, issuedAt, expiresAt } } = verified
  console.log(`channel-main: v${version}, signer ${keyId}, sequence ${sequence}, issued ${new Date(issuedAt * 1000).toISOString()}, expires ${new Date(expiresAt * 1000).toISOString()}, envelope sha256:${envelopeDigest}`)
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  try { await run() } catch (error) { console.error(`Channel maintenance failed: ${error.message}`); process.exitCode = 1 }
}
