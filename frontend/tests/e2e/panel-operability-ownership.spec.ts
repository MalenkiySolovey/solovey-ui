import { expect, test } from '@playwright/test'
import fs from 'node:fs'
import path from 'node:path'

import { frontendRoot } from './helpers'
import { readFrontendBuildInputs } from '../../scripts/component-build-inputs.mjs'

test('neutral panel operability does not name optional components', () => {
  const panelOperabilityPath = path.join(frontendRoot, 'tests/e2e/panel-operability.spec.ts')
  const panelOperabilitySource = fs.readFileSync(panelOperabilityPath, 'utf8')
  for (const component of readFrontendBuildInputs(frontendRoot).entries) {
    expect(typeof component.id, 'component input id').toBe('string')
    expect(
      panelOperabilitySource.includes(component.id as string),
      `neutral panel operability must not name component ${String(component.id)}`,
    ).toBe(false)
  }

  expect(panelOperabilitySource).not.toContain('api/components/')
})
