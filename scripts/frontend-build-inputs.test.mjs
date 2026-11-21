import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import {execFileSync} from 'node:child_process'
import {runFrontendTask} from './frontend.mjs'
import {digest, frontendDigest, regularSourceTree} from './component-source-inputs.mjs'
import {projectFrontendBuildInputs} from './frontend-build-inputs.mjs'
import {frontendAPIFiles, readFrontendBuildInputs, validateFrontendBuildInputs, selectFrontendEntries} from './frontend-source.mjs'
import {frontendSourceDirectory} from './frontend-source.mjs'

function fixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'frontend inputs '))
  t.after(() => fs.rmSync(root, {recursive: true, force: true}))
  const frontend = path.join(root, 'frontend')
  const write = (name, bytes) => { fs.mkdirSync(path.dirname(path.join(root, name)), {recursive: true}); fs.writeFileSync(path.join(root, name), bytes) }
  const api = Object.fromEntries(frontendAPIFiles.map(name => [name, name === '.node-version' ? '25.9.0\n' : name === 'package-lock.json' ? '{}\n' : 'export {}\n']))
  for (const [name, bytes] of Object.entries(api)) write('frontend/' + name, bytes)
  const source = {id: 'telegram', module: 'github.com/MalenkiySolovey/solovey-component-telegram', version: 'v0.1.0', sourceCommit: '1'.repeat(40), sourceTree: '', moduleChecksum: `h1:${'a'.repeat(43)}=`, goModChecksum: `h1:${'b'.repeat(43)}=`, manifestSHA256: '', frontendSHA256: '', files: {}}
  const pin = {repository: 'MalenkiySolovey/solovey-ui', apiVersion: '1.0', sourceCommit: '1'.repeat(40), files: Object.fromEntries(Object.entries(api).map(([name, bytes]) => [name, digest(bytes)]))}
  const contents = {'go.mod': 'module example\n', 'go.sum': 'sum\n', 'LICENSE': 'GPL-3.0\n', 'NOTICE': 'Attribution\n', 'SOURCE_PROVENANCE.json': '{}\n', 'HOST_UI_PIN.json': JSON.stringify(pin), 'component.json': JSON.stringify({id: 'telegram', frontend: {entries: ['frontend/index.ts']}}), 'frontend/index.ts': 'export const register = () => import("./view.ts")\n', 'frontend/view.ts': 'export default true\n', 'frontend/locales/en.json': '{}\n', 'frontend/view.test.ts': 'export {}\n'}
  for (const [name, bytes] of Object.entries(contents)) { write('.component-sources/telegram/' + name, bytes); source.files[name] = digest(bytes) }
  source.sourceTree = regularSourceTree(path.join(root, '.component-sources/telegram'))
  source.manifestSHA256 = source.files['component.json']
  source.frontendSHA256 = frontendDigest(source.files)
  const sourceLock = {schema: 'solovey.component-source-inputs/v1', sources: [source]}
  write('components/sources.lock.json', JSON.stringify(sourceLock))
  write('components/telegram/component.json', contents['component.json'])
  write('components/panel-example/component.json', JSON.stringify({id: 'panel-example', frontend: {entries: ['frontend/index.ts']}}))
  write('components/panel-example/frontend/index.ts', 'export const register = () => {}\n')
  write('components/panel-example/frontend/locales/ru.json', '{}\n')
  write('components/panel-example/owner.go', 'package owner\n')
  const prepare = () => projectFrontendBuildInputs(root, {frontendRoot: frontend, directory: path.join(frontend, '.component-inputs')})
  const read = prepared => readFrontendBuildInputs(frontend, prepared.manifestSHA256)
  const inputRoot = path.join(frontend, '.component-inputs')
  const manifestPath = path.join(inputRoot, 'manifest.json')
  const manifest = () => JSON.parse(fs.readFileSync(manifestPath))
  const reseal = value => { const bytes = JSON.stringify(value, null, 2) + '\n'; fs.writeFileSync(manifestPath, bytes); return {manifestSHA256: digest(bytes)} }
  return {root, frontend, source, sourceLock, prepare, read, inputRoot, manifestPath, manifest, reseal, write}
}

