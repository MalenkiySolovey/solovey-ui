import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'
import { sha256 } from './frontend-artifacts.mjs'
import { compositionDigest, compositionSchema } from './release-composition.mjs'
import { compositionBuildFields, observeComponentArchive, verifyCompositionBuildFields, verifyPanelModules, verifyStagedFrontend } from './release-composition-build.mjs'
import { observePanelArchives, verifyArchiveBuildInfo } from './release-candidate.mjs'

const hash = value => sha256(value)
const target = { name: 'panel.tar.gz', platform: 'linux', arch: 'amd64', profile: 'full', provider: 'sing-box' }
const frontend = { archiveSHA256: hash('front'), embedded: [['index.html', hash('app')]], packed: [['optional/component.json', hash('pack')]] }
const pin = (module, version = 'v1.0.0') => ({ module, version, moduleChecksum: 'h1:fixture' })
const backend = pin('example.test/backend'), host = pin('example.test/host'), optional = pin('example.test/component')
const copy = { module: 'example.test/engine-copy', version: 'v1.2.3', sum: 'h1:core' }
const graph = {
  schema: compositionSchema, toolchain: { go: '1.26.11' }, eligibility: { linux: [target, {...target, profile: 'core'}] },
  profiles: { full: { frontend }, core: { frontend: { archiveSHA256: hash('core') } } },
  backend: { source: backend }, shared: [host, {...pin('example.test/contracts'), contractVersion: 'test/v1'}],
  components: { external: [{...optional, id: 'optional'}] },
  providers: { 'sing-box': { engine: { module: copy.module }, copies: [{original: {module: 'example.test/engine'}, replacement: copy}] } },
}
const receipt = { graph, sha256: compositionDigest(graph) }
const binary = 'this fixture is never executed'

test('a complete build binding rejects stale composition, wrong profile, wrong graph and mixed binary', () => {
  const fields = compositionBuildFields(receipt, target, frontend, hash(binary))
  verifyCompositionBuildFields(fields, receipt, target, frontend, hash(binary))
  assert.throws(() => verifyCompositionBuildFields(fields, receipt, target, frontend, hash('other candidate')), /panel_sha256/)
  assert.throws(() => verifyCompositionBuildFields(fields, receipt, target, {...frontend, packed: []}, hash(binary)), /component_files_sha256/)
  assert.throws(() => compositionBuildFields(receipt, {...target, profile: 'core'}, frontend, hash(binary)), /Frontend profile/)
  assert.throws(() => compositionBuildFields(receipt, {...target, provider: 'shtorm-extended'}, frontend, hash(binary)), /unadmitted/)
  assert.throws(() => compositionBuildFields({...receipt, sha256: hash('stale')}, target, frontend, hash(binary)), /composition digest/)
  const changed = {...graph, toolchain: {go: '1.2.3'}}
  assert.throws(() => verifyCompositionBuildFields(fields, { graph: changed, sha256: compositionDigest(changed) }, target, frontend, hash(binary)), /archive composition_sha256/)
})

test('actual Go module metadata must match the selected provider and runtime profile', () => {
  const module = ({module, version, moduleChecksum}) => ({Path: module, Version: version, Sum: moduleChecksum})
  const info = { GoVersion: 'go1.26.11', Settings: [{Key:'GOOS', Value:'linux'}, {Key:'GOARCH', Value:'amd64'}, {Key:'-tags', Value:'with_acme'}],
    Deps: [module(backend), module(host), module(optional), {Path: 'example.test/engine', Replace: module({...copy, moduleChecksum: copy.sum})}] }
  verifyPanelModules(info, target, receipt)
  assert.throws(() => verifyPanelModules({...info, Deps: info.Deps.slice(1)}, target, receipt), /backend/)
  const minimal = {...info, Settings: info.Settings.map(item => item.Key === '-tags' ? {...item, Value: 'with_acme,minimal'} : item)}
  assert.throws(() => verifyPanelModules(minimal, {...target, profile: 'core'}, receipt), /Core links optional component/)
  verifyPanelModules({...minimal, Deps: minimal.Deps.filter(item => item.Path !== optional.module)}, {...target, profile: 'core'}, receipt)
  const substituted = structuredClone(info); substituted.Deps.at(-1).Replace.Sum = 'h1:unexpected'
  assert.throws(() => verifyPanelModules(substituted, target, receipt), /engine-copy/)
})

