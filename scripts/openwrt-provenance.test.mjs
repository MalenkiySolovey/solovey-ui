import assert from 'node:assert/strict'
import crypto from 'node:crypto'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { verifyApkRootfs } from './openwrt-apk-rootfs.mjs'
import { createApkMetadataProof } from './openwrt-apk-metadata.mjs'
import {
  createSourceFingerprint,
  enumerateSourcePaths,
  verifySourceFingerprint,
} from './openwrt-package-source-fingerprint.mjs'
import {
  createStageManifest,
  executablePaths,
  verifyStageManifest,
} from './openwrt-stage-manifest.mjs'
import {
  createSDKTreeProof,
  pinArchive as pinSDKArchive,
  validateArchiveEntries,
  verifyExtractedSDK,
  verifySDKTreeProof,
} from './openwrt-sdk-archive.mjs'
import {
  createDependencyProof,
  createToolchainProof,
  pinArchive as pinGoArchive,
  verifyDependencyProof,
  verifyToolchainProof,
} from './openwrt-go-authority.mjs'
import {
  createPackageHostToolProof,
  verifyPackageHostToolProof,
} from './openwrt-package-host-tools.mjs'

const scriptRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const openWrtRuntimeDependencies = Object.freeze(['dropbear', 'libc', 'logd', 'nftables', 'procd', 'ubus', 'uci'])

const roots = []
test.after(() => {
  for (const root of roots) {
    makeWritableRecursively(root)
    fs.rmSync(root, { recursive: true, force: true })
  }
})

test('OpenWrt target profiles resolve coordinates and reject cross-target assumptions', () => {
  const profileScript = path.join(scriptRoot, 'scripts/openwrt-target-profile.sh')
  const resolve = profile => {
    const command = [
      `source ${shellQuote(profileScript)}`,
      `openwrt_target_profile_load ${shellQuote(profile)}`,
      'printf "%s\\n" "$OPENWRT_PROFILE_KEY|$OPENWRT_TARGET|$OPENWRT_SUBTARGET|$OPENWRT_PACKAGE_ARCH|$OPENWRT_GOARCH|$OPENWRT_CC_TARGET|$OPENWRT_SDK_FILENAME|$OPENWRT_CONFIG_TARGET|$OPENWRT_CONFIG_SUBTARGET|$OPENWRT_CONFIG_ARCH"',
    ].join('; ')
    const result = spawnSync('bash', ['-c', command], { encoding: 'utf8' })
    assert.equal(result.status, 0, result.stderr)
    return result.stdout.trim()
  }

  assert.equal(
    resolve('x86-64'),
    'x86-64|x86|64|x86_64|amd64|x86_64-openwrt-linux-musl|openwrt-sdk-25.12.5-x86-64_gcc-14.3.0_musl.Linux-x86_64.tar.zst|CONFIG_TARGET_x86=y|CONFIG_TARGET_x86_64=y|CONFIG_TARGET_ARCH_PACKAGES="x86_64"',
  )
  assert.equal(
    resolve('rockchip-armv8'),
    'rockchip-armv8|rockchip|armv8|aarch64_generic|arm64|aarch64-openwrt-linux-musl|openwrt-sdk-25.12.5-rockchip-armv8_gcc-14.3.0_musl.Linux-x86_64.tar.zst|CONFIG_TARGET_rockchip=y|CONFIG_TARGET_rockchip_armv8=y|CONFIG_TARGET_ARCH_PACKAGES="aarch64_generic"',
  )
  assert.notEqual(resolve('x86-64').split('|')[3], resolve('rockchip-armv8').split('|')[3])
  const mismatch = spawnSync('bash', ['-c', `source ${shellQuote(profileScript)}; openwrt_target_profile_load unsupported`], { encoding: 'utf8' })
  assert.notEqual(mismatch.status, 0)
})

test('source identity is build-owned, includes owned untracked input, and excludes cache/history', () => {
  const source = makeSource()
  const before = createSourceFingerprint(source, buildFacts())

  write(source, 'api/untracked_product.go', 'package api\nconst provenanceFixture = true\n')
  write(source, 'util/password/embedded-dictionary.txt', 'target bytes\n')
  assert.ok(enumerateSourcePaths(source).includes('api/untracked_product.go'))
  assert.ok(enumerateSourcePaths(source).includes('util/password/embedded-dictionary.txt'))
  const withOwnedInput = createSourceFingerprint(source, buildFacts())
  assert.notEqual(withOwnedInput.sourceFingerprint, before.sourceFingerprint)

  write(source, 'frontend/node_modules/cache/history.bin', 'irrelevant cache')
  write(source, 'frontend/build-history.log', 'process history')
  write(source, 'components/widget/implementation-notes.md', 'process history')
  const withExcludedHistory = createSourceFingerprint(source, buildFacts())
  assert.equal(withExcludedHistory.sourceFingerprint, withOwnedInput.sourceFingerprint)
})

test('package recipe and generator changes alter source identity', () => {
  const source = makeSource()
  const before = createSourceFingerprint(source, buildFacts())
  append(source, 'deploy/openwrt/package/solovey-ui/Makefile', '\n# target recipe change\n')
  const recipeChanged = createSourceFingerprint(source, buildFacts())
  assert.notEqual(recipeChanged.sourceFingerprint, before.sourceFingerprint)
  append(source, 'scripts/generate-component-imports.mjs', '\n// generator change\n')
  const generatorChanged = createSourceFingerprint(source, buildFacts())
  assert.notEqual(generatorChanged.sourceFingerprint, recipeChanged.sourceFingerprint)
  append(source, 'scripts/frontend-runtime-closure.mjs', '\n// closure verifier change\n')
  const closureVerifierChanged = createSourceFingerprint(source, buildFacts())
  assert.notEqual(closureVerifierChanged.sourceFingerprint, generatorChanged.sourceFingerprint)
})

test('changed target-affecting flag changes build and source identity', () => {
  const source = makeSource()
  const first = createSourceFingerprint(source, buildFacts())
  const changed = buildFacts()
  changed.go.tags.push('changed_target_flag')
  const second = createSourceFingerprint(source, changed)
  assert.notEqual(second.build.identity, first.build.identity)
  assert.notEqual(second.sourceFingerprint, first.sourceFingerprint)
})

test('stale generated Go and stale frontend outputs fail verification', () => {
  const source = makeSource()
  const fingerprint = createSourceFingerprint(source, buildFacts())
  append(source, 'app/components_generated.go', '\n// stale\n')
  assert.throws(() => verifySourceFingerprint(fingerprint, source, { verifyGenerated: true }), /generated output content mismatch/)
  write(source, 'app/components_generated.go', generatedGo())
  append(source, 'web/html/index.html', 'stale frontend')
  assert.throws(() => verifySourceFingerprint(fingerprint, source, { verifyGenerated: true }), /generated output content mismatch/)
})

test('changed staged binary is rejected', () => {
  const { source, stage, manifest } = makeStage()
  appendAbsolute(stagePath(stage, executablePaths[0]), 'tamper')
  assert.throws(() => verifyStageManifest(manifest, stage, source), /stage payload record mismatch/)
})

