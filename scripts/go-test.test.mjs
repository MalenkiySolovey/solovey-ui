import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import test from 'node:test'
import { allocateShards, parseInput, runTests } from './go-test.mjs'

const module = 'github.com/MalenkiySolovey/solovey-ui'
const flags = ['-count=1', '-p', '1', '-timeout', '30m']
const input = (index, total = 4, extra = []) => ['--shard-index', String(index), '--shard-total', String(total), ...flags, ...extra, '--', './...']
const capabilities = JSON.parse(fs.readFileSync(new URL('./go-test-capabilities.json', import.meta.url))).linuxRootPackages
const weighted = JSON.parse(fs.readFileSync(new URL('./go-test-shard-weights.json', import.meta.url)))

function fakeGo(packages, outcomes = []) {
  const listed = []
  const tested = []
  return {
    listed, tested,
    exec(command, args) {
      assert.equal(command, 'go')
      listed.push(args)
      return args[1] === '-m' ? `${module}\n` : `${packages.join('\n')}\n`
    },
    spawn(command, args) {
      assert.equal(command, 'go')
      tested.push(args)
      return { status: outcomes[tested.length - 1] ?? 0 }
    },
  }
}

test('allocation is deterministic, disjoint, complete and sorted, including tiny and overlapping selections', () => {
  for (const size of [0, 1, 3, 4, 5, 57, 250]) {
    const packages = Array.from({ length: size }, (_, i) => `${module}/package-${i}`)
    const weights = Object.fromEntries(packages.map((name, i) => [name, (i * 7) % 31 + 1]))
    for (const total of [1, 2, 4, 16]) {
      const shards = allocateShards(packages, total, weights)
      assert.deepEqual(allocateShards([...packages].reverse(), total, weights), shards)
      assert.deepEqual(allocateShards([...packages, ...packages], total, weights), shards)
      assert.equal(new Set(shards.flat()).size, shards.flat().length)
      assert.deepEqual(shards.flat().sort(), [...packages].sort())
      for (const shard of shards) assert.deepEqual(shard, [...shard].sort())
      if (total === 1) assert.deepEqual(shards[0], [...packages].sort())
    }
  }
})

test('measured weights balance a skewed suite and never introduce packages', () => {
  const shards = allocateShards(['a', 'b', 'c', 'd', 'e'], 4, { a: 100, b: 60, c: 40, d: 20, e: 10, absent: 999 })
  assert.deepEqual(shards, [['a'], ['b'], ['c'], ['d', 'e']])
  for (const profile of Object.values(weighted.profiles)) {
    for (const value of Object.values(profile)) assert.ok(Number.isFinite(value) && value > 0)
  }
  assert.throws(() => allocateShards(['a'], 4, { a: 0 }))
  assert.throws(() => allocateShards(['a'], 4, { a: NaN }))
  assert.throws(() => allocateShards(['a'], 17))
})

test('invalid or unsafe shard arguments fail before Go runs', () => {
  const invalid = [
    ['--shard-index', '1'], ['--shard-total', '4'],
    ['--shard-index', '0', '--shard-total', '4'],
    ['--shard-index', '-1', '--shard-total', '4'],
    ['--shard-index', '5', '--shard-total', '4'],
    ['--shard-index', '1', '--shard-total', '17'],
    ['--shard-index', '1.5', '--shard-total', '4'],
    ['--shard-index', '1e0', '--shard-total', '4'],
    ['--shard-index', '9007199254740993', '--shard-total', '4'],
    ['--shard-index', '1', '--shard-index', '1', '--shard-total', '4'],
    ['--shard-index=1=2', '--shard-total=4'], ['--shard-unknown', '1'],
    ['--shard-index=', '--shard-total=4'],
  ]
  for (const options of invalid) {
    const fake = fakeGo([])
    assert.throws(() => runTests([...options, ...flags, '--', './...'], fake))
    assert.deepEqual(fake.listed, [])
    assert.deepEqual(fake.tested, [])
  }
  for (const unsafe of [[], ['-p', '4', '-count=1'], ['-p=1', '-count=2'], [...flags, '-p=4'], [...flags, '-count=2']]) {
    assert.throws(() => parseInput(['--shard-index=1', '--shard-total=4', ...unsafe, '--', './...']))
  }
  assert.throws(() => parseInput(['--shard-index', '--']))
  assert.throws(() => parseInput([...flags, '--']))
  assert.throws(() => parseInput(flags))
  assert.deepEqual(parseInput(['--shard-index=1', '--shard-total=4', ...flags, '--', './...']).shard, { index: 1, total: 4 })
})

test('unsharded Windows and other portable invocations retain Go pattern handling and flags', () => {
  for (const platform of ['win32', 'darwin']) {
    const fake = fakeGo([])
    assert.equal(runTests([...flags, '-race', '--', './...', './api/...'], { ...fake, platform }), 0)
    assert.deepEqual(fake.listed, [])
    assert.deepEqual(fake.tested, [['test', ...flags, '-race', './...', './api/...']])
    if (platform === 'win32') {
      assert.equal(process.env.TEMP, fs.realpathSync.native(os.tmpdir()))
      assert.equal(process.env.TMP, process.env.TEMP)
    }
  }
})

