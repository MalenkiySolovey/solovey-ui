// Execution-only admission; product contracts come from the immutable checkout.
import assert from 'node:assert/strict'
import crypto from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { pathToFileURL } from 'node:url'

const sha = bytes => crypto.createHash('sha256').update(bytes).digest('hex')
const run = (command, args, options = {}) => execFileSync(command, args, { encoding: 'utf8', maxBuffer: 16 << 20, ...options })
const json = file => JSON.parse(fs.readFileSync(file, 'utf8'))
const load = name => import(pathToFileURL(path.resolve('scripts', name)))

// Windows emits commit and the complete source fingerprint. The verified
// disclosure binds that same commit to its Git tree; Windows BUILD_INFO does
// not invent the Linux producer's additional tree field.
export function verifyArchiveBuildInfo(data, target, { commit, tree, fingerprint, tag }) {
  const fields = Object.fromEntries(data.split(/\r?\n/).filter(line => line.includes('=')).map(line => {
    const separator = line.indexOf('='); return [line.slice(0, separator), line.slice(separator + 1)]
  }))
  assert.ok(['linux', 'windows'].includes(target.platform), 'unsupported archive metadata platform')
  for (const [name, expected] of Object.entries({ commit, source_fingerprint: fingerprint, version: tag,
    platform: `${target.platform}/${target.arch}`, profile: target.profile })) assert.equal(fields[name], expected, `archive ${name} differs`)
  if (target.platform === 'linux' || fields.tree !== undefined) assert.equal(fields.tree, tree, 'archive tree differs')
  if (target.provider !== undefined) assert.equal(fields.core_provider, target.provider, 'archive provider differs')
}

// An execution-only recovery orchestrator can be newer than its immutable
// product. v1 products predate release-providers.mjs and retain their original
// Official-only archive convention; v2 always uses the product's policy owner.
export async function candidatePanelInventory(signed, targets, loadPolicy = () => load('release-providers.mjs')) {
  if (signed.schema === undefined || signed.schema === 'solovey.release/v1') {
    return { providerAware: false, panels: targets.linux.flatMap(({ platform }) =>
      ['full', 'core'].map(profile => ({ name: `solovey-ui${profile === 'core' ? '-core' : ''}-linux-${platform}.tar.gz`, arch: platform, profile }))) }
  }
  const { linuxPanelArtifacts, providerSchema } = await loadPolicy()
  assert.equal(signed.schema, providerSchema, 'unsupported product release schema')
  return { providerAware: true, panels: linuxPanelArtifacts(signed.schema) }
}

export async function candidateAssetInventory(signed, targets) {
  if (signed.schema === 'solovey.release/v2') {
    const { publicAssetNames, windowsPanelArtifacts } = await load('release-providers.mjs')
    return { names: publicAssetNames(signed.schema), windows: windowsPanelArtifacts(signed.schema) }
  }
  assert.ok(signed.schema === undefined || signed.schema === 'solovey.release/v1', 'unsupported product release schema')
  const { panels } = await candidatePanelInventory(signed, targets)
  const windows = targets.windows.flatMap(arch => targets.windowsProfiles.map(profile => ({
    name: `solovey-ui${profile === 'core' ? '-core' : ''}-windows-${arch}.zip`, arch, profile,
  })))
  const payloads = [...panels, ...windows].map(item => item.name)
  payloads.push('solovey-ui-components.tar.gz', 'solovey-ui-release.json', ...targets.releaseMetadata,
    'solovey-ui-openwrt-x86-64.apk', 'solovey-ui-openwrt-rockchip-armv8.apk', 'solovey-ui-friendlywrt-storage.json')
  return { names: payloads.flatMap(name => [name, `${name}.sha256`]).sort(), windows }
}

export function requireExternalCandidate(checkout, directory) {
  const relative = path.relative(fs.realpathSync(checkout), fs.realpathSync(directory))
  assert.ok(relative && (relative === '..' || relative.startsWith('..' + path.sep) || path.isAbsolute(relative)), 'candidate outputs must be outside the immutable checkout')
}

export function verifyRecoveryBinding(receipt, actual, sourceRunID) {
  assert.equal(receipt.schema, 'solovey.release-recovery/v1')
  assert.equal(String(receipt.sourceRun.id), String(sourceRunID))
  for (const name of ['commit', 'tag', 'fingerprint', 'envelopeSHA256', 'sequence']) assert.deepEqual(actual[name], receipt[name], name)
  assert.equal(actual.sequence, receipt.sourceRun.number * 1000 + receipt.sourceRun.attempt)
  assert.deepEqual(actual.assets, receipt.assets, 'recovery must retain every original candidate byte')
}

async function verifyRegistryStillBound(receipt) {
  const image = receipt.repository.toLowerCase()
  assert.match(image, /^[\w.-]+\/[\w.-]+$/)
  const tokenResponse = await fetch(`https://ghcr.io/token?scope=repository:${image}:pull`, { signal: AbortSignal.timeout(30000) })
  assert.ok(tokenResponse.ok); const { token } = await tokenResponse.json()
  const bindings = receipt.registryBindings ?? [{ suffix: '', digest: receipt.registryDigest }]
  assert.ok(bindings.length > 0 && bindings.length <= 2)
  assert.equal(new Set(bindings.map(binding => binding.suffix)).size, bindings.length)
  for (const binding of bindings) {
    assert.ok(['', '-shtorm-extended'].includes(binding.suffix)); assert.match(binding.digest, /^sha256:[a-f0-9]{64}$/)
    for (const ref of [receipt.tag, receipt.tag.includes('-') ? 'beta' : 'latest'].map(ref => ref + binding.suffix)) {
      const response = await fetch(`https://ghcr.io/v2/${image}/manifests/${ref}`, { headers: {
        Authorization: `Bearer ${token}`, Accept: 'application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json',
      }, signal: AbortSignal.timeout(60000) })
      assert.ok(response.ok)
      const digest = 'sha256:' + sha(Buffer.from(await response.arrayBuffer()))
      assert.equal(digest, binding.digest, 'published registry output changed after recovery admission')
      assert.equal(response.headers.get('docker-content-digest'), digest)
    }
  }
}