test('changed component asset is rejected', () => {
  const { source, stage, manifest } = makeStage()
  appendAbsolute(stagePath(stage, 'usr/lib/solovey-ui/components/widget/frontend/assets/widget.js'), 'tamper')
  assert.throws(() => verifyStageManifest(manifest, stage, source), /stage payload record mismatch/)
})

test('extra and missing payload are rejected', () => {
  const extra = makeStage()
  write(extra.stage, 'payload/usr/lib/solovey-ui/extra', 'unexpected')
  assert.throws(() => verifyStageManifest(extra.manifest, extra.stage, extra.source), /path\/type set mismatch/)

  const missing = makeStage()
  fs.rmSync(stagePath(missing.stage, 'usr/lib/solovey-ui/components/widget/frontend/assets/widget.js'))
  assert.throws(() => verifyStageManifest(missing.manifest, missing.stage, missing.source), /path\/type set mismatch/)
})

test('wrong executable mode and ELF architecture are rejected', () => {
  const modeCase = makeStage()
  fs.chmodSync(stagePath(modeCase.stage, executablePaths[0]), 0o644)
  assert.throws(() => verifyStageManifest(modeCase.manifest, modeCase.stage, modeCase.source), /wrong file mode/)

  const architectureCase = makeStage()
  const binary = stagePath(architectureCase.stage, executablePaths[0])
  const bytes = fs.readFileSync(binary)
  bytes.writeUInt16LE(183, 18)
  fs.writeFileSync(binary, bytes)
  fs.chmodSync(binary, 0o755)
  assert.throws(() => verifyStageManifest(architectureCase.manifest, architectureCase.stage, architectureCase.source), /wrong ELF architecture/)
})

test('unexpected symlink is rejected', () => {
  const fixture = makeStage()
  fs.symlinkSync('BUILD_INFO.txt', stagePath(fixture.stage, 'usr/lib/solovey-ui/unexpected-link'))
  assert.throws(() => verifyStageManifest(fixture.manifest, fixture.stage, fixture.source), /unexpected symlink/)
})

test('complete directory tree additions, removals, modes, and type substitutions are rejected', () => {
  for (const mode of [0o755, 0o777]) {
    const fixture = makeStage()
    const extra = stagePath(fixture.stage, `usr/lib/solovey-ui/extra-${mode.toString(8)}`)
    fs.mkdirSync(extra, { mode })
    fs.chmodSync(extra, mode)
    assert.throws(() => verifyStageManifest(fixture.manifest, fixture.stage, fixture.source), /path\/type set mismatch/)
  }

  const missing = makeStage()
  fs.rmSync(stagePath(missing.stage, 'usr/share/licenses/solovey-ui'), { recursive: true })
  assert.throws(() => verifyStageManifest(missing.manifest, missing.stage, missing.source), /path\/type set mismatch/)

  const wrongMode = makeStage()
  fs.chmodSync(stagePath(wrongMode.stage, 'usr/lib/solovey-ui/components'), 0o777)
  assert.throws(() => verifyStageManifest(wrongMode.manifest, wrongMode.stage, wrongMode.source), /wrong directory mode/)

  const fileToDirectory = makeStage()
  const asset = stagePath(fileToDirectory.stage, 'usr/lib/solovey-ui/components/widget/frontend/assets/widget.js')
  fs.rmSync(asset)
  fs.mkdirSync(asset, { mode: 0o755 })
  assert.throws(() => verifyStageManifest(fileToDirectory.manifest, fileToDirectory.stage, fileToDirectory.source), /path\/type set mismatch/)

  const directoryToFile = makeStage()
  const assets = stagePath(directoryToFile.stage, 'usr/lib/solovey-ui/components/widget/frontend/assets')
  fs.rmSync(assets, { recursive: true })
  fs.writeFileSync(assets, 'wrong type')
  fs.chmodSync(assets, 0o644)
  assert.throws(() => verifyStageManifest(directoryToFile.manifest, directoryToFile.stage, directoryToFile.source), /path\/type set mismatch/)
})

test('unsupported special payload type is rejected where mkfifo is available', t => {
  if (process.platform !== 'linux') return t.skip('mkfifo fixture requires Linux')
  const fixture = makeStage()
  const fifo = stagePath(fixture.stage, 'usr/lib/solovey-ui/unexpected-fifo')
  const result = spawnSync('mkfifo', [fifo], { encoding: 'utf8' })
  if (result.error?.code === 'ENOENT') return t.skip('mkfifo is unavailable')
  assert.equal(result.status, 0, result.stderr)
  assert.throws(() => verifyStageManifest(fixture.manifest, fixture.stage, fixture.source), /unsupported type/)
})

test('SDK archive validation rejects traversal and extracted symlink escape', () => {
  assert.equal(validateArchiveEntries(['sdk', 'sdk/include', 'sdk/include/package.mk'], 'sdk'), true)
  assert.throws(() => validateArchiveEntries(['sdk', 'sdk/../escape'], 'sdk'), /escapes|canonical/)
  assert.throws(() => validateArchiveEntries(['/sdk/package.mk'], 'sdk'), /unsafe|escapes/)

  const extraction = temporaryDirectory('solovey-sdk-extraction-')
  for (const file of ['include/package.mk', 'include/package-pack.mk', 'include/version.mk', 'scripts/feeds']) {
    write(extraction, `sdk/${file}`, 'fixture\n')
  }
  fs.mkdirSync(path.join(extraction, 'sdk', 'staging_dir', 'host', 'bin'), { recursive: true })
  fs.symlinkSync('/usr/bin/gawk', path.join(extraction, 'sdk', 'staging_dir', 'host', 'bin', 'awk'))
  const proof = verifyExtractedSDK(extraction, 'sdk', 'sdk.tar.zst', '0'.repeat(64))
  assert.match(proof.sdkIdentity, /^[0-9a-f]{64}$/)
  const injected = path.join(extraction, 'sdk', 'staging_dir', 'host', 'bin', 'injected')
  fs.symlinkSync('/unrecognized/absolute', injected)
  assert.throws(() => verifyExtractedSDK(extraction, 'sdk', 'sdk.tar.zst', '0'.repeat(64)), /unrecognized absolute target/)
  fs.rmSync(injected)
  fs.symlinkSync('../../outside', path.join(extraction, 'sdk', 'escape'))
  assert.throws(() => verifyExtractedSDK(extraction, 'sdk', 'sdk.tar.zst', '0'.repeat(64)), /symlink escapes/)
})

test('SDK authority binds every file byte, mode, path, type and symlink target', t => {
  const baseline = makeSDKTree()
  const proof = createSDKTreeProof(baseline, sdkMetadata())
  assert.equal(verifySDKTreeProof(proof, baseline), true)

  for (const mutation of [
    root => fs.writeFileSync(path.join(root, 'include/package.mk'), 'different bytes with same entry count\n'),
    root => fs.chmodSync(path.join(root, 'include/package.mk'), 0o600),
    root => write(root, 'unexpected/file', 'extra\n'),
    root => fs.rmSync(path.join(root, 'include/version.mk')),
    root => { const file = path.join(root, 'scripts/feeds'); fs.rmSync(file); fs.mkdirSync(file) },
    root => { const link = path.join(root, 'relative-link'); fs.rmSync(link); fs.symlinkSync('include/version.mk', link) },
  ]) {
    const candidate = cloneAuthorityRoot(baseline, 'sdk')
    mutation(candidate)
    assert.throws(() => verifySDKTreeProof(proof, candidate), /SDK tree proof differs|unsupported|unrecognized|invalid/)
  }

  if (process.platform !== 'linux') return t.skip('special-file SDK mutation requires Linux')
  const special = cloneAuthorityRoot(baseline, 'sdk')
  const fifo = path.join(special, 'unexpected-fifo')
  const result = spawnSync('mkfifo', [fifo], { encoding: 'utf8' })
  if (result.error?.code === 'ENOENT') return t.skip('mkfifo is unavailable')
  assert.equal(result.status, 0, result.stderr)
  assert.throws(() => verifySDKTreeProof(proof, special), /unsupported entry type/)
})

