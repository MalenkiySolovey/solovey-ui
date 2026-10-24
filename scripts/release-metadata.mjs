// Canonical corresponding-source, dependency notice and SBOM producer.
// Package downloads are immutable Go module identities or lockfile SRI bytes.
import assert from 'node:assert/strict'
import crypto from 'node:crypto'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { resolveBuildPlan } from './core-provider-build.mjs'
import { readExtendedProvenance } from './extended-integration-provenance.mjs'
import { linuxBuildTargets } from './release-providers.mjs'

export const metadataNames = JSON.parse(fs.readFileSync(new URL('./release-targets.json', import.meta.url), 'utf8')).releaseMetadata.toSorted()
const sha = bytes => crypto.createHash('sha256').update(bytes).digest('hex')
const json = file => JSON.parse(fs.readFileSync(file, 'utf8'))
const jsonStream = text => (text.match(/^{[\s\S]*?^}/gm) ?? []).map(JSON.parse)
const writeJSON = (file, value) => fs.writeFileSync(file, JSON.stringify(value, null, 2) + '\n')
const run = (command, args, options = {}) => execFileSync(command, args, { encoding: 'utf8', maxBuffer: 64 << 20, ...options })
const digestFile = async file => {
  const hash = crypto.createHash('sha256')
  for await (const chunk of fs.createReadStream(file)) hash.update(chunk)
  return hash.digest('hex')
}

export function mergeProviderModuleGraphs(graphs) {
  const modules = new Map(), seen = new Set(), providers = []
  for (const graph of graphs) {
    assert.ok(!seen.has(graph.provider), 'duplicate provider module graph')
    seen.add(graph.provider)
    const records = [], paths = new Set()
    for (const module of graph.modules.filter(item => !item.Main)) {
      const actual = module.Replace ?? module
      assert.ok(module.Path && module.Version && !paths.has(module.Path) && !actual.Replace, 'ambiguous provider module binding')
      paths.add(module.Path)
      assert.ok(actual.Path && actual.Version?.startsWith('v') && !actual.Path.startsWith('.') && !/[\\\s]/.test(actual.Path), 'unpinned provider module source')
      const sum = actual.Sum || graph.sums?.get(`${actual.Path} ${actual.Version}`)
      const goModSum = actual.GoModSum || graph.sums?.get(`${actual.Path} ${actual.Version}/go.mod`)
      if (goModSum) assert.match(goModSum, /^h1:[A-Za-z0-9+/]{43}=$/, 'invalid provider go.mod checksum')
      if (sum) assert.match(sum, /^h1:[A-Za-z0-9+/]{43}=$/, 'invalid provider source checksum')
      const record = { module: module.Path, requiredVersion: module.Version, sourceModule: actual.Path, version: actual.Version }
      records.push(record)
      const identity = `${actual.Path}@${actual.Version}`
      let item = modules.get(identity)
      if (item) {
        if (item.moduleSum && sum) assert.equal(item.moduleSum, sum, 'provider graphs disagree on immutable module bytes')
        item.moduleSum ||= sum ?? ''
        if (item.goModSum && goModSum) assert.equal(item.goModSum, goModSum, 'provider graphs disagree on immutable go.mod bytes')
        item.goModSum ||= goModSum ?? ''
      } else {
        item = { identity, name: actual.Path, version: actual.Version, moduleSum: sum ?? '', goModSum: goModSum ?? '', providers: [], bindings: [] }
        modules.set(identity, item)
      }
      item.providers.push(graph.provider)
      item.bindings.push({ provider: graph.provider, module: module.Path, version: module.Version })
    }
    records.sort((a, b) => a.module < b.module ? -1 : a.module > b.module ? 1 : 0)
    assert.ok(records.length > 0, 'empty provider module graph')
    providers.push({ id: graph.provider, source: graph.source, modFile: graph.modFile,
      modFileSha256: graph.modFileSha256, sumFileSha256: graph.sumFileSha256,
      // This digest binds the complete resolved module/replacement coordinates.
      // Content sums are authenticated at download and recorded per SBOM
      // component; Go's lazy graph can list unused modules absent from go.sum.
      moduleGraphDigest: sha(JSON.stringify(records)), moduleCount: records.length,
      dependencies: records.map(item => `${item.sourceModule}@${item.version}`).sort() })
  }
  for (const item of modules.values()) {
    item.providers = [...new Set(item.providers)].sort()
    item.bindings.sort((a, b) => {
      const left = `${a.provider}\0${a.module}`, right = `${b.provider}\0${b.module}`
      return left < right ? -1 : left > right ? 1 : 0
    })
  }
  return { providers: providers.sort((a, b) => a.id < b.id ? -1 : a.id > b.id ? 1 : 0), modules: [...modules.values()].sort((a, b) => a.identity < b.identity ? -1 : a.identity > b.identity ? 1 : 0) }
}

