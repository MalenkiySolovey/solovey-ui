import { expect, test } from '@playwright/test'
import { csrfToken, enableComponent, login, mutationHeaders, readServerState } from './helpers'

for (const mode of ['classic', 'nexus']) {
  test(`${mode} projects the real Component route from permissions and enabled state`, async ({ page }) => {
    await page.addInitScript(selected => window.localStorage.setItem('sui:ui:mode', selected), mode)
    await login(page)
    await enableComponent(page, 'observability-extra')
    await page.goto('hosts/local/diagnostics')
    await expect(page.locator('.diagnostics')).toBeVisible()
    await expect(page.locator('a[href$="/hosts/local/diagnostics"]').first()).toHaveCount(1)
    await expect(page.getByTestId('host-context')).toContainText('Host: Local')

    const postureURL = new URL('api/v1/security/posture', readServerState().baseURL).toString()
    await page.route(postureURL, async route => {
      const response = await route.fetch()
      const body = await response.json()
      body.obj.componentUIReadPermissions = ['unknown.permission']
      await route.fulfill({ response, json: body })
    })
    await page.reload()
    await expect(page.getByText('This feature is unavailable on this Host. Check the installed Components in Settings.')).toBeVisible()
    await expect(page.locator('.diagnostics')).toHaveCount(0)
    await expect(page.locator('a[href$="/hosts/local/diagnostics"]')).toHaveCount(0)
    await page.unroute(postureURL)

    const token = await csrfToken(page)
    const disabled = await page.request.post('api/update/components/observability-extra/disable', { headers: mutationHeaders(token) })
    expect(disabled.ok()).toBe(true)
    expect((await disabled.json()).success).toBe(true)
    await page.reload()
    await expect(page.getByText('This feature is unavailable on this Host. Check the installed Components in Settings.')).toBeVisible()
    await expect(page.locator('a[href$="/hosts/local/diagnostics"]')).toHaveCount(0)
  })
}
