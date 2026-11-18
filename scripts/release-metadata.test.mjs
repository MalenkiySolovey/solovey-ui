import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import crypto from 'node:crypto'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { coreSourceProperties, componentSourceProperties, inspectDisclosureArchive, mergeProviderModuleGraphs, verifyProviderInventory, verifyComponentInventory, verifyBackendInventory, verifyCompositionInventory, verifyDisclosedFiles, verifyArchivedProduct } from './release-metadata.mjs'
import { compositionDigest, compositionSchema } from './release-composition.mjs'
import { resolveProviderSource } from './core-provider-build.mjs'

const sum = label => 'h1:' + crypto.createHash('sha256').update(label).digest('base64')
const source = (Path, Version) => ({ Path, Version, Sum: sum(Path + Version), GoModSum: sum(Path + Version + '/go.mod') })

test('source archive, SBOM and provenance must agree with the rederived composition', () => {
  const shared = { module: 'example.invalid/shared', version: 'v1.0.0', moduleChecksum: sum('source'),
    goModChecksum: sum('mod'), sourceCommit: '1'.repeat(40), sourceTree: '2'.repeat(40), provenanceSHA256: '3'.repeat(64) }
  const graph = { schema: compositionSchema, shared: [shared], providers: { 'sing-box': {}, 'shtorm-extended': {} } }
  const composition = { graph, sha256: compositionDigest(graph) }
  const provenance = { composition: structuredClone(composition) }, archive = structuredClone(composition)
  const bom = { metadata: { component: { properties: [{ name: 'solovey:compositionSHA256', value: composition.sha256 }] } },
    components: [{ name: shared.module, version: shared.version, properties: [
      { name: 'solovey:sourceAuthority', value: 'https://' + shared.module },
      { name: 'solovey:sourceCommit', value: shared.sourceCommit },
      { name: 'solovey:sourceTree', value: shared.sourceTree },
      { name: 'solovey:sourceProvenanceSHA256', value: shared.provenanceSHA256 },
    ] }] }
  const modules = [{ name: shared.module, version: shared.version, moduleSum: shared.moduleChecksum, goModSum: shared.goModChecksum,
    providers: ['shtorm-extended', 'sing-box'] }]
  verifyCompositionInventory(provenance, bom, { modules }, composition, archive)
  assert.throws(() => verifyCompositionInventory({}, bom, { modules }, composition, archive), /unknown composition schema/)
  assert.throws(() => verifyCompositionInventory(provenance, bom, { modules }, composition, undefined), /unknown composition schema/)
  const mutated = structuredClone(archive)
  mutated.graph.shared[0].sourceTree = '4'.repeat(40)
  mutated.sha256 = compositionDigest(mutated.graph)
  assert.throws(() => verifyCompositionInventory(provenance, bom, { modules }, composition, mutated), /verified Product inputs/)
  assert.throws(() => verifyCompositionInventory(provenance, { ...bom, components: [] }, { modules }, composition, archive), /missing.*SBOM/)
  assert.throws(() => verifyCompositionInventory(provenance, bom, { modules: [] }, composition, archive), /missing.*provider union/)
  const omitted = structuredClone(bom)
  omitted.metadata.component.properties = []
  assert.throws(() => verifyCompositionInventory(provenance, omitted, { modules }, composition, archive), /SBOM composition digest differs/)
})
test('SBOM core disclosure separates upstream authority, owned commits and the actual build tree',()=>{
  const lock=JSON.parse(fs.readFileSync(new URL('../deploy/dependencies/sing-box-extended-integration.json',import.meta.url),'utf8'))
  const engine=lock.sources[0]
  const properties=Object.fromEntries(coreSourceProperties(engine).map(item=>[item.name,item.value]))
  assert.equal(properties['solovey:sourceAuthority'],'https://github.com/shtorm-7/sing-box-extended')
  assert.equal(properties['solovey:copyRepository'],'https://github.com/MalenkiySolovey/sing-box-extended')
  assert.equal(properties['solovey:sourceTree'],engine.patchSeries.patchedTree)
  assert.deepEqual(JSON.parse(properties['solovey:patchCommits']),engine.patchSeries.patchCommits)
  const changed=structuredClone(engine);changed.patchSeries.patchedTree='0'.repeat(40)
  assert.throws(()=>coreSourceProperties(changed))
})
function graphs() {
  const common = source('example.invalid/shared', 'v1.0.0')
  return [
    { provider: 'sing-box', modules: [common, { Path: 'engine.invalid/core', Version: 'v1.14.2', Replace: source('maintained.invalid/core', 'v1.14.3-0.20261012000247-8b914e4be510') }] },
    { provider: 'shtorm-extended', modules: [structuredClone(common), { Path: 'engine.invalid/core', Version: 'v1.14.3', Replace: source('maintained.invalid/core', 'v1.14.4-0.20261023040931-b5f1b37ff6d4') }, source('example.invalid/native-only', 'v2.0.0')] },
  ].map(graph => ({ ...graph, source: resolveProviderSource({ provider: graph.provider }).source }))
}

