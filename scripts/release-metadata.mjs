// Canonical corresponding-source, dependency notice and SBOM producer.
// Package downloads are immutable Go module identities or lockfile SRI bytes.
import assert from 'node:assert/strict'
import crypto from 'node:crypto'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { fileURLToPath, pathToFileURL } from 'node:url'

export const metadataNames = JSON.parse(fs.readFileSync(new URL('./release-targets.json', import.meta.url), 'utf8')).releaseMetadata.toSorted()
const sha = bytes => crypto.createHash('sha256').update(bytes).digest('hex')
const json = file => JSON.parse(fs.readFileSync(file, 'utf8'))
const jsonStream = text => (text.match(/^{[\s\S]*?^}/gm) ?? []).map(JSON.parse)
const writeJSON = (file, value) => fs.writeFileSync(file, JSON.stringify(value, null, 2) + '\n')
const run = (command, args, options = {}) => execFileSync(command, args, { encoding: 'utf8', maxBuffer: 64 << 20, ...options })
const digestFile = async file => {
  const hash = crypto.createHash('sha256')
  for await (const chunk of fs.createReadStream(file)) hash.update(chunk)
  return hash.digest('hex')
}

export async function verifyMetadata(directory, commit) {
  assert.match(commit, /^[a-f0-9]{40}$/)
  assert.deepEqual(fs.readdirSync(directory).sort(), metadataNames.flatMap(n => [n, `${n}.sha256`]).sort())
  for (const name of metadataNames) {
    const file = path.join(directory, name)
    assert.ok(fs.lstatSync(file).isFile() && fs.statSync(file).size > 0)
    assert.equal(fs.readFileSync(`${file}.sha256`, 'utf8'), `${await digestFile(file)}  ${name}\n`)
  }
  const p = json(path.join(directory, 'solovey-ui-provenance.json'))
  assert.equal(p.source.commit, commit)
  assert.equal(p.source.tree, run('git', ['rev-parse', `${commit}^{tree}`]).trim())
  assert.equal(p.source.fingerprint, run(process.execPath, ['scripts/release-source.mjs', process.cwd()]).trim())
  assert.equal(p.schema, 'solovey.release-provenance/v1')
  assert.equal(p.officialSemanticBaseline.version, 'v1.14.2')
  assert.equal(p.officialByteIdentical, false)
  assert.deepEqual(p.subjects.map(s => s.name).sort(), metadataNames.filter(n => n !== 'solovey-ui-provenance.json'))
  for (const entry of p.subjects) {
    const file = path.join(directory, entry.name)
    assert.equal(await digestFile(file), entry.sha256)
    assert.equal(fs.statSync(file).size, entry.size)
  }
  const bom = json(path.join(directory, 'solovey-ui-sbom.cdx.json'))
  assert.equal(bom.bomFormat, 'CycloneDX')
  assert.equal(bom.metadata.component.version, p.version)
  assert.equal(bom.metadata.component.properties.find(x => x.name === 'solovey:sourceCommit').value, commit)
  for (const integration of p.integrations) {
    const component = bom.components.find(c => c.name === integration.replacement.module)
    assert.equal(component?.version, integration.replacement.version)
    assert.equal(component.properties.find(x => x.name === 'solovey:sourceCommit').value, integration.replacement.commit)
    assert.equal(component.properties.find(x => x.name === 'solovey:patchSha256').value, integration.patch.sha256)
  }
  return p
}

