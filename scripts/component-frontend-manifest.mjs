import fs from 'node:fs'
import path from 'node:path'

const componentIdPattern = /^[a-z0-9-]+$/
const frontendEntryPattern = /^(src|frontend)\/[A-Za-z0-9_./-]+\.(ts|vue)$/

export function readComponentFrontendEntries(componentsDir) {
  if (!fs.existsSync(componentsDir)) {
    throw new Error(`components directory not found: ${componentsDir}`)
  }

  const entriesByComponent = {}
  for (const entry of fs.readdirSync(componentsDir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
    if (!entry.isDirectory()) continue

    const manifestPath = path.join(componentsDir, entry.name, 'component.json')
    if (!fs.existsSync(manifestPath)) continue

    const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'))
    if (!componentIdPattern.test(manifest.id ?? '')) {
      throw new Error(`component manifest has invalid id: ${manifestPath}`)
    }
    if (manifest.id !== entry.name) {
      throw new Error(`component manifest id ${manifest.id} does not match directory ${entry.name}`)
    }

    const entries = manifest.frontend?.entries ?? []
    if (!Array.isArray(entries)) {
      throw new Error(`component ${manifest.id} frontend.entries must be an array`)
    }
    if (entries.length === 0) {
      const frontendDir = path.join(componentsDir, entry.name, 'frontend')
      if (containsFrontendSource(frontendDir)) {
        throw new Error(`component ${manifest.id} has frontend source but does not declare frontend.entries`)
      }
      continue
    }

    for (const frontendEntry of entries) {
      if (typeof frontendEntry !== 'string' || !frontendEntryPattern.test(frontendEntry)) {
        throw new Error(`component ${manifest.id} has invalid frontend entry: ${frontendEntry}`)
      }
    }
    entriesByComponent[manifest.id] = entries
  }

  return entriesByComponent
}

export function flattenComponentFrontendEntries(entriesByComponent) {
  return Object.values(entriesByComponent).flat()
}

function containsFrontendSource(directory) {
  if (!fs.existsSync(directory)) return false
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const candidate = path.join(directory, entry.name)
    if (entry.isDirectory() && containsFrontendSource(candidate)) return true
    if (entry.isFile() && /\.(ts|vue)$/.test(entry.name) && !/\.(spec|test)\.ts$/.test(entry.name)) return true
  }
  return false
}
