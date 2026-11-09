// Build-time inputs only. Runtime admission and component.json remain owned by
// the existing ComponentHost and its canonical manifest parser.
import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
import {createHash} from 'node:crypto'
import {execFileSync} from 'node:child_process'
import {fileURLToPath} from 'node:url'

export const productRoot = fileURLToPath(new URL('../', import.meta.url))
export const digest = bytes => createHash('sha256').update(bytes).digest('hex')
const sourceKeys = ['id','module','version','sourceCommit','sourceTree','moduleChecksum','goModChecksum','manifestSHA256','frontendSHA256','files']
const strictKeys = (value, keys) => assert.deepEqual(Object.keys(value).sort(), [...keys].sort(), 'unknown or missing source input fields')

export function safeSourcePath(name) {
  assert.equal(typeof name, 'string')
  assert.ok(name.length < 240 && !/[\\\x00-\x1f<>:"|?*]/.test(name), 'unsafe source path')
  assert.ok(name.split('/').every(part => part && part !== '.' && part !== '..' && !part.startsWith('.') && !/[. ]$/.test(part)), 'unsafe source path')
  return name
}

export function frontendDigest(files) {
  return digest(JSON.stringify(Object.entries(files).filter(([name]) => name.startsWith('frontend/')).sort(([a],[b]) => a < b ? -1 : a > b ? 1 : 0)))
}

export function validateSourceLock(lock) {
  strictKeys(lock, ['schema','sources'])
  assert.equal(lock.schema, 'solovey.component-source-inputs/v1')
  assert.ok(Array.isArray(lock.sources) && lock.sources.length <= 64, 'invalid source input count')
  const ids = new Set()
  for (const source of lock.sources) {
    strictKeys(source, sourceKeys)
    assert.match(source.id, /^[a-z0-9]+(?:-[a-z0-9]+)*$/)
    assert.ok(!ids.has(source.id), 'duplicate component source')
    ids.add(source.id)
    assert.equal(source.module, `github.com/MalenkiySolovey/solovey-component-${source.id}`, 'unapproved component source owner')
    assert.match(source.version, /^v0\.\d+\.\d+$|^v1\.\d+\.\d+$/, 'immutable module version required')
    for (const field of ['sourceCommit','sourceTree']) assert.match(source[field], /^[0-9a-f]{40}$/)
    for (const field of ['moduleChecksum','goModChecksum']) assert.match(source[field], /^h1:[A-Za-z0-9+/]{43}=$/)
    for (const field of ['manifestSHA256','frontendSHA256']) assert.match(source[field], /^[0-9a-f]{64}$/)
    assert.ok(source.files && Object.keys(source.files).length <= 1024, 'invalid source file count')
    for (const [name, hash] of Object.entries(source.files)) {
      safeSourcePath(name)
      assert.match(hash, /^[0-9a-f]{64}$/)
    }
    for (const name of ['go.mod','go.sum','LICENSE','NOTICE','SOURCE_PROVENANCE.json','component.json','frontend/index.ts'])
      assert.ok(source.files[name], `missing source provenance: ${name}`)
    assert.equal(source.files['component.json'], source.manifestSHA256)
    assert.equal(frontendDigest(source.files), source.frontendSHA256, 'frontend input digest mismatch')
  }
  return lock
}

export function readSourceLock(root = productRoot) {
  const filename = path.join(root, 'components/sources.lock.json')
  if (!fs.existsSync(filename)) {
    assert.notEqual(path.resolve(root), path.resolve(productRoot), 'component source lock is missing')
    return {schema:'solovey.component-source-inputs/v1', sources:[]}
  }
  assert.ok(fs.statSync(filename).size <= 256 * 1024, 'oversize component source lock')
  return validateSourceLock(JSON.parse(fs.readFileSync(filename, 'utf8')))
}

function regularPath(directory, name) {
  let current = directory
  for (const part of safeSourcePath(name).split('/')) {
    current = path.join(current, part)
    const stat = fs.lstatSync(current)
    assert.ok(!stat.isSymbolicLink(), 'component source symlink rejected')
  }
  assert.ok(fs.statSync(current).isFile(), 'component source must be a regular file')
  return current
}

export function verifySourceFiles(directory, source) {
  assert.ok(fs.lstatSync(directory).isDirectory() && !fs.lstatSync(directory).isSymbolicLink(), 'unsafe source directory')
  let total = 0
  for (const [name, hash] of Object.entries(source.files)) {
    const filename = regularPath(directory, name)
    total += fs.statSync(filename).size
    assert.ok(total <= 16 * 1024 * 1024, 'component source exceeds build input bound')
    assert.equal(digest(fs.readFileSync(filename)), hash, `component source drift: ${name}`)
  }
  const frontendFiles = []
  const walkFrontend = (directory, prefix) => {
    for (const entry of fs.readdirSync(directory, {withFileTypes:true})) {
      const name = safeSourcePath(`${prefix}/${entry.name}`)
      assert.ok(!entry.isSymbolicLink(), 'frontend source symlink rejected')
      if (entry.isDirectory()) walkFrontend(path.join(directory,entry.name), name)
      else {
        assert.ok(entry.isFile(), 'frontend source must be regular')
        frontendFiles.push(name)
      }
    }
  }
  walkFrontend(path.join(directory,'frontend'), 'frontend')
  assert.deepEqual(frontendFiles.sort(), Object.keys(source.files).filter(name => name.startsWith('frontend/')).sort(), 'untrusted or incomplete frontend source closure')
  const manifest = JSON.parse(fs.readFileSync(path.join(directory, 'component.json'), 'utf8'))
  assert.equal(manifest.id, source.id, 'component source identity mismatch')
  assert.deepEqual(manifest.frontend.entries, ['frontend/index.ts'], 'unsupported frontend composition')
  return source
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

function stageRoot(root) {
  const directory = path.join(root, '.component-sources')
  if (fs.existsSync(directory)) assert.ok(!fs.lstatSync(directory).isSymbolicLink(), 'source staging symlink rejected')
  return directory
}

export function componentSourceRoot(id, root = productRoot) {
  const source = readSourceLock(root).sources.find(source => source.id === id)
  if (!source) return path.join(root, 'components', id)
  const directory = path.join(stageRoot(root), id)
  verifySourceFiles(directory, source)
  assert.equal(digest(fs.readFileSync(path.join(root,'components',id,'component.json'))), source.manifestSHA256, 'generated component manifest drift')
  return directory
}

export function componentManifestKey(id, entry, root = productRoot) {
  return path.relative(path.join(root,'frontend'), path.join(componentSourceRoot(id,root), safeSourcePath(entry))).replaceAll(path.sep, '/')
}

export function validateBuildEnvironment(env) {
  assert.ok(!env.GOWORK || env.GOWORK === 'off', 'component build rejects a Go workspace')
  const flags = (env.GOFLAGS ?? '').split(/\s+/).filter(Boolean)
  assert.ok(flags.every(flag => ['-mod=readonly','-modfile=go.extended.mod'].includes(flag)) && new Set(flags).size === flags.length, 'component build rejects Go flag substitution')
}

export function prepareComponentSources(root = productRoot) {
  const lock = readSourceLock(root)
  if (lock.sources.length === 0) return {sources:[], inputDigest:digest(JSON.stringify(lock))}
  validateBuildEnvironment(process.env)
  const go = args => JSON.parse(execFileSync('go', args, {cwd:root, encoding:'utf8', env:{...process.env,GOWORK:'off',GOFLAGS:'-mod=readonly'}, maxBuffer:4*1024*1024}))
  const accepted = []
  for (const source of lock.sources) {
    const downloaded = go(['mod','download','-json',`${source.module}@${source.version}`])
    validateModuleResolution(downloaded, source)
    verifySourceFiles(downloaded.Dir, source)
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
