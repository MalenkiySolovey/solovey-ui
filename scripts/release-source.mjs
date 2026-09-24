// Content identity includes installer, build/release scripts and workflow inputs,
// unlike the narrower OpenWrt package fingerprint. Generated/ignored outputs do
// not enter source authority. No checkout-root or timestamp enters the digest.
import crypto from 'node:crypto'
import fs from 'node:fs'
import path from 'node:path'
import { execFileSync } from 'node:child_process'

const source = path.resolve(process.argv[2] ?? '.')
const output = process.argv[3] ? path.resolve(process.argv[3]) : null
const paths = execFileSync('git', ['ls-files', '-z', '--cached', '--others', '--exclude-standard'], { cwd: source, encoding: 'utf8', maxBuffer: 16 * 1024 * 1024 }).split('\0').filter(Boolean)
const records = []
const entries = execFileSync('git', ['ls-files', '--stage', '-z'], { cwd: source, encoding: 'utf8', maxBuffer: 16 * 1024 * 1024 }).split('\0').filter(Boolean)
const modes = new Map(entries.map(entry => [entry.slice(entry.indexOf('\t') + 1), entry.slice(0, 6)]))
for (const name of [...new Set(paths)].sort()) {
  // The embed directory is replaced by the frontend build. Its tracked empty
  // checkout placeholders are not build inputs; frontend sources are inventoried.
  if (name.startsWith('web/html/')) continue
  const file = path.join(source, name)
  if (!fs.existsSync(file)) continue // a tracked deletion is absent from the new tree
  if (!fs.lstatSync(file).isFile()) throw new Error(`release source must be a regular file: ${name}`)
  let bytes = fs.readFileSync(file)
  // Match this repository's text=auto/eol=lf checkout contract. Windows working
  // copies may still carry CRLF; those presentation bytes do not enter a tag.
  if (!bytes.subarray(0, 8000).includes(0)) bytes = Buffer.from(bytes.toString('latin1').replace(/\r\n/g, '\n'), 'latin1')
  records.push({ path: name, mode: modes.get(name) ?? '100644', size: bytes.length, sha256: crypto.createHash('sha256').update(bytes).digest('hex') })
}
if (records.length === 0) throw new Error('empty release source inventory')
const sourceFingerprint = crypto.createHash('sha256').update(JSON.stringify(records)).digest('hex')
const identity = { schema: 'solovey.release-source/v1', sourceFingerprint, files: records }
if (output) fs.writeFileSync(output, `${JSON.stringify(identity, null, 2)}\n`)
console.log(sourceFingerprint)
