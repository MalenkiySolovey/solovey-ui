import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { createRequire } from 'node:module'
import test from 'node:test'

const require = createRequire(import.meta.url)
const YAML = require('../frontend/node_modules/yaml')
const read = name => YAML.parse(fs.readFileSync(`.github/workflows/${name}.yml`, 'utf8'))
const release = read('release')
const productRef = '${{ needs.release-preflight.outputs.product-commit }}'
const checkouts = job => job.steps?.filter(step => step.uses?.startsWith('actions/checkout@')) ?? []

test('matrix admission can recover old Official-only products without a newer provider policy module', t => {
  const command = release.jobs['release-preflight'].steps.find(step => step.id === 'targets').run
  const code = command.match(/node --input-type=module -e '([\s\S]*)'/)?.[1]
  assert.ok(code)
  const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'release-legacy-matrix-'))
  t.after(() => { assert.equal(path.dirname(fs.realpathSync(scratch)), fs.realpathSync(os.tmpdir())); fs.rmSync(scratch, { recursive: true, force: true }) })
  fs.mkdirSync(path.join(scratch, 'scripts'))
  const linux = [{ platform: 'amd64', arch: 'amd64', naive: true }]
  fs.writeFileSync(path.join(scratch, 'scripts/release-targets.json'), JSON.stringify({ linux }))
  const output = path.join(scratch, 'output')
  execFileSync(process.execPath, ['--input-type=module', '-e', code], { cwd: scratch, env: { ...process.env, GITHUB_OUTPUT: output } })
  assert.equal(fs.readFileSync(output, 'utf8'), 'linux-matrix=' + JSON.stringify({ include: linux }) + '\n')
  fs.unlinkSync(output)
  execFileSync(process.execPath, ['--input-type=module', '-e', code], { env: { ...process.env, GITHUB_OUTPUT: output } })
  const current = JSON.parse(fs.readFileSync(output, 'utf8').trim().slice('linux-matrix='.length))
  assert.ok(current.include.some(item => item.provider === 'shtorm-extended'))
})