test('Product projects exact pinned UI, retains locales/tests, excludes Go, and repeats deterministically', t => {
  const f = fixture(t), first = f.prepare(), accepted = f.read(first)
  assert.deepEqual(first.components, ['panel-example', 'telegram'])
  assert.deepEqual(accepted.manifest.sourceLock, f.sourceLock)
  assert.equal(first.sourceLockDigest, digest(JSON.stringify(f.sourceLock)))
  assert.ok(fs.existsSync(path.join(f.inputRoot, 'components/telegram/frontend/view.test.ts')))
  assert.ok(fs.existsSync(path.join(f.inputRoot, 'components/panel-example/frontend/locales/ru.json')))
  assert.ok(!fs.existsSync(path.join(f.inputRoot, 'components/panel-example/owner.go')))
  assert.ok(!fs.existsSync(path.join(f.inputRoot, 'components/telegram/go.mod')))
  assert.equal(f.prepare().manifestSHA256, first.manifestSHA256)
  assert.ok(!fs.existsSync(path.join(f.frontend, '.component-inputs.lock')))
})

test('an immutable headless component does not alter the admitted Frontend tuple', t => {
  const f = fixture(t), before = f.prepare()
  const manifest = JSON.stringify({id: 'zapret', delivery: 'in-process'})
  const files = Object.fromEntries(['go.mod', 'go.sum', 'LICENSE', 'NOTICE', 'SOURCE_PROVENANCE.json'].map(name => [name, digest(name)]))
  files['component.json'] = digest(manifest)
  const headless = {...f.source, id: 'zapret', module: 'github.com/MalenkiySolovey/solovey-component-zapret',
    manifestSHA256: files['component.json'], frontendSHA256: frontendDigest(files), files}
  f.write('components/zapret/component.json', manifest)
  f.write('components/sources.lock.json', JSON.stringify({...f.sourceLock, sources: [...f.sourceLock.sources, headless]}))
  assert.deepEqual(f.prepare(), before)
  headless.files['frontend/index.ts'] = digest('export {}')
  f.write('components/sources.lock.json', JSON.stringify({...f.sourceLock, sources: [...f.sourceLock.sources, headless]}))
  assert.throws(() => f.prepare(), /headless module cannot declare Frontend inputs/)
})

test('a new pinned input tuple does not read or project the legacy Panel Update archive', t => {
  const f = fixture(t), expected = f.prepare()
  const pin = JSON.parse(fs.readFileSync(new URL('../deploy/dependencies/solovey-frontend.json', import.meta.url)))
  Object.assign(pin.input, {manifestSHA256: expected.manifestSHA256, sourceLockDigest: expected.sourceLockDigest})
  for (const [name, profile] of Object.entries(pin.profiles)) {
    if (name !== 'core-empty-inputs') profile.inputManifestSHA256 = expected.manifestSHA256
  }
  f.write('deploy/dependencies/solovey-frontend.json', JSON.stringify(pin))
  assert.deepEqual(f.prepare(), expected)
  assert.ok(!fs.existsSync(path.join(f.inputRoot, 'components/panel-update-ui')))
})

test('consumer verifies in isolation with no Product siblings, Go, Git, or PATH', t => {
  const f = fixture(t), prepared = f.prepare()
  const isolated = fs.mkdtempSync(path.join(os.tmpdir(), 'isolated frontend '))
  t.after(() => fs.rmSync(isolated, {recursive: true, force: true}))
  fs.cpSync(f.frontend, isolated, {recursive: true})
  const scripts = path.join(frontendSourceDirectory, 'scripts')
  fs.mkdirSync(path.join(isolated, 'scripts'))
  for (const name of ['component-build-inputs.mjs', 'component-source-integrity.mjs']) fs.copyFileSync(path.join(scripts, name), path.join(isolated, 'scripts', name))
  const result = JSON.parse(execFileSync(process.execPath, ['scripts/component-build-inputs.mjs', 'verify'], {cwd: isolated, encoding: 'utf8', env: {...process.env, PATH: '', SOLOVEY_UI_INPUTS_SHA256: prepared.manifestSHA256}}))
  assert.deepEqual(result.components, prepared.components)
  assert.equal(result.manifestSHA256, prepared.manifestSHA256)
})

