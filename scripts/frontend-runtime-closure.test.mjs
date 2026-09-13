import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { spawnSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

const verifier = fileURLToPath(new URL('./frontend-runtime-closure.mjs', import.meta.url))

test('frontend runtime closure accepts one coherent build generation', t => {
  const fixture = createFixture(t)
  const result = verify(fixture)
  assert.equal(result.status, 0, result.stderr)
  const proof = JSON.parse(fs.readFileSync(path.join(fixture.components, 'runtime-closure.json'), 'utf8'))
  assert.equal(proof.counts.runtime, 2)
  assert.deepEqual(proof.runtimeAssets.map(asset => asset.file), ['assets/app.js', 'assets/component.js'])
})

test('frontend runtime closure rejects a missing runtime-required component asset', t => {
  const fixture = createFixture(t)
  fs.unlinkSync(path.join(fixture.components, 'example', 'frontend', 'assets', 'component.js'))
  const result = verify(fixture)
  assert.notEqual(result.status, 0)
  assert.match(result.stderr, /packaged closure mismatch/)
})

test('frontend runtime closure rejects stale assets from another generation', t => {
  const fixture = createFixture(t)
  fs.writeFileSync(path.join(fixture.dist, 'assets', 'stale.js'), 'stale')
  const result = verify(fixture)
  assert.notEqual(result.status, 0)
  assert.match(result.stderr, /core frontend assets mismatch/)
})

function createFixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'solovey-frontend-closure-'))
  t.after(() => fs.rmSync(root, { recursive: true, force: true }))
  const dist = path.join(root, 'dist')
  const components = path.join(root, 'components')
  fs.mkdirSync(path.join(dist, '.vite'), { recursive: true })
  fs.mkdirSync(path.join(dist, 'assets'), { recursive: true })
  fs.mkdirSync(path.join(components, 'example', 'frontend', 'assets'), { recursive: true })

  writeJSON(path.join(dist, '.vite', 'manifest.json'), {
    'index.html': {
      file: 'assets/app.js',
      isEntry: true,
      dynamicImports: ['../components/example/frontend/index.ts'],
    },
    '../components/example/frontend/index.ts': {
      file: 'assets/component.js',
      isDynamicEntry: true,
      src: '../components/example/frontend/index.ts',
    },
  })
  fs.writeFileSync(path.join(dist, 'index.html'), '<script type="module" src="./assets/app.js"></script>')
  fs.writeFileSync(path.join(dist, 'assets', 'app.js'), 'import("./component.js")')
  fs.writeFileSync(path.join(components, 'example', 'frontend', 'assets', 'component.js'), 'export const register = () => {}')
  writeJSON(path.join(components, 'example', 'component.json'), {
    id: 'example',
    delivery: 'in-process',
    frontend: { entries: ['frontend/index.ts'] },
  })
  writeJSON(path.join(components, 'example', 'frontend', 'assets.json'), {
    schemaVersion: 1,
    component: 'example',
    entries: ['frontend/index.ts'],
    files: ['assets/component.js'],
  })
  writeJSON(path.join(components, 'installed.json'), {
    version: 1,
    profile: 'full',
    binary: 'full',
    components: [{ id: 'example', delivery: 'in-process', installed: true }],
  })
  return { root, dist, components }
}

function verify(fixture) {
  return spawnSync(process.execPath, [
    verifier,
    '--dist', fixture.dist,
    '--components-dir', fixture.components,
    '--out', path.join(fixture.components, 'runtime-closure.json'),
  ], { encoding: 'utf8' })
}

function writeJSON(file, value) {
  fs.writeFileSync(file, `${JSON.stringify(value, null, 2)}\n`)
}
