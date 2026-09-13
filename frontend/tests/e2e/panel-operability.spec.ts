import { expect, test } from '@playwright/test'

import {
  runRegisteredComponentOperability,
  successfulObject,
  type CatalogComponent,
} from './component-operability'
import { login } from './helpers'

type CatalogInventory = {
  installed: CatalogComponent[]
}

test('authenticated panel composition remains operable through its production owners', async ({ page }) => {
  test.setTimeout(180_000)

  const componentWarnings: string[] = []
  const pageErrors: string[] = []
  const missingScripts: string[] = []
  page.on('console', message => {
    if (message.type() === 'warning' && message.text().includes('[componentSystem]')) {
      componentWarnings.push(message.text())
    }
  })
  page.on('pageerror', error => pageErrors.push(error.message))
  page.on('response', response => {
    if (response.status() === 404 && response.request().resourceType() === 'script') {
      missingScripts.push(response.url())
    }
  })

  const realtimeToken = page.waitForResponse(response => (
    response.request().method() === 'POST' && response.url().endsWith('/api/realtime/ws-token')
  ))
  const realtimeSocket = page.waitForEvent('websocket', {
    predicate: socket => socket.url().includes('/api/realtime/ws'),
  })

  await login(page)
  const realtimeTokenResponse = await realtimeToken
  const realtimeTokenBody = await realtimeTokenResponse.text()
  expect(
    realtimeTokenResponse.ok(),
    `realtime token status=${realtimeTokenResponse.status()} body=${realtimeTokenBody}`,
  ).toBeTruthy()
  await realtimeSocket

  const inventory = await successfulObject<CatalogInventory>(
    await page.request.get('api/update/components'),
  )
  const installed = inventory.installed.filter(component => component.installed)
  expect(installed.length).toBeGreaterThan(0)

  await page.goto('settings')
  await page.getByRole('tab', { name: 'Maintenance' }).click()
  await expect(page.getByText('Installed components')).toBeVisible()
  for (const component of installed) {
    await expect(page.getByText(component.id, { exact: false }).first()).toBeVisible()
  }

  await runRegisteredComponentOperability(page, installed)

  await page.goto('ssh-management')
  await expect(page.getByRole('heading', { name: 'SSH and management recovery' })).toBeVisible()

  await page.goto('operations')
  await expect(page.getByRole('heading', { name: 'Operations' })).toBeVisible()
  await expect(page.getByText('Bounded ResourcePressureV1 posture', { exact: true })).toBeVisible()

  await page.goto('security')
  await expect(page.getByRole('heading', { name: 'Account security' })).toBeVisible()
  await expect(page.getByText('Trusted proxies')).toBeVisible()

  await page.goto('settings')
  await page.getByRole('tab', { name: 'Maintenance' }).click()
  await expect(page.getByText('Config Doctor').first()).toBeVisible()
  await page.getByRole('button', { name: 'Run Doctor' }).first().click()
  await expect(page.getByText(/Build sing-box config|Dry config check|sing-box core/).first()).toBeVisible()

  await page.goto('deployment')
  await expect(page.getByText('Deployment doctor')).toBeVisible()
  const deploymentDoctorResponse = await page.request.get('api/v1/operations/deployment/doctor')
  const deploymentDoctorBody = await deploymentDoctorResponse.json()
  expect(deploymentDoctorResponse.ok(), deploymentDoctorBody.msg).toBeTruthy()
  if (!deploymentDoctorBody.success) {
    expect(deploymentDoctorBody.msg).toBe('deployment_provider_unavailable')
  }
  expect(deploymentDoctorBody.msg).not.toBe('request_budget_rejected')

  expect(componentWarnings).toEqual([])
  expect(pageErrors).toEqual([])
  expect(missingScripts).toEqual([])
})
