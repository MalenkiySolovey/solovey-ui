import { expect, test } from '@playwright/test'
import { csrfToken, login } from './helpers'

test('settings path conflict is rejected by the local panel', async ({ page }) => {
  await login(page)
  const token = await csrfToken(page)
  const settingsResponse = await page.request.get('api/settings')
  const settingsBody = await settingsResponse.json()
  expect(settingsBody.success).toBe(true)

  const payload = {
    ...settingsBody.obj,
    subPath: '/phase6-conflict/',
    subJsonPath: '/phase6-conflict/',
    subClashPath: '/phase6-clash/',
  }

  const response = await page.request.post('api/save', {
    headers: { 'X-CSRF-Token': token },
    form: {
      object: 'settings',
      action: 'set',
      data: JSON.stringify(payload),
    },
  })
  const body = await response.json()

  expect(body.success).toBe(false)
  expect(String(body.msg).toLowerCase()).toMatch(/path|conflict|subscription|duplicate/)
})

test('setting info labels remain readable and tooltip has opaque contrast', async ({ page }) => {
  await login(page)
  await page.goto('settings')
  await expect(page.getByRole('tab', { name: 'Interface' })).toBeVisible()

  const labels = page.locator('.setting-info-field .v-field-label--floating')
  expect(await labels.count()).toBeGreaterThan(0)
  const labelStyle = await labels.first().evaluate((element) => {
    const style = getComputedStyle(element)
    return { overflow: style.overflow, textOverflow: style.textOverflow }
  })
  expect(labelStyle).toEqual({ overflow: 'visible', textOverflow: 'clip' })

  const icons = page.locator('.setting-info-icon')
  expect(await icons.count()).toBeGreaterThan(0)
  await icons.first().click()
  const tooltip = page.locator('.setting-info-tooltip:visible')
  await expect(tooltip).toBeVisible()
  const tooltipStyle = await tooltip.evaluate((element) => {
    const style = getComputedStyle(element)
    return { background: style.backgroundColor, color: style.color, opacity: style.opacity }
  })
  expect(tooltipStyle).toEqual({
    background: 'rgb(24, 27, 33)',
    color: 'rgb(248, 250, 252)',
    opacity: '1',
  })
})
