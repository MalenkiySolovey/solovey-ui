import fs from 'node:fs'
import process from 'node:process'

// Reject mislabeled artifacts at the build owner, before compiler execution.
const targets = JSON.parse(fs.readFileSync(new URL('./release-targets.json', import.meta.url), 'utf8')).linux
const [platform, naive] = process.argv.slice(2)
const target = targets.find(value => value.platform === platform)
if (!target || process.env.GOOS !== 'linux' || process.env.GOARCH !== target.arch ||
    (target.goarm && process.env.GOARM !== target.goarm) || naive !== String(target.naive)) {
  console.error('release target does not match canonical architecture/GOARM/naive contract')
  process.exitCode = 1
}
