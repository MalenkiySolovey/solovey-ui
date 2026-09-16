import crypto from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'

// This is the package-owned source-to-installed mapping for Unix text assets.
// Stage construction, stage verification, APK verification and the static gate
// all consume this same contract so a new shipping script cannot bypass the LF
// boundary by being added to only one checker.
export const packageUnixTextAssets = Object.freeze([
  Object.freeze({
    source: 'deploy/openwrt/solovey-ui.init',
    installed: 'etc/init.d/solovey-ui',
    mode: '0755',
    kind: 'openwrt-init',
  }),
  Object.freeze({
    source: 'deploy/openwrt/solovey-openwrt-prepare',
    installed: 'usr/lib/solovey-ui/solovey-openwrt-prepare',
    mode: '0755',
    kind: 'openwrt-preparation',
  }),
  Object.freeze({
    source: 'deploy/openwrt/solovey-ui-upgrade.sh',
    installed: 'lib/upgrade/solovey-ui.sh',
    mode: '0755',
    kind: 'openwrt-upgrade',
  }),
  Object.freeze({
    source: 'deploy/openwrt/solovey-ui.keep',
    installed: 'lib/upgrade/keep.d/solovey-ui',
    mode: '0644',
    kind: 'openwrt-preservation-list',
  }),
])

export const packageLifecycleSourcePaths = Object.freeze([
  'deploy/openwrt/package/solovey-ui/Makefile',
])

export const packagePipelineUnixTextPaths = Object.freeze([
  'deploy/openwrt/package/solovey-ui/files/validate-stage.sh',
  'scripts/build-linux-target.sh',
  'scripts/openwrt-package-build.sh',
  'scripts/openwrt-package-process.sh',
  'scripts/openwrt-stage-build.sh',
  'scripts/openwrt-target-profile.sh',
])

export const sourceUnixTextPaths = Object.freeze([
  ...packageUnixTextAssets.map(asset => asset.source),
  ...packageLifecycleSourcePaths,
  ...packagePipelineUnixTextPaths,
])

export function classifyUnixLineEndings(bytes) {
  if (!Buffer.isBuffer(bytes)) bytes = Buffer.from(bytes)
  let crlf = 0
  let lf = 0
  let bareCR = 0
  for (let index = 0; index < bytes.length; index += 1) {
    if (bytes[index] === 0x0a) {
      lf += 1
      if (index > 0 && bytes[index - 1] === 0x0d) crlf += 1
    } else if (bytes[index] === 0x0d && (index + 1 >= bytes.length || bytes[index + 1] !== 0x0a)) {
      bareCR += 1
    }
  }
  const bareLF = lf - crlf
  let classification = 'LF_ONLY'
  if (bareCR > 0 || (crlf > 0 && bareLF > 0)) classification = 'MIXED_EOL'
  else if (crlf > 0) classification = 'CRLF'
  return { classification, lf, crlf, bareLF, bareCR }
}

export function assertUnixLF(bytes, label) {
  const result = classifyUnixLineEndings(bytes)
  if (result.classification !== 'LF_ONLY') {
    throw new Error(`${label} must be LF-only; found ${result.classification} (crlf=${result.crlf}, bareCR=${result.bareCR}, bareLF=${result.bareLF})`)
  }
  return result
}

export function verifySourceUnixTextAssets(sourceRoot) {
  return sourceUnixTextPaths.map(relativePath => {
    const bytes = fs.readFileSync(path.join(sourceRoot, ...relativePath.split('/')))
    assertUnixLF(bytes, `Unix source asset ${relativePath}`)
    return byteProof(relativePath, bytes)
  })
}

export function proveInstalledUnixTextAssets(root) {
  return packageUnixTextAssets.map(asset => {
    const bytes = fs.readFileSync(path.join(root, ...asset.installed.split('/')))
    assertUnixLF(bytes, `installed Unix asset ${asset.installed}`)
    return { ...byteProof(asset.installed, bytes), source: asset.source, mode: asset.mode, kind: asset.kind }
  })
}

function byteProof(assetPath, bytes) {
  const firstLF = bytes.indexOf(0x0a)
  return {
    path: assetPath,
    size: bytes.length,
    sha256: crypto.createHash('sha256').update(bytes).digest('hex'),
    lineEndings: 'LF_ONLY',
    firstLineTerminatorHex: firstLF < 0 ? null : (firstLF > 0 && bytes[firstLF - 1] === 0x0d ? '0d0a' : '0a'),
  }
}
