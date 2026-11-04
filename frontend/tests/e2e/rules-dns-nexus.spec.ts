import { expect, test } from '@playwright/test'

import { login } from './helpers'

// The Rules/DNS entity forms now render via FormShell (drawer in Nexus). Their
// views still mount those modals closed, so verify the pages render (the shell
// sidebar stays present => no blank-page crash from a closed FormShell drawer).
test('nexus rules and dns pages render without blanking', async ({ page }) => {
  test.setTimeout(60_000)

  await login(page)

  await page.goto('rules')
  await expect(page.getByRole('link', { name: 'Inbounds' })).toBeVisible()

  await page.goto('dns')
  await expect(page.getByRole('link', { name: 'Inbounds' })).toBeVisible()
})

for (const mode of ['classic', 'nexus']) test(`${mode} DNS exposes mandatory TLS and only available rule action fields`, async ({ page }) => {
  test.setTimeout(60_000)
  await page.addInitScript(layout => window.localStorage.setItem('sui:ui:mode', layout), mode)
  await login(page)
  const response = await page.request.get('api/editor-contract')
  const contract = await response.json()
  expect(contract.success).toBe(true)
  const capabilities = await (await page.request.get('api/capabilities')).json()
  expect(capabilities.success).toBe(true)
  await page.goto('dns')
  await page.getByRole('button', { name: 'Add Dns Server', exact: true }).first().click()
  let form = page.getByRole('dialog').filter({ hasText: 'Add DNS Server' })
  for (const type of ['tls', 'quic', 'https', 'h3']) {
    await form.getByRole('combobox', { name: 'Type', exact: true }).press('Enter')
    const option = page.getByRole('option', { name: type, exact: true })
    const available = capabilities.obj.facts.some((fact: { category: string; type: string; available: boolean }) => fact.category === 'dns' && fact.type === type && fact.available)
    if (!available) {
      await expect(option).toHaveCount(0)
      await page.keyboard.press('Escape')
      continue
    }
    await option.click()
    await expect(form.getByRole('checkbox', { name: 'Disable SNI', exact: true })).toBeVisible()
    await expect(form.getByRole('checkbox', { name: 'Enable TLS', exact: true })).toHaveCount(0)
  }
  await form.getByRole('button', { name: 'Close', exact: true }).first().click()
  await page.getByRole('button', { name: 'Discard', exact: true }).click()
  await page.getByRole('button', { name: 'Add Dns Rule', exact: true }).first().click()
  form = page.getByRole('dialog').filter({ hasText: 'Add DNS Rule' })
  const fields = contract.obj.dnsActions.route ?? []
  for (const [key, label] of [
    ['race', 'Race with following rules'],
    ['speculative', 'Speculative evaluation'],
    ['disable_optimistic_cache', 'Disable optimistic cache'],
  ]) {
    const control = form.getByRole('checkbox', { name: label, exact: true })
    if (fields.includes(key)) await expect(control).toBeVisible()
    else await expect(control).toHaveCount(0)
  }
  await form.getByRole('combobox', { name: 'Action', exact: true }).press('Enter')
  for (const action of ['evaluate', 'respond']) {
    const option = page.getByRole('option', { name: action, exact: true })
    if (Object.hasOwn(contract.obj.dnsActions, action)) await expect(option).toBeVisible()
    else await expect(option).toHaveCount(0)
  }
})
