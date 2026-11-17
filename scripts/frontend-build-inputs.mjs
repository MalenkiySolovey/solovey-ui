// Product owns preparation and provenance. The Frontend only verifies/consumes
// the resulting hash-pinned UI projection; it never resolves a Go module.
import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
import {fileURLToPath} from 'node:url'
import {productRoot, digest, safeSourcePath, frontendDigest, readSourceLock, prepareComponentSources, componentSourceRoot, regularSourceTree} from './component-source-inputs.mjs'
import {readComponentFrontendEntries} from './component-frontend-manifest.mjs'
import {buildInputSchema, frontendAPIFiles, frontendProjectionFiles, readFrontendBuildInputs} from '../frontend/scripts/component-build-inputs.mjs'

function localFrontendFiles(directory) {
  assert.ok(fs.lstatSync(directory).isDirectory() && !fs.lstatSync(directory).isSymbolicLink(), 'unsafe frontend source root')
  const manifest = path.join(directory, 'component.json'), stat = fs.lstatSync(manifest)
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

export function projectFrontendBuildInputs(root = productRoot) {
  const sourceLock = readSourceLock(root), catalog = readComponentFrontendEntries(path.join(root, 'components'))
  const frontendRoot = fs.realpathSync.native(path.join(root, 'frontend'))
  const target = path.join(frontendRoot, '.component-inputs'), lock = path.join(frontendRoot, '.component-inputs.lock')
  const existing = fs.lstatSync(target, {throwIfNoEntry: false})
  assert.ok(!existing || existing.isDirectory() && !existing.isSymbolicLink(), 'unsafe frontend input destination')
  fs.mkdirSync(lock)
  let temporary, preserved = false
  try {
    temporary = fs.mkdtempSync(path.join(frontendRoot, '.component-inputs-stage-'))
    const candidate = path.join(temporary, 'candidate'), previous = path.join(temporary, 'previous')
    fs.mkdirSync(path.join(candidate, 'components'), {recursive: true})
    const components = []
    for (const [id, entries] of Object.entries(catalog)) {
      assert.deepEqual(entries, ['frontend/index.ts'], 'unsupported frontend composition')
      const directory = componentSourceRoot(id, root), external = sourceLock.sources.find(source => source.id === id)
      // Canonical prepare verifies the complete module Git tree. Its stage is
      // the accepted file subset; componentSourceRoot rechecks those bytes.
      const files = external ? frontendProjectionFiles(external.files) : localFrontendFiles(directory)
      const destination = path.join(candidate, 'components', id)
      for (const name of Object.keys(files)) {
        fs.mkdirSync(path.dirname(path.join(destination, name)), {recursive: true})
        fs.copyFileSync(path.join(directory, name), path.join(destination, name))
      }
      components.push({id, inputTree: regularSourceTree(destination), manifestSHA256: files['component.json'], frontendSHA256: frontendDigest(files), files})
    }
    const manifest = {schema: buildInputSchema, sourceLock, sourceLockDigest: digest(JSON.stringify(sourceLock)), frontendFiles: Object.fromEntries(frontendAPIFiles.map(name => [name, digest(fs.readFileSync(path.join(frontendRoot, name)))])), components}
    const bytes = JSON.stringify(manifest, null, 2) + '\n', manifestSHA256 = digest(bytes)
    fs.writeFileSync(path.join(candidate, 'manifest.json'), bytes)
    readFrontendBuildInputs(frontendRoot, manifestSHA256, candidate)
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
    return {schema: buildInputSchema, directory: target, manifestSHA256, sourceLockDigest: manifest.sourceLockDigest, components: components.map(component => component.id)}
  } finally {
    // Only this invocation's checked temporary child is disposable.
    if (temporary) {
      assert.ok(path.dirname(temporary) === frontendRoot && path.basename(temporary).startsWith('.component-inputs-stage-'))
      if (!preserved) fs.rmSync(temporary, {recursive: true, force: true})
    }
    fs.rmdirSync(lock)
  }
}

export function prepareFrontendBuildInputs(root = productRoot) {
  prepareComponentSources(root)
  return projectFrontendBuildInputs(root)
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  assert.deepEqual(process.argv.slice(2), ['prepare'], 'expected prepare')
  console.log(JSON.stringify(prepareFrontendBuildInputs()))
}