export function readProviderModuleGraphs(requestRoot) {
  const graphs = [], downloads = new Map()
  const providers = [...new Set(linuxBuildTargets().map(target => target.provider))].sort()
  const environment = { ...process.env, GOFLAGS: '', GOWORK: 'off' }
  for (const provider of providers) {
    const plan = resolveBuildPlan({ provider })
    const sumFile = plan.modFile.replace(/\.mod$/, '.sum')
    const sums = fs.readFileSync(sumFile, 'utf8')
    let cwd = process.cwd(), modFile = plan.modFile
    if (requestRoot) {
      cwd = path.join(requestRoot, provider); fs.mkdirSync(cwd)
      fs.copyFileSync(plan.modFile, path.join(cwd, 'go.mod'))
      fs.copyFileSync(sumFile, path.join(cwd, 'go.sum'))
      modFile = 'go.mod'
      for (const item of jsonStream(run('go', ['mod', 'download', '-json', 'all'], { cwd, env: environment }))) {
        const key = `${item.Path}@${item.Version}`, previous = downloads.get(key)
        if (previous) assert.equal(item.Sum, previous.Sum, 'downloaded provider source differs')
        downloads.set(key, item)
      }
      run('go', ['mod', 'verify'], { cwd, env: environment })
      assert.equal(fs.readFileSync(path.join(cwd, 'go.mod'), 'utf8').replaceAll('\r\n', '\n'), fs.readFileSync(plan.modFile, 'utf8').replaceAll('\r\n', '\n'), 'isolated source download changed the pinned module graph')
    }
    const modules = jsonStream(run('go', ['list', '-mod=readonly', `-modfile=${modFile}`, '-m', '-json', 'all'], { cwd, env: environment }))
    graphs.push({ provider, source: plan.source, modFile: plan.modFile, modFileSha256: sha(fs.readFileSync(plan.modFile, 'utf8').replaceAll('\r\n', '\n')), sumFileSha256: sha(sums.replaceAll('\r\n', '\n')),
      modules, sums: new Map(sums.trim().split(/\r?\n/).map(line => { const [name, version, sum] = line.split(' '); return [`${name} ${version}`, sum] })) })
  }
  return { inventory: mergeProviderModuleGraphs(graphs), downloads }
}