test('prepared SDK identity encodes private-root symlinks portably', () => {
  const first = makeSDKTree()
  const second = makeSDKTree()
  for (const root of [first, second]) {
    const link = path.join(root, 'staging_dir', 'host', 'bin', 'ldconfig')
    fs.mkdirSync(path.dirname(link), { recursive: true })
    fs.symlinkSync(path.join(root, 'scripts', 'feeds'), link)
  }
  const metadata = { ...sdkMetadata(), phase: 'prepared-for-package' }
  const proofA = createSDKTreeProof(first, metadata)
  const proofB = createSDKTreeProof(second, metadata)
  assert.equal(proofA.sdkIdentity, proofB.sdkIdentity)
  const linkRecord = proofA.tree.records.find(record => record.path === 'staging_dir/host/bin/ldconfig')
  assert.equal(linkRecord.target, '<sdk-root>/scripts/feeds')
  assert.equal(verifySDKTreeProof(proofA, second), true)
})

test('package host-tool identity binds the exact canonical Make bytes', t => {
  if (process.platform !== 'linux') return t.skip('GNU Make executable identity fixture requires Linux')
  const root = temporaryDirectory('solovey-package-tool-')
  const makeProgram = path.join(root, 'make')
  fs.copyFileSync('/usr/bin/make', makeProgram)
  fs.chmodSync(makeProgram, 0o755)
  const proofA = createPackageHostToolProof(makeProgram, process.execPath)
  assert.equal(verifyPackageHostToolProof(proofA, makeProgram, process.execPath), true)
  fs.appendFileSync(makeProgram, '\nchanged package-tool bytes\n')
  assert.throws(
    () => verifyPackageHostToolProof(proofA, makeProgram, process.execPath),
    /differs from the executable bytes/,
  )
  const proofB = createPackageHostToolProof(makeProgram, process.execPath)
  assert.notEqual(proofB.make.executableSha256, proofA.make.executableSha256)
  assert.notEqual(proofB.packageHostToolIdentity, proofA.packageHostToolIdentity)
})

