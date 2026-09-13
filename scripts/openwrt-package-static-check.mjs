#!/usr/bin/env node

import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const sourceRoot = path.resolve(argumentValue('--source-root') ?? scriptRoot)
const recipe = read('deploy/openwrt/package/solovey-ui/Makefile')
const preparation = read('deploy/openwrt/solovey-openwrt-prepare')
const lifecycle = read('cmd/solovey-openwrt-lifecycle/main_linux.go')
const stageValidator = read('deploy/openwrt/package/solovey-ui/files/validate-stage.sh')
const initScript = read('deploy/openwrt/solovey-ui.init')
const upgradeScript = read('deploy/openwrt/solovey-ui-upgrade.sh')
const keep = read('deploy/openwrt/solovey-ui.keep')
const packageDriver = read('scripts/openwrt-package-build.sh')
const stageProducer = read('scripts/openwrt-stage-build.sh')
const targetProfile = read('scripts/openwrt-target-profile.sh')
const targetBuilder = read('scripts/build-linux-target.sh')
const fingerprint = read('scripts/openwrt-package-source-fingerprint.mjs')
const stageManifest = read('scripts/openwrt-stage-manifest.mjs')
const sdkArchive = read('scripts/openwrt-sdk-archive.mjs')
const goAuthority = read('scripts/openwrt-go-authority.mjs')
const apkRootfs = read('scripts/openwrt-apk-rootfs.mjs')
const apkMetadata = read('scripts/openwrt-apk-metadata.mjs')
const packageHostTools = read('scripts/openwrt-package-host-tools.mjs')
const packageProcess = read('scripts/openwrt-package-process.sh')
const releaseWorkflow = read('.github/workflows/release.yml')
const evidenceOperator = read('cmd/solovey-evidence/main.go')

const executables = [
  ['solovey-ui', 'main.go'],
  ['solovey-privileged-broker', './cmd/solovey-privileged-broker'],
  ['solovey-ssh-proof', './cmd/solovey-ssh-proof'],
  ['solovey-evidence', './cmd/solovey-evidence'],
  ['solovey-broker-readiness', './cmd/solovey-broker-readiness'],
  ['solovey-openwrt-preservation', './cmd/solovey-openwrt-preservation'],
  ['solovey-openwrt-owner-manifest', './components/server-protection/cmd/solovey-openwrt-owner-manifest'],
  ['solovey-openwrt-broker-manifest', './cmd/solovey-openwrt-broker-manifest'],
  ['solovey-openwrt-durability', './cmd/solovey-openwrt-durability'],
  ['solovey-openwrt-lifecycle', './cmd/solovey-openwrt-lifecycle'],
]

for (const [executable, goPackage] of executables) {
  assert.ok(targetBuilder.includes(goPackage), `target builder omits Go entrypoint ${goPackage}`)
  assert.match(stageProducer, new RegExp(`\\b${escapeRegExp(executable)}\\b`), `stage producer omits ${executable}`)
  assert.match(stageManifest, new RegExp(`usr/lib/solovey-ui/${escapeRegExp(executable)}`), `stage contract omits ${executable}`)
}

assert.match(evidenceOperator, /len\(args\) == 1 && args\[0\] == "recent"/)
assert.match(evidenceOperator, /broker\.ReadRecentDiagnostics/)
assert.doesNotMatch(evidenceOperator, /case "(?:shell|exec|write|delete|clear)"/)