test('SBOM inventory preserves distinct provider versions and attributes shared and native-only dependencies', () => {
  const inventory = mergeProviderModuleGraphs(graphs())
  assert.equal(inventory.modules.length, 4)
  assert.equal(inventory.providers.length, 2)
  const shared = inventory.modules.find(item => item.name === 'example.invalid/shared')
  assert.deepEqual(shared.providers, ['shtorm-extended', 'sing-box'])
  const extendedOnly = inventory.modules.find(item => item.name === 'example.invalid/native-only')
  assert.deepEqual(extendedOnly.providers, ['shtorm-extended'])
  const engines = inventory.modules.filter(item => item.name === 'maintained.invalid/core')
  assert.equal(engines.length, 2)
  assert.notEqual(engines[0].version, engines[1].version)
  assert.notEqual(inventory.providers[0].moduleGraphDigest, inventory.providers[1].moduleGraphDigest)
  assert.deepEqual(inventory.providers.map(item => item.moduleCount), [3, 2])
  assert.ok(inventory.providers.every(item => item.dependencies.every(identity => !identity.startsWith('engine.invalid/'))))
})

test('module graph identities are independent of provider and module enumeration order', () => {
  const original = graphs(), changed = structuredClone(original).reverse()
  for (const graph of changed) graph.modules.reverse()
  assert.deepEqual(mergeProviderModuleGraphs(changed), mergeProviderModuleGraphs(original))
})

test('ambiguous module bindings, unpinned replacements and conflicting source checksums fail closed', () => {
  for (const mutate of [
    value => value.push(value[0]),
    value => value[0].modules.push(value[0].modules[0]),
    value => { value[0].modules[1].Replace.Path = '../local-engine' },
    value => { value[0].modules[1].Replace.Replace = {} },
    value => { value[1].modules[0].Sum = sum('different source bytes') },
    value => { value[0].modules[0].GoModSum = 'unverified' },
  ]) {
    const value = graphs(); mutate(value)
    assert.throws(() => mergeProviderModuleGraphs(value))
  }
})

function disclosure() {
  const inventory = mergeProviderModuleGraphs(graphs())
  const provenance = { version: '2026.4.1', coreProviders: inventory.providers.map(({ dependencies, ...provider }) => provider),
    inventory: { goModules: inventory.modules.length, goModulesByProvider: Object.fromEntries(inventory.providers.map(item => [item.id, item.moduleCount])) } }
  const components = inventory.modules.map(module => ({ 'bom-ref': module.identity, name: module.name, version: module.version,
    purl: `pkg:golang/${module.name}@${encodeURIComponent(module.version)}`, properties: [
      { name: 'solovey:coreProviders', value: module.providers.join(',') }, { name: 'solovey:moduleSum', value: module.moduleSum },
      { name: 'solovey:goModSum', value: module.goModSum },
      ...module.bindings.map(binding => ({ name: `solovey:requires:${binding.provider}`, value: `${binding.module}@${binding.version}` })),
    ] }))
  const dependencies = []
  for (const provider of inventory.providers) {
    const ref = `solovey-ui@${provenance.version}?core-provider=${provider.id}`
    components.push({ 'bom-ref': ref, type: 'application', properties: [
      { name: 'solovey:coreProvider', value: provider.id }, { name: 'solovey:coreProviderSource', value: JSON.stringify(provider.source) },
      { name: 'solovey:moduleGraphDigest', value: provider.moduleGraphDigest },
    ] })
    dependencies.push({ ref, dependsOn: provider.dependencies })
  }
  const root = `solovey-ui@${provenance.version}`
  dependencies.push({ ref: root, dependsOn: inventory.providers.map(item => `${root}?core-provider=${item.id}`) })
  return { inventory, provenance, bom: { metadata: { component: { 'bom-ref': root } }, components, dependencies } }
}