test('verified SDK package boundary ignores caller Make, PATH, flags, and post-proof mutation wrapper', t => {
  if (process.platform !== 'linux') return t.skip('canonical package process boundary requires Linux')
  const root = temporaryDirectory('solovey-package-boundary-')
  const sdk = path.join(root, 'sdk')
  const output = path.join(root, 'output')
  const attack = path.join(root, 'attack')
  fs.mkdirSync(output, { recursive: true })
  fs.mkdirSync(attack, { recursive: true })
  write(sdk, 'include/package.mk', 'fixture\n')
  write(sdk, 'include/package-pack.mk', 'fixture\n')
  write(sdk, 'include/version.mk', 'fixture\n')
  write(sdk, 'scripts/feeds', '#!/bin/sh\n')
  fs.chmodSync(path.join(sdk, 'scripts/feeds'), 0o755)
  write(sdk, 'package/solovey-ui/Makefile', 'PKG_MAINTAINER:=MalenkiySolovey\n')
  write(sdk, 'Makefile', [
    '.PHONY: package/solovey-ui/compile recursive',
    'package/solovey-ui/compile:',
    '\t@$(MAKE) --no-print-directory OUTPUT_DIR="$(OUTPUT_DIR)" recursive',
    'recursive:',
    '\t@printf "%s\\n" "$(MAKE)" > "$(OUTPUT_DIR)/recursive-make.txt"',
    '\t@printf "%s\\n" "$${MAKE-}|$${MAKEFLAGS-}|$${GNUMAKEFLAGS-}|$${MFLAGS-}|$${BASH_ENV-}|$${ENV-}|$${CDPATH-}|$${SHELL-}" > "$(OUTPUT_DIR)/environment.txt"',
    '',
  ].join('\n'))
  const wrapperMarker = path.join(output, 'wrapper-executed')
  const mutationMarker = path.join(output, 'wrapper-mutated')
  const wrapper = path.join(attack, 'make')
  fs.writeFileSync(wrapper, [
    '#!/bin/sh',
    `printf executed > ${shellQuote(wrapperMarker)}`,
    `sed -i 's/MalenkiySolovey/AuditMutation/' ${shellQuote(path.join(sdk, 'package/solovey-ui/Makefile'))}`,
    `printf mutated > ${shellQuote(mutationMarker)}`,
    'exec /usr/bin/make "$@"',
    '',
  ].join('\n'))
  fs.chmodSync(wrapper, 0o755)
  const bashEnvMarker = path.join(output, 'bash-env-executed')
  const bashEnv = path.join(attack, 'bash-env.sh')
  fs.writeFileSync(bashEnv, `printf executed > ${shellQuote(bashEnvMarker)}\n`)
  const proof = createSDKTreeProof(sdk, { ...sdkMetadata(), phase: 'prepared-for-package' })
  assert.equal(verifySDKTreeProof(proof, sdk), true)

  const helper = path.join(scriptRoot, 'scripts/openwrt-package-process.sh')
  const script = [
    'set -euo pipefail',
    `source ${shellQuote(helper)}`,
    `export PATH=${shellQuote(`${attack}:/usr/bin:/bin`)}`,
    `export MAKE=${shellQuote(wrapper)}`,
    "export MAKEFLAGS='caller-makeflags'",
    "export GNUMAKEFLAGS='caller-gnumakeflags'",
    "export MFLAGS='caller-mflags'",
    `export BASH_ENV=${shellQuote(bashEnv)}`,
    "export ENV='caller-env'",
    "export CDPATH='caller-cdpath'",
    `openwrt_package_make /usr/bin/make /usr/bin:/bin ${shellQuote(path.join(root, 'home'))} ${shellQuote(path.join(root, 'xdg-config'))} ${shellQuote(path.join(root, 'xdg-cache'))} ${shellQuote(path.join(root, 'tmp'))} -C ${shellQuote(sdk)} --no-print-directory OUTPUT_DIR=${shellQuote(output)} package/solovey-ui/compile`,
  ].join('\n')
  for (const directory of ['home', 'xdg-config', 'xdg-cache', 'tmp']) fs.mkdirSync(path.join(root, directory))
  const result = spawnSync('/usr/bin/bash', ['-c', script], { encoding: 'utf8', timeout: 30_000 })
  assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`)
  assert.equal(fs.existsSync(wrapperMarker), false)
  assert.equal(fs.existsSync(mutationMarker), false)
  assert.equal(fs.existsSync(bashEnvMarker), false)
  assert.equal(fs.readFileSync(path.join(sdk, 'package/solovey-ui/Makefile'), 'utf8'), 'PKG_MAINTAINER:=MalenkiySolovey\n')
  assert.equal(fs.readFileSync(path.join(output, 'recursive-make.txt'), 'utf8').trim(), '/usr/bin/make')
  assert.doesNotMatch(fs.readFileSync(path.join(output, 'environment.txt'), 'utf8'), /caller-|\/attack\//)
  assert.equal(verifySDKTreeProof(proof, sdk), true)
})

test('production archive pinning never reopens a replaced caller path', () => {
  for (const pin of [pinSDKArchive, pinGoArchive]) {
    const root = temporaryDirectory('solovey-private-archive-')
    const caller = path.join(root, 'caller.archive')
    const privateArchive = path.join(root, 'private.archive')
    const original = Buffer.from('authenticated archive bytes\n')
    fs.writeFileSync(caller, original)
    const digest = sha256Bytes(original)
    assert.equal(pin(caller, privateArchive, digest), digest)
    fs.rmSync(caller)
    fs.writeFileSync(caller, 'replacement after private pin\n')
    assert.deepEqual(fs.readFileSync(privateArchive), original)
    assert.throws(() => pin(caller, path.join(root, 'wrong.archive'), digest), /checksum mismatch/)
  }
})

test('Go toolchain authority rejects byte, mode, path, type and symlink mutation', () => {
  const baseline = makeGoToolchain()
  const proof = createToolchainProof(baseline, goToolchainMetadata())
  assert.equal(verifyToolchainProof(proof, baseline), true)
  for (const mutation of [
    root => fs.writeFileSync(path.join(root, 'src/runtime/runtime.go'), 'changed bytes\n'),
    root => fs.chmodSync(path.join(root, 'bin/go'), 0o700),
    root => write(root, 'src/extra.go', 'package src\n'),
    root => fs.rmSync(path.join(root, 'bin/gofmt')),
    root => { const file = path.join(root, 'VERSION'); fs.rmSync(file); fs.mkdirSync(file) },
    root => { const link = path.join(root, 'src-link'); fs.rmSync(link); fs.symlinkSync('src/runtime', link) },
  ]) {
    const candidate = cloneAuthorityRoot(baseline, 'go')
    mutation(candidate)
    assert.throws(() => verifyToolchainProof(proof, candidate), /differs|omits|required|unsupported/)
  }
})

test('two fresh authenticated module graphs have one identity and altered sums fail', () => {
  const first = makeDependencyAuthority()
  const second = makeDependencyAuthority()
  const proofA = createDependencyProof(first.modules, first.goMod, first.goSum)
  const proofB = createDependencyProof(second.modules, second.goMod, second.goSum)
  assert.equal(proofA.dependencyIdentity, proofB.dependencyIdentity)
  assert.equal(verifyDependencyProof(proofA, second.modules, second.goMod, second.goSum), true)
  const tampered = structuredClone(second.modules)
  tampered[1].Sum = authenticatedSum(9)
  assert.throws(() => verifyDependencyProof(proofA, tampered, second.goMod, second.goSum), /differs/)
})

test('tampered cached Go module source fails the production go mod verify boundary without changing declared identity', t => {
  if (process.platform !== 'linux') return t.skip('Go module-cache verification gate requires Linux')
  const goProgram = process.env.SUI_TEST_GO_PROGRAM ?? 'go'
  const root = temporaryDirectory('solovey-go-mod-verify-')
  const goMod = path.join(root, 'go.mod')
  const goSum = path.join(root, 'go.sum')
  fs.writeFileSync(goMod, 'module github.com/MalenkiySolovey/solovey-ui\n\ngo 1.26.6\n\nrequire github.com/google/uuid v1.6.0\n')
  fs.writeFileSync(goSum, [
    'github.com/google/uuid v1.6.0 h1:NIvaJDMOsjHA8n1jAhLSgzrAzy1Hgr+hNrb57e+94F0=',
    'github.com/google/uuid v1.6.0/go.mod h1:TIyPZe4MgqvfeYDBFedMoGGpEw/LqOeaOT+nhxU+yHo=',
    '',
  ].join('\n'))
  const environment = {
    ...process.env,
    GOENV: 'off', GOWORK: 'off', GOTOOLCHAIN: 'local', GOFLAGS: '-mod=readonly',
    GOPROXY: 'https://proxy.golang.org', GOSUMDB: 'sum.golang.org', GOPRIVATE: '', GONOSUMDB: '', GONOPROXY: '', GOVCS: '*:off',
    GOMODCACHE: path.join(root, 'mod-cache'), GOCACHE: path.join(root, 'build-cache'), GOPATH: path.join(root, 'gopath'),
  }
  const runGo = args => spawnSync(goProgram, args, { cwd: root, env: environment, encoding: 'utf8', timeout: 120_000 })
  const download = runGo(['mod', 'download', 'all'])
  assert.equal(download.status, 0, download.stderr)
  assert.equal(runGo(['mod', 'verify']).status, 0)
  const graph = runGo(['list', '-m', '-json', 'all'])
  assert.equal(graph.status, 0, graph.stderr)
  const proofFile = path.join(root, 'GO_DEPENDENCY_AUTHORITY.json')
  const create = spawnSync(process.execPath, [path.join(scriptRoot, 'scripts/openwrt-go-authority.mjs'), 'create-dependencies',
    '--go-mod', goMod, '--go-sum', goSum, '--out', proofFile], { input: graph.stdout, encoding: 'utf8' })
  assert.equal(create.status, 0, create.stderr)
  const identityBefore = readJSON(proofFile).dependencyIdentity
  const modBefore = sha256Bytes(fs.readFileSync(goMod))
  const sumBefore = sha256Bytes(fs.readFileSync(goSum))
  const moduleDirectory = runGo(['list', '-m', '-f', '{{.Dir}}', 'github.com/google/uuid'])
  assert.equal(moduleDirectory.status, 0, moduleDirectory.stderr)
  const sourceFile = fs.readdirSync(moduleDirectory.stdout.trim()).find(name => name.endsWith('.go'))
  assert.ok(sourceFile)
  const cachedSource = path.join(moduleDirectory.stdout.trim(), sourceFile)
  fs.chmodSync(cachedSource, 0o644)
  fs.appendFileSync(cachedSource, '\n// authenticated-cache-tamper\n')
  const verification = runGo(['mod', 'verify'])
  assert.notEqual(verification.status, 0)
  assert.match(`${verification.stdout}\n${verification.stderr}`, /modified|dir has been modified/i)
  assert.equal(readJSON(proofFile).dependencyIdentity, identityBefore)
  assert.equal(sha256Bytes(fs.readFileSync(goMod)), modBefore)
  assert.equal(sha256Bytes(fs.readFileSync(goSum)), sumBefore)
})

test('package driver rejects an arbitrary SDK directory and a wrong archive digest', t => {
  if (process.platform !== 'linux') return t.skip('package driver contract is exercised on Linux')
  const packageScript = path.join(scriptRoot, 'scripts/openwrt-package-build.sh')
  const arbitrary = spawnSync('bash', [packageScript, '--sdk', '/tmp/arbitrary'], { encoding: 'utf8', timeout: 5_000 })
  assert.notEqual(arbitrary.status, 0)
  assert.match(arbitrary.stderr, /unknown argument: --sdk/)

  const root = temporaryDirectory('solovey-sdk-hash-')
  const archive = path.join(root, 'wrong.tar.zst')
  const goArchive = path.join(root, 'go.tar.gz')
  const trustRoots = path.join(root, 'trust-roots.b64')
  fs.writeFileSync(archive, 'not the official SDK')
  fs.writeFileSync(goArchive, 'not the official Go toolchain')
  fs.writeFileSync(trustRoots, 'dHJ1c3Q=\n')
  const wrongHash = spawnSync('bash', [packageScript,
    '--sdk-archive', archive,
    '--go-toolchain-archive', goArchive,
    '--source', scriptRoot,
    '--trust-roots-file', trustRoots,
    '--out', path.join(root, 'output'),
  ], { encoding: 'utf8', timeout: 10_000 })
  assert.notEqual(wrongHash.status, 0)
  assert.match(`${wrongHash.stdout}\n${wrongHash.stderr}`, /FAILED|checksum|did NOT match|sha256sum/i)
})

test('package driver rejects caller GNU Make control variables before derivation', t => {
  if (process.platform !== 'linux') return t.skip('package driver environment gate requires Linux')
  const packageScript = path.join(scriptRoot, 'scripts/openwrt-package-build.sh')
  for (const name of ['MAKE', 'MAKEFLAGS', 'GNUMAKEFLAGS', 'MFLAGS', 'MAKELEVEL', 'MAKEFILES']) {
    const result = spawnSync('/usr/bin/bash', [packageScript,
      '--sdk-archive', '/missing/sdk.tar.zst',
      '--go-toolchain-archive', '/missing/go.tar.gz',
      '--source', scriptRoot,
      '--trust-roots-file', '/missing/trust-roots.b64',
      '--out', `/tmp/unused-${name}`,
    ], { encoding: 'utf8', env: { ...process.env, [name]: `caller-${name}` }, timeout: 5_000 })
    assert.notEqual(result.status, 0, name)
    assert.match(result.stderr, new RegExp(`forbidden package-build environment override is set: ${name}`))
  }
})

test('canonical stage producer fails closed on ambient selectors and toolchain argument overrides', t => {
  if (process.platform !== 'linux') return t.skip('canonical producer contract is exercised on Linux')
  const stageScript = path.join(scriptRoot, 'scripts/openwrt-stage-build.sh')
  const root = temporaryDirectory('solovey-environment-')
  const trustRoots = path.join(root, 'trust-roots.b64')
  const goArchive = path.join(root, 'go.tar.gz')
  fs.writeFileSync(trustRoots, 'dHJ1c3Q=\n')
  fs.writeFileSync(goArchive, 'fixture archive\n')
  const base = {
    ...process.env,
    GOOS: 'linux',
    GOARCH: 'amd64',
    CGO_ENABLED: '1',
    CGO_LDFLAGS: '-fuse-ld=lld',
    CC: '/usr/bin/true --target=x86_64-openwrt-linux-musl --sysroot=/tmp',
    CXX: '/usr/bin/true --target=x86_64-openwrt-linux-musl --sysroot=/tmp',
  }
  for (const forbidden of ['GOROOT', 'GOMODCACHE', 'GOPATH', 'GOCACHE', 'GOPROXY', 'GOSUMDB', 'GONOSUMDB', 'GOPRIVATE', 'GONOPROXY', 'GOVCS', 'GOAUTH', 'GOINSECURE', 'GOTELEMETRY']) delete base[forbidden]
  for (const [name, value, pattern] of [
    ['SUI_E2E', '1', /forbidden target selector.*SUI_E2E/],
    ['SUI_E2E_WEB_PATH', '/injected/', /forbidden target selector.*SUI_E2E_WEB_PATH/],
    ['SUI_E2E_BACKEND_ORIGIN', 'http://injected', /forbidden target selector.*SUI_E2E_BACKEND_ORIGIN/],
    ['VITE_ENABLE_NEXUS', '1', /forbidden target selector.*VITE_ENABLE_NEXUS/],
    ['GOEXPERIMENT', 'fieldtrack', /forbidden target environment override.*GOEXPERIMENT/],
    ['GOROOT', '/caller/go', /forbidden target environment override.*GOROOT/],
    ['GOMODCACHE', '/caller/mod', /forbidden target environment override.*GOMODCACHE/],
    ['GOPATH', '/caller/gopath', /forbidden target environment override.*GOPATH/],
    ['GOCACHE', '/caller/cache', /forbidden target environment override.*GOCACHE/],
    ['GOPROXY', 'https:\/\/caller.invalid', /forbidden target environment override.*GOPROXY/],
    ['GOSUMDB', 'off', /forbidden target environment override.*GOSUMDB/],
  ]) {
    const output = path.join(root, `output-${name}`)
    const result = spawnSync('bash', [stageScript, '--source', scriptRoot, '--out', output, '--trust-roots-file', trustRoots, '--go-toolchain-archive', goArchive], {
      encoding: 'utf8', env: { ...base, [name]: value }, timeout: 5_000,
    })
    assert.notEqual(result.status, 0, name)
    assert.match(result.stderr, pattern)
    assert.equal(fs.existsSync(output), false)
  }
  for (const [name, value] of [
    ['CC', `${base.CC} -fno-ident`],
    ['CXX', `${base.CXX} -fno-ident`],
  ]) {
    const output = path.join(root, `output-${name}-argument`)
    const result = spawnSync('bash', [stageScript, '--source', scriptRoot, '--out', output, '--trust-roots-file', trustRoots, '--go-toolchain-archive', goArchive], {
      encoding: 'utf8', env: { ...base, [name]: value }, timeout: 5_000,
    })
    assert.notEqual(result.status, 0, name)
    assert.match(result.stderr, new RegExp(`${name} must be the canonical compiler`))
    assert.equal(fs.existsSync(output), false)
  }
})

test('APK rootfs verifier accepts only the stage tree plus narrow apk-tools metadata', () => {
  const valid = makeApkRootfs()
  const proof = verifyApkRootfs(valid.fixture.manifest, valid.rootfs, 'a'.repeat(64))
  assert.equal(proof.fileCount, valid.fixture.manifest.payload.fileCount)
  assert.equal(proof.directoryCount, valid.fixture.manifest.payload.directoryCount)

  const extra = makeApkRootfs()
  write(extra.rootfs, 'usr/lib/solovey-ui/injected', 'extra')
  assert.throws(() => verifyApkRootfs(extra.fixture.manifest, extra.rootfs), /path\/type mismatch/)

  const missing = makeApkRootfs()
  fs.rmSync(path.join(missing.rootfs, 'usr/lib/solovey-ui/BUILD_INFO.txt'))
  assert.throws(() => verifyApkRootfs(missing.fixture.manifest, missing.rootfs), /path\/type mismatch/)

  const bytes = makeApkRootfs()
  fs.appendFileSync(path.join(bytes.rootfs, 'usr/lib/solovey-ui/BUILD_INFO.txt'), 'tamper')
  assert.throws(() => verifyApkRootfs(bytes.fixture.manifest, bytes.rootfs), /differs from the canonical stage/)

  const mode = makeApkRootfs()
  fs.chmodSync(path.join(mode.rootfs, 'usr/lib/solovey-ui/BUILD_INFO.txt'), 0o600)
  assert.throws(() => verifyApkRootfs(mode.fixture.manifest, mode.rootfs), /differs from the canonical stage/)

  const type = makeApkRootfs()
  const buildInfo = path.join(type.rootfs, 'usr/lib/solovey-ui/BUILD_INFO.txt')
  fs.rmSync(buildInfo)
  fs.mkdirSync(buildInfo)
  assert.throws(() => verifyApkRootfs(type.fixture.manifest, type.rootfs), /path\/type mismatch/)
})

test('same rootfs with changed APK Maintainer fails independent metadata provenance', () => {
  const rootfs = makeApkRootfs()
  const rootfsA = verifyApkRootfs(rootfs.fixture.manifest, rootfs.rootfs, 'a'.repeat(64))
  const rootfsB = verifyApkRootfs(rootfs.fixture.manifest, rootfs.rootfs, 'b'.repeat(64))
  assert.equal(rootfsA.rootfsIdentity, rootfsB.rootfsIdentity)
  const context = {
    version: '2026.3.0',
    sourceFingerprint: '1'.repeat(64),
    stageIdentity: '2'.repeat(64),
    packageHostToolIdentity: '3'.repeat(64),
    apkSha256: '4'.repeat(64),
    release: 1,
  }
  const canonicalProof = createApkMetadataProof(apkMetadataDump(), context)
  assert.equal(canonicalProof.metadata.maintainer, 'MalenkiySolovey')
  assert.equal(canonicalProof.metadata.installedSize, 30)
  assert.throws(
    () => createApkMetadataProof(apkMetadataDump('AuditMutation'), { ...context, apkSha256: '5'.repeat(64) }),
    /metadata mismatch: maintainer/,
  )
})

test('APK metadata requires the provider-neutral OpenWrt runtime capability closure', () => {
  const context = {
    version: '2026.3.0',
    sourceFingerprint: '1'.repeat(64),
    stageIdentity: '2'.repeat(64),
    packageHostToolIdentity: '3'.repeat(64),
    apkSha256: '4'.repeat(64),
    release: 1,
  }
  const proof = createApkMetadataProof(apkMetadataDump(), context)
  assert.deepEqual(proof.metadata.dependencies, openWrtRuntimeDependencies)
  for (const providerVariant of ['nftables-json', 'nftables-nojson']) {
    const variantDependencies = openWrtRuntimeDependencies.map(value => value === 'nftables' ? providerVariant : value)
    assert.throws(
      () => createApkMetadataProof(apkMetadataDump('MalenkiySolovey', 'x86_64', variantDependencies), context),
      /metadata mismatch: dependencies/,
    )
  }
})

test('APK metadata proof follows the stage package architecture', () => {
  const context = {
    version: '2026.3.0',
    sourceFingerprint: '1'.repeat(64),
    stageIdentity: '2'.repeat(64),
    packageHostToolIdentity: '3'.repeat(64),
    apkSha256: '4'.repeat(64),
    packageArchitecture: 'aarch64_generic',
    release: 1,
  }
  const proof = createApkMetadataProof(apkMetadataDump('MalenkiySolovey', 'aarch64_generic'), context)
  assert.equal(proof.metadata.architecture, 'aarch64_generic')
  assert.throws(
    () => createApkMetadataProof(apkMetadataDump(), context),
    /metadata mismatch: architecture/,
  )
})

test('source identity A cannot validate a stage created from source B', () => {
  const sourceA = makeSource()
  const sourceB = cloneDirectory(sourceA)
  append(sourceB, 'api/product.go', '\nconst sourceB = true\n')
  const fixtureB = makeStage(sourceB)
  assert.throws(() => verifyStageManifest(fixtureB.manifest, fixtureB.stage, sourceA), /source input content mismatch/)
})

test('stale component metadata is rejected', () => {
  const fixture = makeStage()
  const installedPath = stagePath(fixture.stage, 'usr/lib/solovey-ui/components/installed.json')
  const installed = readJSON(installedPath)
  installed.components[0].id = 'stale-widget'
  fs.writeFileSync(installedPath, `${JSON.stringify(installed, null, 2)}\n`)
  assert.throws(() => verifyStageManifest(fixture.manifest, fixture.stage, fixture.source), /ENOENT|missing|stale component/)
})

test('BUILD_INFO changes stage identity', () => {
  const first = makeStage()
  const secondStage = cloneDirectory(first.stage)
  fs.rmSync(path.join(secondStage, 'STAGE_MANIFEST.json'))
  appendAbsolute(stagePath(secondStage, 'usr/lib/solovey-ui/BUILD_INFO.txt'), 'build_note=changed\n')
  const second = createStageManifest(secondStage, first.source)
  assert.notEqual(second.buildInfoSha256, first.manifest.buildInfoSha256)
  assert.notEqual(second.stageIdentity, first.manifest.stageIdentity)
})

test('absolute source and stage paths do not affect canonical identities', () => {
  const sourceA = makeSource()
  const sourceB = cloneDirectory(sourceA)
  const stageA = makeStage(sourceA)
  const stageB = makeStage(sourceB)
  assert.notEqual(sourceA, sourceB)
  assert.notEqual(stageA.stage, stageB.stage)
  assert.equal(stageA.fingerprint.sourceFingerprint, stageB.fingerprint.sourceFingerprint)
  assert.equal(stageA.manifest.payload.fileDigest, stageB.manifest.payload.fileDigest)
  assert.equal(stageA.manifest.payload.treeDigest, stageB.manifest.payload.treeDigest)
  assert.equal(stageA.manifest.stageIdentity, stageB.manifest.stageIdentity)
})

function makeApkRootfs() {
  const fixture = makeStage()
  const rootfs = temporaryDirectory('solovey-apk-rootfs-')
  fs.rmSync(rootfs, { recursive: true })
  fs.cpSync(path.join(fixture.stage, 'payload'), rootfs, { recursive: true, preserveTimestamps: false })
  fs.mkdirSync(path.join(rootfs, 'lib/apk/packages'), { recursive: true, mode: 0o755 })
  fs.chmodSync(path.join(rootfs, 'lib/apk'), 0o755)
  fs.chmodSync(path.join(rootfs, 'lib/apk/packages'), 0o755)
  const packageFiles = [
    ...fixture.manifest.payload.entries.filter(entry => entry.type === 'file').map(entry => `/${entry.path}`),
    '/lib/apk/packages/solovey-ui.rusers',
  ].sort((left, right) => Buffer.from(left).compare(Buffer.from(right)))
  write(rootfs, 'lib/apk/packages/solovey-ui.list', `${packageFiles.join('\n')}\n`)
  write(rootfs, 'lib/apk/packages/solovey-ui.rusers', 'solovey-ui:solovey-ui\n')
  return { fixture, rootfs }
}

function apkMetadataDump(maintainer = 'MalenkiySolovey', architecture = 'x86_64', dependencies = openWrtRuntimeDependencies) {
  return [
    '#%SCHEMA: 676B6370',
    '# ADB block, size: 1, compat: 0, ver: 0',
    'info:',
    '  name: solovey-ui',
    '  version: 2026.3.0-r1',
    `  hashes: ${'a'.repeat(40)}`,
    '  description: |',
    '    Solovey UI management panel packaged for the OpenWrt 25.12 apk backend. The package owns its fixed procd topology and exact logical sysupgrade preservation integration. Runtime self-replacement remains package-managed.',
    `  arch: ${architecture}`,
    '  license: GPL-3.0-only',
    '  origin: feeds/base/solovey-ui',
    `  maintainer: ${maintainer}`,
    '  url: https://github.com/MalenkiySolovey/solovey-ui',
    '  installed-size: 30',
    `  depends: # ${dependencies.length} items`,
    ...dependencies.map(value => `    - ${value}`),
    '  provides: # 1 items',
    '    - solovey-ui-any',
    'paths: # 1 items',
    '  - name: usr/lib/solovey-ui',
    '    files: # 2 items',
    '      - name: first',
    '        size: 10',
    '      - name: second',
    '        size: 20',
    '',
  ].join('\n')
}

