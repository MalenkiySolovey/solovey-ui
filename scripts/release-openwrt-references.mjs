// Materialize the existing workspace references required by the immutable
// persistence verifier. No upstream implementation is copied into the product.
import assert from 'node:assert/strict'
import fs from 'node:fs'
import path from 'node:path'
import { execFileSync } from 'node:child_process'

const lockFile = process.argv[2]
assert.ok(fs.existsSync('scripts/openwrt-persistence-authority.mjs'), 'immutable product cwd required')
const workspace = path.resolve(process.cwd(), '../..')
const bytes = fs.readFileSync(lockFile)
const lock = bytes.toString('utf8')
fs.mkdirSync(path.join(workspace, 'upstreams'), { recursive: true })
fs.copyFileSync(lockFile, path.join(workspace, 'upstreams/OPENWRT_REFERENCE_LOCK.md'))
for (const id of ['A1', 'A2']) {
  const section = lock.split(`#### ${id}. `)[1]?.split('\n#### ')[0]
  assert.ok(section)
  const field = name => section.match(new RegExp(`\\*\\*${name}\\*\\*: \x60([^\x60]+)\x60`))[1]
  const relative = field('Directory')
  const revision = field('HEAD SHA')
  const remote = field('Remote URL')
  assert.match(relative, /^upstreams\/openwrt-[a-z0-9.-]+$/)
  assert.match(revision, /^[a-f0-9]{40}$/)
  assert.match(remote, /^https:\/\/git\.openwrt\.org\/(?:openwrt\/openwrt|project\/fstools)\.git$/)
  const destination = path.join(workspace, relative)
  assert.ok(!fs.existsSync(destination), 'reference output already exists')
  const git = args => execFileSync('git', args, { stdio: 'inherit' })
  git(['init', destination])
  git(['-C', destination, 'remote', 'add', 'origin', remote])
  git(['-C', destination, 'fetch', '--depth=1', 'origin', revision])
  git(['-C', destination, 'checkout', '--detach', 'FETCH_HEAD'])
  assert.equal(execFileSync('git', ['-C', destination, 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim(), revision)
}
execFileSync(process.execPath, ['scripts/openwrt-persistence-authority.mjs', '--check'], { stdio: 'inherit' })