assert.match(recipe, /^\s*DEPENDS:=\+procd \+dropbear \+ubus \+uci \+logd \+nftables\s*$/m)
assert.doesNotMatch(recipe, /\+(?:nftables-json|nftables-nojson|firewall4|jansson)\b/)
assert.match(recipe, /USERID:=solovey-ui:solovey-ui/)
assert.match(recipe, /solovey-openwrt-lifecycle reconcile/)
assert.match(recipe, /\$\(SOLOVEY_UI_STAGE_DIR\)\/payload\/\. \$\(1\)\//)
assert.equal((recipe.match(/\$\(SOLOVEY_UI_STAGE_DIR\)\/payload\/\./g) ?? []).length, 1)
assert.doesNotMatch(recipe, /\$\(SOLOVEY_UI_SOURCE_DIR\)\/deploy\/openwrt\/(?:solovey-ui\.init|solovey-ui-upgrade\.sh|solovey-ui\.keep)/)
assert.doesNotMatch(recipe, /Package\/solovey-ui\/conffiles/)
assert.doesNotMatch(recipe, /^\s*(?:apk|opkg|eval)\b/m)

assert.match(stageValidator, /openwrt-stage-manifest\.mjs" verify/)
assert.match(stageValidator, /--source-root "\$source_dir"/)
assert.match(stageValidator, /node_program=\$\{3:\?stage-bound Node program is required\}/)
assert.match(stageValidator, /"\$node_program" "\$source_dir\/scripts\/openwrt-stage-manifest\.mjs" verify/)
assert.match(recipe, /SOLOVEY_UI_NODE_PROGRAM/)
assert.doesNotMatch(stageValidator, /SOURCE_FINGERPRINT\.json.*grep|git -C/)

assert.match(stageProducer, /generate-component-imports\.mjs" --profile full --check/)
assert.match(stageProducer, /openwrt-package-source-fingerprint\.mjs" materialize/)
assert.match(stageProducer, /npm ci/)
assert.match(stageProducer, /SOLOVEY_UI_PROFILE=full npm run build/)
assert.match(stageProducer, /extract-component-frontend\.mjs/)
assert.match(stageProducer, /write-component-installed-metadata\.mjs/)
assert.match(stageProducer, /frontend-runtime-closure\.mjs/)
assert.match(stageProducer, /bash scripts\/build-linux-target\.sh --mode openwrt/)
assert.match(stageProducer, /openwrt-stage-manifest\.mjs" create/)
assert.match(stageProducer, /openwrt-stage-manifest\.mjs" verify/)
assert.match(stageProducer, /--target-profile/)
assert.match(packageDriver, /--target-profile/)
assert.match(packageDriver, /openwrt-target-profile\.sh/)
assert.match(targetProfile, /rockchip-armv8/)
assert.match(targetProfile, /aarch64_generic/)
assert.match(targetProfile, /GOARCH.*arm64/)
assert.match(targetProfile, /aarch64-openwrt-linux-musl/)
assert.doesNotMatch(targetProfile, /NanoPi|R76S|RK3576|FriendlyELEC/)
for (const canonicalGoFact of ['GOENV=off', 'GOTOOLCHAIN=local', 'GOWORK=off', 'GOAMD64=', 'GO111MODULE=on']) {
  assert.match(stageProducer, new RegExp(canonicalGoFact), `stage producer omits ${canonicalGoFact}`)
}
for (const privateRoot of ['GOROOT', 'GOMODCACHE', 'GOCACHE', 'GOPATH']) {
  assert.match(stageProducer, new RegExp(`for variable in[^;]*\\b${privateRoot}\\b`, 's'), `stage producer does not reject ambient ${privateRoot}`)
}
assert.match(stageProducer, /GOPROXY=https:\/\/proxy\.golang\.org'/)
assert.doesNotMatch(stageProducer, /GOPROXY=https:\/\/proxy\.golang\.org,direct/)
assert.match(stageProducer, /run_go mod verify/)
assert.match(stageProducer, /verify-toolchain/)
assert.match(stageProducer, /verify-dependencies/)
assert.match(stageProducer, /SOLOVEY_GO_AUTHORITY_VERIFIED=1/)
assert.match(targetBuilder, /OpenWrt Go executable differs from the private GOROOT authority/)
assert.match(stageProducer, /env -i/)
assert.match(stageProducer, /SUI_E2E\|SUI_E2E_\*\|VITE_\*/)
assert.match(stageProducer, /CC must be the canonical compiler plus exactly target and sysroot arguments/)
assert.match(stageProducer, /CXX must be the canonical compiler plus exactly target and sysroot arguments/)
for (const identity of ['BF_GO_SHA256', 'BF_NODE_SHA256', 'BF_NPM_SHA256', 'BF_CC_SHA256', 'BF_CXX_SHA256', 'BF_LLD_SHA256', 'BF_SYSROOT_SHA256', 'BF_RESOURCE_DIR_SHA256']) {
  assert.match(stageProducer, new RegExp(`\\b${identity}\\b`), `stage producer omits ${identity}`)
}

assert.doesNotMatch(packageDriver, /--stage\)|--stage <|stage_dir=\$\{2:\?--stage/)
assert.doesNotMatch(packageDriver, /--sdk\)|--sdk </)
assert.match(packageDriver, /openwrt-stage-build\.sh/)
assert.match(packageDriver, /openwrt-stage-manifest\.mjs" verify/)
assert.match(packageDriver, /openwrt-sdk-archive\.mjs" validate-list/)
assert.match(packageDriver, /openwrt-sdk-archive\.mjs" verify-extracted/)
assert.match(packageDriver, /openwrt-sdk-archive\.mjs" verify-tree/)
assert.match(packageDriver, /private_sdk_archive/)
assert.match(packageDriver, /expected exactly four OpenWrt defconfig PID scratch files/)
assert.match(packageDriver, /\^\\\.\(files\|overrides\)-\(package\|target\)info-\[0-9\]\+\$/)
assert.doesNotMatch(packageDriver, /tar --zstd -(?:t|x)f "\$sdk_archive"/)
assert.match(packageDriver, /openwrt-apk-rootfs\.mjs/)
assert.match(packageDriver, /--allow-untrusted extract/)
assert.match(packageDriver, /package\/solovey-ui\/compile V=s/)
assert.match(packageDriver, /CONFIG_NO_STRIP=y/)
assert.match(packageDriver, /OPENWRT_MAKE_PROGRAM='\/usr\/bin\/make'/)
assert.doesNotMatch(packageDriver, /command -v make/)
assert.match(packageDriver, /source "\$source_dir\/scripts\/openwrt-package-process\.sh"/)
assert.match(packageDriver, /openwrt-package-host-tools\.mjs" create/)
assert.equal((packageDriver.match(/openwrt-package-host-tools\.mjs" verify/g) ?? []).length, 2)
assert.match(packageDriver, /openwrt-sdk-archive\.mjs" verify-tree[\s\S]*run_package_make -C "\$sdk"/)
assert.match(packageDriver, /openwrt-apk-metadata\.mjs/)
assert.match(packageDriver, /APK_METADATA_PROOF\.json/)
assert.match(packageDriver, /adbdump "\$artifact_copy"/)
assert.match(packageDriver, /extract --destination "\$apk_rootfs" --no-chown "\$artifact_copy"/)
assert.match(packageDriver, /forbidden package-build environment override/)
assert.doesNotMatch(packageDriver, /\bgo build\b|\bnpm ci\b/)
assert.match(packageDriver, /CONFIG_TARGET_\$\{?OPENWRT|OPENWRT_CONFIG_TARGET/)
assert.match(packageDriver, /OPENWRT_ELF_FILE_PATTERN/)

assert.match(packageProcess, /\/usr\/bin\/env -i/)
assert.match(packageProcess, /\[\[ "\$package_make_program" == '\/usr\/bin\/make' \]\]/)
assert.match(packageProcess, /"\$package_make_program" "\$@"/)
assert.doesNotMatch(packageProcess, /command -v|(?:^|\s)make(?:\s|$)/m)

assert.match(packageHostTools, /openwrt-package-host-tools\/v1/)
assert.match(packageHostTools, /callerPathInherited: false/)
assert.match(packageHostTools, /executableSha256/)
assert.match(packageHostTools, /realpath-of-fixed-absolute-path/)
assert.match(packageHostTools, /packageHostToolIdentity/)
for (const selector of ['MAKE', 'MAKEFLAGS', 'GNUMAKEFLAGS', 'MFLAGS', 'MAKELEVEL', 'MAKEFILES']) {
  assert.match(packageHostTools, new RegExp(`'${selector}'`), `package tool contract omits ${selector}`)
}

assert.match(apkMetadata, /openwrt-apk-metadata-proof\/v1/)
for (const field of ['name', 'version', 'release', 'architecture', 'license', 'origin', 'maintainer', 'url', 'description', 'dependencies', 'provides', 'installedSize']) {
  assert.match(apkMetadata, new RegExp(`\\b${field}\\b`), `APK metadata proof omits ${field}`)
}
assert.match(apkMetadata, /sum-of-adb-path-file-sizes/)
assert.match(apkMetadata, /MalenkiySolovey/)

assert.match(fingerprint, /sourceFingerprintSchema = 'solovey-ui\/openwrt-package-source-fingerprint\/v4'/)
assert.match(fingerprint, /scripts\/openwrt-go-authority\.mjs/)
assert.match(fingerprint, /scripts\/frontend-runtime-closure\.mjs/)
for (const packageBoundaryScript of ['openwrt-package-process.sh', 'openwrt-package-host-tools.mjs', 'openwrt-apk-metadata.mjs']) {
  assert.match(fingerprint, new RegExp(escapeRegExp(packageBoundaryScript)), `source identity omits ${packageBoundaryScript}`)
}
assert.match(fingerprint, /'ipmonitor', 'logger'/)
assert.match(fingerprint, /generatedOutputDigest/)
assert.match(fingerprint, /buildIdentity/)
assert.doesNotMatch(fingerprint, /git ls-files|git status|\.gitignore|trackedDiffSha256|baseHead|originMain|releaseTagBase/)

assert.match(stageManifest, /stageManifestSchema = 'solovey-ui\/openwrt-target-stage\/v2'/)
for (const field of ['path', 'type', 'sha256', 'size', 'mode', 'kind', 'architecture']) {
  assert.match(stageManifest, new RegExp(`\\b${field}\\b`))
}
assert.match(stageManifest, /stageIdentity/)
assert.match(stageManifest, /payloadTreeDigest/)
assert.match(stageManifest, /directoryCount/)
assert.match(stageManifest, /wrong directory mode/)
assert.match(stageManifest, /unexpected symlink/)
assert.match(targetBuilder, /-buildid=/)
assert.match(stageProducer, /PANEL_LDFLAGS_CONTRACT="-buildid=/)
assert.match(stageProducer, /HELPER_LDFLAGS_CONTRACT="-buildid=/)
assert.match(sdkArchive, /fresh-private-direct-from-verified-private-archive/)
assert.match(sdkArchive, /SDK symlink escapes the extraction root/)
for (const field of ['treeDigest', 'sha256', 'size', 'mode', 'target']) assert.match(sdkArchive, new RegExp(`\\b${field}\\b`))
assert.match(goAuthority, /official-go-download-sha256-private-copy/)
assert.match(goAuthority, /fresh-private-gomodcache-go-sumdb/)
assert.match(goAuthority, /exact-openwrt-target-package-dependencies/)
assert.match(goAuthority, /Go toolchain proof differs from the tree that will be executed/)
assert.match(apkRootfs, /APK installed rootfs differs from the canonical stage/)
assert.match(apkRootfs, /apk-tools-v3-generated-package-registration/)

assert.match(releaseWorkflow, /bash scripts\/build-linux-target\.sh --mode release --profile full/)
assert.match(releaseWorkflow, /bash scripts\/build-linux-target\.sh --mode release --profile core/)
assert.doesNotMatch(releaseWorkflow.slice(releaseWorkflow.indexOf('  build-linux:')), /\bgo build\b/)

assert.match(initScript, /procd_set_param user solovey-ui/)
assert.match(initScript, /SUI_DEPLOYMENT_KIND=openwrt-package-managed/)
assert.match(initScript, /solovey-openwrt-lifecycle" broker-entry/)
assert.match(initScript, /solovey-openwrt-lifecycle" panel-entry/)
assert.match(initScript, /service_triggers\(\)[\s\S]*procd_add_reload_trigger firewall/)
assert.match(initScript, /reload_service\(\)[\s\S]*procd_send_signal solovey-ui panel HUP/)
assert.match(lifecycle, /--transport=standalone-owned/)
assert.match(lifecycle, /--ssh-implementation=dropbear/)
assert.match(lifecycle, /--ssh-service-control=procd/)
assert.match(lifecycle, /--ssh-log-evidence=logread/)
assert.match(lifecycle, /--deployment-backend=package-managed/)
assert.match(lifecycle, /--update-mode=package-managed/)
assert.doesNotMatch(initScript, /--broker-profile=/)
assert.doesNotMatch(initScript, /SOLOVEY_UI_SSH_(?:IMPLEMENTATION|SERVICE_CONTROL|LOG_EVIDENCE|JOURNALD_UNITS)/)
assert.doesNotMatch(initScript, /\b(?:mkdir|chown|chmod)\b/)
assert.match(preparation, /chown root:solovey-ui \/run\/solovey-ui/)
assert.match(preparation, /chmod 0750 \/run\/solovey-ui/)
assert.match(preparation, /chown solovey-ui:solovey-ui \/run\/solovey-ui\/server-protection/)
assert.match(preparation, /chmod 0700 \/run\/solovey-ui\/server-protection/)
assert.match(preparation, /solovey-openwrt-durability \|\| exit 1/)
assert.match(preparation, /id -u solovey-ui/)
assert.match(preparation, /id -g solovey-ui/)
assert.match(preparation, /chown root:root \/etc\/solovey-ui/)
assert.match(preparation, /chmod 0711 \/etc\/solovey-ui/)
assert.doesNotMatch(preparation, /chown root:solovey-ui \/etc\/solovey-ui/)
assert.match(preparation, /preservation_root=\/etc\/solovey-ui\/db\/sysupgrade-preservation/)
assert.match(preparation, /repair_preservation_file "\$preservation_root\/database\.db" 536870912/)
assert.match(preparation, /repair_preservation_file "\$preservation_root\/metadata\.json" 16384/)
assert.match(preparation, /chown solovey-ui:solovey-ui "\$path"/)
assert.match(preparation, /chmod 0600 "\$path"/)
assert.doesNotMatch(preparation, /(?:chown|chmod) -R/)
assert.match(preparation, /solovey-openwrt-owner-manifest \|\| exit 1/)
assert.match(initScript, /SOLOVEY_PREPARE=\$SOLOVEY_ROOT\/solovey-openwrt-prepare/)
assert.match(initScript, /"\$SOLOVEY_PREPARE" \|\| return 1/)
assert.match(initScript, /SOLOVEY_LIFECYCLE=\$SOLOVEY_ROOT\/solovey-openwrt-lifecycle/)
assert.match(initScript, /"\$SOLOVEY_LIFECYCLE" startup-restore \|\| return 1/)
assert.ok(initScript.indexOf('startup-restore') < initScript.indexOf('procd_open_instance root-broker'))
assert.doesNotMatch(recipe, /\/etc\/init\.d\/solovey-ui (?:enable|start)/)
assert.match(recipe, /Package\/solovey-ui\/preinst/)
assert.match(recipe, /PKG_UPGRADE/)
assert.match(recipe, /solovey-openwrt-lifecycle reconcile/)
assert.match(recipe, /Package\/solovey-ui\/prerm/)
assert.match(recipe, /solovey-openwrt-lifecycle pre-remove/)
assert.match(lifecycle, /completePreRemove\(context\.Background\(\), protectionhelper\.RemoveManagedTableForPackageRemoval/)
assert.ok(lifecycle.indexOf('removeManagedTable(ctx)') < lifecycle.indexOf('removeRuntime()'))
assert.match(upgradeScript, /solovey_ui_preservation_helper prepare \|\|/)
assert.match(upgradeScript, /start-stop-daemon -S -x "\$helper" -c solovey-ui:solovey-ui -- "\$@" 1>&2/)
for (const fact of ['SUI_DB_FOLDER=', 'SUI_DEPLOYMENT_KIND=', 'SUI_COMPONENTS_INSTALLED_FILE=', 'start-stop-daemon -S']) {
  assert.equal(upgradeScript.split(fact).length - 1, 1, `one preservation environment owner: ${fact}`)
}
assert.match(upgradeScript, /trap 'solovey_ui_finalize_preservation_backup "\$\?"' EXIT/)
assert.match(upgradeScript, /local action=fail-backup/)
assert.match(upgradeScript, /\[ "\$status" -ne 0 \] \|\| action=complete-backup/)
assert.match(upgradeScript, /solovey_ui_preservation_helper "\$action" \|\|/)
assert.match(upgradeScript, /preservation cleanup is deferred to startup reconciliation/)
assert.match(upgradeScript, /\[ -z "\$\{CONF_RESTORE:-\}" \] \|\| \{[\s\S]*does not support native sysupgrade backup restore; no files were applied[\s\S]*exit 1/)
assert.doesNotMatch(upgradeScript, /\b(?:apk|opkg|eval)\b/)
assert.deepEqual(keep.trim().split(/\r?\n/).filter(line => line && !line.startsWith('#')), [
  '/etc/solovey-ui/openwrt-instance-id',
  '/etc/solovey-ui/db/sysupgrade-preservation/database.db',
  '/etc/solovey-ui/db/sysupgrade-preservation/metadata.json',
])

console.log('[openwrt-package-static-check] package and provenance contracts verified')

function read(relativePath) {
  return fs.readFileSync(path.join(sourceRoot, relativePath), 'utf8')
}

function argumentValue(name) {
  const index = process.argv.indexOf(name)
  if (index === -1) return undefined
  const value = process.argv[index + 1]
  if (!value || value.startsWith('--')) throw new Error(`${name} requires a value`)
  return value
}

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}