function makeSource() {
  const root = temporaryDirectory('solovey-source-')
  write(root, 'LICENSE', 'fixture license\n')
  write(root, 'go.mod', 'module example.invalid/fixture\n')
  write(root, 'main.go', 'package main\nfunc main() {}\n')
  write(root, 'api/product.go', 'package api\n')
  write(root, 'config/identity/version', '2026.3.0\n')
  write(root, 'deploy/openwrt/package/solovey-ui/Makefile', 'target recipe\n')
  write(root, 'deploy/openwrt/solovey-ui.init', '#!/bin/sh\n')
  write(root, 'deploy/openwrt/solovey-openwrt-prepare', '#!/bin/sh\n')
  write(root, 'frontend/package-lock.json', '{"lockfileVersion":3}\n')
  write(root, 'frontend/src/main.ts', 'export const product = true\n')
  write(root, 'components/widget/component.json', `${JSON.stringify({ id: 'widget', delivery: 'in-process', frontend: { entries: ['frontend/index.ts'] } }, null, 2)}\n`)
  write(root, 'components/widget/frontend/index.ts', 'export default true\n')
  write(root, 'scripts/generate-component-imports.mjs', '// generator\n')
  write(root, 'scripts/frontend-runtime-closure.mjs', '// closure verifier\n')
  write(root, 'scripts/openwrt-stage-build.sh', '#!/bin/sh\n')
  write(root, 'scripts/openwrt-stage-manifest.mjs', '// manifest owner\n')
  write(root, 'scripts/openwrt-package-source-fingerprint.mjs', '// fingerprint owner\n')
  write(root, 'app/components_generated.go', generatedGo())
  write(root, 'cmd/optional_commands_generated.go', generatedGo('cmd'))
  write(root, 'cmd/solovey-privileged-broker/components_generated.go', generatedGo('main'))
  write(root, 'web/html/index.html', '<!doctype html>\n')
  write(root, '.release/components/widget/component.json', fs.readFileSync(path.join(root, 'components/widget/component.json')))
  write(root, '.release/components/widget/frontend/assets.json', `${JSON.stringify({ schemaVersion: 1, component: 'widget', entries: ['frontend/index.ts'], files: ['assets/widget.js'] }, null, 2)}\n`)
  write(root, '.release/components/widget/frontend/assets/widget.js', 'export default true;\n')
  write(root, '.release/components/installed.json', `${JSON.stringify(installedMetadata(), null, 2)}\n`)
  return root
}

