// Local build and candidate evidence only; the signed release envelope is unchanged.
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { pathToFileURL } from 'node:url'
import { resolveFrontendDistribution, sha256, verifyFrontendReleaseInput } from './frontend-artifacts.mjs'
import { compositionDigest, compositionSchema } from './release-composition.mjs'
import { observePanelArchives, verifyArchiveBuildInfo } from './release-candidate.mjs'

const json = file => JSON.parse(fs.readFileSync(file, 'utf8'))
const run = (program, args) => execFileSync(program, args, { encoding: 'utf8', windowsHide: true, maxBuffer: 4 << 20 })
const digest = entries => sha256(JSON.stringify(entries))

export function verifyPanelModules(info, { platform, arch, profile, provider }, { graph }) {
  const plan = graph.providers[provider]
  assert.ok(plan, 'unadmitted build provider')
  assert.equal(info.GoVersion, `go${graph.toolchain.go}`, 'built Panel Go toolchain differs')
  const settings = new Map(info.Settings.map(({ Key, Value }) => [Key, Value]))
  assert.equal(settings.size, info.Settings.length, 'ambiguous Panel build setting')
  const tags = new Set(settings.get('-tags')?.split(','))
  assert.equal(settings.get('GOOS'), platform, 'built Panel OS differs')
  const actualArch = settings.get('GOARCH') === 'arm' ? `armv${settings.get('GOARM')?.split(',')[0]}` : settings.get('GOARCH')
  assert.equal(actualArch, arch, 'built Panel architecture differs')
  assert.equal(tags.has('minimal'), profile === 'core', 'built Panel profile differs')
  assert.equal(tags.has('shtorm_extended'), provider === 'shtorm-extended', 'built Panel provider tag differs')
  const dependencies = new Map(info.Deps.map(item => [item.Path, item]))
  assert.equal(dependencies.size, info.Deps.length, 'ambiguous linked Panel module')
  const binding = (module, version, checksum, actual) => {
    assert.deepEqual(actual, { Path: module, Version: version, Sum: checksum }, `built Panel module differs: ${module}`)
  }
  for (const item of [graph.backend.source, ...graph.shared]) {
    // Management contracts are also used by conformance tests, not necessarily linked into Panel.
    if (!item.contractVersion || dependencies.has(item.module)) binding(item.module, item.version, item.moduleChecksum, dependencies.get(item.module))
  }
  for (const item of graph.components.external) {
    if (profile === 'core') assert.ok(!dependencies.has(item.module), `Core links optional component: ${item.id}`)
    else binding(item.module, item.version, item.moduleChecksum, dependencies.get(item.module))
  }
  for (const { original, replacement } of plan.copies) {
    const actual = dependencies.get(original.module)
    if (replacement.module === plan.engine.module || actual) binding(replacement.module, replacement.version, replacement.sum, actual?.Replace)
  }
}

export function compositionBuildFields(receipt, target, frontend, binarySHA256) {
  const { graph } = receipt
  const { platform, arch, profile, provider } = target
  assert.equal(graph.schema, compositionSchema, 'unknown Product composition')
  assert.equal(receipt.sha256, compositionDigest(graph), 'Product composition digest differs')
  assert.ok(graph.eligibility[platform]?.some(item => item.arch === arch && item.profile === profile && item.provider === provider), 'unadmitted Product build target')
  assert.equal(frontend.archiveSHA256, graph.profiles[profile].frontend.archiveSHA256, 'release Frontend profile differs from Product composition')
  assert.match(binarySHA256, /^[a-f0-9]{64}$/, 'built Panel hash is required')
  return { composition_sha256: receipt.sha256, frontend_archive_sha256: frontend.archiveSHA256,
    frontend_embedded_sha256: digest(frontend.embedded), frontend_component_files_sha256: digest(frontend.packed), panel_sha256: binarySHA256 }
}

export function verifyCompositionBuildFields(fields, receipt, target, frontend, binarySHA256) {
  for (const [key, value] of Object.entries(compositionBuildFields(receipt, target, frontend, binarySHA256))) {
    assert.equal(fields[key], value, `archive ${key} differs from Product composition/build`)
  }
}

export async function referenceFrontends(root = process.cwd()) {
  const { directory: full } = await resolveFrontendDistribution({ root, key: 'full' })
  const { directory: core } = await resolveFrontendDistribution({ root, key: 'core' })
  const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'solovey-composition-frontends-'))
  try {
    const split = path.join(scratch, 'dist'), packs = path.join(scratch, 'packs')
    fs.cpSync(full, split, { recursive: true })
    run(process.execPath, [path.join(root, 'scripts/extract-component-frontend.mjs'), '--dist', split,
      '--components-dir', path.join(root, 'components'), '--out-dir', packs, '--prune-dist'])
    run(process.execPath, [path.join(root, 'scripts/write-component-installed-metadata.mjs'), '--components-dir', packs])
    return {
      core: await verifyFrontendReleaseInput(core, { root, key: 'core' }),
      full: await verifyFrontendReleaseInput(full, { root, key: 'full' }),
      split: await verifyFrontendReleaseInput(split, { root, key: 'full', componentsDirectory: packs }),
    }
  } finally { fs.rmSync(scratch, { recursive: true, force: true }) }
}

