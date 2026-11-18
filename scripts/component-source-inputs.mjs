// Build-time inputs only. Runtime admission and component.json remain owned by
// the existing ComponentHost and its canonical manifest parser.
import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
import {execFileSync} from 'node:child_process'
import {fileURLToPath} from 'node:url'
import {digest, frontendDigest, safeSourcePath, validateSourceLock, verifySourceFiles, regularSourceTree, verifyUIAPIInputs} from './frontend-source.mjs'
import {frontendSourceDirectory} from './frontend-source.mjs'

export {digest, frontendDigest, safeSourcePath, validateSourceLock, verifySourceFiles, regularSourceTree}

export const productRoot = fileURLToPath(new URL('../', import.meta.url))
export function readSourceLock(root = productRoot) {
  const filename = path.join(root, 'components/sources.lock.json')
  if (!fs.existsSync(filename)) {
    assert.notEqual(path.resolve(root), path.resolve(productRoot), 'component source lock is missing')
    return {schema:'solovey.component-source-inputs/v1', sources:[]}
  }
  assert.ok(fs.statSync(filename).size <= 256 * 1024, 'oversize component source lock')
  return validateSourceLock(JSON.parse(fs.readFileSync(filename, 'utf8')))
}

export function validateModuleResolution(resolved, source) {
  assert.ok(!resolved.Error && !resolved.Replace && !resolved.Main, 'component module source substitution rejected')
  assert.equal(resolved.Path, source.module)
  assert.equal(resolved.Version, source.version)
  assert.equal(resolved.Sum, source.moduleChecksum, 'component module checksum mismatch')
  assert.equal(resolved.GoModSum, source.goModChecksum, 'component module metadata checksum mismatch')
  assert.equal(resolved.Origin?.Hash, source.sourceCommit, 'component source commit unavailable or mismatched')
  assert.equal(resolved.Origin.VCS, 'git')
  if (resolved.Origin.URL) assert.equal(resolved.Origin.URL, `https://${source.module}`)
}

export function validateModuleRequirement(metadata, source) {
  const requirements = (metadata.Require ?? []).filter(item => item.Path === source.module)
  assert.equal(requirements.length, 1, 'component module pin required')
  assert.equal(requirements[0].Version, source.version, 'component module requirement mismatch')
  assert.ok(!requirements[0].Indirect, 'component module must be a direct input')
  assert.ok(!(metadata.Replace ?? []).some(item => item.Old.Path === source.module), 'component replace rejected')
}

export function verifyUIAPI(root, directory, frontendRoot = frontendSourceDirectory) {
  return verifyUIAPIInputs(frontendRoot, directory)
}

export function verifyModuleSource(directory, source) {
  verifySourceFiles(directory, source)
  assert.equal(regularSourceTree(directory), source.sourceTree, 'component Git source tree mismatch')
  return source
}

export function verifyComponentManifest(id, filename, root = productRoot) {
  const source = readSourceLock(root).sources.find(source => source.id === id)
  if (source) assert.equal(digest(fs.readFileSync(filename)), source.manifestSHA256, 'generated component manifest drift')
}

function stageRoot(root) {
  const directory = path.join(root, '.component-sources')
  if (fs.existsSync(directory)) assert.ok(!fs.lstatSync(directory).isSymbolicLink(), 'source staging symlink rejected')
  return directory
}

export function componentSourceRoot(id, root = productRoot, frontendRoot = frontendSourceDirectory) {
  const source = readSourceLock(root).sources.find(source => source.id === id)
  if (!source) return path.join(root, 'components', id)
  const directory = path.join(stageRoot(root), id)
  verifySourceFiles(directory, source)
  verifyComponentManifest(id, path.join(root,'components',id,'component.json'), root)
  verifyUIAPI(root, directory, frontendRoot)
  return directory
}

export function componentManifestKey(id, entry, root = productRoot) {
  // Offline pack verification derives the key from the accepted lock and does
  // not require a module cache or an editable checkout of its producer.
  const external = readSourceLock(root).sources.some(source => source.id === id)
  return `../${external ? '.component-sources' : 'components'}/${id}/${safeSourcePath(entry)}`
}

export function validateBuildEnvironment(env) {
  assert.ok(!env.GOWORK || env.GOWORK === 'off', 'component build rejects a Go workspace')
  const flags = (env.GOFLAGS ?? '').split(/\s+/).filter(Boolean)
  assert.ok(flags.every(flag => ['-mod=readonly','-modfile=go.extended.mod'].includes(flag)) && new Set(flags).size === flags.length, 'component build rejects Go flag substitution')
}

export function prepareComponentSources(root = productRoot, frontendRoot = frontendSourceDirectory) {
  const lock = readSourceLock(root)
  if (lock.sources.length === 0) return {sources:[], inputDigest:digest(JSON.stringify(lock))}
  validateBuildEnvironment(process.env)
  const go = args => JSON.parse(execFileSync('go', args, {cwd:root, encoding:'utf8', env:{...process.env,GOWORK:'off',GOFLAGS:'-mod=readonly'}, maxBuffer:4*1024*1024}))
  const accepted = []
  for (const source of lock.sources) {
    const downloaded = go(['mod','download','-json',`${source.module}@${source.version}`])
    validateModuleResolution(downloaded, source)
    verifyModuleSource(downloaded.Dir, source)
    verifyUIAPI(root, downloaded.Dir, frontendRoot)
    for (const modfile of ['go.mod','go.extended.mod'].filter(name => fs.existsSync(path.join(root,name)))) {
      validateModuleRequirement(go(['mod','edit','-json',`-modfile=${modfile}`]), source)
      const selected = go(['list','-m','-json','-mod=readonly',`-modfile=${modfile}`,source.module])
      validateModuleResolution({...selected, Origin:downloaded.Origin}, source)
      assert.equal(path.resolve(selected.Dir), path.resolve(downloaded.Dir), 'component selected source mismatch')
    }
    const parent = stageRoot(root)
    fs.mkdirSync(parent, {recursive:true})
    const target = path.join(parent,source.id)
    // Every invocation verifies the cache before copying. The stage is disposable,
    // generated input; it is never an editable source authority.
    if (fs.existsSync(target)) {
      assert.ok(!fs.lstatSync(target).isSymbolicLink(), 'source staging symlink rejected')
      fs.rmSync(target, {recursive:true, force:true})
    }
    fs.mkdirSync(target)
    for (const name of Object.keys(source.files)) {
      const destination = path.join(target,name)
      fs.mkdirSync(path.dirname(destination), {recursive:true})
      fs.copyFileSync(path.join(downloaded.Dir,name), destination)
    }
    verifySourceFiles(target, source)
    const manifest = path.join(root,'components',source.id,'component.json')
    assert.equal(digest(fs.readFileSync(manifest)), source.manifestSHA256, 'generated manifest differs from accepted source')
    accepted.push({id:source.id,module:source.module,version:source.version,sourceCommit:source.sourceCommit,frontendSHA256:source.frontendSHA256})
  }
  return {sources:accepted,inputDigest:digest(JSON.stringify(lock))}
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  assert.deepEqual(process.argv.slice(2), ['prepare'], 'expected prepare')
  console.log(JSON.stringify(prepareComponentSources()))
}