test('no implicit Product source fallback when declared inputs are absent', t => {
  const f = fixture(t)
  assert.throws(() => readFrontendBuildInputs(f.frontend, '0'.repeat(64)), /ENOENT/)
})
for (const expected of [undefined, '', 'latest', '0'.repeat(64)]) {
  test('reject missing or incorrect caller SHA256: ' + String(expected), t => {
    const f = fixture(t); f.prepare()
    assert.throws(() => readFrontendBuildInputs(f.frontend, expected))
  })
}
test('manifest bytes are immutable, including whitespace', t => {
  const f = fixture(t), prepared = f.prepare()
  fs.appendFileSync(f.manifestPath, ' ')
  assert.throws(() => f.read(prepared), /manifest SHA256/)
})
for (const [name, mutate] of [
  ['schema', m => { m.schema = 'solovey.frontend-build-inputs/v2' }],
  ['unknown field', m => { m.url = 'https://example.invalid/ui.js' }],
  ['dynamic source', m => { m.components[0].source = 'https://example.invalid/ui.js' }],
  ['duplicate ID', m => { m.components.push(structuredClone(m.components[0])) }],
  ['invalid ID', m => { m.components[0].id = '../escape' }],
  ['unbound source lock', m => { m.sourceLockDigest = '0'.repeat(64) }],
  ['mutable module version', m => { m.sourceLock.sources[0].version = 'main'; m.sourceLockDigest = digest(JSON.stringify(m.sourceLock)) }],
  ['foreign module owner', m => { m.sourceLock.sources[0].module = 'github.com/other/telegram'; m.sourceLockDigest = digest(JSON.stringify(m.sourceLock)) }],
  ['invalid module checksum', m => { m.sourceLock.sources[0].moduleChecksum = 'changed'; m.sourceLockDigest = digest(JSON.stringify(m.sourceLock)) }],
  ['wrong manifest hash', m => { m.components[0].manifestSHA256 = '0'.repeat(64) }],
  ['non-UI file', m => { m.components[0].files['owner.go'] = '0'.repeat(64) }],
  ['external UI substitution', m => { const c = m.components.find(c => c.id === 'telegram'); c.files['frontend/view.ts'] = '0'.repeat(64); c.frontendSHA256 = frontendDigest(c.files) }],
]) {
  test('reject malformed input contract: ' + name, t => {
    const f = fixture(t); f.prepare(); const m = f.manifest(); mutate(m)
    assert.throws(() => validateFrontendBuildInputs(m))
  })
}
test('reject wrong projected Git tree even when manifest is caller-pinned', t => {
  const f = fixture(t); f.prepare(); const m = f.manifest(); m.components[0].inputTree = '0'.repeat(40)
  assert.throws(() => f.read(f.reseal(m)), /input tree mismatch/)
})
for (const name of ['frontend/index.ts', 'frontend/view.ts', 'component.json']) {
  test('reject changed and missing UI file: ' + name, t => {
    const f = fixture(t), prepared = f.prepare(), filename = path.join(f.inputRoot, 'components/telegram', name)
    fs.appendFileSync(filename, 'changed')
    assert.throws(() => f.read(prepared), /source drift/)
    fs.unlinkSync(filename)
    assert.throws(() => f.read(prepared), /ENOENT/)
  })
}
test('reject extra UI source and extra non-UI source', t => {
  const f = fixture(t), prepared = f.prepare(), directory = path.join(f.inputRoot, 'components/telegram')
  fs.writeFileSync(path.join(directory, 'frontend/injected.ts'), 'export {}')
  assert.throws(() => f.read(prepared), /closure/)
  fs.unlinkSync(path.join(directory, 'frontend/injected.ts'))
  fs.writeFileSync(path.join(directory, 'injected.js'), 'export {}')
  assert.throws(() => f.read(prepared), /tree mismatch/)
})
test('reject component identity drift even after rehashing a local UI projection', t => {
  const f = fixture(t); f.prepare(); const m = f.manifest(), c = m.components[0]
  const directory = path.join(f.inputRoot, 'components', c.id), filename = path.join(directory, 'component.json')
  fs.writeFileSync(filename, JSON.stringify({id: 'other-component', frontend: {entries: ['frontend/index.ts']}}))
  c.manifestSHA256 = c.files['component.json'] = digest(fs.readFileSync(filename))
  c.inputTree = regularSourceTree(directory)
  assert.throws(() => f.read(f.reseal(m)), /identity mismatch/)
})
for (const name of ['.node-version', 'package-lock.json', 'src/componentSystem/contributions.ts']) {
  test('reject frontend toolchain/API drift: ' + name, t => {
    const f = fixture(t), prepared = f.prepare()
    fs.appendFileSync(path.join(f.frontend, name), 'changed')
    assert.throws(() => f.read(prepared), /toolchain input drift/)
  })
}
test('reject extra catalogue/root entries, missing component, and directory junction', t => {
  const f = fixture(t), prepared = f.prepare(), directory = path.join(f.inputRoot, 'components/telegram')
  const extra = path.join(f.inputRoot, 'components/unknown')
  fs.mkdirSync(extra)
  assert.throws(() => f.read(prepared), /unexpected or missing/)
  fs.rmdirSync(extra)
  fs.writeFileSync(path.join(f.inputRoot, 'unexpected.json'), '{}')
  assert.throws(() => f.read(prepared), /unexpected frontend input root/)
  fs.unlinkSync(path.join(f.inputRoot, 'unexpected.json'))
  fs.renameSync(directory, path.join(f.root, 'alternate'))
  assert.throws(() => f.read(prepared), /unexpected or missing/)
  fs.symlinkSync(path.join(f.root, 'alternate'), directory, process.platform === 'win32' ? 'junction' : 'dir')
  assert.throws(() => f.read(prepared), /unsafe source directory/)
})
test('producer rejects full-source drift and preserves previous accepted UI inputs', t => {
  const f = fixture(t), accepted = f.prepare()
  fs.appendFileSync(path.join(f.root, '.component-sources/telegram/go.mod'), 'changed')
  assert.throws(() => f.prepare(), /source drift/)
  assert.equal(f.read(accepted).manifestSHA256, accepted.manifestSHA256)
  assert.ok(!fs.existsSync(path.join(f.frontend, '.component-inputs.lock')))
})
test('producer rejects concurrent publication and leaves accepted inputs intact', t => {
  const f = fixture(t), accepted = f.prepare(), lock = path.join(f.frontend, '.component-inputs.lock')
  fs.mkdirSync(lock)
  assert.throws(() => f.prepare(), /EEXIST/)
  assert.equal(f.read(accepted).manifestSHA256, accepted.manifestSHA256)
  fs.rmdirSync(lock)
})
test('an explicitly pinned empty catalogue is valid for a core-only frontend', t => {
  const f = fixture(t); f.prepare(); const m = f.manifest()
  m.components = []
  const directory = path.join(f.inputRoot, 'components')
  fs.rmSync(directory, {recursive: true}); fs.mkdirSync(directory)
  assert.deepEqual(f.read(f.reseal(m)).entries, [])
})