export async function verifyCandidate(directory, { commit, tag, fingerprint, trustRootsBase64, openwrtVerifier, receipt, receiptRequired = false } = {}) {
  assert.match(commit ?? '', /^[a-f0-9]{40}$/); assert.match(fingerprint ?? '', /^[a-f0-9]{64}$/)
  assert.ok(!receiptRequired || receipt, 'recovery requires its original-byte receipt')
  requireExternalCandidate(process.cwd(), directory)
  assert.equal(run('git', ['rev-parse', 'HEAD']).trim(), commit)
  assert.equal(run('git', ['status', '--porcelain']).trim(), '', 'candidate verification requires a clean immutable product checkout')
  assert.equal(run(process.execPath, ['scripts/release-source.mjs', process.cwd()]).trim(), fingerprint)
  const { verifyReleaseSet, verifyWindowsSet, targets } = await load('release-verify.mjs')
  const { verifyMetadata } = await load('release-metadata.mjs')
  const linux = path.join(directory, 'linux'), windows = path.join(directory, 'windows')
  const signed = await verifyReleaseSet(linux, { version: tag, trustRootsBase64 })
  const panelInventory = await candidatePanelInventory(signed, targets)
  const inventory = await candidateAssetInventory(signed, targets)
  await verifyWindowsSet(windows)
  const disclosure = await verifyMetadata(path.join(directory, 'metadata'), commit)
  run(process.execPath, [openwrtVerifier, 'verify', path.join(directory, 'openwrt'), commit], { stdio: 'inherit' })
  // Read archive metadata without extraction or executing any candidate binary.
  const archiveMetadata = JSON.parse(run('python3', ['-c', `
import json, pathlib, sys, tarfile, zipfile
root=pathlib.Path(sys.argv[1]); panels=json.loads(sys.argv[2]); windows=json.loads(sys.argv[3]); result=[]
for target in panels:
 with tarfile.open(root/'linux'/target['name']) as archive:
  result.append({'target':target,'data':archive.extractfile('solovey-ui/BUILD_INFO.txt').read().decode()})
for target in windows:
 with zipfile.ZipFile(root/'windows'/target['name']) as archive:
  names=[n for n in archive.namelist() if n.replace(chr(92),'/')=='solovey-ui-windows/BUILD_INFO.txt']
  assert len(names)==1
  result.append({'target':target,'data':archive.read(names[0]).decode()})
print(json.dumps(result))
`, directory, JSON.stringify(panelInventory.panels.map(target => ({ ...target, platform: 'linux',
    provider: panelInventory.providerAware ? target.provider : undefined }))), JSON.stringify(inventory.windows.map(target => ({ ...target, platform: 'windows' })))]))
  assert.equal(disclosure.source.fingerprint, fingerprint, 'archive source fingerprint differs from verified disclosure')
  for (const { data, target } of archiveMetadata) verifyArchiveBuildInfo(data, target, {
    commit, tree: disclosure.source.tree, fingerprint, tag,
  })
  const assets = [], names = new Set()
  for (const relative of ['linux', 'windows', 'metadata', 'openwrt/assets']) {
    for (const name of fs.readdirSync(path.join(directory, relative)).sort()) {
      assert.ok(!names.has(name), 'duplicate public candidate name'); names.add(name)
      const file = path.join(directory, relative, name), stat = fs.lstatSync(file)
      assert.ok(stat.isFile() && stat.size > 0)
      const hash = crypto.createHash('sha256')
      for await (const bytes of fs.createReadStream(file)) hash.update(bytes)
      assets.push({ name, size: stat.size, sha256: hash.digest('hex') })
    }
  }
  assert.deepEqual([...names].sort(), inventory.names, 'public release must contain every canonical provider artifact and no extras')
  assets.sort((a, b) => a.name.localeCompare(b.name))
  const actual = { commit, tag, fingerprint, envelopeSHA256: sha(fs.readFileSync(path.join(linux, 'solovey-ui-release.json'))), sequence: signed.sequence, assets }
  if (receipt) {
    const recovery = json(receipt)
    assert.equal(recovery.repository.toLowerCase(), process.env.GITHUB_REPOSITORY.toLowerCase())
    verifyRecoveryBinding(recovery, actual, process.env.RESUME_RUN_ID)
    await verifyRegistryStillBound(recovery)
  } else assert.ok(!receiptRequired, 'recovery requires its original-byte receipt')
  assert.equal(run('git', ['status', '--porcelain']).trim(), '')
  assert.equal(run(process.execPath, ['scripts/release-source.mjs', process.cwd()]).trim(), fingerprint, 'verification must not change product source identity')
  return actual
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  const result = await verifyCandidate(process.env.RELEASE_CANDIDATE_DIR, {
    commit: process.env.PRODUCT_COMMIT, tag: process.env.RELEASE_TAG_INPUT,
    fingerprint: process.env.PRODUCT_FINGERPRINT, trustRootsBase64: process.env.SUI_RELEASE_TRUST_ROOTS_B64,
    openwrtVerifier: path.join(process.env.RUNNER_TEMP, 'release-openwrt.mjs'), receipt: process.env.RELEASE_RECOVERY_RECEIPT || undefined,
    receiptRequired: Boolean(process.env.RESUME_RUN_ID),
  })
  console.log(JSON.stringify({ status: 'PASS', ...result }))
}