function makeStage(source = makeSource()) {
  const facts = buildFacts()
  const fingerprint = createSourceFingerprint(source, facts)
  const stage = temporaryDirectory('solovey-stage-')
  const prefix = 'payload/usr/lib/solovey-ui'
  for (const executable of executablePaths) {
    const file = stagePath(stage, executable)
    fs.mkdirSync(path.dirname(file), { recursive: true })
    fs.writeFileSync(file, fakeELF())
    fs.chmodSync(file, 0o755)
  }
  write(stage, `${prefix}/SOURCE_FINGERPRINT.json`, `${JSON.stringify(fingerprint, null, 2)}\n`)
  write(stage, `${prefix}/BUILD_INFO.txt`, buildInfo(fingerprint))
  write(stage, `${prefix}/GO_TOOLCHAIN_AUTHORITY.json`, `${JSON.stringify(fixtureToolchainProof(facts), null, 2)}\n`)
  write(stage, `${prefix}/GO_DEPENDENCY_AUTHORITY.json`, `${JSON.stringify(fixtureDependencyProof(facts), null, 2)}\n`)
  write(stage, `${prefix}/solovey-openwrt-prepare`, fs.readFileSync(path.join(source, 'deploy/openwrt/solovey-openwrt-prepare')))
  write(stage, `${prefix}/components/installed.json`, `${JSON.stringify(installedMetadata(), null, 2)}\n`)
  write(stage, `${prefix}/components/runtime-closure.json`, '{"schema":"solovey-ui/frontend-runtime-closure/v1"}\n')
  write(stage, `${prefix}/components/widget/component.json`, fs.readFileSync(path.join(source, 'components/widget/component.json')))
  write(stage, `${prefix}/components/widget/frontend/assets.json`, `${JSON.stringify({ schemaVersion: 1, component: 'widget', entries: ['frontend/index.ts'], files: ['assets/widget.js'] }, null, 2)}\n`)
  write(stage, `${prefix}/components/widget/frontend/assets/widget.js`, 'export default true;\n')
  write(stage, 'payload/etc/init.d/solovey-ui', '#!/bin/sh\n')
  write(stage, 'payload/lib/upgrade/solovey-ui.sh', '#!/bin/sh\n')
  write(stage, 'payload/lib/upgrade/keep.d/solovey-ui', '/etc/solovey-ui/db/snapshot\n')
  write(stage, 'payload/usr/share/licenses/solovey-ui/LICENSE', 'fixture license\n')
  fs.chmodSync(stagePath(stage, 'etc/init.d/solovey-ui'), 0o755)
  fs.chmodSync(stagePath(stage, 'lib/upgrade/solovey-ui.sh'), 0o755)
  fs.chmodSync(stagePath(stage, 'usr/lib/solovey-ui/solovey-openwrt-prepare'), 0o755)
  const manifest = createStageManifest(stage, source)
  fs.writeFileSync(path.join(stage, 'STAGE_MANIFEST.json'), `${JSON.stringify(manifest, null, 2)}\n`)
  return { source, stage, facts, fingerprint, manifest }
}

