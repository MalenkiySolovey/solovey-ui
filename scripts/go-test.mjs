// Shared CI, audit and release test executor. Package selection is preserved;
// Linux capability requirements change execution identity, never assertions.
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFileSync, spawnSync } from 'node:child_process'

const here = path.dirname(fileURLToPath(import.meta.url))
// Shard coordinates are one-based and deliberately bounded. They are executor
// options, never Go flags; malformed coordinates fail before invoking Go.
export function parseInput(input) {
  const separator = input.indexOf('--')
  if (separator < 0) throw new Error('usage: go-test.mjs [--shard-index N --shard-total M] [go test flags] -- <package patterns>')
  const flags = []
  const shard = {}
  for (let i = 0; i < separator; i++) {
    const value = input[i]
    if (!value.startsWith('--shard-')) { flags.push(value); continue }
    const [option, inline] = value.split('=')
    if (!['--shard-index', '--shard-total'].includes(option) || value.split('=').length > 2) throw new Error('unknown shard option')
    const key = option.slice('--shard-'.length)
    if (Object.hasOwn(shard, key)) throw new Error('duplicate shard option')
    const number = inline ?? (i + 1 < separator ? input[++i] : '')
    if (!/^[1-9][0-9]*$/.test(number)) throw new Error('shard coordinates must be positive integers')
    shard[key] = Number(number)
  }
  const patterns = input.slice(separator + 1)
  if (!patterns.length) throw new Error('explicit package selection required')
  if (Object.keys(shard).length) {
    if (!Number.isSafeInteger(shard.index) || !Number.isSafeInteger(shard.total) || shard.total > 16 || shard.index > shard.total) throw new Error('shard index/total required: 1 <= index <= total <= 16')
    for (const option of ['-p', '-count']) {
      const values = flags.flatMap((flag, i) => flag === option ? [flags[i + 1]] : flag.startsWith(`${option}=`) ? [flag.slice(option.length + 1)] : [])
      if (!values.length || values.some(value => value !== '1')) throw new Error(`sharded execution requires ${option} 1`)
    }
  }
  return { flags, patterns, shard: Object.keys(shard).length ? shard : undefined }
}

// Longest-first placement with stable lexical ties. Weights only affect
// allocation; go list remains the sole authority for package membership.
export function allocateShards(packages, total, weights = {}) {
  if (!Number.isInteger(total) || total < 1 || total > 16) throw new Error('invalid shard total')
  const names = [...new Set(packages)].sort()
  const weight = name => {
    const value = weights[name] ?? 1
    if (!Number.isFinite(value) || value <= 0) throw new Error(`invalid package weight: ${name}`)
    return value
  }
  const shards = Array.from({ length: total }, () => [])
  const loads = Array(total).fill(0)
  for (const name of [...names].sort((a, b) => weight(b) - weight(a) || (a < b ? -1 : a > b ? 1 : 0))) {
    const index = loads.indexOf(Math.min(...loads))
    shards[index].push(name)
    loads[index] += weight(name)
  }
  return shards.map(selected => selected.sort())
}

export function runTests(input, { platform = process.platform, exec = execFileSync, spawn = spawnSync } = {}) {
  const { flags, patterns, shard } = parseInput(input)
  const run = args => {
    const result = spawn('go', ['test', ...args], { stdio: 'inherit' })
    if (result.error) throw result.error
    return result.status ?? 1
  }
  if (platform === 'win32') {
    // Fixtures bind exact canonical paths. Windows TEMP may be an 8.3 alias;
    // supply the real directory spelling before Go creates any fixture paths.
    const temporary = fs.realpathSync.native(os.tmpdir())
    process.env.TEMP = temporary
    process.env.TMP = temporary
  }
  if (platform !== 'linux' && !shard) return run([...flags, ...patterns])
  const listFlags = []
  for (let i = 0; i < flags.length; i++) {
    if (flags[i] === '-tags') listFlags.push(flags[i], flags[++i])
    else if (flags[i].startsWith('-tags=')) listFlags.push(flags[i])
  }
  // Race is also a build constraint: enumerate race-only packages when sharding
  // a race suite. The existing unsharded Linux invocation remains unchanged.
  if (shard) listFlags.push(...flags.filter(flag => flag === '-race' || flag.startsWith('-race=')))
  const module = exec('go', ['list', '-m'], { encoding: 'utf8' }).trim()
  let packages = exec('go', ['list', ...listFlags, ...patterns], { encoding: 'utf8' }).trim().split(/\r?\n/).filter(Boolean)
  if (!packages.length) throw new Error('go list selected no packages')
  if (shard) {
    const config = JSON.parse(fs.readFileSync(path.join(here, 'go-test-shard-weights.json'), 'utf8'))
    const profile = `${platform}${flags.includes('-race') || flags.includes('-race=true') ? '-race' : ''}`
    const weights = Object.fromEntries(Object.entries(config.profiles[profile] ?? {}).map(([name, value]) => [`${module}/${name}`, value]))
    const universe = [...new Set(packages)].sort()
    packages = allocateShards(universe, shard.total, weights)[shard.index - 1]
    console.log(`[go-test] shard ${shard.index}/${shard.total}: ${packages.length}/${universe.length} packages`)
    for (const name of packages) console.log(`[go-test] selected: ${name}`)
    // An empty shard is an explicit no-op; never pass an empty selection to Go,
    // where it would silently run the current directory's package again.
    if (!packages.length) return 0
  }
  if (platform !== 'linux') return run([...flags, ...packages])
  const { linuxRootPackages } = JSON.parse(fs.readFileSync(path.join(here, 'go-test-capabilities.json'), 'utf8'))
  const privileged = packages.filter(name => Object.hasOwn(linuxRootPackages, name.slice(module.length + 1)))
  const ordinary = packages.filter(name => !privileged.includes(name))
  let cover
  for (let i = 0; i < flags.length; i++) {
    if (flags[i] === '-coverprofile') { cover = flags[i + 1]; flags.splice(i, 2); break }
    if (flags[i].startsWith('-coverprofile=')) { cover = flags[i].split('=')[1]; flags.splice(i, 1); break }
  }
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'solovey-go-test-'))
  const profiles = []
  let status = 0
  try {
    for (const [name, selected] of [['ordinary', ordinary], ['root', privileged]]) {
      if (!selected.length) continue
      console.log(`[go-test] ${name}: ${selected.length} packages`)
      const args = [...flags]
      if (cover) {
        const profile = path.join(temp, `${name}.cover`)
        profiles.push(profile)
        args.push('-coverprofile', profile)
      }
      if (name === 'root') args.push('-exec', `bash '${path.join(here, 'go-test-root-exec.sh')}'`)
      status = run([...args, ...selected]) || status
    }
    if (cover) {
      let mode
      const rows = []
      for (const profile of profiles) {
        if (!fs.existsSync(profile)) { status = 1; continue }
        const [header, ...body] = fs.readFileSync(profile, 'utf8').trimEnd().split('\n')
        if (mode && header !== mode) throw new Error('coverage modes differ')
        mode = header
        rows.push(...body)
      }
      if (mode) fs.writeFileSync(cover, `${mode}\n${rows.join('\n')}\n`)
    }
  } finally {
    fs.rmSync(temp, { recursive: true, force: true })
  }
  return status
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) process.exit(runTests(process.argv.slice(2)))