export function verifyProviderInventory(provenance, bom, inventory) {
  assert.deepEqual(provenance.coreProviders, inventory.providers.map(({ dependencies, ...provider }) => provider), 'provider source or resolved graph disclosure differs')
  assert.equal(provenance.inventory.goModules, inventory.modules.length, 'source union inventory differs')
  assert.deepEqual(provenance.inventory.goModulesByProvider, Object.fromEntries(inventory.providers.map(item => [item.id, item.moduleCount])), 'provider graph counts differ')
  const components = new Map(bom.components.map(item => [item['bom-ref'], item]))
  assert.equal(components.size, bom.components.length, 'duplicate SBOM component identity')
  const property = (component, name) => {
    const values = component.properties?.filter(item => item.name === name) ?? []
    assert.equal(values.length, 1, 'SBOM source property is missing or ambiguous')
    return values[0].value
  }
  const goComponents = bom.components.filter(item => item.purl?.startsWith('pkg:golang/'))
  assert.equal(goComponents.length, inventory.modules.length, 'SBOM omits or invents Go sources')
  for (const module of inventory.modules) {
    const component = components.get(module.identity)
    assert.ok(component && component.name === module.name && component.version === module.version, 'SBOM provider module identity differs')
    assert.equal(component.purl, `pkg:golang/${module.name}@${encodeURIComponent(module.version)}`, 'SBOM provider package URL differs')
    assert.equal(property(component, 'solovey:coreProviders'), module.providers.join(','), 'SBOM provider attribution differs')
    for (const [field, name] of [['moduleSum', 'solovey:moduleSum'], ['goModSum', 'solovey:goModSum']]) {
      const checksum = property(component, name)
      assert.match(checksum ?? '', /^h1:[A-Za-z0-9+/]{43}=$/, 'SBOM lacks authenticated source content checksum')
      if (module[field]) assert.equal(checksum, module[field], 'SBOM differs from the locked source checksum')
    }
    const bindings = component.properties.filter(item => item.name.startsWith('solovey:requires:'))
      .map(item => `${item.name}\0${item.value}`).sort()
    assert.deepEqual(bindings, module.bindings.map(binding => `solovey:requires:${binding.provider}\0${binding.module}@${binding.version}`).sort(), 'SBOM original module bindings differ')
  }
  const common = bom.components.filter(item => item['bom-ref'].startsWith('npm:') || item['bom-ref'].startsWith('native:')).map(item => item['bom-ref'])
  const variants = bom.components.filter(item => item.properties?.some(value => value.name === 'solovey:coreProvider'))
  assert.equal(variants.length, inventory.providers.length, 'SBOM build flavor inventory differs')
  assert.equal(components.size, goComponents.length + common.length + variants.length, 'SBOM contains an unclassified source')
  assert.equal(new Set(bom.dependencies.map(item => item.ref)).size, bom.dependencies.length, 'SBOM dependency graph is ambiguous')
  for (const provider of inventory.providers) {
    const ref = `solovey-ui@${provenance.version}?core-provider=${provider.id}`
    const component = components.get(ref)
    assert.ok(component && component.type === 'application', 'SBOM provider application is missing')
    assert.equal(property(component, 'solovey:coreProvider'), provider.id)
    assert.equal(property(component, 'solovey:coreProviderSource'), JSON.stringify(provider.source))
    assert.equal(property(component, 'solovey:moduleGraphDigest'), provider.moduleGraphDigest)
    const edges = bom.dependencies.filter(item => item.ref === ref)
    assert.equal(edges.length, 1, 'SBOM provider dependency graph is ambiguous')
    assert.deepEqual(edges[0].dependsOn, [...new Set([...provider.dependencies, ...common])].sort(), 'SBOM provider dependency graph differs')
  }
  const root = `solovey-ui@${provenance.version}`
  assert.equal(bom.metadata.component['bom-ref'], root, 'SBOM root application differs')
  assert.deepEqual(bom.dependencies.find(item => item.ref === root)?.dependsOn,
    inventory.providers.map(item => `${root}?core-provider=${item.id}`), 'SBOM omits a provider build flavor')
}