test('failed publication restores the accepted projection before releasing the lock', t => {
  const f = fixture(t), accepted = f.prepare(), original = fs.renameSync
  fs.renameSync = (source, target) => {
    if (path.basename(source) === 'candidate') throw new Error('injected publication failure')
    return original(source, target)
  }
  try { assert.throws(() => f.prepare(), /injected publication failure/) }
  finally { fs.renameSync = original }
  assert.equal(f.read(accepted).manifestSHA256, accepted.manifestSHA256)
  assert.ok(!fs.existsSync(path.join(f.frontend, '.component-inputs.lock')))
  assert.deepEqual(fs.readdirSync(f.frontend).filter(name => name.startsWith('.component-inputs-stage-')), [])
})

test('profile selection is drawn only from declared entries; Core/minimal admit no optional UI', t => {
  const f = fixture(t), input = f.read(f.prepare())
  assert.deepEqual(selectFrontendEntries(input, {}).map(entry => entry.id), ['panel-example', 'telegram'])
  assert.deepEqual(selectFrontendEntries(input, {SOLOVEY_UI_COMPONENT_IDS: 'telegram, telegram'}).map(entry => entry.id), ['telegram'])
  for (const profile of ['core', 'minimal']) assert.deepEqual(selectFrontendEntries(input, {SOLOVEY_UI_PROFILE: profile}), [])
  assert.deepEqual(selectFrontendEntries(input, {SOLOVEY_UI_COMPONENT_IDS: ''}), [])
  assert.throws(() => selectFrontendEntries(input, {SOLOVEY_UI_COMPONENT_IDS: 'absent'}), /unavailable/)
  assert.throws(() => selectFrontendEntries(input, {SOLOVEY_UI_COMPONENT_IDS: 'https://example.invalid/ui.js'}), /invalid/)
  assert.throws(() => selectFrontendEntries(input, {SOLOVEY_UI_PROFILE: 'unknown'}), /unsupported/)
})

