import assert from 'node:assert/strict'
import fs from 'node:fs'
import test from 'node:test'
import { verifyInterruptedRun, verifyRetainedArtifacts, verifyRegistryDescriptorBinding } from './release-resume.mjs'

const targets = JSON.parse(fs.readFileSync(new URL('./release-targets.json', import.meta.url), 'utf8'))
function fixture() {
  const context = { repository: 'owner/project', commit: 'a'.repeat(40), tag: 'v2026.4.0', currentRunID: 124, targets }
  const run = { id: 123, repository: { full_name: context.repository }, path: '.github/workflows/release.yml', event: 'push', head_sha: context.commit, head_branch: context.tag, status: 'completed', conclusion: 'failure', run_number: 59, run_attempt: 1 }
  const names = ['Release preflight', 'test-installer', 'component-profile-checks', 'build-frontend', 'Prepare source, SBOM and dependency notices', 'Assemble and verify complete Linux release', 'build-windows / build-frontend', 'build-windows / verify-windows', 'Canonical OpenWrt x86-64', 'Canonical OpenWrt rockchip-armv8', 'Verify complete OpenWrt and FriendlyWrt candidate', 'build-docker / publication-authority', 'build-docker / publish-manifest', ...targets.linux.map(t => 'Build-' + t.platform), ...targets.windows.flatMap(arch => targets.windowsProfiles.map(p => `build-windows / Build-windows-${arch}-${p}`)), ...targets.docker.map(arch => 'build-docker / build-' + arch)]
  const jobs = names.map(name => ({ name, conclusion: 'success', steps: [] }))
  jobs.push(...targets.extended.linux.map(platform => ({ name: `Build-${targets.extended.provider}-${platform}`, conclusion: 'success', steps: [] })))
  const step = (name, title) => jobs.find(j => j.name === name).steps.push({ name: title, conclusion: 'success' })
  step('Release preflight', 'Verify immutable publication identity before any registry write')
  for (const arch of targets.docker) step('build-docker / build-' + arch, 'Build and push Docker image by digest')
  step('build-docker / publish-manifest', 'Create and push multi-platform manifest')
  jobs.push({ name: 'Publish complete verified release', conclusion: 'failure', steps: [] })
  return { run, jobs, context }
}

test('a failed canonical tag-push publication with complete successful producers is resumable', () => {
  const { run, jobs, context } = fixture(); assert.equal(verifyInterruptedRun(run, jobs, context), true)
  run.event = 'workflow_dispatch'; assert.equal(verifyInterruptedRun(run, jobs, context), true)
})

test('recovery rejects source/tag drift, unrelated events, nonterminal/successful and current runs', () => {
  for (const [key, value] of [['head_sha', 'b'.repeat(40)], ['head_branch', 'v2026.4.1'], ['event', 'pull_request'], ['status', 'in_progress'], ['conclusion', 'success'], ['id', 124], ['path', '.github/workflows/other.yml']]) {
    const { run, jobs, context } = fixture(); run[key] = value
    assert.throws(() => verifyInterruptedRun(run, jobs, context), key)
  }
})

test('preflight artifacts and incomplete or failed package producers cannot become a publication candidate', () => {
  for (const title of ['Build and push Docker image by digest', 'Create and push multi-platform manifest', 'Verify immutable publication identity before any registry write']) {
    const { run, jobs, context } = fixture(); jobs.flatMap(j => j.steps).find(s => s.name === title).conclusion = 'skipped'
    assert.throws(() => verifyInterruptedRun(run, jobs, context), /publication step not completed/)
  }
  for (const name of ['Prepare source, SBOM and dependency notices', 'Canonical OpenWrt x86-64', 'Build-amd64', 'Build-shtorm-extended-amd64', 'build-windows / verify-windows']) {
    const { run, jobs, context } = fixture(); jobs.find(j => j.name === name).conclusion = 'failure'
    assert.throws(() => verifyInterruptedRun(run, jobs, context), name)
  }
  const { run, jobs, context } = fixture(); jobs.pop()
  assert.throws(() => verifyInterruptedRun(run, jobs, context), /missing source-run job/)
})

test('retained candidates require unique immutable digests, complete inventory and live retention', () => {
  const artifact = { id: 1, name: 'complete-linux-release', size_in_bytes: 12, digest: 'sha256:' + 'a'.repeat(64), expired: false, expires_at: new Date(Date.now() + 86400000).toISOString() }
  assert.equal(verifyRetainedArtifacts([artifact], [artifact.name])[0].id, 1)
  for (const items of [[], [artifact, artifact], [{ ...artifact, expired: true }], [{ ...artifact, expires_at: '2000-01-01T00:00:00Z' }], [{ ...artifact, digest: null }], [{ ...artifact, size_in_bytes: 0 }]]) assert.throws(() => verifyRetainedArtifacts(items, [artifact.name]))
})

test('public multiarch index must exactly contain both canonical runtime and attestation producers', () => {
  const runtime = arch => ({ digest: 'sha256:' + arch, platform: { os: 'linux', architecture: arch } })
  const attestation = arch => ({ digest: 'sha256:proof-' + arch, platform: { os: 'unknown', architecture: 'unknown' }, annotations: { 'vnd.docker.reference.type': 'attestation-manifest', 'vnd.docker.reference.digest': 'sha256:' + arch } })
  const producers = ['amd64', 'arm64'].map(arch => ({ manifests: [runtime(arch), attestation(arch)] }))
  const index = { manifests: producers.flatMap(p => p.manifests) }; verifyRegistryDescriptorBinding(index, producers)
  for (const change of [i => i.manifests.pop(), i => { i.manifests[0].digest = 'replacement' }, i => { i.manifests[1].annotations['vnd.docker.reference.digest'] = 'other' }]) {
    const changed = structuredClone(index); change(changed); assert.throws(() => verifyRegistryDescriptorBinding(changed, producers))
  }
})