test('runner-dependent contexts are excluded from workflow and job environments', () => {
  for (const filename of fs.readdirSync('.github/workflows').filter(name => /\.ya?ml$/.test(name))) {
    const workflow = YAML.parse(fs.readFileSync(`.github/workflows/${filename}`, 'utf8'))
    for (const [location, environment] of [['workflow', workflow.env], ...Object.entries(workflow.jobs ?? {}).map(([name, job]) => [name, job.env])]) {
      for (const [name, value] of Object.entries(environment ?? {})) {
        for (const [expression] of String(value).matchAll(/\$\{\{[\s\S]*?\}\}/g)) {
          assert.doesNotMatch(expression, /\b(?:runner|job|steps|env)\s*[.\[]/, `${filename}: ${location}.env.${name} references a context unavailable before runner assignment`)
        }
      }
    }
  }
})

test('CI and publication execute the native installer ownership contract', () => {
  for (const job of [read('ci').jobs.installer, release.jobs['test-installer']]) {
    assert.ok(job.steps.some(step => step.uses?.startsWith('actions/setup-go@')))
    const gate = job.steps.find(step => step.run === 'bash tests/installer/native-ownership.sh')
    assert.ok(gate)
    assert.equal(gate.if, undefined)
    assert.equal(gate['continue-on-error'], undefined)
  }
})

test('orchestrator is validated before immutable product checkout and resolution', () => {
  const job = release.jobs['release-preflight']
  assert.deepEqual(checkouts(job).map(step => step.with.ref), [
    '${{ github.sha }}', "${{ github.event_name == 'workflow_dispatch' && inputs.preflight_only && inputs.source_commit || format('refs/tags/{0}', env.RELEASE_TAG_INPUT) }}",
  ])
  const validation = job.steps.findIndex(step => step.run?.includes('release-execution.test.mjs'))
  const product = job.steps.findIndex(step => step.name === 'Check out immutable product source')
  const resolution = job.steps.findIndex(step => step.id === 'product')
  assert.ok(validation < product && product < resolution)
  assert.match(job.steps[resolution].run, /git rev-parse HEAD/)
  assert.match(job.steps[resolution].run, /node scripts\/release-source.mjs/)
  const gate = job.steps.find(step => step.run?.includes('release-publish-gate.mjs" before'))
  assert.equal(gate.if, "github.event_name == 'push' || inputs.preflight_only == false")
  assert.equal(gate.env.PRODUCT_COMMIT, '${{ steps.product.outputs.commit }}')
  const sourceSelection = job.steps[resolution]
  assert.ok(sourceSelection.run.includes('[ "$PREFLIGHT_ONLY" = true ] && [ -z "$RESUME_RUN_ID" ]'))
  assert.ok(sourceSelection.run.includes('[ "$COMMIT" = "$PREPARATION_SOURCE_COMMIT" ]'))
  assert.ok(sourceSelection.run.includes('^[a-f0-9]{40}$'))
})

test('every product checkout and reusable build is bound to resolved product source', () => {
  for (const [name, job] of Object.entries(release.jobs)) {
    if (name === 'release-preflight') continue
    assert.ok([job.needs].flat().includes('release-preflight'), name)
    if (['resume-candidate', 'build-openwrt', 'verify-openwrt', 'publish-linux', 'component-profile-checks'].includes(name)) {
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

test('Windows metadata retains clean product identity across profile generation', () => {
  const steps = read('windows').jobs['build-windows'].steps
  const identityIndex = steps.findIndex(step => step.id === 'source_identity')
  assert.ok(identityIndex >= 0, 'resolve product identity before generated profile inputs change')
  const identity = steps[identityIndex]
  assert.equal(identity.if, undefined)
  assert.equal(identity['continue-on-error'], undefined)
  assert.match(identity.run, /git status --porcelain/)
  assert.match(identity.run, /node scripts\/release-source\.mjs/)
  const generatedIndex = steps.findIndex(step => step.run?.includes('scripts/generate-component-imports.mjs'))
  assert.ok(identityIndex < generatedIndex, 'Core generation changes tracked Linux-only imports')
  const metadata = steps.find(step => step.run?.includes('source_fingerprint='))
  assert.ok(metadata.run.includes('source_fingerprint=${{ steps.source_identity.outputs.fingerprint }}'))
  assert.ok(!metadata.run.includes('source_fingerprint=$(node'), 'recovery requires the original clean fingerprint')
})

test('interrupted publication recovery is gated and never rebuilds or overwrites registry', () => {
  for (const name of ['test-installer', 'build-frontend', 'component-profile-checks', 'build-linux', 'build-docker', 'build-windows', 'release-metadata', 'assemble-linux', 'build-openwrt', 'verify-openwrt']) {
    assert.equal(release.jobs[name].if, "inputs.resume_run_id == ''", name)
  }
  const recovery = release.jobs['resume-candidate']
  assert.equal(recovery.if, "inputs.resume_run_id != ''")
  assert.ok(recovery.steps.some(step => step.run?.includes('release-resume.mjs')))
  assert.ok(release.jobs['publish-linux'].if.includes("needs.resume-candidate.result == 'success'"))
  assert.ok(release.jobs['publish-linux'].if.includes("needs.build-docker.result == 'success'"))
})

test('publication gates and metadata use product identity while preflight cannot publish', () => {
  const job = release.jobs['publish-linux']
  assert.match(job.if, /inputs.preflight_only == false/)
  const publish = job.steps.find(step => step.run?.includes('gh release edit "$RELEASE_TAG"'))?.run ?? ''
  assert.ok(publish.includes('PUBLISH_ARGS=(--draft=false --notes-file "$RUNNER_TEMP/release-notes.md")'))
  assert.ok(publish.includes('[[ "$RELEASE_TAG" == *-* ]] && PUBLISH_ARGS+=(--prerelease --latest=false)'))
  assert.ok(publish.includes('gh release edit "$RELEASE_TAG" "${PUBLISH_ARGS[@]}"'))
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

test('release uses the shared capability owner with the complete original package selection', () => {
  const steps = release.jobs['component-profile-checks'].steps
  const run = steps.find(step => step.name === 'Component profile tests').run
  assert.ok(run.includes('go-test.mjs" -p 1 -count=1 -v -- ./internal/components/... ./components/... ./api ./app ./web'))
  assert.ok(run.includes('go-test.mjs" -p 1 -tags minimal -count=1 -- ./internal/components/... ./api ./app ./web'))
  const preserve = steps.findIndex(step => step.name === 'Preserve test execution owner')
  assert.ok(preserve < steps.findIndex(step => step.name === 'Check out repository'))
  assert.ok(steps[preserve].run.includes('go-test-capabilities.json'))
})


test('canonical OpenWrt packages and FriendlyWrt bytes are mandatory even during recovery', () => {
  const build = release.jobs['build-openwrt']
  assert.deepEqual(build.strategy.matrix.profile, ['x86-64', 'rockchip-armv8'])
  assert.equal(build.if, "inputs.resume_run_id == ''")
  const adapter = fs.readFileSync('scripts/release-openwrt-build.sh', 'utf8')
  assert.match(adapter, /scripts\/openwrt-package-build.sh/)
  assert.match(adapter, /openwrt_target_profile_load/)
  assert.ok(!adapter.includes('SOLOVEY_UI_COMPONENT_IDS'))
  const verify = release.jobs['verify-openwrt']
  assert.ok(verify.steps.some(step => step.run?.includes('cp deploy/friendlywrt/deployment-storage.json')))
  assert.ok(release.jobs['publish-linux'].if.includes("needs.verify-openwrt.result == 'success'"))
  assert.ok(release.jobs['build-docker'].needs.includes('verify-openwrt'))
})

test('publication requires source disclosure and private Docker preflight retains verifiable OCI bytes', () => {
  const publisher = release.jobs['publish-linux']
  assert.ok(release.jobs['build-docker'].needs.includes('release-metadata'), 'registry writes must wait for complete source/license disclosure')
  assert.ok(publisher.needs.includes('release-metadata'))
  assert.ok(publisher.if.includes("needs.release-metadata.result == 'success'"))
  assert.ok(publisher.steps.some(step => step.run?.includes('scripts/release-metadata.mjs verify "$RELEASE_CANDIDATE_DIR/metadata"')))
  assert.ok(publisher.steps.find(step => step.uses?.startsWith('softprops/action-gh-release@')).with.files.includes('/metadata/*'))
  const docker = read('docker').jobs.build.steps.find(step => step.name === 'Preflight Docker image')
  assert.equal(docker.with.push, false)
  assert.ok(docker.with.outputs.startsWith('type=oci,'))
  assert.equal(docker.with.sbom, true)
  assert.equal(docker.with.provenance, 'mode=max')
  const windows = read('windows').jobs['build-windows'].strategy.matrix
  assert.deepEqual(windows.arch, ['amd64', 'arm64'])
  assert.deepEqual(windows.profile, ['full', 'core'])
})

test('recovery graph uses original admission while fresh publication retains every producer gate', () => {
  const publisher = release.jobs['publish-linux']
  const expression = publisher.if.replace(/needs\.([\w-]+)\.result/g, 'needs["$1"].result')
  const evaluate = Function('needs', 'inputs', 'github', 'always', 'cancelled', `return (${expression})`)
  const needs = Object.fromEntries(publisher.needs.map(name => [name, { result: 'skipped' }]))
  needs['release-preflight'].result = 'success'; needs['resume-candidate'].result = 'success'
  const inputs = { resume_run_id: '123', preflight_only: false }, github = { event_name: 'workflow_dispatch' }
  const admit = () => evaluate(needs, inputs, github, () => true, () => false)
  assert.equal(admit(), true)
  needs['resume-candidate'].result = 'failure'; assert.equal(admit(), false)
  needs['resume-candidate'].result = 'success'; inputs.preflight_only = true; assert.equal(admit(), false)
  inputs.preflight_only = false; inputs.resume_run_id = ''
  for (const item of Object.values(needs)) item.result = 'success'
  needs['resume-candidate'].result = 'skipped'; assert.equal(admit(), true)
  for (const name of publisher.needs.filter(n => n !== 'resume-candidate')) {
    needs[name].result = 'failure'; assert.equal(admit(), false, name); needs[name].result = 'success'
  }
})

test('publication staging and original-byte receipt gate every external write', () => {
  const publisher = release.jobs['publish-linux'], recovery = release.jobs['resume-candidate']
  for (const job of [publisher, recovery]) {
    assert.equal(job.env?.RELEASE_CANDIDATE_DIR, undefined)
    const initialization = job.steps.findIndex(step => step.run?.includes('RELEASE_CANDIDATE_DIR='))
    assert.equal(initialization, 0, 'resolve external staging before every candidate consumer')
    assert.ok(job.steps[initialization].run.includes('"$RUNNER_TEMP"'))
    assert.ok(job.steps[initialization].run.includes('>> "$GITHUB_ENV"'))
    assert.equal(job.steps[initialization].if, undefined)
    assert.equal(job.steps[initialization]['continue-on-error'], undefined)
  }
  for (const step of publisher.steps.filter(s => s.uses?.startsWith('actions/download-artifact@'))) {
    assert.ok(step.with.path.startsWith('${{ env.RELEASE_CANDIDATE_DIR }}') || step.with.path.startsWith('${{ runner.temp }}'), step.name)
  }
  const verification = publisher.steps.findIndex(s => s.run === 'node "$RUNNER_TEMP/release-candidate.mjs"')
  const publication = publisher.steps.findIndex(s => s.uses?.startsWith('softprops/action-gh-release@'))
  assert.ok(verification >= 0 && verification < publication)
  assert.equal(publisher.steps[verification].env.RESUME_RUN_ID, '${{ inputs.resume_run_id }}')
  assert.ok(publisher.steps[verification].env.RELEASE_RECOVERY_RECEIPT.includes('RECOVERY_RECEIPT.json'))
  const uploads = recovery.steps.filter(s => s.uses?.startsWith('actions/upload-artifact@'))
  assert.deepEqual(uploads.map(s => s.with.name).sort(), ['complete-linux-release', 'solovey-ui-windows-recovered', 'complete-release-metadata', 'complete-openwrt-release', 'release-recovery-receipt'].sort())
  assert.ok(uploads.every(s => s.with['retention-days'] <= 7))
})
