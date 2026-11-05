import fs from 'node:fs'
import path from 'node:path'
import { pathToFileURL } from 'node:url'
import { resolveProviderSource } from './core-provider-build.mjs'

export function releaseOCIFlavor(provider = 'sing-box') {
  const resolved = resolveProviderSource({ provider })
  return { provider: resolved.provider, artifact_suffix: provider === 'sing-box' ? '' : `-${provider}` }
}

export function releaseOCIIdentity(tag, sourceVersion, revision) {
  const match = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([a-z0-9-]+(?:\.[a-z0-9-]+)*))?$/.exec(sourceVersion ?? '')
  if (!match || match[4]?.split('.').some(part => /^0[0-9]+$/.test(part)) || tag !== `v${sourceVersion}`) {
    throw new Error('OCI release tag must equal the valid source version with a v prefix')
  }
  if (!/^[a-f0-9]{40}$/.test(revision ?? '')) throw new Error('OCI release revision must be the exact source commit')
  return { version: tag, commit: revision, source: 'https://github.com/MalenkiySolovey/solovey-ui' }
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  try {
    const identity = releaseOCIIdentity(process.argv[2], fs.readFileSync('config/identity/version', 'utf8').trim(), process.argv[3])
    if (process.argv[4] !== undefined) Object.assign(identity, releaseOCIFlavor(process.argv[4]))
    for (const [key, value] of Object.entries(identity)) console.log(`${key}=${value}`)
  } catch (error) {
    console.error(`OCI release identity failed: ${error.message}`)
    process.exitCode = 1
  }
}
