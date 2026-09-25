import assert from 'node:assert/strict'
import crypto from 'node:crypto'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
import test from 'node:test'
import { pathToFileURL } from 'node:url'

const commit = 'a'.repeat(40)
function fixture(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'publication-gate-'))
  t.after(() => fs.rmSync(dir, { force: true, recursive: true }))
  const assets = path.join(dir, 'assets')
  fs.mkdirSync(assets)
  const content = Buffer.from('public candidate bytes')
  fs.writeFileSync(path.join(assets, 'candidate.tar.gz'), content)
  const asset = { name: 'candidate.tar.gz', size: content.length, state: 'uploaded', digest: 'sha256:' + crypto.createHash('sha256').update(content).digest('hex') }
  const preload = path.join(dir, 'transport.mjs')
  fs.writeFileSync(preload, `
    const replies=JSON.parse(process.env.TEST_REPLIES);
    globalThis.fetch=async (url,options)=>{
      if(options.redirect!=='error'||!url.startsWith('https://api.github.com/repos/owner/repo/')) throw new Error('unsafe transport');
      const key=url.slice('https://api.github.com/repos/owner/repo/'.length);
      const reply=replies[key];
      if(!reply) throw new Error('unexpected API request');
      return {ok:reply.status===200,status:reply.status,json:async()=>reply.body};
    };
  `)
  const replies = {
    'git/ref/tags/v2030.1.0': { status: 200, body: { object: { type: 'commit', sha: commit } } },
    'releases/tags/v2030.1.0': { status: 404 },
    'releases?per_page=100&page=1': { status: 200, body: [] },
  }
  const run = mode => spawnSync(process.execPath, ['--import', pathToFileURL(preload).href, 'scripts/release-publish-gate.mjs', mode, 'v2030.1.0', assets], {
    env: { ...process.env, GITHUB_REPOSITORY: 'owner/repo', GITHUB_SHA: commit, GH_TOKEN: 'test-placeholder', TEST_REPLIES: JSON.stringify(replies) }, encoding: 'utf8',
  })
  return { run, replies, asset }
}
test('publication requires immutable tag identity and refuses existing public version', t => {
  const { run, replies } = fixture(t)
  assert.equal(run('before').status, 0)
  replies['git/ref/tags/v2030.1.0'].body.object.sha = 'b'.repeat(40)
  assert.notEqual(run('before').status, 0)
  replies['git/ref/tags/v2030.1.0'].body.object.sha = commit
  replies['releases/tags/v2030.1.0'] = { status: 200, body: { id: 1, draft: false, target_commitish: commit } }
  assert.notEqual(run('before').status, 0)
  replies['releases/tags/v2030.1.0'] = { status: 403 }
  assert.notEqual(run('before').status, 0)
})
test('draft promotion requires the exact uploaded complete byte inventory', t => {
  const { run, replies, asset } = fixture(t)
  replies['releases/tags/v2030.1.0'] = { status: 200, body: { id: 1, draft: true, target_commitish: commit } }
  const endpoint = 'releases/1/assets?per_page=100&page=1'
  replies[endpoint] = { status: 200, body: [asset] }
  assert.equal(run('uploaded').status, 0)
  replies[endpoint].body = []
  assert.notEqual(run('uploaded').status, 0)
  replies[endpoint].body = [{ ...asset, digest: 'sha256:' + '0'.repeat(64) }]
  assert.notEqual(run('uploaded').status, 0)
  replies[endpoint].body = [{ ...asset, state: 'starter' }]
  assert.notEqual(run('uploaded').status, 0)
})


test('draft omitted by tag endpoint is verified through authenticated inventory', t => {
  const { run, replies, asset } = fixture(t)
  const draft = { id: 2, tag_name: 'v2030.1.0', draft: true, target_commitish: commit }
  replies['releases?per_page=100&page=1'].body = [draft]
  replies['releases/2/assets?per_page=100&page=1'] = { status: 200, body: [asset] }
  assert.equal(run('uploaded').status, 0)
  draft.target_commitish = 'b'.repeat(40)
  assert.notEqual(run('before').status, 0)
  draft.target_commitish = commit
  replies['releases?per_page=100&page=1'].body.push({ ...draft, id: 3 })
  assert.notEqual(run('uploaded').status, 0)
})
