import process from 'node:process'
import { linuxBuildTargets } from './release-providers.mjs'

// Reject mislabeled artifacts at the build owner, before compiler execution.
const [platform, naive, provider = 'sing-box'] = process.argv.slice(2)
const target = linuxBuildTargets().find(value => value.platform === platform && value.provider === provider)
if (!target || process.env.GOOS !== 'linux' || process.env.GOARCH !== target.arch ||
    (target.goarm && process.env.GOARM !== target.goarm) || naive !== String(target.naive)) {
  console.error('release target does not match canonical architecture/GOARM/naive contract')
  process.exitCode = 1
}
