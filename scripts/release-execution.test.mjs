import assert from 'node:assert/strict'
import fs from 'node:fs'
import { createRequire } from 'node:module'
import test from 'node:test'

const require = createRequire(import.meta.url)
const YAML = require('../frontend/node_modules/yaml')
const read = name => YAML.parse(fs.readFileSync(`.github/workflows/${name}.yml`, 'utf8'))
const release = read('release')
const productRef = '${{ needs.release-preflight.outputs.product-commit }}'
const checkouts = job => job.steps?.filter(step => step.uses?.startsWith('actions/checkout@')) ?? []

test('orchestrator is validated before immutable product checkout and resolution', () => {
  const job = release.jobs['release-preflight']
  assert.deepEqual(checkouts(job).map(step => step.with.ref), [
    '${{ github.sha }}', 'refs/tags/${{ env.RELEASE_TAG_INPUT }}',
  ])
  const validation = job.steps.findIndex(step => step.run?.includes('release-execution.test.mjs'))
  const product = job.steps.findIndex(step => step.name === 'Check out immutable product source')
  const resolution = job.steps.findIndex(step => step.id === 'product')
  assert.ok(validation < product && product < resolution)
  assert.match(job.steps[resolution].run, /git rev-parse HEAD/)
  assert.match(job.steps[resolution].run, /node scripts\/release-source.mjs/)
  const gate = job.steps.find(step => step.run?.includes('release-publish-gate.mjs" before'))
  assert.equal(gate.if, undefined)
  assert.equal(gate.env.PRODUCT_COMMIT, '${{ steps.product.outputs.commit }}')
})

test('every product checkout and reusable build is bound to resolved product source', () => {
  for (const [name, job] of Object.entries(release.jobs)) {
    if (name === 'release-preflight') continue
    assert.ok([job.needs].flat().includes('release-preflight'), name)
    if (['resume-candidate', 'build-openwrt', 'verify-openwrt', 'publish-linux'].includes(name)) {
      assert.deepEqual(checkouts(job).map(step => step.with.ref), ['${{ github.sha }}', productRef])
      continue
    }
    for (const checkout of checkouts(job)) assert.equal(checkout.with.ref, productRef, name)
    if (job.uses) assert.equal(job.with.source_commit, productRef, name)
  }
  for (const name of ['windows', 'docker']) {
    const workflow = read(name)
    assert.equal(workflow.on.workflow_call.inputs.source_commit.required, true)
    for (const job of Object.values(workflow.jobs)) {
      for (const checkout of checkouts(job)) {
        assert.equal(checkout.with.ref, "${{ inputs.source_commit || format('refs/tags/{0}', inputs.tag) }}")
      }
    }
  }
})

test('Windows producer writes canonical checksum bytes and verifies the pair before eligibility', () => {
  const workflow = read('windows')
  const checksum = workflow.jobs['build-windows'].steps.find(step => step.name === 'Write package checksum')
  assert.match(checksum.run, /createReadStream\(name\)/)
  assert.ok(checksum.run.includes("${hash.digest('hex')}  ${name}\\n"))
  const gate = workflow.jobs['verify-windows']
  assert.equal(gate.needs, 'build-windows')
  assert.ok(gate.steps.some(step => step.run?.includes('scripts/release-verify.mjs windows-assets')))
  assert.equal(gate.if, undefined)
})

test('interrupted publication recovery is gated and never rebuilds or overwrites registry', () => {
  assert.equal(release.jobs['build-linux'].if, "inputs.resume_run_id == ''")
  assert.equal(release.jobs['build-docker'].if, "inputs.resume_run_id == ''")
  assert.equal(release.jobs['build-windows'].if, "inputs.resume_run_id == ''")
  const recovery = release.jobs['resume-candidate']
  assert.equal(recovery.if, "inputs.resume_run_id != ''")
  assert.ok(recovery.steps.some(step => step.run?.includes('release-resume.mjs')))
  assert.ok(release.jobs['publish-linux'].if.includes("needs.resume-candidate.result == 'success'"))
  assert.ok(release.jobs['publish-linux'].if.includes("needs.build-docker.result == 'success'"))
})

test('publication gates and metadata use product identity while preflight cannot publish', () => {
  const job = release.jobs['publish-linux']
  assert.match(job.if, /inputs.preflight_only == false/)
  for (const step of job.steps) {
    if (step.run?.includes('node "$RUNNER_TEMP/release-publish-gate.mjs"')) assert.equal(step.env.PRODUCT_COMMIT, productRef)
    if (step.uses?.startsWith('softprops/action-gh-release@')) assert.equal(step.with.target_commitish, productRef)
  }
  const build = release.jobs['build-linux'].steps.map(step => step.run ?? '').join('\n')
  assert.ok(!build.includes('commit=${GITHUB_SHA}'))
  assert.ok(build.includes(`commit=${productRef}`))
  assert.ok(build.includes('needs.release-preflight.outputs.source-fingerprint'))
  assert.equal(release.jobs['build-docker'].with.preflight_only,
    "${{ github.event_name == 'workflow_dispatch' && inputs.preflight_only }}")
})

test('ordinary coverage plus privileged package retains the complete original test surface', () => {
  const steps = release.jobs['component-profile-checks'].steps
  const ordinary = steps.find(step => step.name === 'Component profile tests').run
  assert.match(ordinary, /go list \.\/internal\/components\/\.\.\. \.\/components\/\.\.\. \.\/api \.\/app \.\/web/)
  assert.match(ordinary, /\[ "\$package" = "\$PRIVILEGED_PACKAGE" \] \|\| ORDINARY_PACKAGES/)
  assert.match(ordinary, /go test -p 1 -count=1 "\$\{ORDINARY_PACKAGES\[@\]\}"/)
  assert.match(ordinary, /go test -p 1 -tags minimal -count=1/)
  assert.ok(!ordinary.includes('sudo'))
  const privileged = steps.find(step => step.name === 'Privileged OpenWrt composition package').run
  assert.match(privileged, /go test -c .* \.\/components\/server-protection\/cmd\/solovey-openwrt-owner-manifest/)
  assert.match(privileged, /sudo -n .* -test.v -test.count=1/)
  assert.match(privileged, /grep -q '\^--- PASS: TestOpenWrtWriterFeedsInstalledLoaderAndBrokerHelperComposition /)
  assert.match(privileged, /! grep -q -- '--- SKIP:'/)
  assert.ok(!privileged.includes('-test.run'))
  assert.ok(!steps.some(step => step['continue-on-error']))
})


test('canonical OpenWrt packages and FriendlyWrt bytes are mandatory even during recovery', () => {
  const build = release.jobs['build-openwrt']
  assert.deepEqual(build.strategy.matrix.profile, ['x86-64', 'rockchip-armv8'])
  assert.equal(build.if, undefined)
  const adapter = fs.readFileSync('scripts/release-openwrt-build.sh', 'utf8')
  assert.match(adapter, /scripts\/openwrt-package-build.sh/)
  assert.match(adapter, /openwrt_target_profile_load/)
  assert.ok(!adapter.includes('SOLOVEY_UI_COMPONENT_IDS'))
  const verify = release.jobs['verify-openwrt']
  assert.ok(verify.steps.some(step => step.run?.includes('cp deploy/friendlywrt/deployment-storage.json')))
  assert.ok(release.jobs['publish-linux'].if.includes("needs.verify-openwrt.result == 'success'"))
  assert.ok(release.jobs['build-docker'].needs.includes('verify-openwrt'))
})