test('disclosure verification requires both exact graphs, source content sums and provider dependency edges', () => {
  const fixture = disclosure()
  verifyProviderInventory(fixture.provenance, fixture.bom, fixture.inventory)
  for (const mutate of [
    value => value.provenance.coreProviders.pop(),
    value => { value.provenance.coreProviders[0].moduleGraphDigest = '0'.repeat(64) },
    value => { value.provenance.coreProviders[0].source = value.provenance.coreProviders[1].source },
    value => value.bom.components.pop(),
    value => value.bom.components.push(value.bom.components[0]),
    value => { value.bom.components[0].version = 'v0.0.0' },
    value => { value.bom.components[0].purl = 'pkg:golang/another-source@v1.0.0' },
    value => { value.bom.components[0].properties[0].value = 'sing-box' },
    value => { value.bom.components[0].properties[1].value = sum('unrelated source') },
    value => value.bom.components[0].properties.push(value.bom.components[0].properties[0]),
    value => value.bom.components[0].properties.push({ name: 'solovey:requires:unknown', value: 'invented-source@v1.0.0' }),
    value => value.bom.dependencies[0].dependsOn.pop(),
    value => value.bom.dependencies.push(value.bom.dependencies[0]),
    value => value.bom.dependencies.at(-1).dependsOn.pop(),
  ]) {
    const value = structuredClone(fixture); mutate(value)
    assert.throws(() => verifyProviderInventory(value.provenance, value.bom, fixture.inventory))
  }
})

test('component provenance rejects a substituted pin, missing source or altered frontend identity', () => {
  const input = { id: 'fixture', module: 'example.org/component', version: 'v1.0.0', sourceCommit: '1'.repeat(40), sourceTree: '2'.repeat(40), manifestSHA256: '3'.repeat(64), frontendSHA256: '4'.repeat(64), moduleChecksum: sum('module'), goModChecksum: sum('metadata') }
  const expected = { inputDigest: '5'.repeat(64), sources: [input] }
  const original = { provenance: { componentInputs: expected }, bom: { metadata: { component: { properties: [{ name: 'solovey:componentInputDigest', value: expected.inputDigest }] } }, components: [{ name: input.module, version: input.version, properties: componentSourceProperties(input) }] }, inventory: { modules: [{ name: input.module, version: input.version, moduleSum: input.moduleChecksum, goModSum: input.goModChecksum }] } }
  verifyComponentInventory(original.provenance, original.bom, original.inventory, expected)
  for (const corrupt of [
    value => { value.provenance.componentInputs.sources[0].sourceCommit = '0'.repeat(40) },
    value => { value.provenance.componentInputs.inputDigest = '0'.repeat(64) },
    value => { value.bom.metadata.component.properties = [] },
    value => { value.bom.components = [] },
    value => { value.inventory.modules = [] },
    value => { value.inventory.modules[0].moduleSum = sum('substitution') },
    value => { value.inventory.modules[0].goModSum = sum('substitution') },
    value => { value.bom.components[0].properties.find(item => item.name === 'solovey:frontendSourceSHA256').value = '0'.repeat(64) },
    value => { value.bom.components[0].properties.push(value.bom.components[0].properties[0]) },
  ]) {
    const fixture = structuredClone(original)
    corrupt(fixture)
    assert.throws(() => verifyComponentInventory(fixture.provenance, fixture.bom, fixture.inventory, expected))
  }
})


