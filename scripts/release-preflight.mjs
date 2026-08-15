import {
  createPrivateKey,
  createPublicKey,
  sign,
  timingSafeEqual,
  verify,
} from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'
import { pathToFileURL } from 'node:url'

const keyIDPattern = /^[a-z0-9][a-z0-9._+-]{0,95}$/
const allowedRootFields = new Set([
  'keyId',
  'publicKey',
  'state',
  'notBefore',
  'notAfter',
  'minSequence',
  'maxSequence',
])

export function validateReleaseSigning({
  trustRootsBase64,
  signingKeyID,
  signingPrivateKeyBase64,
  sequence,
  now = new Date(),
}) {
  const missing = []
  if (!trustRootsBase64) missing.push('SUI_RELEASE_TRUST_ROOTS_B64 repository variable')
  if (!signingKeyID) missing.push('SUI_RELEASE_SIGNING_KEY_ID repository variable')
  if (!signingPrivateKeyBase64) missing.push('SUI_RELEASE_SIGNING_PRIVATE_KEY_B64 repository secret')
  if (missing.length > 0) throw new Error(`missing required release configuration: ${missing.join(', ')}`)
  if (!keyIDPattern.test(signingKeyID)) throw new Error('release signing key ID is invalid')
  if (!Number.isSafeInteger(sequence) || sequence <= 0) throw new Error('release sequence is invalid')

  const rootsPayload = decodeStrictBase64('release trust roots', trustRootsBase64, 64 << 10)
  let roots
  try {
    roots = JSON.parse(rootsPayload.toString('utf8'))
  } catch {
    throw new Error('release trust roots JSON is invalid')
  }
  if (!Array.isArray(roots) || roots.length === 0 || roots.length > 8) {
    throw new Error('release trust roots must contain between one and eight roots')
  }

  const nowSeconds = Math.floor(now.getTime() / 1000)
  const seen = new Set()
  let signingRoot
  for (const root of roots) {
    if (!root || Array.isArray(root) || typeof root !== 'object') throw new Error('release trust root entry is invalid')
    for (const field of Object.keys(root)) {
      if (!allowedRootFields.has(field)) throw new Error(`release trust root contains unknown field ${field}`)
    }
    if (!keyIDPattern.test(root.keyId ?? '') || seen.has(root.keyId)) throw new Error('release trust root key ID is invalid or duplicated')
    seen.add(root.keyId)
    if (!['ACTIVE', 'NEXT', 'RETIRED'].includes(root.state)) throw new Error(`release trust root ${root.keyId} has an invalid state`)
    for (const field of ['notBefore', 'notAfter', 'minSequence']) {
      if (!Number.isSafeInteger(root[field]) || root[field] <= 0) throw new Error(`release trust root ${root.keyId} has invalid ${field}`)
    }
    if (root.maxSequence !== undefined && (!Number.isSafeInteger(root.maxSequence) || root.maxSequence <= 0)) {
      throw new Error(`release trust root ${root.keyId} has invalid maxSequence`)
    }
    if (root.notAfter <= root.notBefore || (root.maxSequence !== undefined && root.maxSequence < root.minSequence)) {
      throw new Error(`release trust root ${root.keyId} has an invalid validity range`)
    }
    root.publicKeyBytes = decodeStrictBase64(`release trust root ${root.keyId} public key`, root.publicKey, 32)
    if (root.publicKeyBytes.length !== 32) throw new Error(`release trust root ${root.keyId} public key must be Ed25519`)
    if (root.keyId === signingKeyID) signingRoot = root
  }

  if (!signingRoot) throw new Error('release signing key ID has no matching public trust root')
  if (!['ACTIVE', 'NEXT'].includes(signingRoot.state)) throw new Error('release signing trust root is retired')
  if (nowSeconds < signingRoot.notBefore || nowSeconds >= signingRoot.notAfter) throw new Error('release signing trust root is outside its validity window')
  if (sequence < signingRoot.minSequence || (signingRoot.maxSequence !== undefined && sequence > signingRoot.maxSequence)) {
    throw new Error('release signing trust root does not authorize the planned sequence')
  }

  const privateKeyDER = decodeStrictBase64('release signing private key', signingPrivateKeyBase64, 4 << 10)
  let privateKey
  try {
    privateKey = createPrivateKey({ key: privateKeyDER, format: 'der', type: 'pkcs8' })
  } catch {
    throw new Error('release signing private key is not valid PKCS8 DER')
  }
  if (privateKey.asymmetricKeyType !== 'ed25519') throw new Error('release signing private key must be Ed25519')

  const derivedJWK = createPublicKey(privateKey).export({ format: 'jwk' })
  const derivedPublicKey = Buffer.from(derivedJWK.x, 'base64url')
  if (derivedPublicKey.length !== signingRoot.publicKeyBytes.length || !timingSafeEqual(derivedPublicKey, signingRoot.publicKeyBytes)) {
    throw new Error('release signing private key does not match the configured public trust root')
  }
  const publicKey = createPublicKey({
    key: { kty: 'OKP', crv: 'Ed25519', x: signingRoot.publicKeyBytes.toString('base64url') },
    format: 'jwk',
  })
  const challenge = Buffer.from('solovey-ui-release-preflight-v1', 'utf8')
  const signature = sign(null, challenge, privateKey)
  if (!verify(null, challenge, publicKey, signature)) throw new Error('release signing challenge verification failed')

  return { keyID: signingRoot.keyId, rootCount: roots.length }
}

function decodeStrictBase64(label, value, maxBytes) {
  if (
    typeof value !== 'string' ||
    value.length === 0 ||
    !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(value)
  ) {
    throw new Error(`${label} encoding is invalid`)
  }
  const decoded = Buffer.from(value, 'base64')
  if (decoded.length === 0 || decoded.length > maxBytes || decoded.toString('base64') !== value) {
    throw new Error(`${label} encoding is invalid`)
  }
  return decoded
}

function parseArguments(argv) {
  const values = new Map()
  for (let index = 0; index < argv.length; index += 2) {
    const name = argv[index]
    const value = argv[index + 1]
    if (!['--tag', '--sequence'].includes(name) || value === undefined) throw new Error(`unsupported or incomplete argument ${name ?? '<missing>'}`)
    values.set(name, value)
  }
  return values
}

function main() {
  try {
    const args = parseArguments(process.argv.slice(2))
    const tag = args.get('--tag') ?? ''
    const sequence = Number(args.get('--sequence'))
    const sourceVersion = fs.readFileSync(path.join(process.cwd(), 'config', 'identity', 'version'), 'utf8').trim()
    if (tag !== `v${sourceVersion}`) throw new Error(`release tag does not match source version ${sourceVersion}`)
    const result = validateReleaseSigning({
      trustRootsBase64: process.env.SUI_RELEASE_TRUST_ROOTS_B64 ?? '',
      signingKeyID: process.env.SUI_RELEASE_SIGNING_KEY_ID ?? '',
      signingPrivateKeyBase64: process.env.SUI_RELEASE_SIGNING_PRIVATE_KEY_B64 ?? '',
      sequence,
    })
    console.log(`release preflight passed for ${tag} (key ${result.keyID}, roots ${result.rootCount})`)
  } catch (error) {
    console.error(`release preflight failed: ${error.message}`)
    process.exitCode = 1
  }
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) main()
