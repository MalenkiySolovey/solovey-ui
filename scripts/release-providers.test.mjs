import assert from 'node:assert/strict'
import fs from 'node:fs'
import { execFileSync, spawnSync } from 'node:child_process'
import test from 'node:test'
import { targets, legacySchema, providerSchema, linuxBuildTargets, linuxPanelArtifacts, installerProviderPolicy } from './release-providers.mjs'
import { linuxAssetNames } from './release-verify.mjs'

test('Official targets and names remain stable while Extended is an explicit Linux amd64 flavor', () => {
  const legacy = linuxBuildTargets(legacySchema)
  assert.equal(legacy.length, 7)
  assert.ok(legacy.every(target => target.provider === 'sing-box' && target.artifact_suffix === ''))
  assert.deepEqual(legacy.map(({ provider, artifact_suffix, ...target }) => target), targets.linux)
  const extended = linuxBuildTargets().filter(target => target.provider !== 'sing-box')
  assert.deepEqual(extended.map(target => target.platform), ['amd64'])
  for (const surface of ['windows', 'docker', 'openwrt']) assert.deepEqual(targets.extended[surface], [])
  assert.equal(linuxAssetNames(legacySchema).length, 15)
  assert.equal(linuxAssetNames(providerSchema).length, 17)
  assert.deepEqual(linuxPanelArtifacts().filter(target => target.provider === 'shtorm-extended').map(target => target.name), [
    'solovey-ui-shtorm-extended-linux-amd64.tar.gz', 'solovey-ui-shtorm-extended-core-linux-amd64.tar.gz',
  ])
  assert.throws(() => linuxBuildTargets('unknown'), /Unsupported release schema/)
})

test('standalone installer embeds the exact generated release policy without a Node dependency', () => {
  const source = fs.readFileSync('install.sh', 'utf8').replaceAll('\r\n', '\n')
  const block = source.match(/# BEGIN GENERATED CORE PROVIDER RELEASE POLICY\n[\s\S]*?# END GENERATED CORE PROVIDER RELEASE POLICY\n/)
  assert.equal(block?.[0], installerProviderPolicy(), 'regenerate with scripts/release-providers.mjs installer-policy')
  assert.ok(!block[0].includes('node '))
})

test('packager and workflow projection reject unsupported provider, target and profile choices', () => {
  const command = ['scripts/release-providers.mjs', 'artifact-name']
  for (const target of linuxPanelArtifacts()) {
    const output = execFileSync(process.execPath, [...command, target.provider, `linux-${target.arch}`, target.profile], { encoding: 'utf8' }).trim()
    assert.equal(output, target.name)
  }
  for (const args of [['shtorm-extended', 'linux-arm64', 'full'], ['unknown', 'linux-amd64', 'core'], ['sing-box', 'linux-amd64', 'unknown']]) {
    assert.notEqual(spawnSync(process.execPath, [...command, ...args], { stdio: 'pipe' }).status, 0)
  }
  assert.deepEqual(JSON.parse(execFileSync(process.execPath, ['scripts/release-providers.mjs', 'matrix'], { encoding: 'utf8' })), { include: linuxBuildTargets() })
})