export async function verifyMetadata(directory, commit) {
  assert.match(commit, /^[a-f0-9]{40}$/)
  assert.deepEqual(fs.readdirSync(directory).sort(), metadataNames.flatMap(n => [n, `${n}.sha256`]).sort())
  for (const name of metadataNames) {
    const file = path.join(directory, name)
    assert.ok(fs.lstatSync(file).isFile() && fs.statSync(file).size > 0)
    assert.equal(fs.readFileSync(`${file}.sha256`, 'utf8'), `${await digestFile(file)}  ${name}\n`)
  }
  const p = json(path.join(directory, 'solovey-ui-provenance.json'))
  assert.equal(p.source.commit, commit)
  assert.equal(p.source.tree, run('git', ['rev-parse', `${commit}^{tree}`]).trim())
  assert.equal(p.source.fingerprint, run(process.execPath, ['scripts/release-source.mjs', process.cwd()]).trim())
  assert.equal(p.schema, 'solovey.release-provenance/v1')
  assert.equal(p.officialSemanticBaseline.version, 'v1.14.2')
  assert.equal(p.officialByteIdentical, false)
  assert.deepEqual(p.subjects.map(s => s.name).sort(), metadataNames.filter(n => n !== 'solovey-ui-provenance.json'))
  for (const entry of p.subjects) {
    const file = path.join(directory, entry.name)
    assert.equal(await digestFile(file), entry.sha256)
    assert.equal(fs.statSync(file).size, entry.size)
  }
  const bom = json(path.join(directory, 'solovey-ui-sbom.cdx.json'))
  assert.equal(bom.bomFormat, 'CycloneDX')
  assert.equal(bom.metadata.component.version, p.version)
  assert.equal(bom.metadata.component.properties.find(x => x.name === 'solovey:sourceCommit').value, commit)
  const { inventory } = readProviderModuleGraphs()
  verifyProviderInventory(p, bom, inventory)
  assert.deepEqual(p.integrations, ['sing-box-ssm', 'sing-quic', 'sing-anytls'].map(name => json(`deploy/dependencies/${name}-integration.json`)))
  assert.deepEqual(p.extendedIntegration, readExtendedProvenance())
  const native = json('deploy/dependencies/cronet-native-source.json')
  assert.deepEqual(p.nativeSource, native)
  const nativeComponent = bom.components.find(c => c.name === 'SagerNet/naiveproxy')
  assert.equal(nativeComponent?.version, native.nativeSource.commit)
  assert.equal(nativeComponent.hashes[0].content, native.nativeSource.archiveSha256)
  for (const integration of [...p.integrations, ...p.extendedIntegration.sources]) {
    const component = bom.components.find(c => c.name === integration.replacement.module && c.version === integration.replacement.version)
    assert.equal(component?.version, integration.replacement.version)
    assert.equal(component.properties.find(x => x.name === 'solovey:sourceCommit').value, integration.replacement.commit)
    assert.equal(component.properties.find(x => x.name === 'solovey:patchSha256').value, integration.patch.sha256)
  }
  return p
}

