// Execution-only recovery retains original bytes; it never builds, signs,
// rewrites checksums or writes the registry.
import assert from 'node:assert/strict'
import crypto from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { pathToFileURL } from 'node:url'
import { requireExternalCandidate, verifyCandidate } from './release-candidate.mjs'

export function verifyInterruptedRun(run, jobs, { repository, commit, tag, currentRunID, targets }) {
  assert.equal(run.repository.full_name.toLowerCase(), repository.toLowerCase())
  assert.equal(run.path, '.github/workflows/release.yml')
  assert.ok(['push', 'workflow_dispatch'].includes(run.event), 'not a canonical publication event')
  assert.equal(run.status, 'completed'); assert.equal(run.conclusion, 'failure')
  assert.notEqual(String(run.id), String(currentRunID))
  assert.ok(Number.isSafeInteger(run.run_number) && Number.isSafeInteger(run.run_attempt) && run.run_attempt > 0)
  if (run.event === 'push') { assert.equal(run.head_sha, commit); assert.equal(run.head_branch, tag) }
  const required = ['Release preflight', 'test-installer', 'component-profile-checks', 'build-frontend',
    'Prepare source, SBOM and dependency notices', 'Assemble and verify complete Linux release',
    'build-windows / build-frontend', 'build-windows / verify-windows',
    'Canonical OpenWrt x86-64', 'Canonical OpenWrt rockchip-armv8', 'Verify complete OpenWrt and FriendlyWrt candidate',
    'build-docker / publication-authority', 'build-docker / publish-manifest',
    ...targets.linux.map(t => 'Build-' + t.platform),
    ...targets.windows.flatMap(arch => targets.windowsProfiles.map(profile => `build-windows / Build-windows-${arch}-${profile}`)),
    ...targets.docker.map(arch => 'build-docker / build-' + arch)]
  assert.equal(new Set(jobs.map(j => j.name)).size, jobs.length, 'ambiguous source-run jobs')
  const job = name => { const found = jobs.find(j => j.name === name); assert.ok(found, `missing source-run job: ${name}`); return found }
  for (const name of required) assert.equal(job(name).conclusion, 'success', name)
  assert.equal(job('Publish complete verified release').conclusion, 'failure')
  for (const item of jobs) assert.ok(['success', 'skipped'].includes(item.conclusion) || item.name === 'Publish complete verified release', 'unclassified source-run failure')
  const step = (name, title) => assert.ok(job(name).steps.some(s => s.name === title && s.conclusion === 'success'), `publication step not completed: ${title}`)
  step('Release preflight', 'Verify immutable publication identity before any registry write')
  for (const arch of targets.docker) step('build-docker / build-' + arch, 'Build and push Docker image by digest')
  step('build-docker / publish-manifest', 'Create and push multi-platform manifest')
  return true
}

export function verifyRetainedArtifacts(artifacts, names) {
  return names.map(name => {
    const matches = artifacts.filter(a => a.name === name)
    assert.equal(matches.length, 1, `missing or ambiguous retained candidate: ${name}`)
    const item = matches[0]
    assert.equal(item.expired, false); assert.ok(item.size_in_bytes > 0)
    assert.match(item.digest, /^sha256:[a-f0-9]{64}$/)
    assert.ok(Date.parse(item.expires_at) > Date.now(), 'retained candidate expired')
    return { id: item.id, name, size: item.size_in_bytes, digest: item.digest, expiresAt: item.expires_at }
  })
}

export function verifyRegistryDescriptorBinding(index, producers) {
  assert.equal(index.manifests.length, 4)
  const runtime = index.manifests.filter(m => m.platform?.os === 'linux' && ['amd64', 'arm64'].includes(m.platform.architecture))
  assert.deepEqual(runtime.map(m => m.platform.architecture).sort(), ['amd64', 'arm64'])
  const expected = producers.flatMap(p => p.manifests.map(m => m.digest)).sort()
  assert.equal(new Set(expected).size, 4)
  assert.deepEqual(index.manifests.map(m => m.digest).sort(), expected, 'public index must equal the canonical producer digests')
  for (const image of runtime) assert.ok(index.manifests.some(m =>
    m.annotations?.['vnd.docker.reference.type'] === 'attestation-manifest' && m.annotations['vnd.docker.reference.digest'] === image.digest))
}

