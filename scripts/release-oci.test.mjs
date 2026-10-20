import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import fs from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'
import { releaseOCIIdentity } from './release-oci.mjs'

const revision = '9555471a21b9b87323c726cb61c2f113b572d83d'
const require = createRequire(import.meta.url)
const YAML = require('../frontend/node_modules/yaml')

test('OCI version and revision bind the exact release and source, including prereleases', () => {
  for (const version of ['2026.4.1', '2026.4.17', '2026.4.2-rc.1']) {
    assert.deepEqual(releaseOCIIdentity(`v${version}`, version, revision), {
      version: `v${version}`, commit: revision, source: 'https://github.com/MalenkiySolovey/solovey-ui',
    })
  }
  const sourceVersion = fs.readFileSync('config/identity/version', 'utf8').trim()
  const cli = spawnSync(process.execPath, ['scripts/release-oci.mjs', `v${sourceVersion}`, revision], { encoding: 'utf8' })
  assert.equal(cli.status, 0, cli.stderr)
  assert.equal(cli.stdout, `version=v${sourceVersion}\ncommit=${revision}\nsource=https://github.com/MalenkiySolovey/solovey-ui\n`)
})

test('missing, malformed or mismatching identities fail before image generation', () => {
  for (const tag of [undefined, '', '2026.4.1', 'v2026.4.0', 'v2026.4.1\nother=value']) {
    assert.throws(() => releaseOCIIdentity(tag, '2026.4.1', revision), /valid source version/)
  }
  for (const version of [undefined, '', 'v2026.4.1', '2026.04.1', '2026.4', '2026.4.1+build', '2026.4.1-RC.1', '2026.4.1-rc.01']) {
    assert.throws(() => releaseOCIIdentity(`v${version}`, version, revision), /valid source version/)
  }
  for (const commit of [undefined, '', revision.slice(0, 12), revision.toUpperCase(), `${revision}\nother=value`]) {
    assert.throws(() => releaseOCIIdentity('v2026.4.1', '2026.4.1', commit), /exact source commit/)
  }
  const cli = spawnSync(process.execPath, ['scripts/release-oci.mjs'], { encoding: 'utf8' })
  assert.notEqual(cli.status, 0)
  assert.equal(cli.stdout, '')
})

test('both platform producers and the final multiarch index consume the same validated identity', () => {
  const workflow = YAML.parse(fs.readFileSync('.github/workflows/docker.yml', 'utf8'))
  const expected = {
    version: '${{ steps.source.outputs.version }}',
    revision: '${{ steps.source.outputs.commit }}',
    source: '${{ steps.source.outputs.source }}',
  }
  assert.deepEqual(workflow.jobs.build.strategy.matrix.include.map(row => row.platform), ['linux/amd64', 'linux/arm64'])
  for (const job of [workflow.jobs.build, workflow.jobs.publish]) {
    const identity = job.steps.find(step => step.id === 'source')
    assert.ok(identity.run.includes('node scripts/release-oci.mjs "$RELEASE_TAG_INPUT" "$(git rev-parse HEAD)"'))
  }
  for (const step of workflow.jobs.build.steps.filter(step => step.uses?.startsWith('docker/build-push-action@'))) {
    for (const [key, value] of Object.entries(expected)) {
      assert.ok(step.with.labels.split('\n').includes(`org.opencontainers.image.${key}=${value}`))
      assert.ok(step.with.annotations.split('\n').includes(`index,manifest:org.opencontainers.image.${key}=${value}`))
    }
    assert.equal(step.with.sbom, true)
    assert.equal(step.with.provenance, 'mode=max')
  }
  const manifest = workflow.jobs.publish.steps.find(step => step.name === 'Create and push multi-platform manifest').run
  assert.ok(manifest.includes('complete amd64/arm64 image digests are required'))
  assert.ok(manifest.includes('$DIGESTS'))
  for (const [key, value] of Object.entries(expected)) assert.ok(manifest.includes(`--annotation "index:org.opencontainers.image.${key}=${value}"`))
  assert.ok(manifest.includes('CHANNEL=latest'))
})
