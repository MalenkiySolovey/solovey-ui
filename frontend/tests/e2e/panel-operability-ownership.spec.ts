import { expect, test } from '@playwright/test'
import fs from 'node:fs'
import path from 'node:path'

import { repoRoot } from './helpers'

test('neutral panel operability does not name optional components', () => {
  const panelOperabilityPath = path.join(repoRoot, 'frontend', 'tests', 'e2e', 'panel-operability.spec.ts')
  const panelOperabilitySource = fs.readFileSync(panelOperabilityPath, 'utf8')
  const componentsRoot = path.join(repoRoot, 'components')

  for (const entry of fs.readdirSync(componentsRoot, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue
    const manifestPath = path.join(componentsRoot, entry.name, 'component.json')
    if (!fs.existsSync(manifestPath)) continue
    const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8')) as { id?: unknown }
    expect(typeof manifest.id, `component manifest id: ${manifestPath}`).toBe('string')
    expect(
      panelOperabilitySource.includes(manifest.id as string),
      `neutral panel operability must not name component ${String(manifest.id)}`,
    ).toBe(false)
  }

  expect(panelOperabilitySource).not.toContain('api/components/')
})
