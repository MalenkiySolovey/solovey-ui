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
  assert.match(image, /^[\w.-]+\/[\w.-]+$/); assert.match(receipt.registryDigest, /^sha256:[a-f0-9]{64}$/)
  const tokenResponse = await fetch(`https://ghcr.io/token?scope=repository:${image}:pull`, { signal: AbortSignal.timeout(30000) })
  assert.ok(tokenResponse.ok); const { token } = await tokenResponse.json()
  for (const ref of [receipt.tag, receipt.tag.includes('-') ? 'beta' : 'latest']) {
    const response = await fetch(`https://ghcr.io/v2/${image}/manifests/${ref}`, { headers: {
      Authorization: `Bearer ${token}`, Accept: 'application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json',
    }, signal: AbortSignal.timeout(60000) })
    assert.ok(response.ok)
    const digest = 'sha256:' + sha(Buffer.from(await response.arrayBuffer()))
    assert.equal(digest, receipt.registryDigest, 'published registry output changed after recovery admission')
    assert.equal(response.headers.get('docker-content-digest'), digest)
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
  await verifyWindowsSet(windows)
  await verifyMetadata(path.join(directory, 'metadata'), commit)
  run(process.execPath, [openwrtVerifier, 'verify', path.join(directory, 'openwrt'), commit], { stdio: 'inherit' })
  // Read archive metadata without extraction or executing any candidate binary.
  run('python3', ['-c', `
import json, pathlib, sys, tarfile, zipfile
root=pathlib.Path(sys.argv[1]); commit, fingerprint, tag=sys.argv[2:]
tree=__import__('subprocess').check_output(['git','rev-parse',commit+'^{tree}'],text=True).strip()
targets=json.loads(pathlib.Path('scripts/release-targets.json').read_text())
def verify(data, platform, profile):
 fields=dict(line.split('=',1) for line in data.decode().splitlines() if '=' in line)
 assert fields['commit']==commit and fields['tree']==tree and fields['source_fingerprint']==fingerprint
 assert fields['version']==tag and fields['platform']==platform and fields['profile']==profile
for target in targets['linux']:
 for profile,prefix in [('full',''),('core','-core')]:
  with tarfile.open(root/'linux'/('solovey-ui'+prefix+'-linux-'+target['platform']+'.tar.gz')) as archive:
   verify(archive.extractfile('solovey-ui/BUILD_INFO.txt').read(),'linux/'+target['platform'],profile)
for arch in targets['windows']:
 for profile in targets['windowsProfiles']:
  prefix='-core' if profile=='core' else ''
  with zipfile.ZipFile(root/'windows'/('solovey-ui'+prefix+'-windows-'+arch+'.zip')) as archive:
   names=[n for n in archive.namelist() if n.replace(chr(92),'/')=='solovey-ui-windows/BUILD_INFO.txt']
   assert len(names)==1
   verify(archive.read(names[0]),'windows/'+arch,profile)
`, directory, commit, fingerprint, tag])
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
  const expectedCount = (targets.linux.length * 2 + 2 + targets.windows.length * targets.windowsProfiles.length + targets.releaseMetadata.length + 3) * 2
  assert.equal(assets.length, expectedCount); assets.sort((a, b) => a.name.localeCompare(b.name))
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
