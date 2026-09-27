import fs from 'node:fs'
import path from 'node:path'
import { pathToFileURL } from 'node:url'

// APK has its own prerelease grammar. Product identities remain SemVer;
// only the package metadata uses this order-preserving encoding.
export function apkPackageVersion(version) {
  const match = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-beta\.([1-9][0-9]*))?$/.exec(version ?? '')
  if (!match) throw new Error('OpenWrt package requires a stable or beta.N product version')
  return `${match[1]}.${match[2]}.${match[3]}${match[4] ? `_beta${match[4]}` : ''}`
}

if (process.argv[1] && pathToFileURL(path.resolve(process.argv[1])).href === import.meta.url) {
  try {
    console.log(apkPackageVersion(fs.readFileSync(process.argv[2], 'utf8').trim()))
  } catch (error) {
    console.error(error.message)
    process.exitCode = 1
  }
}