function buildFacts() {
  return {
    target: {
      release: '25.12.5', sourceTag: 'v25.12.5', sourceCommit: 'f'.repeat(40),
      target: 'x86', subtarget: '64', packageArchitecture: 'x86_64',
      sdkFilename: 'openwrt-sdk.tar.zst', sdkSha256: '0'.repeat(64),
    },
    go: {
      version: 'go1.26.6', goos: 'linux', goarch: 'amd64', cgoEnabled: true,
      trimpath: true, buildvcs: false, tags: ['with_musl'],
      panelLdflags: '-w -s', helperLdflags: '-w -s',
      toolchain: {
        archiveFilename: 'go1.26.6.linux-amd64.tar.gz', archiveSha256: '2'.repeat(64),
        treeDigest: '3'.repeat(64), entryCount: 3, identity: '4'.repeat(64),
        authentication: 'official-go-download-sha256-private-copy',
      },
      dependencies: {
        goModSha256: '5'.repeat(64), goSumSha256: '6'.repeat(64), moduleGraphDigest: '7'.repeat(64),
        moduleCount: 2, identity: '8'.repeat(64), authentication: 'fresh-private-gomodcache-go-sumdb',
      },
    },
    cgo: { compiler: 'clang version 19', target: 'x86_64-openwrt-linux-musl' },
    cronet: { commit: 'e'.repeat(40) },
    frontend: { node: 'v25.9.0', npm: '11.6.0', install: 'npm ci', command: 'full build', profile: 'full' },
    generated: { componentImportsProfile: 'full', componentAssetsProfile: 'full' },
    releaseTrustRootsSha256: '1'.repeat(64),
  }
}

