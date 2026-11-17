import { expect, test, type APIResponse, type Page } from '@playwright/test'
import fs from 'node:fs'
import path from 'node:path'
import { pathToFileURL } from 'node:url'

import { enableComponent, frontendRoot } from './helpers'

export type CatalogComponent = {
  id: string
  installed: boolean
  active: boolean
}

type ComponentOperabilityContext = {
  component: CatalogComponent
  page: Page
  expect: typeof expect
  successfulObject: typeof successfulObject
}

type ComponentOperabilityScenario = (context: ComponentOperabilityContext) => Promise<void>

const scenarioRelativePath = path.join('frontend', 'tests', 'e2e', 'panel-operability.mjs')

export const successfulObject = async <T>(response: APIResponse) => {
  const body = await response.json().catch(() => ({ success: false, msg: response.statusText() }))
  expect(response.ok(), body.msg).toBeTruthy()
  expect(body.success, body.msg).toBe(true)
  return body.obj as T
}

export const runRegisteredComponentOperability = async (
  page: Page,
  catalog: CatalogComponent[],
) => {
  const installedByID = new Map(
    catalog.filter(component => component.installed).map(component => [component.id, component]),
  )
  const scenarios = await discoverComponentOperabilityScenarios()

  for (const [componentID, scenario] of scenarios) {
    const component = installedByID.get(componentID)
    expect(
      component,
      `component-owned operability scenario requires installed registry entry ${componentID}`,
    ).toBeDefined()

    await test.step(`component-owned operability: ${componentID}`, async () => {
      if (!component!.active) {
        await enableComponent(page, componentID)
        component!.active = true
      }
      await scenario({ component: component!, page, expect, successfulObject })
    })
  }
}

const discoverComponentOperabilityScenarios = async () => {
  const { readFrontendBuildInputs } = await import(pathToFileURL(path.join(frontendRoot, 'scripts/component-build-inputs.mjs')).href)
  const inputs = readFrontendBuildInputs(frontendRoot)
  const scenarios = new Map<string, ComponentOperabilityScenario>()
  for (const component of inputs.entries) {
    const scenarioPath = path.join(component.root, scenarioRelativePath)
    if (!fs.existsSync(scenarioPath)) continue
    const contribution = await import(pathToFileURL(scenarioPath).href) as { run?: unknown }
    if (typeof contribution.run !== 'function') {
      throw new Error(`component operability scenario must export run(): ${scenarioPath}`)
    }
    scenarios.set(component.id, contribution.run as ComponentOperabilityScenario)
  }

  return scenarios
}
