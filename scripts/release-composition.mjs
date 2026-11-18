// Derived build evidence. The signed release envelope remains the update authority.
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import { pathToFileURL } from 'node:url'
import { prepareBackendSource } from './backend-source-inputs.mjs'
import { readSourceLock, regularSourceTree, validateBuildEnvironment, validateModuleRequirement } from './component-source-inputs.mjs'
import { resolveBuildPlan } from './core-provider-build.mjs'
import { readExtendedProvenance } from './extended-integration-provenance.mjs'
import { readFrontendPin, resolveFrontendDistribution, sha256 } from './frontend-artifacts.mjs'
import { prepareFrontendBuildInputs } from './frontend-build-inputs.mjs'
import { openwrtBuildTargets, linuxPanelArtifacts, windowsPanelArtifacts, targets } from './release-providers.mjs'
import { validatePin as validateHostPin } from './verify-host-pin.mjs'
import { validatePin as validateContractPin } from './verify-contract-pin.mjs'

export const compositionSchema = 'solovey.product-composition/v1'
const repository = 'github.com/MalenkiySolovey/solovey-ui'
const sha1 = /^[a-f0-9]{40}$/
const h1 = /^h1:[A-Za-z0-9+/]{43}=$/
const versioned = /^v\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?$/
const text = file => fs.readFileSync(file, 'utf8').replaceAll('\r\n', '\n')
const json = file => JSON.parse(text(file))
const run = (command, args, options = {}) => execFileSync(command, args, {
  encoding: 'utf8', maxBuffer: 16 << 20, windowsHide: true, ...options,
}).trim()

// Sorted object keys, array order preserved, UTF-8 and no whitespace or LF.
function canonical(value) {
  if (Array.isArray(value)) return `[${value.map(canonical).join(',')}]`
  if (value !== null && typeof value === 'object') {
    return `{${Object.keys(value).sort().map(key => `${JSON.stringify(key)}:${canonical(value[key])}`).join(',')}}`
  }
  assert.ok(value === null || ['string', 'boolean'].includes(typeof value) || Number.isSafeInteger(value), 'invalid composition value')
  return JSON.stringify(value)
}

export function compositionDigest(graph) {
  return sha256(canonical(graph))
}

export function verifyComposition(receipt, expected) {
  assert.equal(receipt?.graph?.schema, compositionSchema, 'unknown composition schema')
  assert.equal(receipt.sha256, compositionDigest(receipt.graph), 'composition digest differs')
  assert.deepEqual(receipt, expected, 'composition receipt differs from verified Product inputs')
  return receipt
}

function productIdentity(root, commit) {
  assert.match(commit ?? '', sha1, 'an exact Product commit is required')
  const git = (...args) => run('git', args, { cwd: root })
  assert.equal(fs.realpathSync(git('rev-parse', '--show-toplevel')), fs.realpathSync(root), 'expected the Product checkout root')
  assert.equal(git('rev-parse', 'HEAD'), commit, 'Product checkout differs from requested commit')
  // The existing fingerprint excludes the generated frontend embed directory.
  // Its actual bytes are admitted separately, not by git status.
  assert.equal(git('status', '--porcelain=v1', '--untracked-files=all', '--', '.', ':(exclude)web/html'), '', 'composition requires clean Product source')
  const tree = git('rev-parse', `${commit}^{tree}`)
  const fingerprint = run(process.execPath, ['scripts/release-source.mjs', root], { cwd: root })
  return { repository, commit, tree, fingerprint, version: text(path.join(root, 'config/identity/version')).trim() }
}

function lockedSums(bytes) {
  const sums = new Map()
  for (const line of bytes.trim().split('\n')) {
    const [module, version, hash, extra] = line.split(' ')
    assert.ok(module && version && h1.test(hash) && extra === undefined, 'malformed Go checksum record')
    const key = `${module} ${version}`
    assert.ok(!sums.has(key), 'ambiguous Go checksum record')
    sums.set(key, hash)
  }
  return sums
}

function moduleSums(sums, module, version) {
  const moduleChecksum = sums.get(`${module} ${version}`), goModChecksum = sums.get(`${module} ${version}/go.mod`)
  assert.ok(h1.test(moduleChecksum) && h1.test(goModChecksum), `missing locked module checksums: ${module}@${version}`)
  return { moduleChecksum, goModChecksum }
}

