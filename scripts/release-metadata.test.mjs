import assert from 'node:assert/strict'
import crypto from 'node:crypto'
import test from 'node:test'
import { mergeProviderModuleGraphs, verifyProviderInventory } from './release-metadata.mjs'
import { resolveProviderSource } from './core-provider-build.mjs'

const sum = label => 'h1:' + crypto.createHash('sha256').update(label).digest('base64')
const source = (Path, Version) => ({ Path, Version, Sum: sum(Path + Version), GoModSum: sum(Path + Version + '/go.mod') })
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
