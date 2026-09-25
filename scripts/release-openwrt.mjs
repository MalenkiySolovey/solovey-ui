// Release admission only. Product/package authorities are loaded from the
// immutable checkout, never from the newer orchestration revision.
import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { pathToFileURL } from 'node:url'

const load = name => import(pathToFileURL(path.resolve('scripts', name)))
const { canonical, sha256, verifySourceFingerprint } = await load('openwrt-package-source-fingerprint.mjs')
const { createApkMetadataProof } = await load('openwrt-apk-metadata.mjs')
const { verifyStageManifest, stageManifestSchema } = await load('openwrt-stage-manifest.mjs')
const { apkRootfsProofSchema } = await load('openwrt-apk-rootfs.mjs')
const { packageHostToolProofSchema, packageExecutionEnvironment } = await load('openwrt-package-host-tools.mjs')
const profiles = ['x86-64', 'rockchip-armv8']
const json = file => JSON.parse(fs.readFileSync(file, 'utf8'))
const digest = file => sha256(fs.readFileSync(file))
const [mode, directory, productCommit, selected, destination] = process.argv.slice(2)
assert.match(productCommit ?? '', /^[a-f0-9]{40}$/)
assert.equal(execFileSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8' }).trim(), productCommit)
execFileSync('git', ['diff', '--exit-code', 'HEAD', '--'], { stdio: 'pipe' })
const tracked = new Set(execFileSync('git', ['ls-files', '-z'], { encoding: 'utf8' }).split('\0'))
const version = fs.readFileSync('config/identity/version', 'utf8').trim()
assert.equal(`v${version}`, process.env.RELEASE_TAG_INPUT)
const release = Number(/^PKG_RELEASE:=(\d+)$/m.exec(fs.readFileSync('deploy/openwrt/package/solovey-ui/Makefile', 'utf8'))[1])

function authority(profile) {
  assert.ok(profiles.includes(profile))
  const coordinates = execFileSync('bash', ['-c', `
    set -euo pipefail
    source scripts/openwrt-target-profile.sh
    openwrt_target_profile_load "$1"
    printf '%s\\n' "profile=$OPENWRT_PROFILE_KEY" "release=$OPENWRT_RELEASE" \
      "target=$OPENWRT_TARGET" "subtarget=$OPENWRT_SUBTARGET" \
      "packageArchitecture=$OPENWRT_PACKAGE_ARCH" "sdkFilename=$OPENWRT_SDK_FILENAME" \
      "sdkSha256=$OPENWRT_SDK_SHA256" "sourceTag=$OPENWRT_SOURCE_TAG" \
      "revision=$OPENWRT_REVISION" "sourceCommit=$OPENWRT_SOURCE_COMMIT" \
      "goarch=$OPENWRT_GOARCH" "platform=$OPENWRT_PLATFORM" "compilerTarget=$OPENWRT_CC_TARGET"
  `, 'profile', profile], { encoding: 'utf8' })
  return Object.fromEntries(coordinates.trim().split('\n').map(line => {
    const index = line.indexOf('=')
    return [line.slice(0, index), line.slice(index + 1)]
  }))
}

function verify(apk, proofDir, profile) {
  const stat = fs.lstatSync(apk)
  assert.ok(stat.isFile() && stat.size > 0 && stat.size < 256 * 1024 * 1024, 'APK size/type')
  const hash = digest(apk)
  const fingerprint = json(path.join(proofDir, 'SOURCE_FINGERPRINT.json'))
  verifySourceFingerprint(fingerprint, process.cwd())
  for (const input of fingerprint.source.inputs) assert.ok(tracked.has(input.path), `untagged source input: ${input.path}`)
  const facts = fingerprint.build.facts
  assert.equal(facts.revision.commit, productCommit, 'OPENWRT_PRODUCT_SOURCE == RELEASE_PRODUCT_SOURCE')
  if (process.env.SUI_RELEASE_TRUST_ROOTS_B64) {
    assert.equal(facts.releaseTrustRootsSha256, sha256(Buffer.from(process.env.SUI_RELEASE_TRUST_ROOTS_B64)))
  }
  const target = authority(profile)
  for (const [key, value] of Object.entries(target)) assert.equal(facts.target[key], value, key)
  assert.equal(facts.frontend.profile, 'full')
  assert.equal(facts.generated.componentImportsProfile, 'full')
  const goScript = fs.readFileSync('scripts/openwrt-stage-build.sh', 'utf8')
  for (const [key, variable] of [['archiveFilename', 'GO_TOOLCHAIN_FILENAME'], ['archiveSha256', 'GO_TOOLCHAIN_SHA256']]) {
    assert.equal(facts.go.toolchain[key], new RegExp(`readonly ${variable}='([^']+)'`).exec(goScript)[1])
  }
  assert.equal(facts.cronet.commit, /readonly CRONET_TOOLCHAIN_COMMIT='([^']+)'/.exec(goScript)[1])
  const stage = json(path.join(proofDir, 'STAGE_MANIFEST.json'))
  assert.equal(stage.schema, stageManifestSchema)
  assert.equal(stage.sourceFingerprint, fingerprint.sourceFingerprint)
  assert.equal(stage.buildIdentity, fingerprint.build.identity)
  assert.deepEqual(stage.target, facts.target)
  assert.equal(stage.payload.treeDigest, sha256(canonical(stage.payload.entries)))
  assert.equal(stage.stageIdentity, sha256(canonical({ schema: stage.schema,
    sourceFingerprint: stage.sourceFingerprint, generatedOutputDigest: stage.generatedOutputDigest,
    buildIdentity: stage.buildIdentity, buildInfoSha256: stage.buildInfoSha256,
    payloadTreeDigest: stage.payload.treeDigest, unixTextAssetDigest: stage.unixTextAssets.digest, target: stage.target })))
  const host = json(path.join(proofDir, 'PACKAGE_HOST_TOOL_AUTHORITY.json'))
  const { packageHostToolIdentity, ...hostMaterial } = host
  assert.equal(host.schema, packageHostToolProofSchema)
  assert.equal(packageHostToolIdentity, sha256(canonical(hostMaterial)))
  assert.deepEqual(host.environment, packageExecutionEnvironment)
  assert.equal(host.node.executableSha256, facts.frontend.nodeExecutableSha256)
  assert.equal(host.node.version, facts.frontend.node)
  const metadata = createApkMetadataProof(fs.readFileSync(path.join(proofDir, 'APK_ADBDUMP.txt'), 'utf8'), {
    version, release, packageArchitecture: target.packageArchitecture, sourceFingerprint: stage.sourceFingerprint,
    stageIdentity: stage.stageIdentity, packageHostToolIdentity: host.packageHostToolIdentity, apkSha256: hash,
  })
  assert.deepEqual(metadata, json(path.join(proofDir, 'APK_METADATA_PROOF.json')))
  const rootfs = json(path.join(proofDir, 'APK_ROOTFS_PROOF.json'))
  assert.equal(rootfs.schema, apkRootfsProofSchema)
  const { proofIdentity, ...material } = rootfs
  assert.equal(proofIdentity, sha256(canonical(material)))
  assert.equal(rootfs.apkSha256, hash)
  assert.equal(rootfs.stageIdentity, stage.stageIdentity)
  assert.equal(rootfs.stageTreeDigest, stage.payload.treeDigest)
  const comparable = stage.payload.entries.map(({ path, type, mode, size, sha256 }) =>
    type === 'file' ? { path, type, mode, size, sha256 } : { path, type, mode })
  assert.equal(rootfs.rootfsIdentity, sha256(canonical(comparable)))
  for (const [name, phase] of [['SDK_EXTRACTED_AUTHORITY.json', 'extracted'], ['SDK_DERIVATION.json', 'prepared-for-package']]) {
    const sdk = json(path.join(proofDir, name))
    assert.equal(sdk.archiveFilename, target.sdkFilename)
    assert.equal(sdk.archiveSha256, target.sdkSha256)
    assert.equal(sdk.phase, phase)
    assert.equal(sdk.tree.treeDigest, sha256(canonical(sdk.tree.records)))
    const { sdkIdentity, ...material } = sdk
    assert.equal(sdkIdentity, sha256(canonical(material)))
  }
  return { profile, productCommit, sourceFingerprint: fingerprint.sourceFingerprint, sha256: hash,
    metadata: metadata.metadata, stageIdentity: stage.stageIdentity, rootfsIdentity: rootfs.rootfsIdentity }
}

