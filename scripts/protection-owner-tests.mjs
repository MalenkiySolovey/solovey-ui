import fs from 'node:fs'
import path from 'node:path'
import os from 'node:os'
import assert from 'node:assert/strict'
import {createHash} from 'node:crypto'
import {execFileSync, spawnSync} from 'node:child_process'
import {fileURLToPath} from 'node:url'
import {prepareComponentSources, readSourceLock, regularSourceTree, validateBuildEnvironment} from './component-source-inputs.mjs'
import {materializeOwnerSource} from './protection-owner-inputs.mjs'

const root = fileURLToPath(new URL('../', import.meta.url))
const module = 'github.com/MalenkiySolovey/solovey-component-server-protection'
const input = readSourceLock(root).sources.find(source => source.id === 'server-protection')
assert.ok(input, 'accepted immutable Server Protection source input is required')
assert.equal(input.module, module)
validateBuildEnvironment(process.env)
prepareComponentSources(root)
const env = {...process.env, GOWORK: 'off'}
const go = args => execFileSync('go', args, {cwd: root, env, encoding: 'utf8', windowsHide: true, maxBuffer: 12 << 20})
const selected = JSON.parse(go(['list', '-m', '-json', module]))
assert.equal(selected.Path, input.module)
assert.equal(selected.Version, input.version)
assert.equal(selected.Sum, input.moduleChecksum)
assert.equal(selected.GoModSum, input.goModChecksum)
assert.ok(!selected.Replace && !selected.Main, 'owner qualification must consume the actual immutable module')
assert.ok(selected.Dir && fs.statSync(selected.Dir).isDirectory())

// Check independent producer imports before adding any Product-owned tests.
// Every production byte comes from the selected immutable archive. Go forbids
// overlays in its module cache, so only this owner-test invocation uses an
// exact generated copy and a temporary alternate modfile. Normal Product
// builds and subprocesses retain their published module dependency.
const dependencyJSON = go(['list', '-deps', '-test', '-json', module + '/...'])
const dependencies = (dependencyJSON.match(/^{[\s\S]*?^}/gm) ?? []).map(JSON.parse)
assert.ok(dependencies.length > 0)
const allowed = new Set([module, 'github.com/MalenkiySolovey/solovey-host', 'golang.org/x/sys'])
for (const dependency of dependencies) if (dependency.Module) {
  assert.ok(allowed.has(dependency.Module.Path), 'private or unreviewed producer dependency: ' + dependency.ImportPath)
  assert.ok(!dependency.Module.Replace, 'producer dependency substitution')
}