// Reuse the source owner's tree and verify the cached Go metadata bytes, not
// just the sums returned by the Go command.
export function verifySharedModule(observed, locked) {
  const { module, version, moduleChecksum, goModChecksum, sourceCommit, sourceTree } = locked
  assert.equal(observed.Path, module, 'shared module owner differs')
  assert.equal(observed.Version, version, 'shared module version differs')
  assert.ok(!observed.Replace && !observed.Main && !observed.Error, 'shared module substitution rejected')
  assert.equal(observed.Sum, moduleChecksum, 'shared module checksum differs')
  assert.equal(observed.GoModSum, goModChecksum, 'shared go.mod checksum differs')
  const origin = observed.Origin
  assert.ok(origin?.VCS === 'git' && origin.URL === `https://${module}` && origin.Ref === `refs/tags/${version}` && origin.Hash === sourceCommit, 'shared module version provenance differs')
  assert.equal(regularSourceTree(observed.Dir), sourceTree, 'shared module cache differs from pinned source tree')
  assert.ok(fs.lstatSync(observed.GoMod).isFile(), 'shared cached go.mod must be a regular file')
  const mod = fs.readFileSync(observed.GoMod)
  assert.equal(`h1:${createHash('sha256').update(`${sha256(mod)}  go.mod\n`).digest('base64')}`, goModChecksum, 'shared cached go.mod differs from go.sum')
  const provenance = fs.readFileSync(path.join(observed.Dir, 'SOURCE_PROVENANCE.json'))
  const proof = JSON.parse(provenance)
  assert.ok(['solovey.host.source-provenance/v1', 'solovey.contracts.source-provenance/v1'].includes(proof.schema), 'unadmitted shared source provenance')
  return { ...locked, sourceRef: origin.Ref, provenanceSHA256: sha256(provenance) }
}

export function verifyFrontendProjection(pin, input, components) {
  assert.equal(input.manifestSHA256, pin.input.manifestSHA256, 'Product Frontend input tuple drift')
  assert.equal(input.sourceLockDigest, pin.input.sourceLockDigest, 'Product component source authority drift')
  const active = components.filter(component => component.hasFrontend).map(component => component.id)
  assert.deepEqual(input.components, active, 'Frontend component inventory differs')
  assert.deepEqual(pin.profiles.full.componentIDs, active, 'Full Frontend selection differs')
  assert.deepEqual(pin.profiles.core.componentIDs, [], 'Core Frontend must omit optional components')
}

export function readComponentCatalog(root, lock) {
  const components = [], owners = new Set()
  const directory = path.join(root, 'components')
  for (const entry of fs.readdirSync(directory, { withFileTypes: true }).sort((a, b) => a.name < b.name ? -1 : 1)) {
    if (!entry.isDirectory()) continue
    const file = path.join(directory, entry.name, 'component.json')
    if (!fs.existsSync(file)) continue
    assert.ok(fs.lstatSync(file).isFile(), 'component manifest is not a regular file')
    const manifest = json(file)
    assert.match(manifest.id, /^[a-z0-9]+(?:-[a-z0-9]+)*$/)
    assert.equal(manifest.id, entry.name, 'component directory identity differs')
    assert.equal(manifest.delivery, 'in-process', 'unadmitted Product runtime component role')
    const external = lock.sources.find(source => source.id === manifest.id)
    if (external) {
      assert.ok(!owners.has(external.module), 'duplicate component source owner')
      owners.add(external.module)
      assert.equal(sha256(fs.readFileSync(file)), external.manifestSHA256, 'Product component manifest differs from source lock')
    }
    components.push({ id: manifest.id, delivery: manifest.delivery, version: manifest.version,
      owner: external?.module ?? repository, manifestSHA256: sha256(text(file)), hasFrontend: Boolean(manifest.frontend?.entries?.length) })
  }
  assert.ok(components.length && owners.size === lock.sources.length, 'missing or ambiguous Component source')
  return components
}