test('unsharded Linux retains canonical list, tags, ordinary/root routing and failure propagation', () => {
  const root = `${module}/internal/ops/privilegedbroker`
  const ordinary = `${module}/api`
  const fake = fakeGo([root, ordinary], [2, 0])
  assert.equal(runTests([...flags, '-tags', 'full', '--', './...'], { ...fake, platform: 'linux' }), 2)
  assert.deepEqual(fake.listed, [['list', '-m'], ['list', '-tags', 'full', './...']])
  assert.deepEqual(fake.tested[0], ['test', ...flags, '-tags', 'full', ordinary])
  assert.equal(fake.tested[1].at(-1), root)
  assert.match(fake.tested[1][fake.tested[1].indexOf('-exec') + 1], /go-test-root-exec\.sh/)
})

test('all Linux capability packages run exactly once via the existing root adapter across shards', () => {
  const roots = Object.keys(capabilities).map(name => `${module}/${name}`)
  const ordinary = [`${module}/api`, `${module}/database`, `${module}/service`]
  const packages = [...roots, ...ordinary]
  const executed = []
  for (let index = 1; index <= 4; index++) {
    const fake = fakeGo(packages)
    assert.equal(runTests(input(index, 4, ['-race', '-tags=full']), { ...fake, platform: 'linux' }), 0)
    assert.deepEqual(fake.listed[1], ['list', '-tags=full', '-race', './...'])
    for (const args of fake.tested) {
      assert.ok(args.includes('-race'))
      assert.ok(args.includes('-count=1'))
      assert.equal(args[args.indexOf('-p') + 1], '1')
      assert.equal(args[args.indexOf('-timeout') + 1], '30m')
      const selected = args.filter(arg => arg.startsWith(`${module}/`))
      for (const name of selected) assert.equal(args.includes('-exec'), roots.includes(name))
      executed.push(...selected)
    }
  }
  assert.deepEqual(executed.sort(), packages.sort())
})

test('Windows shards have the exact canonical union, serial flags and no root routing', () => {
  const packages = ['api', 'service', 'database', 'internal/ops/privilegedbroker', 'web'].map(name => `${module}/${name}`)
  const executed = []
  for (let index = 1; index <= 4; index++) {
    const fake = fakeGo([...packages, packages[0]])
    assert.equal(runTests(input(index, 4, ['-race']), { ...fake, platform: 'win32' }), 0)
    assert.equal(fake.tested.length, 1)
    assert.deepEqual(fake.tested[0].slice(0, 7), ['test', ...flags, '-race'])
    assert.ok(!fake.tested[0].includes('-exec'))
    executed.push(...fake.tested[0].filter(arg => arg.startsWith(`${module}/`)))
  }
  assert.deepEqual(executed.sort(), packages.sort())
})

test('single shard preserves unsharded Linux handling and Windows package selection', () => {
  const packages = ['api', 'internal/ops/privilegedbroker', 'web'].map(name => `${module}/${name}`)
  const before = fakeGo(packages)
  const after = fakeGo(packages)
  runTests([...flags, '--', './...'], { ...before, platform: 'linux' })
  runTests(input(1, 1), { ...after, platform: 'linux' })
  assert.deepEqual(after.tested, before.tested)
  const windows = fakeGo(packages)
  runTests(input(1, 1), { ...windows, platform: 'win32' })
  assert.deepEqual(windows.tested, [['test', ...flags, ...packages]])
})

test('empty shards never invoke Go with an implicit current-directory selection', () => {
  const fake = fakeGo([`${module}/api`])
  assert.equal(runTests(input(4), { ...fake, platform: 'linux' }), 0)
  assert.deepEqual(fake.tested, [])
  assert.throws(() => runTests(input(1), { ...fakeGo([]), platform: 'linux' }), /selected no packages/)
})

test('Go list/build errors and privileged failures fail closed', () => {
  assert.throws(() => runTests(input(1), { exec: () => { throw new Error('list failed') } }), /list failed/)
  const fake = fakeGo([`${module}/internal/ops/privilegedbroker`], [1])
  assert.equal(runTests(input(1, 1), { ...fake, platform: 'linux' }), 1)
  assert.throws(() => runTests([...flags, '--', './...'], { platform: 'win32', spawn: () => ({ error: new Error('build failed') }) }), /build failed/)
  assert.equal(runTests([...flags, '--', './...'], { platform: 'win32', spawn: () => ({ status: null }) }), 1)
})

test('Linux coverage still merges ordinary/root profiles and removes scratch output', () => {
  const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'go-shard-cover-test-'))
  try {
    const cover = path.join(scratch, 'coverage.out')
    const fake = fakeGo([`${module}/api`, `${module}/internal/ops/privilegedbroker`])
    const profiles = []
    const spawn = (command, args) => {
      profiles.push(args[args.indexOf('-coverprofile') + 1])
      fs.writeFileSync(profiles.at(-1), `mode: set\n${args.at(-1)}/file.go:1.1,2.1 1 1\n`)
      return fake.spawn(command, args)
    }
    assert.equal(runTests(input(1, 1, ['-coverprofile', cover]), { ...fake, spawn, platform: 'linux' }), 0)
    assert.equal(fs.readFileSync(cover, 'utf8').split('mode: set').length, 2)
    assert.match(fs.readFileSync(cover, 'utf8'), /api\/file\.go/)
    assert.match(fs.readFileSync(cover, 'utf8'), /privilegedbroker\/file\.go/)
    assert.ok(profiles.every(file => !fs.existsSync(file)))
  } finally {
    fs.rmSync(scratch, { recursive: true, force: true })
  }
})