test('Backend disclosure rejects omitted source, substituted module and ambiguous authority', () => {
  const input = JSON.parse(fs.readFileSync(new URL('../deploy/dependencies/solovey-backend-source.json', import.meta.url), 'utf8'))
  const expected = { source: input, inputDigest: crypto.createHash('sha256').update(JSON.stringify(input)).digest('hex') }
  const properties = Object.entries({ sourceAuthority: 'https://' + input.module, sourceCommit: input.sourceCommit,
    sourceTree: input.sourceTree, sourceProvenanceSHA256: input.provenanceSHA256 }).map(([name, value]) => ({ name: 'solovey:' + name, value }))
  const original = { provenance: { backendInput: expected }, bom: { metadata: { component: { properties: [
    { name: 'solovey:backendInputDigest', value: expected.inputDigest }] } }, components: [
    { name: input.module, version: input.version, properties, licenses: [{ license: { id: 'GPL-3.0-only' } }] }] }, inventory: { modules: [
    { name: input.module, version: input.version, moduleSum: input.moduleChecksum, goModSum: input.goModChecksum }] } }
  verifyBackendInventory(original.provenance, original.bom, original.inventory, input)
  for (const corrupt of [
    value => { delete value.provenance.backendInput },
    value => { value.provenance.backendInput.source.sourceCommit = '0'.repeat(40) },
    value => { value.provenance.backendInput.inputDigest = '0'.repeat(64) },
    value => { value.bom.metadata.component.properties = [] },
    value => { value.bom.components = [] },
    value => { value.bom.components.push(value.bom.components[0]) },
    value => { value.bom.components[0].version = 'v0.0.0' },
    value => { value.bom.components[0].licenses = [] },
    value => { value.bom.components[0].properties[0].value = 'https://example.invalid/backend' },
    value => { value.bom.components[0].properties.push(value.bom.components[0].properties[0]) },
    value => { value.inventory.modules = [] },
    value => { value.inventory.modules.push(value.inventory.modules[0]) },
    value => { value.inventory.modules[0].version = 'v0.0.0' },
    value => { value.inventory.modules[0].moduleSum = sum('substituted source') },
    value => { value.inventory.modules[0].goModSum = sum('substituted metadata') },
  ]) {
    const fixture = structuredClone(original)
    corrupt(fixture)
    assert.throws(() => verifyBackendInventory(fixture.provenance, fixture.bom, fixture.inventory, input))
  }
})