async function produce(directory, commit) {
  const root = process.cwd()
  assert.match(commit, /^[a-f0-9]{40}$/)
  assert.equal(run('git', ['rev-parse', 'HEAD']).trim(), commit)
  run('git', ['diff', '--exit-code', 'HEAD', '--'])
  assert.equal(run('git', ['status', '--porcelain']).trim(), '', 'metadata requires a clean exact source checkout')
  const tree = run('git', ['rev-parse', 'HEAD^{tree}']).trim()
  const timestamp = run('git', ['show', '-s', '--format=%cI', commit]).trim()
  const version = fs.readFileSync('config/identity/version', 'utf8').trim()
  const fingerprint = run(process.execPath, ['scripts/release-source.mjs', root]).trim()
  const integrations = ['sing-box-ssm', 'sing-quic', 'sing-anytls'].map(name => json(`deploy/dependencies/${name}-integration.json`))
  const extendedIntegration = readExtendedProvenance()
  for (const name of ['ssm', 'quic', 'anytls']) run(process.execPath, [`scripts/${name}-integration-provenance.mjs`, 'check'])
  const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'solovey-release-metadata-'))
  const source = path.join(scratch, 'source'), notices = path.join(scratch, 'notices')
  fs.mkdirSync(directory, { recursive: true })
  assert.equal(fs.readdirSync(directory).length, 0, 'metadata destination must be empty')
  try {
    fs.mkdirSync(source); fs.mkdirSync(notices)
    const archive = path.join(scratch, 'project.tar')
    run('git', ['archive', '--format=tar', `--output=${archive}`, commit])
    const project = path.join(source, 'solovey-ui')
    fs.mkdirSync(project)
    run('tar', ['-xf', archive, '-C', project])
    for (const name of ['LICENSE', 'NOTICE.md']) fs.copyFileSync(name, path.join(notices, name))

    // Resolve in an isolated module request directory: downloading unused
    // platform sources cannot edit the frozen product's go.mod/go.sum.
    const request = path.join(scratch, 'modules')
    fs.mkdirSync(request)
    const { inventory, downloads } = readProviderModuleGraphs(request)
    console.log(`Authenticated Go source archives: ${downloads.size}; unique selected sources: ${inventory.modules.length}`)
    const npm = json('frontend/package-lock.json')
    const components = [], dependencies = [], sourceRecords = [], noticeRecords = []
    const goDirectory = path.join(source, 'dependency-sources', 'go')
    const npmDirectory = path.join(source, 'dependency-sources', 'npm')
    fs.mkdirSync(goDirectory, { recursive: true }); fs.mkdirSync(npmDirectory, { recursive: true })
    const addNotice = (identity, relative, bytes) => {
      const prefix = sha(identity).slice(0, 24), target = path.join(notices, prefix, relative)
      fs.mkdirSync(path.dirname(target), { recursive: true }); fs.writeFileSync(target, bytes)
      noticeRecords.push({ dependency: identity, path: `${prefix}/${relative.replaceAll('\\', '/')}`, sha256: sha(bytes), size: bytes.length })
    }
    const scanNotices = (identity, base, relative = '') => {
      for (const e of fs.readdirSync(path.join(base, relative), { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
        const name = path.join(relative, e.name)
        if (e.isDirectory()) scanNotices(identity, base, name)
        else if (e.isFile() && /^(?:licen[cs]e|copying|notice|copyright)(?:[._-]|$)/i.test(e.name)) addNotice(identity, name, fs.readFileSync(path.join(base, name)))
      }
    }
    // Native libcronet Go modules contain prebuilt libraries and their wrapper
    // license. Include the corresponding pinned C++ source and original
    // Chromium/third-party notices as a separate, authenticated source input.
    const native = json('deploy/dependencies/cronet-native-source.json')
    assert.equal(native.schema, 'solovey.native-cronet-source/v1')
    assert.match(native.nativeSource.commit, /^[a-f0-9]{40}$/)
    assert.equal(native.nativeSource.archiveURL, `https://codeload.github.com/SagerNet/naiveproxy/tar.gz/${native.nativeSource.commit}`)
    const libraries = inventory.modules.filter(m => m.name.startsWith('github.com/sagernet/cronet-go/lib/'))
    assert.ok(libraries.length > 0 && libraries.every(m => m.version === native.goModuleVersion), 'native source lock differs from resolved library modules')
    const nativeDirectory = path.join(source, 'dependency-sources', 'native')
    fs.mkdirSync(nativeDirectory, { recursive: true })
    const nativeName = `naiveproxy-${native.nativeSource.commit}.tar.gz`, nativeFile = path.join(nativeDirectory, nativeName)
    const response = await fetch(native.nativeSource.archiveURL, { redirect: 'error', signal: AbortSignal.timeout(300000) })
    assert.ok(response.ok, `native source HTTP ${response.status}`)
    const nativeHash = crypto.createHash('sha256'), fd = fs.openSync(nativeFile, 'wx')
    let nativeSize = 0
    try {
      for await (const bytes of response.body) {
        nativeSize += bytes.length
        assert.ok(nativeSize <= native.nativeSource.archiveSize, 'native source archive exceeds its pinned size')
        nativeHash.update(bytes); fs.writeSync(fd, bytes)
      }
    } finally { fs.closeSync(fd) }
    assert.equal(nativeSize, native.nativeSource.archiveSize)
    assert.equal(nativeHash.digest('hex'), native.nativeSource.archiveSha256)
    const nativeIdentity = `native:SagerNet/naiveproxy@${native.nativeSource.commit}`
    const nativeEntries = new Map()
    for (const entry of native.notices) {
      assert.ok(!entry.path.includes('..') && !path.isAbsolute(entry.path))
      nativeEntries.set(native.nativeSource.archivePrefix + entry.path, entry)
    }
    const nativeOrder = run('tar', ['-tzf', nativeFile]).trim().split('\n').filter(name => nativeEntries.has(name))
    assert.equal(nativeOrder.length, native.notices.length)
    const nativeNotices = execFileSync('tar', ['-xOf', nativeFile, '--', ...nativeOrder], { maxBuffer: 8 << 20 })
    let offset = 0
    for (const name of nativeOrder) {
      const entry = nativeEntries.get(name), bytes = nativeNotices.subarray(offset, offset + entry.size)
      assert.equal(bytes.length, entry.size); assert.equal(sha(bytes), entry.sha256)
      addNotice(nativeIdentity, entry.path, bytes)
      offset += entry.size
    }
    assert.equal(offset, nativeNotices.length)
    sourceRecords.push({ ecosystem: 'native-git', name: 'SagerNet/naiveproxy', version: native.nativeSource.commit, source: `dependency-sources/native/${nativeName}`, sha256: native.nativeSource.archiveSha256, distribution: native.nativeSource.archiveURL })
    components.push({ type: 'library', 'bom-ref': nativeIdentity, name: 'SagerNet/naiveproxy', version: native.nativeSource.commit, scope: 'optional', licenses: [{ license: { id: 'BSD-3-Clause' } }], hashes: [{ alg: 'SHA-256', content: native.nativeSource.archiveSha256 }], externalReferences: [{ type: 'vcs', url: `${native.nativeSource.repository}/tree/${native.nativeSource.commit}` }], properties: [{ name: 'solovey:nativeVersion', value: native.nativeVersion }, { name: 'solovey:nativeLicenseScope', value: native.licenseScope }] })
    writeJSON(path.join(source, 'NATIVE_SOURCE_DISCLOSURE.json'), native)
    writeJSON(path.join(notices, 'NATIVE_SOURCE_DISCLOSURE.json'), native)
    console.log(`Authenticated native source snapshot and ${native.notices.length} original notices`)
    for (const module of inventory.modules) {
      const bytes = downloads.get(module.identity)
      assert.ok(bytes && !bytes.Error && bytes.Zip && bytes.Dir && bytes.Sum && bytes.GoModSum, `missing immutable source: ${module.name}`)
      if (module.moduleSum) assert.equal(bytes.Sum, module.moduleSum)
      if (module.goModSum) assert.equal(bytes.GoModSum, module.goModSum)
      const identity = module.identity, name = `${sha(identity)}.zip`
      fs.copyFileSync(bytes.Zip, path.join(goDirectory, name))
      sourceRecords.push({ ecosystem: 'golang', name: module.name, version: module.version, providers: module.providers,
        source: `dependency-sources/go/${name}`, sha256: await digestFile(bytes.Zip), moduleSum: bytes.Sum, goModSum: bytes.GoModSum })
      scanNotices(identity, bytes.Dir)
      const integration = integrations.find(i => i.replacement.module === module.name && i.replacement.version === module.version)
      const extended = extendedIntegration.sources.find(i => i.replacement.module === module.name && i.replacement.version === module.version)
      const properties = [{ name: 'solovey:moduleSum', value: bytes.Sum }, { name: 'solovey:goModSum', value: bytes.GoModSum },
        { name: 'solovey:coreProviders', value: module.providers.join(',') }]
      for (const binding of module.bindings) properties.push({ name: `solovey:requires:${binding.provider}`, value: `${binding.module}@${binding.version}` })
      if (integration) properties.push({ name: 'solovey:sourceCommit', value: integration.replacement.commit }, { name: 'solovey:officialSemanticBaseline', value: `${integration.semanticBaseline.module}@${integration.semanticBaseline.version}` }, { name: 'solovey:patchSha256', value: integration.patch.sha256 }, { name: 'solovey:officialByteIdentical', value: 'false' })
      if (extended) properties.push({ name: 'solovey:sourceCommit', value: extended.replacement.commit }, { name: 'solovey:upstreamSource', value: `${extended.original.repository}@${extended.original.commit}` }, { name: 'solovey:patchSha256', value: extended.patch.sha256 }, { name: 'solovey:upstreamByteIdentical', value: 'false' })
      const license = integration?.license ?? extended?.license
      components.push({ type: 'library', 'bom-ref': identity, name: module.name, version: module.version, scope: 'optional', purl: `pkg:golang/${module.name}@${encodeURIComponent(module.version)}`, ...(license ? { licenses: [{ license: { id: license } }] } : {}), properties })
    }
    const npmSources = new Map()
    for (const [location, item] of Object.entries(npm.packages).sort(([a], [b]) => a.localeCompare(b))) {
      if (!location) continue
      assert.ok(item.resolved?.startsWith('https://registry.npmjs.org/') && item.integrity, `unpinned npm source ${location}`)
      const name = item.name ?? location.slice(location.lastIndexOf('node_modules/') + 'node_modules/'.length)
      const identity = `npm:${name}@${item.version}`
      if (npmSources.has(identity)) {
        assert.equal(npmSources.get(identity).integrity, item.integrity, 'duplicate npm source integrity differs')
        continue
      }
      const response = await fetch(item.resolved, { redirect: 'error', signal: AbortSignal.timeout(60000) })
      assert.ok(response.ok, `npm source HTTP ${response.status}: ${name}`)
      const bytes = Buffer.from(await response.arrayBuffer())
      assert.ok(bytes.length > 0 && bytes.length < 128 * 1024 * 1024)
      const sri = item.integrity.split(/\s+/).find(x => x.startsWith('sha512-'))
      assert.ok(sri, `npm source requires SHA512 SRI: ${name}`)
      assert.equal('sha512-' + crypto.createHash('sha512').update(bytes).digest('base64'), sri)
      const filename = `${sha(identity)}.tgz`, file = path.join(npmDirectory, filename)
      fs.writeFileSync(file, bytes); npmSources.set(identity, { file, integrity: item.integrity })
      if (npmSources.size % 50 === 0) console.log(`Authenticated npm source archives: ${npmSources.size}`)
      sourceRecords.push({ ecosystem: 'npm', name, version: item.version, source: `dependency-sources/npm/${filename}`, sha256: sha(bytes), integrity: item.integrity, distribution: item.resolved })
      const members = run('tar', ['-tzf', file]).trim().split('\n')
      for (const member of members) {
        if (!member.endsWith('/') && /(?:^|\/)(?:licen[cs]e|copying|notice|copyright)(?:[._-]|$)/i.test(member) && !member.includes('..') && !path.isAbsolute(member)) {
          addNotice(identity, member, execFileSync('tar', ['-xOf', file, '--', member], { maxBuffer: 8 << 20 }))
        }
      }
      components.push({ type: 'library', 'bom-ref': identity, name, version: item.version, scope: item.dev || item.optional ? 'optional' : 'required', purl: `pkg:npm/${name.replace('@', '%40')}@${item.version}`, ...(item.license ? { licenses: [{ expression: item.license }] } : {}), hashes: [{ alg: 'SHA-512', content: Buffer.from(sri.slice(7), 'base64').toString('hex') }] })
    }
    const sharedSources = components.filter(item => item['bom-ref'].startsWith('npm:') || item['bom-ref'] === nativeIdentity).map(item => item['bom-ref'])
    const variants = inventory.providers.map(item => ({ type: 'application', 'bom-ref': `solovey-ui@${version}?core-provider=${item.id}`,
      name: 'solovey-ui', version, properties: [{ name: 'solovey:coreProvider', value: item.id },
        { name: 'solovey:coreProviderSource', value: JSON.stringify(item.source) }, { name: 'solovey:moduleGraphDigest', value: item.moduleGraphDigest }] }))
    components.push(...variants)
    for (const [index, provider] of inventory.providers.entries()) dependencies.push({ ref: variants[index]['bom-ref'], dependsOn: [...new Set([...provider.dependencies, ...sharedSources])].sort() })
    components.sort((a, b) => a['bom-ref'].localeCompare(b['bom-ref']))
    const app = { type: 'application', 'bom-ref': `solovey-ui@${version}`, name: 'solovey-ui', version, licenses: [{ license: { id: 'GPL-3.0-only' } }], properties: [{ name: 'solovey:sourceCommit', value: commit }, { name: 'solovey:sourceTree', value: tree }, { name: 'solovey:sourceFingerprint', value: fingerprint }] }
    dependencies.push({ ref: app['bom-ref'], dependsOn: variants.map(item => item['bom-ref']) })
    const bom = { bomFormat: 'CycloneDX', specVersion: '1.6', version: 1, metadata: { timestamp, component: app, properties: [{ name: 'solovey:inventoryScope', value: 'Complete resolved Official and Extended Go module graphs, npm lockfile and pinned native C++ source inventory; provider edges distinguish both graphs. Includes platform alternatives and build tools, not a claim that every source is linked into every artifact. Graph digests bind resolved coordinates; individual components bind authenticated source content checksums.' }] }, components, dependencies }
    writeJSON(path.join(directory, 'solovey-ui-sbom.cdx.json'), bom)
    sourceRecords.sort((a, b) => a.source.localeCompare(b.source))
    noticeRecords.sort((a, b) => a.path.localeCompare(b.path))
    writeJSON(path.join(source, 'DEPENDENCY_SOURCES.json'), sourceRecords)
    writeJSON(path.join(source, 'SOURCE_IDENTITY.json'), { commit, tree, version, sourceFingerprint: fingerprint })
    writeJSON(path.join(notices, 'LICENSE_INVENTORY.json'), noticeRecords)
    for (const integration of [...integrations, ...extendedIntegration.sources]) assert.ok(noticeRecords.some(n => n.dependency === `${integration.replacement.module}@${integration.replacement.version}`), 'integration license missing')
    for (const [name, input] of [['solovey-ui-source.tar.gz', source], ['solovey-ui-license-notices.tar.gz', notices]]) {
      const tar = path.join(scratch, `${name}.tar`)
      run('tar', ['--sort=name', '--mtime=@0', '--owner=0', '--group=0', '--numeric-owner', '--mode=u+rwX,go+rX,go-w', '--format=gnu', '-cf', tar, '-C', input, '.'])
      const fd = fs.openSync(path.join(directory, name), 'w')
      try { run('gzip', ['-n', '-c', tar], { stdio: ['ignore', fd, 'inherit'] }) } finally { fs.closeSync(fd) }
      assert.ok(fs.statSync(path.join(directory, name)).size < 2 * 1024 ** 3, 'source archive exceeds the public asset bound')
    }
    const subjects = []
    for (const name of metadataNames.filter(n => n !== 'solovey-ui-provenance.json')) subjects.push({ name, sha256: await digestFile(path.join(directory, name)), size: fs.statSync(path.join(directory, name)).size })
    writeJSON(path.join(directory, 'solovey-ui-provenance.json'), { schema: 'solovey.release-provenance/v1', version, source: { repository: 'https://github.com/MalenkiySolovey/solovey-ui', commit, tree, fingerprint }, officialSemanticBaseline: integrations[0].semanticBaseline, officialByteIdentical: false, integrations, extendedIntegration,
      coreProviders: inventory.providers.map(({ dependencies, ...provider }) => provider), nativeSource: native, toolchain: { go: run('go', ['version']).trim(), node: process.version, npm: run('npm', ['--version']).trim() }, builder: '.github/workflows/release.yml:release-metadata; scripts/release-metadata.mjs', inventory: { goModules: inventory.modules.length, goModulesByProvider: Object.fromEntries(inventory.providers.map(item => [item.id, item.moduleCount])), npmPackages: npmSources.size, nativeSourceSnapshots: 1, dependencyNotices: noticeRecords.length }, compatibility: json('scripts/release-compatibility.json'), subjects })
    for (const name of metadataNames) fs.writeFileSync(path.join(directory, `${name}.sha256`), `${await digestFile(path.join(directory, name))}  ${name}\n`)
    assert.equal(run(process.execPath, ['scripts/release-source.mjs', root]).trim(), fingerprint, 'producer changed frozen source')
    await verifyMetadata(directory, commit)
    console.log(JSON.stringify({ status: 'PASS', commit, tree, assets: metadataNames, goModules: inventory.modules.length, goModulesByProvider: Object.fromEntries(inventory.providers.map(item => [item.id, item.moduleCount])), npmPackages: npmSources.size, dependencyNotices: noticeRecords.length }))
  } finally {
    const actual = fs.realpathSync(scratch)
    assert.equal(path.dirname(actual), fs.realpathSync(os.tmpdir()))
    assert.ok(path.basename(actual).startsWith('solovey-release-metadata-'))
    fs.rmSync(actual, { recursive: true, force: true })
  }
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  const [mode, directory, commit] = process.argv.slice(2)
  assert.ok(directory && commit && ['produce', 'verify'].includes(mode), `usage: node ${fileURLToPath(import.meta.url)} <produce|verify> <empty-output-directory> <exact-commit>`)
  if (mode === 'produce') await produce(path.resolve(directory), commit)
  else { await verifyMetadata(path.resolve(directory), commit); console.log('release source, SBOM, licenses and provenance verified') }
}
