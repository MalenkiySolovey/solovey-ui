import { expect, test } from '@playwright/test'

import { login } from './helpers'

test('nexus failover editor renders group fields and hides server fields', async ({ page }) => {
  test.setTimeout(60_000)

  await login(page)
  await page.goto('outbounds')
  await page.getByRole('button', { name: 'Add', exact: true }).first().click()

  const drawer = page.getByRole('dialog')
  await expect(drawer).toContainText('Add Outbound')
  await drawer.locator('.v-select').filter({ hasText: 'Type' }).first().click()
  await page.getByRole('option', { name: 'Failover', exact: true }).click()

  await expect(drawer).toContainText('Failover Group')
  await expect(drawer).toContainText('Priority Outbounds')
  await expect(drawer).toContainText('Probe URL')
  await expect(drawer.getByRole('button', { name: 'Add Outbound' })).toBeVisible()
  await expect(drawer.getByLabel('Server Address')).toHaveCount(0)
  await expect(drawer.getByLabel('Server Port')).toHaveCount(0)
})
