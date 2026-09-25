// Execution-only recovery: retain signed product bytes and validate every identity
// before completing an interrupted transaction. Never rebuild or rewrite GHCR.
import assert from 'node:assert/strict'
import crypto from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { pathToFileURL } from 'node:url'

const runID = process.env.RESUME_RUN_ID
const repository = process.env.GITHUB_REPOSITORY
const commit = process.env.PRODUCT_COMMIT
const fingerprint = process.env.PRODUCT_FINGERPRINT
const tag = process.env.RELEASE_TAG_INPUT
assert.match(runID ?? '', /^[1-9][0-9]+$/)
assert.match(repository ?? '', /^[\w.-]+\/[\w.-]+$/)
assert.match(commit ?? '', /^[a-f0-9]{40}$/)
assert.match(fingerprint ?? '', /^[a-f0-9]{64}$/)
assert.match(tag ?? '', /^v\d+\.\d+\.\d+(?:-[a-z0-9.-]+)?$/)
const gh = args => execFileSync('gh', args, { encoding: 'utf8', maxBuffer: 16 * 1024 * 1024 })
const run = JSON.parse(gh(['api', `repos/${repository}/actions/runs/${runID}`]))
assert.equal(run.repository.full_name.toLowerCase(), repository.toLowerCase())
assert.equal(run.path, '.github/workflows/release.yml')
assert.equal(run.event, 'workflow_dispatch')
assert.equal(run.status, 'completed')
assert.equal(run.conclusion, 'failure')
assert.notEqual(String(run.id), process.env.GITHUB_RUN_ID)
execFileSync('git', ['merge-base', '--is-ancestor', run.head_sha, process.env.GITHUB_SHA])
assert.equal(execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim(), commit)
execFileSync(process.execPath, [path.join(process.env.RUNNER_TEMP, 'release-publish-gate.mjs'), 'before', tag], {
  stdio: 'inherit', env: { ...process.env, GITHUB_SHA: commit },
})
for (const [name, directory] of [
  ['complete-linux-release', 'release-assets'],
  ['solovey-ui-windows-amd64', 'windows-assets'],
  ['solovey-ui-windows-arm64', 'windows-assets'],
]) gh(['run', 'download', runID, '--repo', repository, '--name', name, '--dir', directory])

const { verifyReleaseSet, verifyWindowsSet, targets } = await import(pathToFileURL(path.resolve('scripts/release-verify.mjs')))
await verifyReleaseSet('release-assets', { version: tag, trustRootsBase64: process.env.SUI_RELEASE_TRUST_ROOTS_B64 })
const envelope = JSON.parse(fs.readFileSync('release-assets/solovey-ui-release.json', 'utf8'))
assert.ok(envelope.manifest.sequence > run.run_number * 1000)
assert.ok(envelope.manifest.sequence <= run.run_number * 1000 + run.run_attempt)
// Validate original checksum before canonicalizing only its binary-mode marker.
for (const arch of targets.windows) {
  const name = `solovey-ui-windows-${arch}.zip`
  const hash = crypto.createHash('sha256')
  for await (const chunk of fs.createReadStream(`windows-assets/${name}`)) hash.update(chunk)
  const digest = hash.digest('hex')
  const sidecar = `windows-assets/${name}.sha256`
  const original = fs.readFileSync(sidecar, 'utf8')
  assert.ok(original === `${digest} *${name}\n` || original === `${digest}  ${name}\n`, `untrusted Windows checksum: ${name}`)
  fs.writeFileSync(sidecar, `${digest}  ${name}\n`)
}
await verifyWindowsSet('windows-assets')
// Read archive metadata without extraction or executing any candidate binary.
execFileSync('python3', ['-c', `
import json, os, pathlib, tarfile, zipfile
commit=os.environ['PRODUCT_COMMIT']; fingerprint=os.environ['PRODUCT_FINGERPRINT']; tag=os.environ['RELEASE_TAG_INPUT']
targets=json.loads(pathlib.Path('scripts/release-targets.json').read_text())
for target in targets['linux']:
 for profile,prefix in [('full',''),('core','-core')]:
  name='solovey-ui'+prefix+'-linux-'+target['platform']+'.tar.gz'
  with tarfile.open('release-assets/'+name) as archive:
   fields=dict(line.split('=',1) for line in archive.extractfile('solovey-ui/BUILD_INFO.txt').read().decode().splitlines() if '=' in line)
  assert fields['commit']==commit and fields['source_fingerprint']==fingerprint and fields['version']==tag
  assert fields['profile']==profile and fields['platform']=='linux/'+target['platform']
for arch in targets['windows']:
 with zipfile.ZipFile('windows-assets/solovey-ui-windows-'+arch+'.zip') as archive:
  names=[name for name in archive.namelist() if name.replace(chr(92),'/')=='solovey-ui-windows/BUILD_INFO.txt']
  assert len(names)==1
  fields=dict(line.split('=',1) for line in archive.read(names[0]).decode().splitlines() if '=' in line)
  assert fields['commit']==commit and fields['source_fingerprint']==fingerprint and fields['version']==tag
  assert fields['platform']=='windows/'+arch and fields['profile']=='full'
`], { stdio: 'inherit' })

const image = repository.toLowerCase()
const tokenResponse = await fetch(`https://ghcr.io/token?scope=repository:${image}:pull`)
assert.ok(tokenResponse.ok)
const { token } = await tokenResponse.json()
async function registry(kind, ref) {
  const response = await fetch(`https://ghcr.io/v2/${image}/${kind}/${ref}`, { headers: {
    Authorization: `Bearer ${token}`, Accept: 'application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json',
  }, signal: AbortSignal.timeout(60000) })
  assert.ok(response.ok, `registry response ${response.status}`)
  const bytes = Buffer.from(await response.arrayBuffer())
  const digest = 'sha256:' + crypto.createHash('sha256').update(bytes).digest('hex')
  if (ref.startsWith('sha256:')) assert.equal(digest, ref)
  return { value: JSON.parse(bytes), digest }
}
const index = await registry('manifests', tag)
assert.equal((await registry('manifests', tag.includes('-') ? 'beta' : 'latest')).digest, index.digest)
const seen = new Set()
for (const item of index.value.manifests) {
  if (item.platform.architecture === 'unknown') {
    assert.equal(item.annotations?.['vnd.docker.reference.type'], 'attestation-manifest')
    continue
  }
  const arch = item.platform.architecture
  assert.ok(['amd64', 'arm64'].includes(arch) && !seen.has(arch))
  assert.equal(item.platform.os, 'linux')
  seen.add(arch)
  const manifest = await registry('manifests', item.digest)
  const config = await registry('blobs', manifest.value.config.digest)
  assert.equal(config.value.architecture, arch)
  assert.equal(config.value.os, 'linux')
  assert.equal(config.value.config.Labels['org.opencontainers.image.revision'], commit)
}
assert.equal(seen.size, 2)
console.log(JSON.stringify({ resumedRun: run.id, productCommit: commit, sourceFingerprint: fingerprint,
  signedSequence: envelope.manifest.sequence, registryDigest: index.digest, status: 'PASS' }))