function buildInfo(fingerprint) {
  return [
    'app=solovey-ui',
    'version=2026.3.0',
    `commit=${'a'.repeat(40)}`,
    'profile=full',
    'platform=amd64',
    'target=25.12.5-x86-64-x86_64',
    'go=go1.26.6',
    `go_toolchain_identity=${fingerprint.build.facts.go.toolchain.identity}`,
    `go_dependency_identity=${fingerprint.build.facts.go.dependencies.identity}`,
    'sing_box=v1.12.0',
    `source_fingerprint=${fingerprint.sourceFingerprint}`,
    `build_identity=${fingerprint.build.identity}`,
    '',
  ].join('\n')
}

function fixtureToolchainProof(facts) {
  return {
    schema: 'solovey-ui/go-toolchain-authority/v1',
    archiveSha256: facts.go.toolchain.archiveSha256,
    tree: { treeDigest: facts.go.toolchain.treeDigest },
    toolchainIdentity: facts.go.toolchain.identity,
  }
}

function fixtureDependencyProof(facts) {
  return {
    schema: 'solovey-ui/go-dependency-authority/v1',
    source: { goMod: { sha256: facts.go.dependencies.goModSha256 }, goSum: { sha256: facts.go.dependencies.goSumSha256 } },
    modules: { moduleGraphDigest: facts.go.dependencies.moduleGraphDigest },
    dependencyIdentity: facts.go.dependencies.identity,
  }
}

function makeSDKTree() {
  const parent = temporaryDirectory('solovey-sdk-tree-')
  const root = path.join(parent, 'sdk')
  for (const file of ['include/package.mk', 'include/package-pack.mk', 'include/version.mk', 'scripts/feeds']) write(root, file, `${file}\n`)
  fs.chmodSync(path.join(root, 'scripts/feeds'), 0o755)
  fs.symlinkSync('include/package.mk', path.join(root, 'relative-link'))
  return root
}

function sdkMetadata() {
  return { archiveFilename: 'sdk.tar.zst', archiveSha256: 'a'.repeat(64), expectedTopLevel: 'sdk', phase: 'extracted' }
}

function makeGoToolchain() {
  const parent = temporaryDirectory('solovey-go-tree-')
  const root = path.join(parent, 'go')
  write(root, 'bin/go', 'go executable fixture\n')
  write(root, 'bin/gofmt', 'gofmt executable fixture\n')
  write(root, 'VERSION', 'go1.26.6\n')
  write(root, 'src/runtime/runtime.go', 'package runtime\n')
  fs.chmodSync(path.join(root, 'bin/go'), 0o755)
  fs.chmodSync(path.join(root, 'bin/gofmt'), 0o755)
  fs.symlinkSync('bin', path.join(root, 'src-link'))
  return root
}

function goToolchainMetadata() {
  return {
    archiveFilename: 'go1.26.6.linux-amd64.tar.gz', archiveSha256: 'b'.repeat(64),
    version: 'go1.26.6', goos: 'linux', goarch: 'amd64',
  }
}

function makeDependencyAuthority() {
  const root = temporaryDirectory('solovey-go-dependencies-')
  const goMod = path.join(root, 'go.mod')
  const goSum = path.join(root, 'go.sum')
  fs.writeFileSync(goMod, 'module github.com/MalenkiySolovey/solovey-ui\n\ngo 1.26.6\n')
  fs.writeFileSync(goSum, `example.invalid/module v1.0.0 ${authenticatedSum(1)}\nexample.invalid/module v1.0.0/go.mod ${authenticatedSum(2)}\n`)
  return {
    goMod,
    goSum,
    modules: [
      { Path: 'github.com/MalenkiySolovey/solovey-ui', Main: true },
      { Path: 'example.invalid/module', Version: 'v1.0.0', Sum: authenticatedSum(1), GoModSum: authenticatedSum(2) },
    ],
  }
}

function authenticatedSum(fill) {
  return `h1:${Buffer.alloc(32, fill).toString('base64')}`
}

function sha256Bytes(value) {
  return crypto.createHash('sha256').update(value).digest('hex')
}

function installedMetadata() {
  return { version: 1, profile: 'full', binary: 'full', components: [{ id: 'widget', delivery: 'in-process', installed: true }] }
}

function fakeELF() {
  const bytes = Buffer.alloc(64)
  bytes[0] = 0x7f
  bytes.write('ELF', 1, 'ascii')
  bytes[4] = 2
  bytes[5] = 1
  bytes.writeUInt16LE(62, 18)
  return bytes
}

function generatedGo(packageName = 'app') {
  return `// Code generated; DO NOT EDIT.\npackage ${packageName}\n`
}

function temporaryDirectory(prefix) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), prefix))
  roots.push(root)
  return root
}

function cloneDirectory(source) {
  const target = temporaryDirectory('solovey-clone-')
  fs.cpSync(source, target, { recursive: true, preserveTimestamps: false })
  return target
}

function cloneAuthorityRoot(source, name) {
  const parent = temporaryDirectory('solovey-authority-clone-')
  const target = path.join(parent, name)
  fs.cpSync(source, target, { recursive: true, preserveTimestamps: false, verbatimSymlinks: true })
  return target
}

function stagePath(stage, relative) {
  return path.join(stage, 'payload', ...relative.split('/'))
}

function write(root, relative, content) {
  const file = path.join(root, ...relative.split('/'))
  fs.mkdirSync(path.dirname(file), { recursive: true })
  fs.writeFileSync(file, content)
  fs.chmodSync(file, 0o644)
}

function append(root, relative, content) {
  appendAbsolute(path.join(root, ...relative.split('/')), content)
}

function appendAbsolute(file, content) {
  fs.appendFileSync(file, content)
}

function readJSON(file) {
  return JSON.parse(fs.readFileSync(file, 'utf8'))
}

function shellQuote(value) {
  return `'${String(value).replaceAll("'", `'"'"'`)}'`
}

function makeWritableRecursively(root) {
  if (!fs.existsSync(root)) return
  const stat = fs.lstatSync(root)
  if (stat.isSymbolicLink()) return
  if (stat.isDirectory()) {
    try { fs.chmodSync(root, stat.mode | 0o700) } catch {}
    for (const entry of fs.readdirSync(root)) makeWritableRecursively(path.join(root, entry))
  } else {
    try { fs.chmodSync(root, stat.mode | 0o600) } catch {}
  }
}