export async function deriveProductComposition(commit, root = process.cwd()) {
  root = fs.realpathSync(root)
  const product = productIdentity(root, commit)
  validateBuildEnvironment(process.env)
  assert.equal(run('go', ['env', 'GOWORK'], { cwd: root }).replace(/^off$/, ''), '', 'Product composition rejects a Go workspace')
  const env = { ...process.env, GOWORK: 'off', GOFLAGS: '-mod=readonly' }
  const go = (...args) => run('go', args, { cwd: root, env })
  const project = name => path.join(root, name)
  const pin = readFrontendPin(root), lock = readSourceLock(root)
  assert.ok(fs.lstatSync(project('components/sources.lock.json')).isFile(), 'expected a regular Product Component source lock')
  const components = readComponentCatalog(root, lock)
  const backend = prepareBackendSource(root)
  const input = prepareFrontendBuildInputs(root)
  verifyFrontendProjection(pin, input, components)
  const profiles = {}
  for (const profile of ['full', 'core']) {
    await resolveFrontendDistribution({ root, pin, key: profile })
    profiles[profile] = { frontend: pin.profiles[profile], runtimeComponents: profile === 'full' ? components.map(value => value.id) : [] }
  }

  const officialCopies = ['ssm', 'quic', 'anytls'].map(name => JSON.parse(run(process.execPath, [`scripts/${name}-integration-provenance.mjs`, 'manifest'], { cwd: root, env })))
  const extendedCopies = [...readExtendedProvenance(root).sources, officialCopies[2]]
  const providers = {}, shared = ['host', 'contracts'].map((owner, index) => {
    const pin = json(project(`${owner}/PIN.json`))
    const validate = index === 0 ? validateHostPin : validateContractPin
    validate(pin)
    run(process.execPath, [`scripts/verify-${owner === 'contracts' ? 'contract' : owner}-pin.mjs`], { cwd: root, env })
    const observed = JSON.parse(go('mod', 'download', '-json', `${pin.module}@${pin.version}`))
    const { module, version, sourceCommit, sourceTree, moduleChecksum, goModChecksum, contractVersion } = pin
    return verifySharedModule(observed, { module, version, sourceCommit, sourceTree, moduleChecksum, goModChecksum,
      ...(contractVersion ? { contractVersion } : {}) })
  })
  const goVersion = go('env', 'GOVERSION').slice(2)
  for (const [provider, copies] of [['sing-box', officialCopies], [targets.extended.provider, extendedCopies]]) {
    assert.ok(!Object.hasOwn(providers, provider), 'duplicate provider')
    const plan = resolveBuildPlan({ provider, repoRoot: root })
    const mod = JSON.parse(go('mod', 'edit', '-json', plan.modFile))
    const sumName = plan.modFile.replace(/\.mod$/, '.sum'), sumBytes = text(project(sumName)), sums = lockedSums(sumBytes)
    assert.equal(mod.Module.Path, repository, 'wrong Product module')
    assert.equal(mod.Go, goVersion, 'Product Go toolchain differs from provider module directive')
    for (const { New: actual } of mod.Replace ?? []) {
      assert.ok(actual.Path.includes('/') && !/^[./]|[\\\s]/.test(actual.Path) && versioned.test(actual.Version ?? ''), 'local or unversioned module replacement')
      moduleSums(sums, actual.Path, actual.Version)
    }
    for (const source of copies) {
      const matches = (mod.Replace ?? []).filter(item => item.Old.Path === source.original.module)
      assert.equal(matches.length, 1, 'ambiguous provider copy binding')
      const replacement = source.replacement
      assert.deepEqual(matches[0].New, { Path: replacement.module, Version: replacement.version }, 'provider copy binding differs')
      assert.deepEqual(moduleSums(sums, replacement.module, replacement.version),
        { moduleChecksum: replacement.sum, goModChecksum: replacement.goModSum }, 'provider copy checksums differ')
    }
    assert.ok(copies.some(source => source.replacement.commit === plan.source.sourceRevision && source.replacement.module === plan.source.module), 'provider source pin differs from copy lineage')
    providers[provider] = { modFile: plan.modFile, modFileSHA256: sha256(text(project(plan.modFile))), sumFile: sumName,
      sumFileSHA256: sha256(sumBytes), engine: plan.source, buildTags: plan.tags.split(',').filter(Boolean), copies }

    for (const input of shared) {
      const { module, version, moduleChecksum, goModChecksum } = input
      validateModuleRequirement(mod, input)
      assert.deepEqual(moduleSums(sums, module, version), { moduleChecksum, goModChecksum }, 'provider shared module checksums differ')
    }
  }
  const nodeVersion = text(project('.node-version')).trim()
  assert.equal(nodeVersion, pin.toolchain.node, 'Product and Frontend Node pins differ')
  const graph = { schema: compositionSchema, canonicalization: 'sha256/sorted-keys-utf8-json/v1', product,
    frontend: { version: pin.version, source: pin.source, sourceArchive: pin.sourceArchive, input: pin.input, toolchain: pin.toolchain },
    backend, shared, components: { sourceLockDigest: input.sourceLockDigest,
      external: lock.sources.map(({ files, ...source }) => source), catalog: components },
    profiles, providers, toolchain: { go: goVersion, node: nodeVersion }, native: json(project('deploy/dependencies/cronet-native-source.json')),
    eligibility: { linux: linuxPanelArtifacts(), windows: windowsPanelArtifacts(), docker: targets.docker,
      dockerExtended: targets.extended.docker, dockerProfile: 'full', openwrt: openwrtBuildTargets(), openwrtProfile: 'full',
      linuxToolchains: targets.linux, compatibility: json(project('scripts/release-compatibility.json')) } }
  assert.deepEqual(productIdentity(root, commit), product, 'input resolution modified frozen Product source')
  return { sha256: compositionDigest(graph), graph }
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  const [command, commit, file, extra] = process.argv.slice(2)
  assert.ok(!extra && (command === 'derive' && !file || command === 'verify' && file),
    'usage: node scripts/release-composition.mjs derive <exact-commit> | verify <exact-commit> <composition.json>')
  const expected = await deriveProductComposition(commit)
  if (command === 'verify') verifyComposition(json(file), expected)
  console.log(JSON.stringify(expected, null, 2))
}
