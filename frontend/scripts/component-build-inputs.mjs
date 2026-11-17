// This generated projection is a build input, not a ComponentHost manifest or
// permission authority. Its external pins are the unchanged Product source lock.
import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
import {fileURLToPath} from 'node:url'
import {digest, frontendDigest, safeSourcePath, validateSourceLock, verifySourceFiles, regularSourceTree, verifyUIAPIInputs} from './component-source-integrity.mjs'

export const buildInputSchema = 'solovey.frontend-build-inputs/v1'
export const frontendAPIFiles = Object.freeze([
  '.node-version', 'package-lock.json', 'src/componentSystem/api.ts',
  'src/componentSystem/contributions.ts', 'src/componentSystem/observation.ts',
  'src/componentSystem/sdk.ts', 'src/componentSystem/subscriptionSources.ts',
])
const keys = (value, expected) => assert.deepEqual(Object.keys(value).sort(), [...expected].sort(), 'unknown or missing frontend build input fields')
const hash = value => assert.match(value, /^[a-f0-9]{64}$/, 'frontend input SHA256 required')

export const frontendProjectionFiles = files => Object.fromEntries(Object.entries(files)
  .filter(([name]) => name === 'component.json' || name === 'HOST_UI_PIN.json' || name.startsWith('frontend/'))
  .sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0))

export function validateFrontendBuildInputs(input) {
  keys(input, ['schema', 'sourceLock', 'sourceLockDigest', 'frontendFiles', 'components'])
  assert.equal(input.schema, buildInputSchema, 'unsupported frontend build input schema')
  validateSourceLock(input.sourceLock)
  hash(input.sourceLockDigest)
  assert.equal(digest(JSON.stringify(input.sourceLock)), input.sourceLockDigest, 'component source lock projection drift')
  keys(input.frontendFiles, frontendAPIFiles)
  Object.values(input.frontendFiles).forEach(hash)
  assert.ok(Array.isArray(input.components) && input.components.length <= 64, 'invalid frontend component input count')
  const ids = new Set()
  for (const component of input.components) {
    keys(component, ['id', 'inputTree', 'manifestSHA256', 'frontendSHA256', 'files'])
    assert.match(component.id, /^[a-z0-9]+(?:-[a-z0-9]+)*$/)
    assert.ok(!ids.has(component.id), 'duplicate frontend component input')
    ids.add(component.id)
    assert.match(component.inputTree, /^[a-f0-9]{40}$/, 'immutable frontend input tree required')
    hash(component.manifestSHA256); hash(component.frontendSHA256)
    assert.ok(component.files && Object.keys(component.files).length <= 1024, 'invalid frontend input file count')
    for (const [name, value] of Object.entries(component.files)) { safeSourcePath(name); hash(value) }
    assert.deepEqual(component.files, frontendProjectionFiles(component.files), 'non-UI source in frontend inputs')
    assert.ok(component.files['frontend/index.ts'], 'missing declared frontend entry')
    assert.equal(component.files['component.json'], component.manifestSHA256, 'frontend manifest digest mismatch')
    assert.equal(frontendDigest(component.files), component.frontendSHA256, 'frontend closure digest mismatch')
    const external = input.sourceLock.sources.find(source => source.id === component.id)
    if (external) assert.deepEqual(component.files, frontendProjectionFiles(external.files), 'frontend input differs from accepted component source lock')
    else assert.ok(!Object.hasOwn(component.files, 'HOST_UI_PIN.json'), 'external UI API pin requires an accepted component source')
  }
  return input
}

// Inputs are supplied inside the Frontend checkout at one declared location.
// There is deliberately no parent-directory discovery, source download or fallback.
export function readFrontendBuildInputs(frontendRoot = fileURLToPath(new URL('../', import.meta.url)), expectedSHA256 = process.env.SOLOVEY_UI_INPUTS_SHA256, directory = path.join(frontendRoot, '.component-inputs')) {
  hash(expectedSHA256)
  for (const name of [directory, path.join(directory, 'components')]) {
    const stat = fs.lstatSync(name)
    assert.ok(stat.isDirectory() && !stat.isSymbolicLink(), 'frontend inputs must be regular directories')
  }
  assert.deepEqual(fs.readdirSync(directory).sort(), ['components', 'manifest.json'], 'unexpected frontend input root file')
  const manifestPath = path.join(directory, 'manifest.json'), stat = fs.lstatSync(manifestPath)
  assert.ok(stat.isFile() && !stat.isSymbolicLink() && stat.size <= 1024 * 1024, 'invalid frontend input manifest file')
  const bytes = fs.readFileSync(manifestPath)
  assert.equal(digest(bytes), expectedSHA256, 'frontend build input manifest SHA256 mismatch')
  const manifest = validateFrontendBuildInputs(JSON.parse(bytes))
  for (const name of frontendAPIFiles) assert.equal(digest(fs.readFileSync(path.join(frontendRoot, name))), manifest.frontendFiles[name], 'frontend UI API or toolchain input drift: ' + name)
  assert.deepEqual(fs.readdirSync(path.join(directory, 'components')).sort(), manifest.components.map(component => component.id).sort(), 'unexpected or missing frontend component input')
  const entries = []
  for (const component of manifest.components) {
    const componentRoot = path.join(directory, 'components', component.id)
    verifySourceFiles(componentRoot, component)
    assert.equal(regularSourceTree(componentRoot), component.inputTree, 'frontend input tree mismatch')
    if (manifest.sourceLock.sources.some(source => source.id === component.id)) verifyUIAPIInputs(frontendRoot, componentRoot)
    entries.push({id: component.id, root: componentRoot, entry: path.join(componentRoot, 'frontend/index.ts')})
  }
  return {manifest, directory, manifestSHA256: expectedSHA256, entries}
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  assert.deepEqual(process.argv.slice(2), ['verify'], 'expected verify')
  const input = readFrontendBuildInputs()
  console.log(JSON.stringify({schema: input.manifest.schema, manifestSHA256: input.manifestSHA256, components: input.entries.map(entry => entry.id)}))
}