const temp = fs.realpathSync.native(os.tmpdir())
const directory = fs.mkdtempSync(path.join(temp, 'solovey-protection-owner-tests-'))
try {
const generated = materializeOwnerSource(selected.Dir, path.join(directory, 'source'), input.sourceTree)
const originalModfile = (process.env.GOFLAGS ?? '').split(/\s+/).includes('-modfile=go.extended.mod') ? 'go.extended.mod' : 'go.mod'
const metadata = JSON.parse(go(['mod', 'edit', '-json', '-modfile=' + originalModfile]))
const modfile = path.join(directory, 'owner.mod')
fs.copyFileSync(path.join(root, originalModfile), modfile)
fs.copyFileSync(path.join(root, originalModfile.replace(/\.mod$/, '.sum')), modfile.replace(/\.mod$/, '.sum'))
for (const replacement of metadata.Replace ?? []) if (!replacement.New.Version) {
  const old = replacement.Old.Path + (replacement.Old.Version ? '@' + replacement.Old.Version : '')
  go(['mod', 'edit', '-modfile=' + modfile, '-replace=' + old + '=' + path.resolve(root, replacement.New.Path)])
}
go(['mod', 'edit', '-modfile=' + modfile, '-replace=' + module + '@' + input.version + '=' + generated.directory])
const testSelection = JSON.parse(go(['list', '-m', '-json', '-modfile=' + modfile, module]))
assert.equal(path.resolve(testSelection.Replace.Dir), path.resolve(generated.directory))
assert.equal(testSelection.Version, input.version)

const sourceRoot = path.join(root, 'tests/owner-integration/server-protection')
const manifest = JSON.parse(fs.readFileSync(path.join(sourceRoot, 'sources.json'), 'utf8'))
assert.equal(manifest.schema, 'solovey.test-source-inputs/v1')
assert.equal(manifest.module, module)
assert.ok(Array.isArray(manifest.sources) && manifest.sources.length > 0 && manifest.sources.length <= 512)
const safeRelative = value => {
  assert.equal(typeof value, 'string')
  assert.ok(value && !/[\\\x00-\x1f<>:"|?*]/.test(value))
  assert.ok(value.split('/').every(part => part && part !== '.' && part !== '..' && !part.startsWith('.')))
  return value
}
const hashes = new Map(), overlay = {}, records = []
let bytes = 0
for (const source of manifest.sources) {
  assert.deepEqual(Object.keys(source).sort(), ['asset', 'file', 'package', 'sha256'])
  const asset = safeRelative(source.asset), relativePackage = safeRelative(source.package)
  assert.ok(asset.endsWith('.go.txt') && source.file.endsWith('_test.go') && path.basename(source.file) === source.file)
  assert.equal(source.file, path.posix.basename(asset).slice(0, -4), 'owner test filename changed')
  assert.match(source.sha256, /^[a-f0-9]{64}$/)
  assert.ok(!hashes.has(asset), 'duplicate owner test asset')
  const filename = path.join(sourceRoot, asset)
  assert.ok(fs.lstatSync(filename).isFile() && !fs.lstatSync(filename).isSymbolicLink())
  const content = fs.readFileSync(filename)
  const formatted = execFileSync('gofmt', [], {cwd: root, input: content, windowsHide: true})
  assert.deepEqual(formatted, content, 'owner test source format drift: ' + asset)
  bytes += content.length
  assert.ok(bytes <= 16 * 1024 * 1024, 'owner test inputs exceed their source bound')
  assert.equal(createHash('sha256').update(content).digest('hex'), source.sha256, 'owner test source drift: ' + asset)
  hashes.set(asset, source.sha256)
  const productionFiles = Object.keys(input.files).filter(name => name.startsWith(relativePackage + '/') && name.endsWith('.go') && !name.endsWith('_test.go'))
  assert.ok(productionFiles.length, 'owner test package is not part of the accepted feature')
  const target = path.join(generated.directory, relativePackage, 'owner_' + source.file)
  assert.ok(!fs.existsSync(target), 'owner overlay must never replace an accepted module file')
  assert.ok(!overlay[target], 'duplicate owner overlay destination')
  overlay[target] = filename
  records.push({asset, package: relativePackage, file: source.file, sha256: source.sha256})
}
const walk = directory => fs.readdirSync(directory, {withFileTypes: true}).flatMap(entry => {
  assert.ok(!entry.isSymbolicLink(), 'owner test source symlink')
  const filename = path.join(directory, entry.name)
  return entry.isDirectory() ? walk(filename) : [path.relative(sourceRoot, filename).split(path.sep).join('/')]
})
assert.deepEqual(walk(sourceRoot).filter(name => name !== 'sources.json').sort(), [...hashes.keys()].sort(), 'missing or unreviewed owner test input')

const flags = process.argv.slice(2)
for (const flag of flags) assert.ok(!/^-(?:overlay|modfile|mod|C)(?:=|$)/.test(flag), 'owner tests cannot override source selection')
const overlayPath = path.join(directory, 'overlay.json')
fs.writeFileSync(overlayPath, JSON.stringify({Replace: overlay}, null, 2) + '\n')
const canonicalSourceHashes = new Map(Object.entries(input.files).filter(([name]) => name.endsWith('.go')).map(([name, hash]) => [name, hash]))
const result = spawnSync('go', ['test', '-modfile=' + modfile, '-overlay=' + overlayPath, ...(flags.length ? flags : ['-race', '-p', '1', '-count=1', '-timeout=10m']), module + '/...'], {
  cwd: root, env: {...env, TEMP: temp, TMP: temp}, stdio: 'inherit', windowsHide: true,
})
if (result.error) throw result.error
// Prove the cache was not edited while the real owner tests ran.
for (const [name, hash] of canonicalSourceHashes) assert.equal(createHash('sha256').update(fs.readFileSync(path.join(selected.Dir, name))).digest('hex'), hash, 'accepted feature source changed during owner tests')
assert.equal(regularSourceTree(selected.Dir), input.sourceTree, 'accepted module cache changed')
assert.equal(regularSourceTree(generated.directory), input.sourceTree, 'generated production source changed')
console.log(JSON.stringify({status: result.status === 0 ? 'PASS' : 'FAIL', module, version: selected.Version, sourceCommit: input.sourceCommit,
  producerPackages: dependencies.filter(item => item.Module?.Path === module).length, ownerTestFiles: records.length,
  ownerTestInputsSHA256: createHash('sha256').update(JSON.stringify(records)).digest('hex'), productionOverrides: 0,
  temporaryTestModuleReplacement: true, generatedSourceFiles: generated.files, generatedSourceTree: generated.tree,
  permanentFeatureReplacement: false, normalProductSubprocessesUsePublishedModule: true, sourceCacheUnchanged: true}))
process.exitCode = result.status ?? 1
} finally {
assert.equal(fs.realpathSync.native(path.dirname(directory)), temp)
assert.ok(path.basename(directory).startsWith('solovey-protection-owner-tests-'))
assert.ok(!fs.lstatSync(directory).isSymbolicLink())
fs.rmSync(directory, {recursive: true})
}