// Hash only regular payload members. Do not extract an untrusted archive to disk.
export function observeComponentArchive(file) {
  const stat = fs.lstatSync(file)
  assert.ok(stat.isFile() && stat.size > 0 && stat.size < (32 << 20), 'invalid component pack archive')
  return JSON.parse(run('python3', ['-c', `
import hashlib, json, sys, tarfile
with tarfile.open(sys.argv[1], 'r|gz') as archive:
    digests={}; names=set(); total=0
    for member in archive:
        name=member.name
        parts=name.split('/')
        assert parts[0]=='components' and all(p and p not in ('.','..') and '\\\\' not in p for p in parts)
        assert name not in names and len(names)<8192
        names.add(name)
        if member.isdir(): continue
        assert member.isreg() and len(parts)>2 and 0<member.size<16*1024*1024
        total+=member.size
        assert total<=96*1024*1024
        with archive.extractfile(member) as contents:
            digests['/'.join(parts[1:])]=hashlib.file_digest(contents,'sha256').hexdigest()
    print(json.dumps(digests))
`, file]))
}

export function verifyStagedFrontend(facts, generated, stageEntries, frontend) {
  assert.equal(facts.frontend.artifact.archiveSHA256, frontend.archiveSHA256, 'OpenWrt stage Frontend profile differs')
  const files = (records, prefix) => Object.fromEntries(records.filter(item => item.path.startsWith(prefix) && item.type !== 'directory')
    .map(({path: name, sha256}) => [name.slice(prefix.length), sha256]))
  assert.deepEqual(files(generated, 'web/html/'), Object.fromEntries(frontend.embedded), 'OpenWrt embed inputs differ')
  const componentFiles = records => Object.fromEntries(Object.entries(records).filter(([name]) => name.includes('/')))
  assert.deepEqual(componentFiles(files(generated, '.release/components/')), Object.fromEntries(frontend.packed), 'OpenWrt generated packs differ')
  assert.deepEqual(componentFiles(files(stageEntries, 'usr/lib/solovey-ui/components/')), Object.fromEntries(frontend.packed), 'OpenWrt staged pack bytes differ')
}

export async function verifyComposedArchives(receipt, { linux, windows, tag }) {
  const frontends = await referenceFrontends()
  const { product, eligibility } = receipt.graph
  assert.equal(tag, `v${product.version}`, 'candidate version differs from Product composition')
  assert.deepEqual(observeComponentArchive(path.join(linux, 'solovey-ui-components.tar.gz')), Object.fromEntries(frontends.split.packed),
    'packaged component byte inventory differs from admitted split Frontend')
  const archives = observePanelArchives(linux, windows, eligibility.linux, windows ? eligibility.windows : [], { hashPanel: true })
  for (const { data, target, binarySHA256 } of archives) {
    const fields = verifyArchiveBuildInfo(data, target, { ...product, tag })
    const key = target.profile === 'full' && target.platform === 'linux' ? 'split' : target.profile
    verifyCompositionBuildFields(fields, receipt, target, frontends[key], binarySHA256)
  }
  return archives.length
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  const [command, ...args] = process.argv.slice(2)
  if (command === 'build' && [6, 7].includes(args.indexOf('--'))) {
    const separator = args.indexOf('--')
    const [file, platform, arch, profile, provider, binary, componentsDirectory] = args.slice(0, separator)
    const [builder, ...builderArgs] = args.slice(separator + 1)
    const receipt = json(file), target = { platform, arch, profile, provider }
    assert.ok(builder && !fs.lstatSync(binary, { throwIfNoEntry: false }), 'composition needs a fresh Panel output')
    assert.equal(Boolean(componentsDirectory), platform === 'linux' && profile === 'full', 'wrong release Frontend layout for target')
    const frontend = await verifyFrontendReleaseInput('web/html', { key: profile, componentsDirectory })
    // The owned build reads this exact, verified tree. Do not issue evidence
    // for a preexisting binary or allow the builder to rewrite its inputs.
    execFileSync(builder, builderArgs, { stdio: ['ignore', 2, 2], windowsHide: true })
    assert.deepEqual(await verifyFrontendReleaseInput('web/html', { key: profile, componentsDirectory }), frontend, 'build modified Frontend inputs')
    assert.ok(fs.lstatSync(binary).isFile(), 'built Panel is not a regular file')
    verifyPanelModules(JSON.parse(run('go', ['version', '-m', '-json', path.resolve(binary)])), target, receipt)
    const hash = createHash('sha256')
    for await (const bytes of fs.createReadStream(binary)) hash.update(bytes)
    for (const [key, value] of Object.entries(compositionBuildFields(receipt, target, frontend, hash.digest('hex')))) console.log(`${key}=${value}`)
  } else if (command === 'verify-linux' && args.length === 3) {
    const [file, linux, tag] = args
    const receipt = json(file)
    const count = await verifyComposedArchives(receipt, { linux, tag })
    console.log(`Product composition ${receipt.sha256}: ${count} Linux panel archives and component bytes verified before signing`)
  } else throw Error('expected build <receipt> <os> <arch> <profile> <provider> <binary> [packs] -- <builder> [args] or verify-linux <receipt> <dir> <tag>')
}
