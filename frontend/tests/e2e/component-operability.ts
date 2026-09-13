import { expect, test, type APIResponse, type Page } from '@playwright/test'
import fs from 'node:fs'
import path from 'node:path'
import { pathToFileURL } from 'node:url'

import { enableComponent, repoRoot } from './helpers'

export type CatalogComponent = {
  id: string
  installed: boolean
  active: boolean
}

type ComponentManifest = {
  id?: unknown
}

type ComponentOperabilityContext = {
  component: CatalogComponent
  page: Page
  expect: typeof expect
  successfulObject: typeof successfulObject
}

type ComponentOperabilityScenario = (context: ComponentOperabilityContext) => Promise<void>

const componentIDPattern = /^[a-z0-9-]+$/
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
  const componentsRoot = path.join(repoRoot, 'components')
  const scenarios = new Map<string, ComponentOperabilityScenario>()
  const componentDirectories = fs.readdirSync(componentsRoot, { withFileTypes: true })
    .filter(entry => entry.isDirectory())
    .sort((left, right) => left.name.localeCompare(right.name))

  for (const directory of componentDirectories) {
    const manifestPath = path.join(componentsRoot, directory.name, 'component.json')
    const scenarioPath = path.join(componentsRoot, directory.name, scenarioRelativePath)
    if (!fs.existsSync(manifestPath) || !fs.existsSync(scenarioPath)) continue

    const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8')) as ComponentManifest
    if (typeof manifest.id !== 'string' || !componentIDPattern.test(manifest.id)) {
      throw new Error(`component manifest has invalid id: ${manifestPath}`)
    }
    if (manifest.id !== directory.name) {
      throw new Error(`component manifest id ${manifest.id} does not match directory ${directory.name}`)
    }
    if (scenarios.has(manifest.id)) {
      throw new Error(`duplicate component operability scenario: ${manifest.id}`)
    }

    const contribution = await import(pathToFileURL(scenarioPath).href) as { run?: unknown }
    if (typeof contribution.run !== 'function') {
      throw new Error(`component operability scenario must export run(): ${scenarioPath}`)
    }
    scenarios.set(manifest.id, contribution.run as ComponentOperabilityScenario)
  }

  return scenarios
}