import {readFrontendPin,sha256 as frontendHash} from './frontend-artifacts.mjs'
import {frontendSourceProperties,verifyFrontendInventory} from './release-metadata.mjs'
test('release disclosure binds Frontend source and all qualified distribution identities',()=>{
 const pin=readFrontendPin(),identity='frontend:solovey-frontend@'+pin.version
 const component={'bom-ref':identity,name:'solovey-frontend',version:pin.version,hashes:[{alg:'SHA-256',content:pin.sourceArchive.sha256}],properties:frontendSourceProperties(pin)}
 const provenance={frontendInput:pin},bom={components:[component],metadata:{component:{properties:[{name:'solovey:frontendInputDigest',value:frontendHash(JSON.stringify(pin))}]}}}
 verifyFrontendInventory(provenance,bom,pin)
 for(const edit of [b=>b.components=[],b=>b.components.push(structuredClone(component)),b=>b.components[0].hashes[0].content='0'.repeat(64),b=>b.components[0].properties.find(p=>p.name==='solovey:frontendArtifacts').value='{}',b=>b.metadata.component.properties[0].value='0'.repeat(64)]){const copy=structuredClone(bom);edit(copy);assert.throws(()=>verifyFrontendInventory(provenance,copy,pin))}
 assert.throws(()=>verifyFrontendInventory({frontendInput:{...pin,version:'0.9.0'}},bom,pin))
})
test('actual source zip and notice archive bytes cannot be omitted or substituted behind regenerated outer digests', {skip: process.platform === 'win32'}, t => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'disclosure-members-'))
  t.after(() => fs.rmSync(temp, {recursive:true, force:true}))
  const name = 'example.invalid/pinned@v1.0.0/LICENSE', pinned = 'original license\n'
  const modulePath = 'dependency-sources/go/fixture.zip', license = 'notices/LICENSE', document = 'INDEX.json'
  const sha = value => crypto.createHash('sha256').update(value).digest('hex')
  const goSum = sum(`${sha(pinned)}  ${name}\n`)
  const src = path.join(temp, 'src'), archive = path.join(temp, 'source.tar.gz')
  fs.mkdirSync(path.join(src, 'dependency-sources/go'), {recursive:true})
  fs.mkdirSync(path.join(src, 'notices'))
  fs.writeFileSync(path.join(src, document), JSON.stringify({source:'committed proof'}))
  fs.writeFileSync(path.join(src, license), pinned)
  const writeZip = content => execFileSync('python3', ['-c', 'import sys,zipfile; z=zipfile.ZipFile(sys.argv[1],"w"); z.writestr(sys.argv[2],sys.argv[3]); z.close()', path.join(src,modulePath), name, content])
  const pack = () => execFileSync('tar', ['-czf', archive, '-C', src, '.'])
  const inspect = () => inspectDisclosureArchive(archive, [document], [modulePath])
  const inventory = (moduleBytes) => [{path:modulePath, sha256:sha(moduleBytes), moduleSum:goSum}, {path:license, sha256:sha(pinned), size:Buffer.byteLength(pinned)}]
  writeZip(pinned); pack()
  const bytes = fs.readFileSync(path.join(src,modulePath)), reported = inspect()
  assert.deepEqual(JSON.parse(Buffer.from(reported.documents[document],'base64')), {source:'committed proof'})
  verifyDisclosedFiles(reported.files, inventory(bytes), 'source')
  writeZip('substituted license'); pack()
  assert.throws(() => verifyDisclosedFiles(inspect().files, inventory(fs.readFileSync(path.join(src,modulePath))), 'source'), /moduleSum differs/)
  writeZip(pinned); fs.unlinkSync(path.join(src,license)); pack()
  assert.throws(() => verifyDisclosedFiles(inspect().files, inventory(bytes), 'notice'), /archive member inventory differs/)
})

test('corresponding Product archive matches frozen Git bytes, executable bits and membership', {skip: process.platform === 'win32'}, t => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'disclosure-project-'))
  t.after(() => fs.rmSync(temp, {recursive:true, force:true}))
  const root = path.join(temp, 'solovey-ui'), archive = path.join(temp, 'source.tar.gz')
  fs.mkdirSync(root)
  const git = (...args) => execFileSync('git', ['-c', 'core.hooksPath=/dev/null', '-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', ...args], { cwd:root, encoding:'utf8' }).trim()
  git('init', '-q')
  const source = path.join(root, 'script.sh')
  fs.writeFileSync(source, 'original\n', {mode:0o755})
  git('add', '--chmod=+x', 'script.sh')
  git('commit', '-qm', 'Disclosure fixture', '--no-gpg-sign')
  const commit = git('rev-parse', 'HEAD')
  const verify = () => {
    execFileSync('tar', ['-czf', archive, '--exclude=.git', '-C', temp, 'solovey-ui'])
    verifyArchivedProduct(inspectDisclosureArchive(archive).files, commit, root)
  }
  verify()
  fs.writeFileSync(source, 'modified\n') // same byte count
  assert.throws(verify, /sha256 differs: solovey-ui\/script.sh/)
  fs.writeFileSync(source, 'original\n')
  fs.chmodSync(source, 0o644)
  assert.throws(verify, /executable differs: solovey-ui\/script.sh/)
  fs.unlinkSync(source)
  assert.throws(verify, /archive member inventory differs/)
})
