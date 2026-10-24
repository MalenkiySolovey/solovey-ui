// The release set is a product contract; workflows and operator tools call
// the same completeness, digest and signature checks before publication.
import crypto from 'node:crypto'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { resolveProviderSource } from './core-provider-build.mjs'
import { targets, legacySchema, providerSchema, linuxPanelArtifacts, withArtifactInspector, inspectPanelArchive, verifyReleaseEnvelope } from './release-providers.mjs'

export { targets }
export const linuxAssetNames = (schema = providerSchema) => [
  ...linuxPanelArtifacts(schema).map(target => target.name),
  'solovey-ui-components.tar.gz',
].sort()

export const windowsAssetNames = () => targets.windows.flatMap(arch =>
  targets.windowsProfiles.map(profile => `solovey-ui${profile === 'core' ? '-core' : ''}-windows-${arch}.zip`)).sort()

export async function verifyWindowsSet(directory) {
  const names = windowsAssetNames()
  const expected = names.flatMap(name => [name, `${name}.sha256`]).sort()
  if (JSON.stringify(fs.readdirSync(directory).sort()) !== JSON.stringify(expected)) throw new Error('Windows release directory is incomplete or contains extra assets')
  for (const name of names) {
    const file = path.join(directory, name)
    const stat = fs.lstatSync(file)
    if (!stat.isFile() || stat.size < 22 || stat.size > 1024 ** 3) throw new Error(`invalid Windows archive ${name}`)
    const hash = crypto.createHash('sha256')
    for await (const chunk of fs.createReadStream(file)) hash.update(chunk)
    const digest = hash.digest('hex')
    const sidecar = path.join(directory, `${name}.sha256`)
    if (!fs.lstatSync(sidecar).isFile() || fs.readFileSync(sidecar, 'utf8') !== `${digest}  ${name}\n`) throw new Error(`Windows checksum mismatch for ${name}`)
  }
  return names
}

