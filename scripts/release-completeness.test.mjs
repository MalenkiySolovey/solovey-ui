import assert from 'node:assert/strict'
import fs from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import * as policy from './release-providers.mjs'
import * as candidate from './release-candidate.mjs'
import { validateReleaseTrain } from './release-preflight.mjs'

const require = createRequire(import.meta.url)
const YAML = require('../frontend/node_modules/yaml')
const workflow = name => YAML.parse(fs.readFileSync(`.github/workflows/${name}.yml`, 'utf8'))

test('R7 milestone admits only its reviewed feature transition from the real previous stable', () => {
  const train = { stableBaseline: '2026.5.0', candidateBase: '2026.5.0', stage: 'stable', previousStable: '2026.4.1', acceptedPrerelease: null, pendingAcceptance: [] }
  assert.doesNotThrow(() => validateReleaseTrain('2026.5.0', train))
  for (const version of ['2026.4.1', '2026.4.2', '2026.5.0-beta.1', '2026.5.1', '2026.6.0']) assert.throws(() => validateReleaseTrain(version, train))
  for (const mutation of [{ previousStable: '2026.4.0' }, { pendingAcceptance: ['upgrade'] }, { acceptedPrerelease: '2026.5.0-beta.1' }, { candidateBase: '2026.6.0' }]) {
    assert.throws(() => validateReleaseTrain('2026.5.0', { ...train, ...mutation }))
  }
})

test('complete public inventory includes every flavor and keeps legacy Official-only recovery bounded', async () => {
  const names = policy.publicAssetNames()
  assert.equal(names.length, 94)
  assert.equal(new Set(names).size, names.length)
  for (const provider of ['', '-shtorm-extended']) for (const profile of ['', '-core']) {
    for (const { platform } of policy.targets.linux) assert.ok(names.includes(`solovey-ui${provider}${profile}-linux-${platform}.tar.gz`))
    for (const arch of policy.targets.windows) assert.ok(names.includes(`solovey-ui${provider}${profile}-windows-${arch}.zip`))
  }
  for (const provider of ['', '-shtorm-extended']) for (const profile of ['x86-64', 'rockchip-armv8']) assert.ok(names.includes(`solovey-ui${provider}-openwrt-${profile}.apk`))
  assert.equal(names.filter(name => name === 'solovey-ui-friendlywrt-storage.json').length, 1)
  assert.ok(!names.some(name => name.endsWith('friendlywrt.apk')))
  const legacy = policy.publicAssetNames(policy.legacySchema)
  assert.equal(legacy.length, 54)
  assert.ok(legacy.every(name => !name.includes('shtorm-extended')))
  const inventory = await candidate.candidateAssetInventory({ schema: policy.providerSchema }, policy.targets)
  assert.deepEqual(inventory.names, names)
  assert.equal(inventory.windows.length, 8)
  const old = structuredClone(policy.targets); delete old.extended
  const recovered = await candidate.candidateAssetInventory({}, old)
  assert.deepEqual(recovered.names, legacy)
  assert.equal(recovered.windows.length, 4)
})

test('canonical release calls both OCI flavors after all package and disclosure gates', () => {
  const release = workflow('release')
  for (const [name, provider] of [['build-docker', 'sing-box'], ['build-docker-extended', 'shtorm-extended']]) {
    const job = release.jobs[name]
    assert.ok(job, `missing canonical ${provider} OCI producer`)
    assert.equal(job.uses, './.github/workflows/docker.yml')
    assert.equal(job.with.core_provider, provider)
    assert.equal(job.if, "inputs.resume_run_id == ''")
    for (const prerequisite of ['release-preflight', 'assemble-linux', 'build-windows', 'verify-openwrt', 'release-metadata']) assert.ok(job.needs.includes(prerequisite))
    assert.ok(release.jobs['publish-linux'].needs.includes(name))
    assert.ok(release.jobs['publish-linux'].if.includes(`needs.${name}.result == 'success'`))
  }
})

test('parallel OCI flavors consume only their two exact digest artifacts', () => {
  const downloads = workflow('docker').jobs.publish.steps.filter(step => step.uses?.startsWith('actions/download-artifact@'))
  assert.deepEqual(downloads.map(step => step.with.name).sort(), [
    'docker-digest-amd64${{ steps.source.outputs.artifact_suffix }}',
    'docker-digest-arm64${{ steps.source.outputs.artifact_suffix }}',
  ])
  assert.ok(downloads.every(step => !step.with.pattern))
})

test('canonical APK matrix passes both providers to the existing package owner', () => {
  const job = workflow('release').jobs['build-openwrt']
  assert.deepEqual(job.strategy.matrix.provider, ['sing-box', 'shtorm-extended'])
  const producer = job.steps.find(step => step.name === 'Execute immutable canonical package producer and admission')
  assert.ok(producer.run.endsWith('"${{ matrix.provider }}"'))
  assert.ok(job.steps.find(step => step.uses?.startsWith('actions/upload-artifact@')).with.name.includes('matrix.provider'))
  const targets = policy.openwrtBuildTargets()
  assert.equal(targets.length, 4)
  assert.equal(new Set(targets.map(item => item.name)).size, 4)
})