test('Product adapter supplies freshly verified inputs and explicit E2E integration without shell argument loss', async t => {
  const f = fixture(t)
  fs.writeFileSync(path.join(f.root, 'components/sources.lock.json'), JSON.stringify({schema: 'solovey.component-source-inputs/v1', sources: []}))
  fs.rmSync(path.join(f.root, 'components/telegram'), {recursive: true})
  const npm = path.join(f.root, 'npm-cli.mjs'), output = path.join(f.root, 'observation.json')
  fs.writeFileSync(npm, `import fs from 'node:fs'; if(process.argv[2]==='--version'){console.log('11.12.1');process.exit(0)} fs.writeFileSync(process.env.SOLOVEY_FRONTEND_TEST_RESULT, JSON.stringify({args: process.argv.slice(2), cwd: process.cwd(), hash: process.env.SOLOVEY_UI_INPUTS_SHA256, server: process.env.SUI_E2E_SERVER_SCRIPT})); process.exitCode = 17`)
  const values = {npm_execpath: npm, SOLOVEY_FRONTEND_TEST_RESULT: output, SOLOVEY_UI_INPUTS_SHA256: 'untrusted'}
  const previous = Object.fromEntries(Object.keys(values).map(name => [name, process.env[name]]))
  Object.assign(process.env, values)
  try { assert.equal(await runFrontendTask('test:e2e', ['--grep', 'two words'], f.root, {frontendRoot: f.frontend}), 17) }
  finally { for (const [name, value] of Object.entries(previous)) { if (value === undefined) delete process.env[name]; else process.env[name] = value } }
  const observation = JSON.parse(fs.readFileSync(output))
  assert.deepEqual(observation.args, ['run', 'test:e2e', '--', '--grep', 'two words'])
  assert.equal(fs.realpathSync.native(observation.cwd), fs.realpathSync.native(f.frontend))
  assert.equal(observation.hash, digest(fs.readFileSync(f.manifestPath)))
  assert.equal(observation.server, path.join(fs.realpathSync.native(f.root), 'tests/e2e/run-server.js'))
  await assert.rejects(() => runFrontendTask('unknown', [], f.root), /unsupported/)
})