export async function verifyReleaseSet(directory, { version, trustRootsBase64, now = Date.now() / 1000, artifactInfo = process.env.SOLOVEY_ARTIFACT_INFO } = {}) {
  const envelopeName = 'solovey-ui-release.json'
  const envelopePath = path.join(directory, envelopeName)
  const envelopeStat = fs.lstatSync(envelopePath)
  if (!envelopeStat.isFile() || envelopeStat.size <= 0 || envelopeStat.size > (2 << 20)) throw Error('release envelope is not a bounded regular file')
  const envelope = JSON.parse(fs.readFileSync(envelopePath, 'utf8'))
  const manifest = envelope.manifest
  if (![legacySchema, providerSchema].includes(envelope.schema) || envelope.algorithm !== 'Ed25519' || manifest?.schema !== envelope.schema) throw new Error('release envelope schema/algorithm is invalid')
  const names = linuxAssetNames(envelope.schema)
  const panelTargets = new Map(linuxPanelArtifacts(envelope.schema).map(target => [target.name, target]))
  const expected = [...names, envelopeName].flatMap(name => [name, `${name}.sha256`]).sort()
  const actual = fs.readdirSync(directory).sort()
  if (JSON.stringify(actual) !== JSON.stringify(expected)) throw new Error('release directory is not the complete canonical Linux set with checksums and envelope')
  const identities = new Map()
  for (const name of [...names, envelopeName]) {
    const file = path.join(directory, name)
    const stat = fs.lstatSync(file)
    if (!stat.isFile() || stat.size <= 0 || stat.size > 1024 ** 3) throw new Error(`invalid release file ${name}`)
    const hash = crypto.createHash('sha256')
    for await (const chunk of fs.createReadStream(file)) hash.update(chunk)
    const digest = hash.digest('hex')
    const sidecar = path.join(directory, `${name}.sha256`)
    if (!fs.lstatSync(sidecar).isFile() || fs.readFileSync(sidecar, 'utf8') !== `${digest}  ${name}\n`) throw new Error(`checksum binding mismatch for ${name}`)
    identities.set(name, { size: stat.size, sha256: digest })
  }
  if (manifest.version !== version?.replace(/^v/, '') || manifest.issuedAt > now || manifest.expiresAt <= now) throw new Error('release version or validity mismatch')
  if (!Number.isSafeInteger(manifest.issuedAt) || !Number.isSafeInteger(manifest.expiresAt) ||
      manifest.expiresAt <= manifest.issuedAt || manifest.expiresAt - manifest.issuedAt > 14 * 86400 ||
      manifest.channel !== (manifest.version.includes('-') ? 'beta' : 'main')) throw new Error('release channel or validity window is invalid')
  if (!Number.isSafeInteger(manifest.sequence) || manifest.sequence <= 0) throw new Error('release sequence is invalid')
  const roots = JSON.parse(Buffer.from(trustRootsBase64 ?? '', 'base64').toString('utf8'))
  if (!Array.isArray(roots) || roots.length === 0 || roots.length > 8) throw new Error('release trust roots are invalid')
  const matching = roots.filter(root => root.keyId === envelope.keyId)
  if (matching.length !== 1) throw new Error('release signer must have exactly one trust root')
  const root = matching[0]
  if (!['ACTIVE', 'NEXT'].includes(root.state) || !(root.notBefore <= now && now < root.notAfter) ||
      !(root.minSequence <= manifest.sequence) || (root.maxSequence !== undefined && manifest.sequence > root.maxSequence)) throw new Error('release signer does not authorize this envelope')
  const publicKey = crypto.createPublicKey({ key: { kty: 'OKP', crv: 'Ed25519', x: Buffer.from(root.publicKey, 'base64').toString('base64url') }, format: 'jwk' })
  if (!crypto.verify(null, Buffer.from(JSON.stringify(manifest)), publicKey, Buffer.from(envelope.signature, 'base64'))) throw new Error('release signature mismatch')
  if (!Array.isArray(manifest.artifacts) || manifest.artifacts.length !== names.length) throw new Error('manifest artifact count mismatch')
  const seen = new Set()
  for (const artifact of manifest.artifacts) {
    const expectedIdentity = identities.get(artifact.name)
    if (!names.includes(artifact.name) || seen.has(artifact.name) || !expectedIdentity || artifact.size !== expectedIdentity.size || artifact.sha256 !== expectedIdentity.sha256) throw new Error('manifest artifact identity mismatch')
    seen.add(artifact.name)
    const component = artifact.name === 'solovey-ui-components.tar.gz'
    const role = component ? 'component-catalog' : artifact.name.includes('-core-') ? 'panel-core' : 'panel-full'
    const arch = component ? 'any' : artifact.name.match(/linux-(.+)\.tar\.gz$/)[1]
    if (artifact.role !== role || artifact.arch !== arch || artifact.platform !== (component ? 'any' : 'linux') || artifact.provenance !== 'github-actions' || artifact.mediaType !== 'application/vnd.solovey.release-set+gzip') throw new Error('manifest role/target binding mismatch')
    if (envelope.schema === providerSchema) {
      if (artifact.coreProvider !== (component ? undefined : panelTargets.get(artifact.name).provider)) throw Error('manifest provider/target binding mismatch')
    } else if (artifact.coreProvider !== undefined) throw Error('legacy manifest cannot declare a provider')
  }
  const catalog = identities.get('solovey-ui-components.tar.gz')
  const componentIDs = new Set()
  if (!Array.isArray(manifest.components) || manifest.components.length === 0) throw new Error('manifest component inventory is missing')
  for (const component of manifest.components) {
    if (!/^[a-z0-9-]+$/.test(component.id ?? '') || componentIDs.has(component.id) || component.artifactSha256 !== catalog.sha256) throw new Error('manifest component catalog binding mismatch')
    componentIDs.add(component.id)
  }
  if (envelope.schema === providerSchema) {
    const sources = [...new Set([...panelTargets.values()].map(target => target.provider))].sort()
      .map(provider => resolveProviderSource({ provider }).source)
    assert.deepEqual(manifest.coreProviders, sources, 'release provider sources differ from the pinned build flavors')
    await withArtifactInspector(artifactInfo, inspector => {
      const observed = verifyReleaseEnvelope(inspector, envelopePath, { trustRoots: roots, now, channel: manifest.channel })
      assert.deepEqual(observed.manifest, manifest, 'runtime release observation differs')
      for (const artifact of manifest.artifacts) {
        if (artifact.coreProvider) inspectPanelArchive(inspector, path.join(directory, artifact.name), panelTargets.get(artifact.name), identities.get(artifact.name))
      }
    })
  } else if (manifest.coreProviders !== undefined) throw Error('legacy release cannot declare provider sources')
  return { schema: manifest.schema, version: manifest.version, sequence: manifest.sequence, artifacts: Object.fromEntries(identities) }
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  try {
    const [directory, version] = process.argv.slice(2)
    if (!directory || !version) throw new Error(`usage: node ${fileURLToPath(import.meta.url)} <assets-dir> <version>`)
    if (process.argv[4] === '--windows') {
      const names = await verifyWindowsSet(directory)
      console.log(`complete Windows release verified: ${names.join(', ')}`)
    } else {
      const result = await verifyReleaseSet(directory, { version, trustRootsBase64: process.env.SUI_RELEASE_TRUST_ROOTS_B64 })
      console.log(`complete signed Linux release verified: ${result.version}, ${Object.keys(result.artifacts).length} files`)
    }
  } catch (error) {
    console.error(`release verification failed: ${error.message}`)
    process.exitCode = 1
  }
}
