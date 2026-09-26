// Shared CI, audit and release test executor. Package selection is preserved;
// Linux capability requirements change execution identity, never assertions.
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFileSync, spawnSync } from 'node:child_process'

const here = path.dirname(fileURLToPath(import.meta.url))
const input = process.argv.slice(2)
const separator = input.indexOf('--')
if (separator < 0) throw new Error('usage: go-test.mjs [go test flags] -- <package patterns>')
const flags = input.slice(0, separator)
const patterns = input.slice(separator + 1)
if (!patterns.length) throw new Error('explicit package selection required')
const run = args => {
  const result = spawnSync('go', ['test', ...args], { stdio: 'inherit' })
  if (result.error) throw result.error
  return result.status ?? 1
}
if (process.platform === 'win32') {
  // Fixtures bind exact canonical paths. Windows TEMP may be an 8.3 alias;
  // supply the real directory spelling before Go creates any fixture paths.
  const temporary = fs.realpathSync.native(os.tmpdir())
  process.env.TEMP = temporary
  process.env.TMP = temporary
}
if (process.platform !== 'linux') process.exit(run([...flags, ...patterns]))
const { linuxRootPackages } = JSON.parse(fs.readFileSync(path.join(here, 'go-test-capabilities.json'), 'utf8'))
const listFlags = []
for (let i = 0; i < flags.length; i++) {
  if (flags[i] === '-tags') listFlags.push(flags[i], flags[++i])
  else if (flags[i].startsWith('-tags=')) listFlags.push(flags[i])
}
const module = execFileSync('go', ['list', '-m'], { encoding: 'utf8' }).trim()
const packages = execFileSync('go', ['list', ...listFlags, ...patterns], { encoding: 'utf8' }).trim().split(/\r?\n/)
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
process.exit(status)
