import { expect, test } from '@playwright/test'
import { login, readServerState, setEnglishLocale } from './helpers'

test('Local bookmarks redirect, survive refresh, and reject an unavailable Host', async ({ page }) => {
  await login(page)
  await page.goto('inbounds?source=bookmark#details')
  await expect(page).toHaveURL(/\/hosts\/local\/inbounds\?source=bookmark#details$/)
  await expect(page.getByTestId('host-context')).toContainText('Host: Local')
  await page.reload()
  await expect(page.getByTestId('host-context')).toContainText('Host: Local')
  await page.goto('hosts/unavailable-example/inbounds')
  await expect(page.getByText('This Host is unavailable. Only Local is supported.')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Add', exact: true })).toHaveCount(0)
  const response = await page.request.get('api/inbounds', { headers: { 'X-Solovey-Host': 'unavailable-example' } })
  expect(response.status()).toBe(404)

  const localTab = await page.context().newPage()
  const localHeaders: string[] = []
  const apiPath = new URL('api/', readServerState().baseURL).pathname
  localTab.on('request', request => {
    if (new URL(request.url()).pathname.startsWith(apiPath)) {
      localHeaders.push(request.headers()['x-solovey-host'])
    }
  })
  await localTab.goto(new URL('hosts/local/dns', readServerState().baseURL).toString())
  await expect(localTab.getByTestId('host-context')).toContainText('Host: Local')
  await expect(page).toHaveURL(/\/hosts\/unavailable-example\/inbounds$/)
  await expect(page.getByText('This Host is unavailable. Only Local is supported.')).toBeVisible()
  expect(localHeaders.length).toBeGreaterThan(0)
  expect(localHeaders.every(host => host === 'local')).toBe(true)
  await localTab.close()
})

test('authentication returns to the explicit Local deep link', async ({ page }) => {
  const state = readServerState()
  await setEnglishLocale(page)
  await page.goto('hosts/local/dns?source=login-bookmark#servers')
  await expect(page).toHaveURL(/\/login\?returnTo=/)
  await page.locator('input').nth(0).fill(state.username)
  await page.locator('input').nth(1).fill(state.password)
  await page.locator('button[type="submit"]').click()
  await expect(page).toHaveURL(new URL('hosts/local/dns?source=login-bookmark#servers', state.baseURL).toString())
  await expect(page.getByTestId('host-context')).toContainText('Host: Local')
})

for (const mode of ['classic', 'nexus']) {
  test(`${mode} keeps the Host visible and a dirty client draft when navigation is cancelled`, async ({ page }) => {
    await page.addInitScript(selected => window.localStorage.setItem('sui:ui:mode', selected), mode)
    await login(page)
    await page.goto('hosts/local/clients')
    await expect(page.getByTestId('host-context')).toContainText('Host: Local')
    await page.getByRole('button', { name: 'Add', exact: true }).first().click()
    const editor = page.getByRole('dialog').filter({ hasText: 'Add Client' })
    await editor.getByRole('textbox').first().fill('r5-unsaved-client')
    page.once('dialog', dialog => dialog.dismiss())
    await page.goto(new URL('hosts/unavailable-example/clients', readServerState().baseURL).toString()).catch(() => undefined)
    await expect(page).toHaveURL(/\/hosts\/local\/clients$/)
    await expect(editor.getByRole('textbox').first()).toHaveValue('r5-unsaved-client')
  })
}
