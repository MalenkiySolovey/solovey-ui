import { expect, test, type Page } from '@playwright/test'

import { csrfToken, login, mutationHeaders } from './helpers'

const chooseSelectOption = async (page: Page, testId: string, value: string) => {
  const select = page.getByTestId(testId)
  await select.scrollIntoViewIfNeeded()
  await select.locator('.v-field').click()
  await page.getByRole('option', { name: value, exact: true }).last().click()
}

const createClient = async (page: Page, name: string) => {
  const token = await csrfToken(page)
  const response = await page.request.post('api/save', {
    headers: mutationHeaders(token),
    form: {
      object: 'clients',
      action: 'new',
      data: JSON.stringify({
        name,
        enable: true,
        inbounds: [],
        links: [
          { type: 'external', remark: 'external-test', uri: 'https://example.com/subscription' },
        ],
      }),
    },
  })
  const body = await response.json()
  expect(body.success).toBe(true)
}

test('personal ops pack doctor presets delivery and client diagnosis smoke', async ({ page }) => {
  test.setTimeout(90_000)

  await login(page)

  await page.goto('settings')
  await page.getByRole('tab', { name: 'Maintenance' }).click()
  await expect(page.getByText('Config Doctor').first()).toBeVisible()
  await page.getByRole('button', { name: 'Run Doctor' }).first().click()
  await expect(page.getByText(/Build sing-box config|Dry config check|sing-box core/).first()).toBeVisible()

  await page.goto('rules')
  await page.getByRole('button', { name: 'Regional presets' }).click()
  const presetDrawer = page.getByTestId('regional-preset-drawer')
  await expect(presetDrawer.getByText('Regional presets')).toBeVisible()
  await chooseSelectOption(page, 'regional-preset-proxy-outbound', 'direct')
  await chooseSelectOption(page, 'regional-preset-direct-outbound', 'direct')
  await page.getByTestId('regional-preset-ru-enabled').locator('input').check({ force: true })
  await page.getByRole('button', { name: 'Preview changes' }).click()
  await expect(presetDrawer.getByText('RU routing and DNS')).toBeVisible()
  await page.getByRole('button', { name: 'Apply presets' }).click()
  await expect(presetDrawer.getByText('Regional presets applied')).toBeVisible()

  const clientName = `ops-${Date.now()}`
  await createClient(page, clientName)
  await page.goto('clients')
  await expect(page.getByText(clientName)).toBeVisible()
  const row = page.locator('.nexus-data-table__row').filter({ hasText: clientName })

  await row.getByRole('button', { name: 'Diagnose' }).click()
  const diagnosis = page.getByRole('dialog').filter({ hasText: 'Client Diagnosis' })
  await expect(diagnosis).toBeVisible()
  const diagnosisReportItem = diagnosis.getByText(/Client enabled|Client inbounds|Subscription formats/).first()
  if (!(await diagnosisReportItem.isVisible().catch(() => false))) {
    await diagnosis.getByRole('button', { name: 'Run Doctor' }).click()
  }
  await expect(diagnosisReportItem).toBeVisible({ timeout: 30_000 })
  await page.keyboard.press('Escape')

  await row.getByRole('button', { name: 'Config' }).click()
  const delivery = page.getByRole('dialog').filter({ hasText: 'Delivery' })
  await expect(delivery).toBeVisible()
  await expect(delivery.getByRole('tab', { name: 'Sing-box' })).toBeVisible()
  await expect(delivery.getByLabel('Subscription URL')).toBeVisible()
})
