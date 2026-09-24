import crypto from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'

// The publisher may create a draft for this immutable tag/commit, but cannot
// rewrite an existing public version. Network/error ambiguity fails closed.
const [mode, tag, ...directories] = process.argv.slice(2)
const repository = process.env.GITHUB_REPOSITORY ?? ''
const commit = process.env.GITHUB_SHA ?? ''
const token = process.env.GH_TOKEN ?? ''
async function read(endpoint, allowAbsent = false) {
  const response = await fetch(`https://api.github.com/repos/${repository}/${endpoint}`, {
    headers: { Accept: 'application/vnd.github+json', Authorization: `Bearer ${token}` },
    signal: AbortSignal.timeout(30000),
    redirect: 'error',
  })
  if (allowAbsent && response.status === 404) return null
  if (!response.ok) throw new Error(`GitHub read failed (${response.status})`)
  return response.json()
}
try {
  if (!['before', 'uploaded'].includes(mode) || !/^v[0-9]+\.[0-9]+\.[0-9]+(?:-[a-z0-9.-]+)?$/.test(tag ?? '') ||
      !/^[\w.-]+\/[\w.-]+$/.test(repository) || !/^[a-f0-9]{40}$/.test(commit) || !token) throw new Error('publication identity is incomplete')
  let ref = (await read(`git/ref/tags/${encodeURIComponent(tag)}`)).object
  for (let depth = 0; ref?.type === 'tag' && depth < 4; depth++) ref = (await read(`git/tags/${ref.sha}`)).object
  if (ref?.type !== 'commit' || ref.sha !== commit) throw new Error('immutable release tag does not point to the tested commit')
  const release = await read(`releases/tags/${encodeURIComponent(tag)}`, true)
  if (release && (!release.draft || release.target_commitish !== commit)) throw new Error('existing release is public or belongs to another source; refusing overwrite')
  if (mode === 'uploaded') {
    if (!release || directories.length === 0) throw new Error('uploaded draft and local candidate directories are required')
    const expected = new Map()
    for (const directory of directories) {
      for (const name of fs.readdirSync(directory)) {
        if (expected.has(name)) throw new Error('duplicate candidate name')
        const file = path.join(directory, name)
        const stat = fs.lstatSync(file)
        if (!stat.isFile()) throw new Error('candidate is not a regular file')
        const hash = crypto.createHash('sha256')
        for await (const chunk of fs.createReadStream(file)) hash.update(chunk)
        expected.set(name, { size: stat.size, digest: `sha256:${hash.digest('hex')}` })
      }
    }
    // Explicit asset endpoint pagination avoids truncating a future inventory.
    const assets = []
    for (let page = 1; page <= 10; page++) {
      const batch = await read(`releases/${release.id}/assets?per_page=100&page=${page}`)
      if (!Array.isArray(batch)) throw new Error('remote asset inventory is invalid')
      assets.push(...batch)
      if (batch.length < 100) break
      if (page === 10) throw new Error('remote asset inventory exceeds bound')
    }
    if (assets.length !== expected.size) throw new Error('remote draft asset set is incomplete or contains extra files')
    for (const asset of assets) {
      const identity = expected.get(asset.name)
      if (!identity || asset.state !== 'uploaded' || identity.size !== asset.size || identity.digest !== asset.digest) throw new Error('remote draft bytes do not match the verified candidate')
      expected.delete(asset.name)
    }
  }
  console.log(`publication ${mode} gate passed for ${tag}`)
} catch (error) {
  console.error(`publication gate failed: ${error.message}`)
  process.exitCode = 1
}
