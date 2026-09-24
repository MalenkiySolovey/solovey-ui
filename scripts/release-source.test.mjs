import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

test('source identity survives build outputs but changes with actual source', t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'release-source-'))
  t.after(() => fs.rmSync(root, { recursive: true, force: true }))
  execFileSync('git', ['init', '--quiet', root])
  fs.mkdirSync(path.join(root, 'web/html'), { recursive: true })
  fs.writeFileSync(path.join(root, 'web/html/index.html'), 'checkout placeholder\n')
  fs.writeFileSync(path.join(root, 'source.txt'), 'actual source\r\n')
  execFileSync('git', ['-C', root, '-c', 'core.autocrlf=false', 'add', '.'])
  const script = fileURLToPath(new URL('./release-source.mjs', import.meta.url))
  const fingerprint = () => execFileSync(process.execPath, [script, root], { encoding: 'utf8' }).trim()
  const before = fingerprint()
  fs.writeFileSync(path.join(root, 'source.txt'), 'actual source\n')
  fs.writeFileSync(path.join(root, 'web/html/index.html'), 'generated production frontend\n')
  fs.writeFileSync(path.join(root, 'web/html/generated.js'), 'generated chunk\n')
  assert.equal(fingerprint(), before)
  fs.writeFileSync(path.join(root, 'source.txt'), 'remediated source\n')
  assert.notEqual(fingerprint(), before)
})
