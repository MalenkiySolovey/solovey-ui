// Product owns preparation and provenance. The Frontend only verifies/consumes
// the resulting hash-pinned UI projection; it never resolves a Go module.
import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
import {fileURLToPath} from 'node:url'
import {productRoot, digest, safeSourcePath, frontendDigest, readSourceLock, headlessSource, prepareComponentSources, componentSourceRoot, regularSourceTree} from './component-source-inputs.mjs'
import {historicalPanelUpdateID, readComponentFrontendEntries} from './component-frontend-manifest.mjs'
import {readFrontendPin, unpackFrontendArchive} from './frontend-artifacts.mjs'
import {buildInputSchema, frontendAPIFiles, frontendProjectionFiles, readFrontendBuildInputs, validateFrontendBuildInputs} from './frontend-source.mjs'
import {frontendSourceDirectory} from './frontend-source.mjs'

const historicalPanelManifestSHA256 = 'e5d37046dbae72dd57a4a547fcfe199a13fcaf2f50027ee2fd8a2d96cabc2c6a'

function localFrontendFiles(directory, manifestFilename = path.join(directory, 'component.json')) {
  assert.ok(fs.lstatSync(directory).isDirectory() && !fs.lstatSync(directory).isSymbolicLink(), 'unsafe frontend source root')
  const manifest = manifestFilename, stat = fs.lstatSync(manifest)
  assert.ok(stat.isFile() && !stat.isSymbolicLink(), 'unsafe frontend component manifest')
  const files = {'component.json': digest(fs.readFileSync(manifest))}
  let count = 0, total = 0
  const walk = (prefix, depth = 0) => {
    assert.ok(depth <= 16, 'frontend source depth exceeds bound')
    const current = path.join(directory, prefix), stat = fs.lstatSync(current)
    assert.ok(stat.isDirectory() && !stat.isSymbolicLink(), 'unsafe frontend source directory')
    for (const entry of fs.readdirSync(current, {withFileTypes: true})) {
      const name = safeSourcePath(prefix + '/' + entry.name)
      assert.ok(!entry.isSymbolicLink(), 'frontend source symlink rejected')
      if (entry.isDirectory()) walk(name, depth + 1)
      else {
        assert.ok(entry.isFile() && ++count <= 1024, 'invalid frontend source file count')
        const bytes = fs.readFileSync(path.join(directory, name))
        total += bytes.length
        assert.ok(total <= 16 * 1024 * 1024, 'frontend source exceeds build input bound')
        files[name] = digest(bytes)
      }
    }
  }
  walk('frontend')
  return frontendProjectionFiles(files)
}

function projectHistoricalPanelInput(root, candidate, pin) {
  const {manifestSHA256, archiveSHA256, sourceLockDigest} = pin.input
  const filename = path.join(root, 'tests/fixtures/frontend-build-inputs', manifestSHA256 + '.json.gz')
  const stat = fs.lstatSync(filename)
  assert.ok(stat.isFile() && !stat.isSymbolicLink() && stat.size <= 32 * 1024 * 1024, 'unsafe historical Frontend input archive')
  const files = unpackFrontendArchive(fs.readFileSync(filename), archiveSHA256, 'inputs')
  const bytes = files.get('manifest.json')
  assert.ok(bytes && digest(bytes) === manifestSHA256, 'historical Frontend input manifest differs')
  const manifest = validateFrontendBuildInputs(JSON.parse(bytes))
  assert.equal(manifest.sourceLockDigest, sourceLockDigest, 'historical Frontend source authority differs')
  const component = manifest.components.find(item => item.id === historicalPanelUpdateID)
  assert.ok(component, 'historical Panel Update input is missing')
  const prefix = 'components/' + historicalPanelUpdateID + '/'
  for (const [name, hash] of Object.entries(component.files)) {
    const bytes = files.get(prefix + name)
    assert.ok(bytes && digest(bytes) === hash, 'historical Panel Update bytes differ: ' + name)
    const target = path.join(candidate, prefix, name)
    fs.mkdirSync(path.dirname(target), {recursive: true})
    fs.writeFileSync(target, bytes, {flag: 'wx'})
  }
  return component
}

