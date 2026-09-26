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
const destinationLock = path.join(workspace, 'upstreams/OPENWRT_REFERENCE_LOCK.md')
if (fs.existsSync(destinationLock)) assert.equal(fs.readFileSync(destinationLock, 'utf8').replace(/\r\n/g, '\n'), lock.replace(/\r\n/g, '\n'), 'existing reference lock differs')
else fs.copyFileSync(lockFile, destinationLock)
const references = []
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
  references.push({ relative, revision, remote })
}
function materialize({ relative, revision, remote }) {
  const destination = path.join(workspace, relative)
  const git = args => execFileSync('git', args, { stdio: 'inherit' })
  if (!fs.existsSync(destination)) {
    git(['init', destination])
    git(['-C', destination, 'remote', 'add', 'origin', remote])
    git(['-C', destination, 'fetch', '--depth=1', 'origin', revision])
    git(['-C', destination, '-c', 'core.autocrlf=false', 'checkout', '--detach', 'FETCH_HEAD'])
  }
  assert.equal(execFileSync('git', ['-C', destination, 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim(), revision)
  assert.equal(execFileSync('git', ['-C', destination, 'status', '--porcelain', '--untracked-files=all'], { encoding: 'utf8' }).trim(), '', 'reference checkout is modified')
}
for (const reference of references) materialize(reference)
if (process.argv.includes('--test-references')) {
  // The exact base-system recipe owns its firewall4 revision; never latest.
  const recipe = fs.readFileSync(path.join(workspace, references[0].relative, 'package/network/config/firewall4/Makefile'), 'utf8')
  assert.ok(recipe.includes('PKG_SOURCE_URL=$(PROJECT_GIT)/project/firewall4.git'))
  const revision = recipe.match(/^PKG_SOURCE_VERSION:=([a-f0-9]{40})\r?$/m)?.[1]
  assert.ok(revision, 'pinned firewall4 recipe revision missing')
  materialize({ relative: 'upstreams/openwrt-firewall4-25.12.5-pinned', revision, remote: 'https://git.openwrt.org/project/firewall4.git' })
}
execFileSync(process.execPath, ['scripts/openwrt-persistence-authority.mjs', '--check'], { stdio: 'inherit' })