async function resume() {
  const runID = process.env.RESUME_RUN_ID, repository = process.env.GITHUB_REPOSITORY
  const commit = process.env.PRODUCT_COMMIT, fingerprint = process.env.PRODUCT_FINGERPRINT, tag = process.env.RELEASE_TAG_INPUT
  assert.match(runID ?? '', /^[1-9][0-9]+$/); assert.match(repository ?? '', /^[\w.-]+\/[\w.-]+$/)
  assert.match(commit ?? '', /^[a-f0-9]{40}$/); assert.match(fingerprint ?? '', /^[a-f0-9]{64}$/)
  assert.match(tag ?? '', /^v\d+\.\d+\.\d+(?:-[a-z0-9.-]+)?$/)
  const gh = args => execFileSync('gh', args, { encoding: 'utf8', maxBuffer: 16 << 20 })
  const api = endpoint => JSON.parse(gh(['api', `repos/${repository}/${endpoint}`]))
  const run = api(`actions/runs/${runID}`), jobs = [], artifacts = []
  for (const [endpoint, key, destination] of [[`actions/runs/${runID}/attempts/${run.run_attempt}/jobs`, 'jobs', jobs], [`actions/runs/${runID}/artifacts`, 'artifacts', artifacts]]) {
    for (let page = 1; page <= 10; page++) {
      const batch = api(`${endpoint}?per_page=100&page=${page}`)[key]
      assert.ok(Array.isArray(batch)); destination.push(...batch)
      if (batch.length < 100) break
      assert.ok(page < 10, 'source-run inventory exceeds bound')
    }
  }
  const targets = JSON.parse(fs.readFileSync('scripts/release-targets.json', 'utf8'))
  verifyInterruptedRun(run, jobs, { repository, commit, tag, currentRunID: process.env.GITHUB_RUN_ID, targets })
  execFileSync('git', ['merge-base', '--is-ancestor', run.head_sha, process.env.GITHUB_SHA])
  execFileSync('git', ['merge-base', '--is-ancestor', commit, run.head_sha])
  execFileSync(process.execPath, [path.join(process.env.RUNNER_TEMP, 'release-publish-gate.mjs'), 'before', tag], { stdio: 'inherit', env: { ...process.env, GITHUB_SHA: commit } })
  const directory = process.env.RELEASE_CANDIDATE_DIR
  fs.mkdirSync(directory, { recursive: true }); requireExternalCandidate(process.cwd(), directory)
  assert.equal(fs.readdirSync(directory).length, 0, 'recovery destination must be empty')
  const candidates = [['complete-linux-release', 'linux'],
    ...targets.windows.flatMap(arch => targets.windowsProfiles.map(profile => [`solovey-ui${profile === 'core' ? '-core' : ''}-windows-${arch}`, 'windows'])),
    ['complete-release-metadata', 'metadata'], ['complete-openwrt-release', 'openwrt'],
    ...targets.docker.map(arch => ['docker-digest-' + arch, 'digests/' + arch])]
  const retained = verifyRetainedArtifacts(artifacts, candidates.map(([name]) => name))
  for (const [name, destination] of candidates) gh(['run', 'download', runID, '--repo', repository, '--name', name, '--dir', path.join(directory, destination)])
  const actual = await verifyCandidate(directory, { commit, tag, fingerprint, trustRootsBase64: process.env.SUI_RELEASE_TRUST_ROOTS_B64, openwrtVerifier: path.join(process.env.RUNNER_TEMP, 'release-openwrt.mjs') })
  assert.equal(actual.sequence, run.run_number * 1000 + run.run_attempt, 'retain the exact original issuance')
  const image = repository.toLowerCase(), tokenResponse = await fetch(`https://ghcr.io/token?scope=repository:${image}:pull`, { signal: AbortSignal.timeout(30000) })
  assert.ok(tokenResponse.ok); const { token } = await tokenResponse.json()
  async function registry(kind, ref) {
    const response = await fetch(`https://ghcr.io/v2/${image}/${kind}/${ref}`, { headers: {
      Authorization: `Bearer ${token}`, Accept: 'application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json',
    }, signal: AbortSignal.timeout(60000) })
    assert.ok(response.ok, `registry response ${response.status}`)
    const bytes = Buffer.from(await response.arrayBuffer()), digest = 'sha256:' + crypto.createHash('sha256').update(bytes).digest('hex')
    if (ref.startsWith('sha256:')) assert.equal(digest, ref)
    if (kind === 'manifests') assert.equal(response.headers.get('docker-content-digest'), digest)
    return { value: JSON.parse(bytes), digest }
  }
  const index = await registry('manifests', tag)
  assert.equal((await registry('manifests', tag.includes('-') ? 'beta' : 'latest')).digest, index.digest)
  const producers = [], producerDigests = []
  for (const arch of targets.docker) {
    const folder = path.join(directory, 'digests', arch), names = fs.readdirSync(folder)
    assert.equal(names.length, 1); assert.match(names[0], /^[a-f0-9]{64}$/); assert.equal(fs.statSync(path.join(folder, names[0])).size, 0)
    const digest = 'sha256:' + names[0], producer = await registry('manifests', digest)
    assert.ok(producer.value.manifests.some(m => m.platform?.os === 'linux' && m.platform.architecture === arch))
    producers.push(producer.value); producerDigests.push({ architecture: arch, digest })
  }
  verifyRegistryDescriptorBinding(index.value, producers)
  for (const item of index.value.manifests.filter(m => m.platform.os === 'linux')) {
    const manifest = await registry('manifests', item.digest), config = await registry('blobs', manifest.value.config.digest)
    assert.equal(config.value.architecture, item.platform.architecture); assert.equal(config.value.os, 'linux')
    assert.equal(config.value.config.Labels['org.opencontainers.image.revision'], commit)
    assert.equal(config.value.config.User, '65532:65532')
  }
  const receipt = { schema: 'solovey.release-recovery/v1', repository, sourceRun: { id: run.id, number: run.run_number, attempt: run.run_attempt, event: run.event, head: run.head_sha }, ...actual, retainedArtifacts: retained, registryDigest: index.digest, producerDigests }
  const receiptDir = path.join(process.env.RUNNER_TEMP, 'release-recovery-receipt')
  fs.mkdirSync(receiptDir, { recursive: true }); fs.writeFileSync(path.join(receiptDir, 'RECOVERY_RECEIPT.json'), JSON.stringify(receipt, null, 2) + '\n')
  console.log(JSON.stringify({ resumedRun: run.id, productCommit: commit, sourceFingerprint: fingerprint, signedSequence: actual.sequence, envelopeSHA256: actual.envelopeSHA256, assetCount: actual.assets.length, registryDigest: index.digest, status: 'PASS' }))
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) await resume()
