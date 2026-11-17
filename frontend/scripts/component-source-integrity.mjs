// Canonical build-input integrity shared by Product source acceptance and the
// standalone Frontend consumer. No Go resolver, Product paths or runtime loading.
import fs from 'node:fs'
import path from 'node:path'
import assert from 'node:assert/strict'
import {createHash} from 'node:crypto'

export const digest = bytes => createHash('sha256').update(bytes).digest('hex')
const sourceKeys = ['id','module','version','sourceCommit','sourceTree','moduleChecksum','goModChecksum','manifestSHA256','frontendSHA256','files']
const strictKeys = (value, keys) => assert.deepEqual(Object.keys(value).sort(), [...keys].sort(), 'unknown or missing source input fields')

export function safeSourcePath(name) {
  assert.equal(typeof name, 'string')
  // Reject control bytes as well as Windows path/stream syntax.
  // eslint-disable-next-line no-control-regex
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
    for (const name of ['go.mod','go.sum','LICENSE','NOTICE','SOURCE_PROVENANCE.json','HOST_UI_PIN.json','component.json','frontend/index.ts'])
      assert.ok(source.files[name], `missing source provenance: ${name}`)
    assert.equal(source.files['component.json'], source.manifestSHA256)
    assert.equal(frontendDigest(source.files), source.frontendSHA256, 'frontend input digest mismatch')
  }
  return lock
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

// The pilot module contains regular 0644 source files. Go's archive preserves
// their bytes and full tree, so reconstruct the actual Git identity locally.
// Executable/symlink source trees require explicit support rather than guessing.
export function regularSourceTree(directory) {
  let count = 0, total = 0
  const walk = (directory, depth = 0) => {
    assert.ok(depth <= 16, 'source tree depth exceeds bound')
    const entries = fs.readdirSync(directory, {withFileTypes:true}).sort((a,b) =>
      Buffer.compare(Buffer.from(a.name + (a.isDirectory() ? '/' : '')), Buffer.from(b.name + (b.isDirectory() ? '/' : ''))))
    const parts = []
    for (const entry of entries) {
      assert.ok(!entry.isSymbolicLink() && !['.git','.hg','.svn','.bzr','node_modules'].includes(entry.name), 'unsupported source tree entry')
      let hash, mode
      if (entry.isDirectory()) { hash = walk(path.join(directory,entry.name), depth + 1); mode = '40000' }
      else {
        assert.ok(entry.isFile() && ++count <= 1024, 'invalid source tree file count')
        const filename = path.join(directory,entry.name)
        total += fs.statSync(filename).size
        assert.ok(total <= 16 * 1024 * 1024, 'source tree exceeds bound')
        const bytes = fs.readFileSync(filename)
        hash = createHash('sha1').update(Buffer.from(`blob ${bytes.length}\0`)).update(bytes).digest('hex')
        mode = '100644'
      }
      parts.push(Buffer.from(`${mode} ${entry.name}\0`), Buffer.from(hash,'hex'))
    }
    const bytes = Buffer.concat(parts)
    return createHash('sha1').update(Buffer.from(`tree ${bytes.length}\0`)).update(bytes).digest('hex')
  }
  return walk(directory)
}

export function verifyUIAPIInputs(frontendRoot, directory) {
  const pin = JSON.parse(fs.readFileSync(path.join(directory,'HOST_UI_PIN.json'), 'utf8'))
  assert.equal(pin.repository, 'MalenkiySolovey/solovey-ui', 'unapproved UI API owner')
  assert.equal(pin.apiVersion, '1.0', 'unsupported UI contribution API')
  assert.match(pin.sourceCommit, /^[a-f0-9]{40}$/)
  const files = ['src/componentSystem/sdk.ts','package-lock.json','.node-version']
  const accepted = [...files, 'src/componentSystem/observation.ts', 'src/componentSystem/api.ts', 'src/componentSystem/contributions.ts', 'src/componentSystem/subscriptionSources.ts']
  assert.ok(files.every(name => Object.hasOwn(pin.files,name)) && Object.keys(pin.files).every(name => accepted.includes(name)), 'unsupported UI API inputs')
  for (const name of Object.keys(pin.files))
    assert.equal(digest(fs.readFileSync(path.join(frontendRoot,name))), pin.files[name], `UI API input drift: ${name}`)
}

