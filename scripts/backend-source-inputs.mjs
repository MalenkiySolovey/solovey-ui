// Build-time Backend identity. Runtime connections and commands remain in the module.
import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import {execFileSync} from 'node:child_process'
import {fileURLToPath, pathToFileURL} from 'node:url'
import {digest, regularSourceTree, validateBuildEnvironment, validateModuleRequirement, validateModuleResolution} from './component-source-inputs.mjs'

const productRoot = fileURLToPath(new URL('../', import.meta.url))
export const backendModule = 'github.com/MalenkiySolovey/solovey-backend'
export const backendLockPath = 'deploy/dependencies/solovey-backend-source.json'
const fields = ['schema','module','version','sourceCommit','sourceTree','moduleChecksum','goModChecksum','provenanceSHA256']

export function validateBackendSourceLock(source) {
  assert.ok(source && typeof source === 'object' && !Array.isArray(source), 'Backend source identity required')
  assert.deepEqual(Object.keys(source).sort(), [...fields].sort(), 'unknown or missing Backend source fields')
  assert.equal(source.schema, 'solovey.backend-source-inputs/v1')
  assert.equal(source.module, backendModule, 'unapproved Backend source owner')
  assert.match(source.version, /^v\d+\.\d+\.\d+(?:-\d{14}-[0-9a-f]{12})?$/, 'immutable Backend module version required')
  for (const field of ['sourceCommit','sourceTree']) assert.match(source[field], /^[0-9a-f]{40}$/)
  for (const field of ['moduleChecksum','goModChecksum']) assert.match(source[field], /^h1:[A-Za-z0-9+/]{43}=$/)
  assert.match(source.provenanceSHA256, /^[0-9a-f]{64}$/)
  return source
}

export function readBackendSourceLock(root = productRoot) {
  const filename = path.join(root, backendLockPath)
  const stat = fs.lstatSync(filename)
  assert.ok(stat.isFile() && !stat.isSymbolicLink() && stat.size <= 64*1024, 'unsafe Backend source lock')
  return validateBackendSourceLock(JSON.parse(fs.readFileSync(filename, 'utf8')))
}

export function verifyBackendModuleSource(directory, source) {
  validateBackendSourceLock(source)
  assert.ok(fs.lstatSync(directory).isDirectory() && !fs.lstatSync(directory).isSymbolicLink(), 'unsafe Backend module directory')
  assert.equal(regularSourceTree(directory), source.sourceTree, 'Backend Git source tree mismatch')
  const bytes = fs.readFileSync(path.join(directory, 'SOURCE_PROVENANCE.json'))
  assert.equal(digest(bytes), source.provenanceSHA256, 'Backend source provenance mismatch')
  const original = JSON.parse(bytes)
  assert.equal(original.sourceRepository, 'https://github.com/MalenkiySolovey/solovey-ui', 'unapproved original Backend source')
  assert.match(original.sourceCommit, /^[0-9a-f]{40}$/)
  assert.match(original.sourceTree, /^[0-9a-f]{40}$/)
  assert.ok(Array.isArray(original.files) && original.files.length > 0, 'Backend source mapping missing')
  for (const filename of ['go.mod','go.sum','LICENSE','NOTICE','README.md','tools/verify-source.mjs']) {
    assert.ok(fs.lstatSync(path.join(directory, filename)).isFile(), 'Backend integration/provenance input missing: '+filename)
  }
  return source
}

export function backendSourceProperties(source) {
  validateBackendSourceLock(source)
  return Object.entries({sourceAuthority:'https://'+source.module, sourceCommit:source.sourceCommit,
    sourceTree:source.sourceTree, sourceProvenanceSHA256:source.provenanceSHA256})
    .map(([name,value]) => ({name:'solovey:'+name,value}))
}

export function verifyProductBackendRetirement(root) {
  const directory = path.join(root, 'backend')
  if (!fs.existsSync(directory)) return
  let count = 0
  const walk = current => {
    assert.ok(!fs.lstatSync(current).isSymbolicLink(), 'editable Backend source alias rejected')
    for (const entry of fs.readdirSync(current, {withFileTypes:true})) {
      assert.ok(++count <= 4096 && !entry.isSymbolicLink(), 'unsafe retired Backend root')
      if (entry.isDirectory()) walk(path.join(current,entry.name))
      else assert.ok(!entry.name.endsWith('.go') && !['go.mod','go.sum'].includes(entry.name), 'editable Product Backend source must be retired')
    }
  }
  walk(directory)
}

export function prepareBackendSource(root = productRoot) {
  const source = readBackendSourceLock(root)
  validateBuildEnvironment(process.env)
  verifyProductBackendRetirement(root)
  const go = args => JSON.parse(execFileSync('go', args, {cwd:root, encoding:'utf8',
    env:{...process.env,GOWORK:'off',GOFLAGS:'-mod=readonly'}, maxBuffer:4*1024*1024}))
  const downloaded = go(['mod','download','-json',source.module+'@'+source.version])
  validateModuleResolution(downloaded, source)
  verifyBackendModuleSource(downloaded.Dir, source)
  for (const modfile of ['go.mod','go.extended.mod']) {
    assert.ok(fs.lstatSync(path.join(root,modfile)).isFile(), 'provider module lock missing')
    validateModuleRequirement(go(['mod','edit','-json','-modfile='+modfile]), source)
    const selected = go(['list','-m','-json','-mod=readonly','-modfile='+modfile,source.module])
    validateModuleResolution({...selected,Origin:downloaded.Origin}, source)
    assert.equal(path.resolve(selected.Dir), path.resolve(downloaded.Dir), 'Backend selected source mismatch')
  }
  return {source,inputDigest:digest(JSON.stringify(source))}
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  assert.equal(process.argv[2], 'verify', 'usage: node scripts/backend-source-inputs.mjs verify')
  console.log(JSON.stringify(prepareBackendSource()))
}