export function projectFrontendBuildInputs(root = productRoot, options = {}) {
  const fullLock = readSourceLock(root)
  const sourceLock = {...fullLock, sources: fullLock.sources.filter(source => !headlessSource(source))}
  const catalog = readComponentFrontendEntries(path.join(root, 'components'))
  const frontendRoot = fs.realpathSync.native(options.frontendRoot || frontendSourceDirectory)
  const pin = fs.existsSync(path.join(root, 'deploy/dependencies/solovey-frontend.json')) ? readFrontendPin(root) : null
  const target = path.resolve(options.directory || path.join(root, '.artifacts/frontend-inputs')), lock = target + '.lock'
  const publicationParent = path.dirname(target)
  fs.mkdirSync(publicationParent, {recursive: true})
  assert.ok(!fs.lstatSync(publicationParent).isSymbolicLink(), 'frontend input parent must be regular')
  const existing = fs.lstatSync(target, {throwIfNoEntry: false})
  assert.ok(!existing || existing.isDirectory() && !existing.isSymbolicLink(), 'unsafe frontend input destination')
  fs.mkdirSync(lock)
  let temporary, preserved = false
  try {
    temporary = fs.mkdtempSync(path.join(publicationParent, '.component-inputs-stage-'))
    const candidate = path.join(temporary, 'candidate'), previous = path.join(temporary, 'previous')
    fs.mkdirSync(path.join(candidate, 'components'), {recursive: true})
    const components = []
    for (const [id, entries] of Object.entries(catalog)) {
      assert.deepEqual(entries, ['frontend/index.ts'], 'unsupported frontend composition')
      const directory = componentSourceRoot(id, root, frontendRoot), external = sourceLock.sources.find(source => source.id === id)
      const manifestFilename = path.join(root, 'components', id, 'component.json')
      // Canonical prepare verifies the complete module Git tree. Its stage is
      // the accepted file subset; componentSourceRoot rechecks those bytes.
      const files = external ? frontendProjectionFiles(external.files) : localFrontendFiles(directory, manifestFilename)
      const destination = path.join(candidate, 'components', id)
      for (const name of Object.keys(files)) {
        fs.mkdirSync(path.dirname(path.join(destination, name)), {recursive: true})
        fs.copyFileSync(name === 'component.json' ? manifestFilename : path.join(directory, name), path.join(destination, name))
      }
      components.push({id, inputTree: regularSourceTree(destination), manifestSHA256: files['component.json'], frontendSHA256: frontendDigest(files), files})
    }
    // Reproduce the publisher's original input tuple. The pinned Frontend
    // validator still verifies every byte, but does not expose the old slot.
    if (pin?.input.manifestSHA256 === historicalPanelManifestSHA256) {
      components.push(projectHistoricalPanelInput(root, candidate, pin))
      components.sort((a, b) => a.id < b.id ? -1 : a.id > b.id ? 1 : 0)
    }
    const manifest = {schema: buildInputSchema, sourceLock, sourceLockDigest: digest(JSON.stringify(sourceLock)), frontendFiles: Object.fromEntries(frontendAPIFiles.map(name => [name, digest(fs.readFileSync(path.join(frontendRoot, name)))])), components}
    const bytes = JSON.stringify(manifest, null, 2) + '\n', manifestSHA256 = digest(bytes)
    if (pin) assert.equal(manifestSHA256, pin.input.manifestSHA256, 'Product Frontend input tuple drift')
    fs.writeFileSync(path.join(candidate, 'manifest.json'), bytes)
    const validated = readFrontendBuildInputs(frontendRoot, manifestSHA256, candidate)
    if (existing) { fs.renameSync(target, previous); preserved = true }
    try { fs.renameSync(candidate, target) }
    catch (error) {
      if (preserved) {
        try { fs.renameSync(previous, target); preserved = false }
        catch (restoreError) { throw new AggregateError([error, restoreError], 'frontend input publication failed; previous inputs retained at ' + previous) }
      }
      throw error
    }
    preserved = false
    return {schema: buildInputSchema, directory: target, manifestSHA256, sourceLockDigest: manifest.sourceLockDigest, components: validated.entries.map(component => component.id)}
  } finally {
    // Only this invocation's checked temporary child is disposable.
    if (temporary) {
      assert.ok(path.dirname(temporary) === publicationParent && path.basename(temporary).startsWith('.component-inputs-stage-'))
      if (!preserved) fs.rmSync(temporary, {recursive: true, force: true})
    }
    fs.rmdirSync(lock)
  }
}

export function prepareFrontendBuildInputs(root = productRoot, options = {}) {
  prepareComponentSources(root, options.frontendRoot || frontendSourceDirectory)
  return projectFrontendBuildInputs(root, options)
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  assert.deepEqual(process.argv.slice(2), ['prepare'], 'expected prepare')
  console.log(JSON.stringify(prepareFrontendBuildInputs()))
}