test('candidate inspection hashes actual panel and pack members and rejects missing members', {skip: process.platform === 'win32'}, t => {
  const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'composition-package-test-'))
  t.after(() => fs.rmSync(scratch, {recursive: true, force: true}))
  const tag = 'v2026.5.1', identity = {commit: '1'.repeat(40), tree: '2'.repeat(40), fingerprint: hash('sources'), tag}
  fs.mkdirSync(path.join(scratch, 'solovey-ui'))
  const fields = compositionBuildFields(receipt, target, frontend, hash(binary))
  const build = {commit:identity.commit, tree:identity.tree, source_fingerprint:identity.fingerprint, version:tag, platform:'linux/amd64', profile:'full', core_provider:'sing-box', ...fields}
  const data = Object.entries(build).map(([key, value]) => `${key}=${value}`).join('\n')
  fs.writeFileSync(path.join(scratch, 'solovey-ui/BUILD_INFO.txt'), data)
  fs.writeFileSync(path.join(scratch, 'solovey-ui/solovey-ui'), binary)
  const tar = (file, member) => execFileSync('tar', ['-czf', path.join(scratch, file), '-C', scratch, member])
  tar(target.name, 'solovey-ui')
  const [seen] = observePanelArchives(scratch, null, [target], [], {hashPanel: true})
  verifyCompositionBuildFields(verifyArchiveBuildInfo(seen.data, target, identity), receipt, target, frontend, seen.binarySHA256)
  assert.throws(() => verifyArchiveBuildInfo(`${seen.data}\ncommit=duplicate`, target, identity), /duplicate/)
  tar(target.name, 'solovey-ui/BUILD_INFO.txt')
  assert.throws(() => observePanelArchives(scratch, null, [target], [], {hashPanel: true}), /Command failed/)
  fs.mkdirSync(path.join(scratch, 'components/optional'), {recursive: true})
  fs.writeFileSync(path.join(scratch, 'components/optional/component.json'), 'pack')
  tar('packs.tar.gz', 'components')
  assert.deepEqual(observeComponentArchive(path.join(scratch, 'packs.tar.gz')), Object.fromEntries(frontend.packed))
  fs.unlinkSync(path.join(scratch, 'components/optional/component.json'))
  tar('packs.tar.gz', 'components')
  assert.notDeepEqual(observeComponentArchive(path.join(scratch, 'packs.tar.gz')), Object.fromEntries(frontend.packed))
  const script = fileURLToPath(new URL('./release-composition-build.mjs', import.meta.url))
  const composition = path.join(scratch, 'composition.json')
  fs.writeFileSync(composition, JSON.stringify(receipt))
  assert.throws(() => execFileSync(process.execPath, [script, 'build', composition, 'linux', 'amd64', 'full', 'sing-box', path.join(scratch, 'solovey-ui/solovey-ui'), 'packs', '--', 'unused'], {stdio:'pipe'}), /fresh Panel output/)
})

test('OpenWrt generated and packaged byte inventories agree with one verified layout', () => {
  const facts = {frontend: {artifact: {archiveSHA256: frontend.archiveSHA256}}}
  const generated = [{path:'web/html/index.html', sha256:hash('app')}, {path:'.release/components/optional/component.json', sha256:hash('pack')}]
  const stage = [{path:'usr/lib/solovey-ui/components/optional/component.json', type:'file', sha256:hash('pack')}]
  verifyStagedFrontend(facts, generated, stage, frontend)
  assert.throws(() => verifyStagedFrontend(facts, generated.slice(1), stage, frontend), /embed inputs/)
  assert.throws(() => verifyStagedFrontend(facts, generated, [], frontend), /staged pack bytes/)
})