if (mode === 'prepare') {
  assert.ok(profiles.includes(selected))
  const artifacts = path.join(directory, 'artifacts')
  const apks = fs.readdirSync(artifacts).filter(name => name.endsWith('.apk'))
  assert.equal(apks.length, 1)
  const stageDir = path.join(directory, 'canonical-stage')
  verifyStageManifest(json(path.join(stageDir, 'STAGE_MANIFEST.json')), stageDir, process.cwd())
  const proofDir = path.join(destination, 'proofs', selected)
  fs.mkdirSync(proofDir, { recursive: true })
  for (const name of ['SOURCE_FINGERPRINT.json', 'STAGE_MANIFEST.json', 'PACKAGE_HOST_TOOL_AUTHORITY.json',
    'APK_METADATA_PROOF.json', 'APK_ROOTFS_PROOF.json', 'SDK_EXTRACTED_AUTHORITY.json', 'SDK_DERIVATION.json']) {
    fs.copyFileSync(path.join(artifacts, name), path.join(proofDir, name))
  }
  fs.copyFileSync(path.join(artifacts, `${apks[0]}.info.txt`), path.join(proofDir, 'APK_ADBDUMP.txt'))
  const result = verify(path.join(artifacts, apks[0]), proofDir, selected)
  const name = `solovey-ui-openwrt-${selected}.apk`
  fs.mkdirSync(path.join(destination, 'assets'), { recursive: true })
  fs.copyFileSync(path.join(artifacts, apks[0]), path.join(destination, 'assets', name))
  fs.writeFileSync(path.join(destination, 'assets', `${name}.sha256`), `${result.sha256}  ${name}\n`)
  console.log(JSON.stringify(result))
} else if (mode === 'verify') {
  const assets = path.join(directory, 'assets')
  const expected = []
  for (const profile of profiles) {
    const name = `solovey-ui-openwrt-${profile}.apk`
    const result = verify(path.join(assets, name), path.join(directory, 'proofs', profile), profile)
    assert.equal(fs.readFileSync(path.join(assets, `${name}.sha256`), 'utf8'), `${result.sha256}  ${name}\n`)
    expected.push(name, `${name}.sha256`)
    console.log(JSON.stringify(result))
  }
  const name = 'solovey-ui-friendlywrt-storage.json'
  const bytes = fs.readFileSync('deploy/friendlywrt/deployment-storage.json')
  assert.deepEqual(fs.readFileSync(path.join(assets, name)), bytes)
  assert.equal(fs.readFileSync(path.join(assets, `${name}.sha256`), 'utf8'), `${sha256(bytes)}  ${name}\n`)
  expected.push(name, `${name}.sha256`)
  assert.deepEqual(fs.readdirSync(assets).sort(), expected.sort())
  console.log('OpenWrt complete candidate / FriendlyWrt exact source byte binding: PASS')
} else throw new Error('expected prepare or verify')
