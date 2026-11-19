import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { test } from 'node:test'
import { regularSourceTree } from './component-source-inputs.mjs'
import { compositionDigest, compositionSchema, readComponentCatalog, verifyComposition, verifyFrontendProjection, verifySharedModule } from './release-composition.mjs'

const makeReceipt = graph => ({ graph, sha256: compositionDigest(graph) })

test('composition uses only stable canonical input facts and preserves ordered selections', () => {
  const input = { schema: compositionSchema, product: { tree: 'tree', commit: 'commit' }, profiles: ['full', 'core'] }
  const same = { profiles: ['full', 'core'], product: { commit: 'commit', tree: 'tree' }, schema: compositionSchema }
  assert.equal(compositionDigest(input), compositionDigest(same))
  assert.notEqual(compositionDigest(input), compositionDigest({ ...same, profiles: ['core', 'full'] }))
  assert.deepEqual(verifyComposition(makeReceipt(same), makeReceipt(input)), makeReceipt(input))
})

test('composition receipt does not accept rewritten inputs, self-consistent substitutions or unknown fields', () => {
  const graph = { schema: compositionSchema, provider: 'sing-box', frontend: { profile: 'core' } }
  const original = makeReceipt(graph)
  assert.throws(() => verifyComposition({ ...original, sha256: 'incorrect' }, original), /composition digest differs/)
  assert.throws(() => verifyComposition({ ...original, extra: 1 }, original), /verified Product inputs/)
  assert.throws(() => verifyComposition(makeReceipt({ ...graph, provider: 'shtorm-extended' }), original), /verified Product inputs/)
  assert.throws(() => verifyComposition(makeReceipt({ ...graph, frontend: { profile: 'full' } }), original), /verified Product inputs/)
  assert.throws(() => verifyComposition(makeReceipt({ ...graph, schema: 'v99' }), original), /unknown composition schema/)
})

test('Frontend profile and caller-trusted Component source projection are independently bound', () => {
  const input = { manifestSHA256: 'trusted', sourceLockDigest: 'locked', components: ['example'] }
  const components = [{ id: 'example', hasFrontend: true }, { id: 'headless', hasFrontend: false }]
  const pin = { input, profiles: { full: { componentIDs: ['example'] }, core: { componentIDs: [] } } }
  verifyFrontendProjection(pin, input, components)
  assert.throws(() => verifyFrontendProjection(pin, { ...input, manifestSHA256: 'foreign' }, components), /input tuple drift/)
  assert.throws(() => verifyFrontendProjection(pin, { ...input, sourceLockDigest: 'foreign' }, components), /source authority drift/)
  assert.throws(() => verifyFrontendProjection(pin, { ...input, components: [] }, components), /component inventory differs/)
  assert.throws(() => verifyFrontendProjection({ ...pin, profiles: { full: pin.profiles.core, core: pin.profiles.full } }, input, components), /Full Frontend selection differs/)
})

test('reject missing Component owners, duplicate source owners and unsupported runtime roles', t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'composition-components-'))
  t.after(() => fs.rmSync(root, { recursive: true, force: true }))
  const create = (id, delivery) => {
    const directory = path.join(root, 'components', id)
    fs.mkdirSync(directory, { recursive: true })
    const bytes = JSON.stringify({ id, version: '1', delivery })
    fs.writeFileSync(path.join(directory, 'component.json'), bytes)
    return { id, module: 'github.com/example/component', manifestSHA256: createHash('sha256').update(bytes).digest('hex') }
  }
  const first = create('a', 'in-process')
  create('panel-update-ui', 'in-process')
  assert.deepEqual(readComponentCatalog(root, { sources: [first] }).map(component => component.id), ['a'])
  assert.throws(() => readComponentCatalog(root, { sources: [first, { ...first, id: 'missing' }] }), /missing or ambiguous Component source/)
  const second = create('b', 'in-process')
  assert.throws(() => readComponentCatalog(root, { sources: [first, second] }), /duplicate component source owner/)
  create('c', 'unknown-role')
  assert.throws(() => readComponentCatalog(root, { sources: [first] }), /unadmitted Product runtime component role/)
})

test('shared module requires tag provenance and authenticates extracted bytes and go.mod', t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'composition-module-'))
  t.after(() => fs.rmSync(root, { recursive: true, force: true }))
  const Dir = path.join(root, 'source'), GoMod = path.join(root, 'v1.0.0.mod')
  fs.mkdirSync(Dir)
  const module = 'github.com/example/shared', version = 'v1.0.0', sourceCommit = 'd'.repeat(40), expected = 'module ' + module + '\n'
  fs.writeFileSync(path.join(Dir, 'go.mod'), expected)
  fs.writeFileSync(path.join(Dir, 'SOURCE_PROVENANCE.json'), JSON.stringify({ schema: 'solovey.host.source-provenance/v1' }))
  fs.writeFileSync(GoMod, expected)
  const hash = createHash('sha256').update(expected).digest('hex')
  const sum = file => 'h1:' + createHash('sha256').update(`${hash}  ${file}\n`).digest('base64')
  const locked = { module, version, sourceCommit, sourceTree: regularSourceTree(Dir), moduleChecksum: sum(`${module}@${version}/go.mod`), goModChecksum: sum('go.mod') }
  const data = { Path: module, Version: version, Dir, GoMod, Sum: locked.moduleChecksum, GoModSum: locked.goModChecksum,
    Origin: { VCS: 'git', URL: 'https://' + module, Ref: `refs/tags/${version}`, Hash: sourceCommit } }
  assert.equal(verifySharedModule(data, locked).sourceCommit, sourceCommit)
  assert.throws(() => verifySharedModule({ ...data, Origin: { ...data.Origin, Ref: 'refs/heads/main' } }, locked), /version provenance differs/)
  assert.throws(() => verifySharedModule({ ...data, Origin: { ...data.Origin, Hash: 'e'.repeat(40) } }, locked), /version provenance differs/)
  assert.throws(() => verifySharedModule({ ...data, Sum: locked.goModChecksum }, locked), /module checksum differs/)
  assert.throws(() => verifySharedModule({ ...data, Replace: { Path: '../shared' } }, locked), /module substitution rejected/)
  fs.writeFileSync(GoMod, 'modified')
  assert.throws(() => verifySharedModule(data, locked), /cached go.mod differs/)
  fs.writeFileSync(path.join(Dir, 'go.mod'), 'modified')
  assert.throws(() => verifySharedModule(data, locked), /cache differs from pinned source tree/)
})
