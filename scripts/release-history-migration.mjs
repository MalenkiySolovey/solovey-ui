// One-time owner-authorized correction, not a general release overwrite API.
// Ordinary release publication retains its immutable-public-release gate.
import assert from 'node:assert/strict'
import crypto from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { linuxAssetNames, verifyReleaseSet } from './release-verify.mjs'
import { validateReleaseSigning } from './release-preflight.mjs'

const repository = 'MalenkiySolovey/solovey-ui'
const packageName = 'malenkiysolovey/solovey-ui'
const oldCommit = 'eabfe4793c25daf46fbff61f6cd98059d286af68'
const stableCommit = '5a48a020e9ac62da03d223b2cf325d470cee88db'
const betaCommit = 'b5a3bdb7af115535d5ef52d39a10ba95b6408177'
const history = '.github/release-history/v2026.3.2'
const original = JSON.parse(fs.readFileSync(`${history}/github-release.json`))
const stableReceipt = JSON.parse(fs.readFileSync(`${history}/stable-baseline.json`))
const oldImage = JSON.parse(fs.readFileSync(`${history}/ghcr.json`)).indexDigest
const stableImage = 'sha256:ebec3dda05de966f5957c03b668924a8409a4e81cbe67a4d44d5f17cc116a367'
const execute = process.env.MIGRATION_EXECUTE === 'true'
const roots = process.env.SUI_RELEASE_TRUST_ROOTS_B64
assert.equal(process.env.GITHUB_REPOSITORY, repository)
assert.ok(process.env.GH_TOKEN)
fs.mkdirSync('.migration', { recursive: true })
const receipt = { execute, original: oldCommit, replacement: betaCommit, stable: stableCommit, steps: [] }
function record(step) {
  receipt.steps.push(step)
  fs.writeFileSync('.migration/receipt.json', `${JSON.stringify(receipt, null, 2)}\n`)
  console.log(step)
}
const sha = bytes => `sha256:${crypto.createHash('sha256').update(bytes).digest('hex')}`
async function api(endpoint, method = 'GET', body) {
  const response = await fetch(`https://api.github.com/${endpoint}`, {
    method, headers: { Authorization: `Bearer ${process.env.GH_TOKEN}`, Accept: 'application/vnd.github+json', 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(60000),
  })
  if (response.status === 404 && method === 'GET') return null
  if (!response.ok) throw new Error(`GitHub ${method} ${endpoint}: ${response.status}`)
  return response.status === 204 ? null : response.json()
}
const repo = endpoint => `repos/${repository}/${endpoint}`
const assets = release => release.assets.map(({ name, size, digest }) => ({ name, size, digest })).sort((a, b) => a.name.localeCompare(b.name))
async function checkedRelease(tag, commit, prerelease) {
  const release = await api(repo(`releases/tags/${tag}`))
  assert.ok(release && !release.draft)
  assert.equal(release.target_commitish, commit)
  assert.equal(release.prerelease, prerelease)
  assert.equal(release.assets.length, 42)
  let ref = (await api(repo(`git/ref/tags/${tag}`))).object
  for (let depth = 0; ref.type === 'tag' && depth < 4; depth++) ref = (await api(repo(`git/tags/${ref.sha}`))).object
  assert.equal(ref.type, 'commit')
  assert.equal(ref.sha, commit)
  return release
}
async function downloadLinux(release, directory) {
  fs.mkdirSync(directory, { recursive: true })
  for (const name of [...linuxAssetNames(), 'solovey-ui-release.json'].flatMap(name => [name, `${name}.sha256`])) {
    const asset = release.assets.find(asset => asset.name === name)
    assert.ok(asset && asset.state === 'uploaded')
    const response = await fetch(asset.browser_download_url, { signal: AbortSignal.timeout(300000) })
    assert.ok(response.ok)
    const bytes = Buffer.from(await response.arrayBuffer())
    assert.equal(bytes.length, asset.size)
    assert.equal(sha(bytes), asset.digest)
    fs.writeFileSync(path.join(directory, name), bytes)
  }
  await verifyReleaseSet(directory, { version: release.tag_name, trustRootsBase64: roots })
  return JSON.parse(fs.readFileSync(path.join(directory, 'solovey-ui-release.json'))).manifest
}

const stable = await checkedRelease('v2026.3.1', stableCommit, false)
assert.deepEqual(assets(stable), stableReceipt.assets)
const beta = await checkedRelease('v2026.3.2-beta.1', betaCommit, true)
let old = await api(repo('releases/tags/v2026.3.2'))
if (old) {
  assert.equal(old.immutable, false)
  assert.equal(old.target_commitish, oldCommit)
  assert.equal(old.id, original.id)
  assert.deepEqual(assets(old), assets(original))
}
const oldRef = await api(repo('git/ref/tags/v2026.3.2'))
if (oldRef) assert.equal(oldRef.object.sha, '6c9c48f94e29b8b7f11d2a9b0abe936348c79c21')
const betaManifest = await downloadLinux(beta, '.migration/beta')
const stableManifest = await downloadLinux(stable, '.migration/stable')
const channel = await api(repo('releases/tags/channel-main'))
assert.ok(channel && !channel.immutable)
const channelAsset = channel.assets.find(asset => asset.name === 'solovey-ui-release.json')
assert.ok(channelAsset)
const channelResponse = await fetch(channelAsset.browser_download_url)
assert.ok(channelResponse.ok)
const channelBytes = Buffer.from(await channelResponse.arrayBuffer())
assert.equal(sha(channelBytes), channelAsset.digest)
const sequence = Math.max(betaManifest.sequence, stableManifest.sequence, JSON.parse(channelBytes).manifest.sequence) + 1
validateReleaseSigning({ trustRootsBase64: roots, signingKeyID: process.env.SUI_RELEASE_SIGNING_KEY_ID,
  signingPrivateKeyBase64: process.env.SUI_RELEASE_SIGNING_PRIVATE_KEY_B64, sequence })
receipt.sequence = sequence
record('Verified unchanged stable inventory, original identity/mutability, and signed rebuilt beta.1')

// Check registry authority before changing any public identity. Never log tokens.
const auth = Buffer.from(`${process.env.GITHUB_ACTOR}:${process.env.GH_TOKEN}`).toString('base64')
const tokenResponse = await fetch(`https://ghcr.io/token?service=ghcr.io&scope=repository:${packageName}:pull,push`, { headers: { Authorization: `Basic ${auth}` } })
assert.ok(tokenResponse.ok, 'registry authority unavailable')
const bearer = (await tokenResponse.json()).token
const accept = 'application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json'
async function registry(ref, method = 'GET', bytes, mediaType) {
  const response = await fetch(`https://ghcr.io/v2/${packageName}/manifests/${ref}`, {
    method, headers: { Authorization: `Bearer ${bearer}`, Accept: accept, ...(mediaType ? { 'Content-Type': mediaType } : {}) }, body: bytes,
  })
  assert.ok(response.ok, `registry ${method} ${ref}: ${response.status}`)
  const body = Buffer.from(await response.arrayBuffer())
  return { bytes: body, digest: method === 'GET' ? sha(body) : response.headers.get('docker-content-digest'), mediaType: response.headers.get('content-type') }
}
const stableIndex = await registry('v2026.3.1')
assert.equal(stableIndex.digest, stableImage)
const latestIndex = await registry('latest')
assert.ok([oldImage, stableImage].includes(latestIndex.digest))
const versions = []
for (let page = 1; page <= 20; page++) {
  const batch = await api(`users/MalenkiySolovey/packages/container/solovey-ui/versions?per_page=100&page=${page}`)
  assert.ok(Array.isArray(batch))
  versions.push(...batch)
  if (batch.length < 100) break
  assert.ok(page < 20, 'package inventory exceeds bound')
}
const oldVersion = versions.find(item => item.name === oldImage)
assert.ok(versions.filter(item => item.metadata.container.tags.includes('v2026.3.2')).every(item => item.name === oldImage))
if (oldVersion) assert.ok(oldVersion.metadata.container.tags.every(tag => ['v2026.3.2', 'latest'].includes(tag)), 'old registry version has unrelated tags')
record('Verified pinned stable registry bytes and bounded old package identity')
if (!old && !oldRef && !oldVersion) {
  assert.equal(latestIndex.digest, stableImage)
  assert.equal((await api(repo('releases/latest'))).tag_name, 'v2026.3.1')
  assert.equal(JSON.parse(channelBytes).manifest.version, '2026.3.1')
  record('ALREADY_COMPLETE: no mutation')
  process.exit(0)
}

// Sign the SAME old stable payload from its original source, with a fresh
// sequence. Neither the v3.1 Release nor its source/BUILD_INFO is rewritten.
const stableSource = path.resolve('.migration/stable-source')
fs.mkdirSync(stableSource, { recursive: true })
const archive = path.resolve('.migration/stable-source.tar')
execFileSync('git', ['archive', '--format=tar', `--output=${archive}`, stableCommit])
execFileSync('tar', ['-xf', archive, '-C', stableSource])
execFileSync(process.execPath, [path.join(stableSource, 'scripts/release-manifest.mjs'), '--version', '2026.3.1', '--sequence', String(sequence), '--channel', 'main', '--issued-at', String(Math.floor(Date.now() / 1000)), '--key-id', process.env.SUI_RELEASE_SIGNING_KEY_ID, '--assets-dir', path.resolve('.migration/stable')], { cwd: stableSource, stdio: 'inherit' })
await verifyReleaseSet('.migration/stable', { version: 'v2026.3.1', trustRootsBase64: roots })
receipt.stableEnvelopeDigest = sha(fs.readFileSync('.migration/stable/solovey-ui-release.json'))
receipt.stableImageDigest = stableImage
record('Fresh monotonic main envelope verifies over unchanged v3.1 payload')
if (!execute) {
  record('DRY_RUN_PASS: no external mutation')
  process.exit(0)
}

// All replacement bytes and authority are verified before the first mutation.
await registry('latest', 'PUT', stableIndex.bytes, stableIndex.mediaType)
assert.equal((await registry('latest')).digest, stableImage)
record('Restored GHCR latest to exact existing stable index')
const payload = fs.readdirSync('.migration/stable').filter(name => !name.startsWith('solovey-ui-release.json')).map(name => `.migration/stable/${name}`)
execFileSync('gh', ['release', 'upload', 'channel-main', ...payload, '--clobber', '--repo', repository], { stdio: 'inherit' })
// Authorizing envelope is replaced LAST; mixed transport bytes fail closed.
execFileSync('gh', ['release', 'upload', 'channel-main', '.migration/stable/solovey-ui-release.json', '.migration/stable/solovey-ui-release.json.sha256', '--clobber', '--repo', repository], { stdio: 'inherit' })
const channelAfter = await api(repo('releases/tags/channel-main'))
const expectedChannel = fs.readdirSync('.migration/stable').map(name => {
  const bytes = fs.readFileSync(path.join('.migration/stable', name))
  return { name, size: bytes.length, digest: sha(bytes) }
}).sort((a, b) => a.name.localeCompare(b.name))
assert.deepEqual(assets(channelAfter), expectedChannel)
record('Restored signed main channel with higher sequence')
old = await api(repo('releases/tags/v2026.3.2'))
if (old) {
  assert.equal(old.immutable, false)
  assert.equal(old.id, original.id)
  assert.equal(old.target_commitish, oldCommit)
  assert.deepEqual(assets(old), assets(original))
  await api(repo(`releases/${old.id}`), 'DELETE')
}
const retiringRef = await api(repo('git/ref/tags/v2026.3.2'))
if (retiringRef) {
  assert.equal(retiringRef.object.sha, '6c9c48f94e29b8b7f11d2a9b0abe936348c79c21')
  await api(repo('git/refs/tags/v2026.3.2'), 'DELETE')
}
record('Retired original misclassified release and tag after replacement verification')
if (oldVersion) await api(`users/MalenkiySolovey/packages/container/solovey-ui/versions/${oldVersion.id}`, 'DELETE')
record('Retired original GHCR version; archived identities remain in source history')
await api(repo(`releases/${stable.id}`), 'PATCH', { make_latest: 'true' })
assert.equal((await api(repo('releases/latest'))).tag_name, 'v2026.3.1')
assert.deepEqual(assets(await checkedRelease('v2026.3.1', stableCommit, false)), stableReceipt.assets)
assert.equal((await registry('latest')).digest, stableImage)
record('MIGRATION_PASS: stable defaults v3.1, coherent beta.1, v3.2 reserved for acceptance')