async function produce(directory, commit) {
  const root = process.cwd()
  assert.match(commit, /^[a-f0-9]{40}$/)
  assert.equal(run('git', ['rev-parse', 'HEAD']).trim(), commit)
  run('git', ['diff', '--exit-code', 'HEAD', '--'])
  assert.equal(run('git', ['status', '--porcelain']).trim(), '', 'metadata requires a clean exact source checkout')
  const tree = run('git', ['rev-parse', 'HEAD^{tree}']).trim()
  const timestamp = run('git', ['show', '-s', '--format=%cI', commit]).trim()
  const version = fs.readFileSync('config/identity/version', 'utf8').trim()
  const fingerprint = run(process.execPath, ['scripts/release-source.mjs', root]).trim()
  const integrations = ['sing-box-ssm', 'sing-quic', 'sing-anytls'].map(name => json(`deploy/dependencies/${name}-integration.json`))
  for (const name of ['ssm', 'quic', 'anytls']) run(process.execPath, [`scripts/${name}-integration-provenance.mjs`, 'check'])
  const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'solovey-release-metadata-'))
  const source = path.join(scratch, 'source'), notices = path.join(scratch, 'notices')
  fs.mkdirSync(directory, { recursive: true })
  assert.equal(fs.readdirSync(directory).length, 0, 'metadata destination must be empty')
  try {
    fs.mkdirSync(source); fs.mkdirSync(notices)
    const archive = path.join(scratch, 'project.tar')
    run('git', ['archive', '--format=tar', `--output=${archive}`, commit])
    const project = path.join(source, 'solovey-ui')
    fs.mkdirSync(project)
    run('tar', ['-xf', archive, '-C', project])
    for (const name of ['LICENSE', 'NOTICE.md']) fs.copyFileSync(name, path.join(notices, name))

    // Resolve in an isolated module request directory: downloading unused
    // platform sources cannot edit the frozen product's go.mod/go.sum.
    const request = path.join(scratch, 'modules')
    fs.mkdirSync(request)
    for (const name of ['go.mod', 'go.sum']) fs.copyFileSync(name, path.join(request, name))
    const downloaded = jsonStream(run('go', ['mod', 'download', '-json', 'all'], { cwd: request, env: { ...process.env, GOWORK: 'off' } }))
    assert.ok(downloaded.length > 0)
    run('go', ['mod', 'verify'], { cwd: request, env: { ...process.env, GOWORK: 'off' } })
    console.log(`Authenticated Go source archives: ${downloaded.length}`)
    const selected = jsonStream(run('go', ['list', '-m', '-json', 'all']))
    const npm = json('frontend/package-lock.json')
    const components = [], dependencies = [], sourceRecords = [], noticeRecords = []
    const goDirectory = path.join(source, 'dependency-sources', 'go')
    const npmDirectory = path.join(source, 'dependency-sources', 'npm')
    fs.mkdirSync(goDirectory, { recursive: true }); fs.mkdirSync(npmDirectory, { recursive: true })
    const addNotice = (identity, relative, bytes) => {
      const prefix = sha(identity).slice(0, 24), target = path.join(notices, prefix, relative)
      fs.mkdirSync(path.dirname(target), { recursive: true }); fs.writeFileSync(target, bytes)
      noticeRecords.push({ dependency: identity, path: `${prefix}/${relative.replaceAll('\\', '/')}`, sha256: sha(bytes), size: bytes.length })
    }
    const scanNotices = (identity, base, relative = '') => {
      for (const e of fs.readdirSync(path.join(base, relative), { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
        const name = path.join(relative, e.name)
        if (e.isDirectory()) scanNotices(identity, base, name)
        else if (e.isFile() && /^(?:licen[cs]e|copying|notice|copyright)(?:[._-]|$)/i.test(e.name)) addNotice(identity, name, fs.readFileSync(path.join(base, name)))
      }
    }
    for (const module of selected.filter(m => !m.Main)) {
      const actual = module.Replace ?? module
      const bytes = downloaded.find(m => m.Path === actual.Path && m.Version === actual.Version)
      assert.ok(bytes && !bytes.Error && bytes.Zip && bytes.Dir && bytes.Sum, `missing immutable source: ${actual.Path}`)
      if (actual.Sum) assert.equal(bytes.Sum, actual.Sum)
      if (actual.GoModSum) assert.equal(bytes.GoModSum, actual.GoModSum)
      const identity = `${actual.Path}@${actual.Version}`, name = `${sha(identity)}.zip`
      fs.copyFileSync(bytes.Zip, path.join(goDirectory, name))
      sourceRecords.push({ ecosystem: 'golang', name: actual.Path, version: actual.Version, source: `dependency-sources/go/${name}`, sha256: await digestFile(bytes.Zip), moduleSum: bytes.Sum, goModSum: bytes.GoModSum })
      scanNotices(identity, bytes.Dir)
      const integration = integrations.find(i => i.replacement.module === actual.Path)
      const properties = [{ name: 'solovey:moduleSum', value: bytes.Sum }, { name: 'solovey:goModSum', value: bytes.GoModSum }]
      if (module.Replace) properties.push({ name: 'solovey:replaces', value: `${module.Path}@${module.Version}` })
      if (integration) properties.push({ name: 'solovey:sourceCommit', value: integration.replacement.commit }, { name: 'solovey:officialSemanticBaseline', value: `${integration.semanticBaseline.module}@${integration.semanticBaseline.version}` }, { name: 'solovey:patchSha256', value: integration.patch.sha256 }, { name: 'solovey:officialByteIdentical', value: 'false' })
      components.push({ type: 'library', 'bom-ref': identity, name: actual.Path, version: actual.Version, scope: 'optional', purl: `pkg:golang/${actual.Path}@${encodeURIComponent(actual.Version)}`, ...(integration ? { licenses: [{ license: { id: integration.license } }] } : {}), properties })
    }
    const npmSources = new Map()
    for (const [location, item] of Object.entries(npm.packages).sort(([a], [b]) => a.localeCompare(b))) {
      if (!location) continue
      assert.ok(item.resolved?.startsWith('https://registry.npmjs.org/') && item.integrity, `unpinned npm source ${location}`)
      const name = item.name ?? location.slice(location.lastIndexOf('node_modules/') + 'node_modules/'.length)
      const identity = `npm:${name}@${item.version}`
      if (npmSources.has(identity)) {
        assert.equal(npmSources.get(identity).integrity, item.integrity, 'duplicate npm source integrity differs')
        continue
      }
      const response = await fetch(item.resolved, { redirect: 'error', signal: AbortSignal.timeout(60000) })
      assert.ok(response.ok, `npm source HTTP ${response.status}: ${name}`)
      const bytes = Buffer.from(await response.arrayBuffer())
      assert.ok(bytes.length > 0 && bytes.length < 128 * 1024 * 1024)
      const sri = item.integrity.split(/\s+/).find(x => x.startsWith('sha512-'))
      assert.ok(sri, `npm source requires SHA512 SRI: ${name}`)
      assert.equal('sha512-' + crypto.createHash('sha512').update(bytes).digest('base64'), sri)
      const filename = `${sha(identity)}.tgz`, file = path.join(npmDirectory, filename)
      fs.writeFileSync(file, bytes); npmSources.set(identity, { file, integrity: item.integrity })
      if (npmSources.size % 50 === 0) console.log(`Authenticated npm source archives: ${npmSources.size}`)
      sourceRecords.push({ ecosystem: 'npm', name, version: item.version, source: `dependency-sources/npm/${filename}`, sha256: sha(bytes), integrity: item.integrity, distribution: item.resolved })
      const members = run('tar', ['-tzf', file]).trim().split('\n')
      for (const member of members) {
        if (!member.endsWith('/') && /(?:^|\/)(?:licen[cs]e|copying|notice|copyright)(?:[._-]|$)/i.test(member) && !member.includes('..') && !path.isAbsolute(member)) {
          addNotice(identity, member, execFileSync('tar', ['-xOf', file, '--', member], { maxBuffer: 8 << 20 }))
        }
      }
      components.push({ type: 'library', 'bom-ref': identity, name, version: item.version, scope: item.dev || item.optional ? 'optional' : 'required', purl: `pkg:npm/${name.replace('@', '%40')}@${item.version}`, ...(item.license ? { licenses: [{ expression: item.license }] } : {}), hashes: [{ alg: 'SHA-512', content: Buffer.from(sri.slice(7), 'base64').toString('hex') }] })
    }
    components.sort((a, b) => a['bom-ref'].localeCompare(b['bom-ref']))
    const app = { type: 'application', 'bom-ref': `solovey-ui@${version}`, name: 'solovey-ui', version, licenses: [{ license: { id: 'GPL-3.0-only' } }], properties: [{ name: 'solovey:sourceCommit', value: commit }, { name: 'solovey:sourceTree', value: tree }, { name: 'solovey:sourceFingerprint', value: fingerprint }] }
    dependencies.push({ ref: app['bom-ref'], dependsOn: components.map(c => c['bom-ref']) })
    const bom = { bomFormat: 'CycloneDX', specVersion: '1.6', version: 1, metadata: { timestamp, component: app, properties: [{ name: 'solovey:inventoryScope', value: 'Complete resolved Go module graph and npm lockfile source inventory; includes platform alternatives and build tools, not a claim that every module is linked into every artifact.' }] }, components, dependencies }
    writeJSON(path.join(directory, 'solovey-ui-sbom.cdx.json'), bom)
    sourceRecords.sort((a, b) => a.source.localeCompare(b.source))
    noticeRecords.sort((a, b) => a.path.localeCompare(b.path))
    writeJSON(path.join(source, 'DEPENDENCY_SOURCES.json'), sourceRecords)
    writeJSON(path.join(source, 'SOURCE_IDENTITY.json'), { commit, tree, version, sourceFingerprint: fingerprint })
    writeJSON(path.join(notices, 'LICENSE_INVENTORY.json'), noticeRecords)
    for (const integration of integrations) assert.ok(noticeRecords.some(n => n.dependency === `${integration.replacement.module}@${integration.replacement.version}`), 'integration license missing')
    for (const [name, input] of [['solovey-ui-source.tar.gz', source], ['solovey-ui-license-notices.tar.gz', notices]]) {
      const tar = path.join(scratch, `${name}.tar`)
      run('tar', ['--sort=name', '--mtime=@0', '--owner=0', '--group=0', '--numeric-owner', '--mode=u+rwX,go+rX,go-w', '--format=gnu', '-cf', tar, '-C', input, '.'])
      const fd = fs.openSync(path.join(directory, name), 'w')
      try { run('gzip', ['-n', '-c', tar], { stdio: ['ignore', fd, 'inherit'] }) } finally { fs.closeSync(fd) }
      assert.ok(fs.statSync(path.join(directory, name)).size < 2 * 1024 ** 3, 'source archive exceeds the public asset bound')
    }
    const subjects = []
    for (const name of metadataNames.filter(n => n !== 'solovey-ui-provenance.json')) subjects.push({ name, sha256: await digestFile(path.join(directory, name)), size: fs.statSync(path.join(directory, name)).size })
    writeJSON(path.join(directory, 'solovey-ui-provenance.json'), { schema: 'solovey.release-provenance/v1', version, source: { repository: 'https://github.com/MalenkiySolovey/solovey-ui', commit, tree, fingerprint }, officialSemanticBaseline: integrations[0].semanticBaseline, officialByteIdentical: false, integrations, toolchain: { go: run('go', ['version']).trim(), node: process.version, npm: run('npm', ['--version']).trim() }, builder: '.github/workflows/release.yml:release-metadata; scripts/release-metadata.mjs', inventory: { goModules: selected.length - 1, npmPackages: npmSources.size, dependencyNotices: noticeRecords.length }, compatibility: json('scripts/release-compatibility.json'), subjects })
    for (const name of metadataNames) fs.writeFileSync(path.join(directory, `${name}.sha256`), `${await digestFile(path.join(directory, name))}  ${name}\n`)
    assert.equal(run(process.execPath, ['scripts/release-source.mjs', root]).trim(), fingerprint, 'producer changed frozen source')
    await verifyMetadata(directory, commit)
    console.log(JSON.stringify({ status: 'PASS', commit, tree, assets: metadataNames, goModules: selected.length - 1, npmPackages: npmSources.size, dependencyNotices: noticeRecords.length }))
  } finally {
    const actual = fs.realpathSync(scratch)
    assert.equal(path.dirname(actual), fs.realpathSync(os.tmpdir()))
    assert.ok(path.basename(actual).startsWith('solovey-release-metadata-'))
    fs.rmSync(actual, { recursive: true, force: true })
  }
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  const [mode, directory, commit] = process.argv.slice(2)
  assert.ok(directory && commit && ['produce', 'verify'].includes(mode), `usage: node ${fileURLToPath(import.meta.url)} <produce|verify> <empty-output-directory> <exact-commit>`)
  if (mode === 'produce') await produce(path.resolve(directory), commit)
  else { await verifyMetadata(path.resolve(directory), commit); console.log('release source, SBOM, licenses and provenance verified') }
}
