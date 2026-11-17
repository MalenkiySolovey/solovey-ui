import { expect, test, type Locator, type Request } from '@playwright/test'
import { login, readServerState } from './helpers'

const isAsset = (url: string) => /\/assets\/.*\.(?:js|css)(?:\?|$)/.test(url)

for (const mode of ['classic', 'nexus']) {
  test(`${mode} loads modules and styles after direct deep links and refresh`, async ({ page }) => {
    await page.addInitScript(selected => window.localStorage.setItem('sui:ui:mode', selected), mode)
    const failures: string[] = []
    const pendingAssets = new Set<Request>()
    page.on('pageerror', error => failures.push(error.message))
    page.on('request', request => {
      if (isAsset(request.url())) pendingAssets.add(request)
    })
    page.on('requestfinished', request => pendingAssets.delete(request))
    page.on('requestfailed', request => {
      pendingAssets.delete(request)
      if (isAsset(request.url())) {
        failures.push(`${request.url()}: ${request.failure()?.errorText}`)
      }
    })
    page.on('response', response => {
      if (isAsset(response.url()) &&
        (!response.ok() || response.headers()['content-type']?.includes('text/html'))) {
        failures.push(`${response.status()} ${response.headers()['content-type']} ${response.url()}`)
      }
    })
    const expectReady = async (content: Locator) => {
      await expect(page.getByTestId('host-context')).toContainText('Host: Local')
      await expect(content).toBeVisible()
      expect(await page.evaluate(() => document.baseURI)).toBe(readServerState().baseURL)
      await expect.poll(() => [...pendingAssets].map(request => request.url()), {
        message: 'finish every module and stylesheet before leaving the document',
      }).toEqual([])
    }

    await login(page)
    // The login URL changes before the nested async Nexus Overview mounts.
    // Let that document finish loading before a hard navigation cancels its imports.
    await expectReady(mode === 'nexus'
      ? page.locator('.nexus-overview')
      : page.getByRole('button', { name: 'Tiles', exact: true }))
    for (const { route, content } of [
      { route: 'hosts/local/clients', content: page.getByRole('button', { name: 'Add', exact: true }).first() },
      { route: 'hosts/local/dns', content: page.getByRole('button', { name: 'Add Dns Server', exact: true }) },
      { route: 'hosts/local/settings', content: page.getByRole('tab', { name: 'Interface', exact: true }) },
    ]) {
      await page.goto(route)
      await expectReady(content)
      await page.reload()
      await expectReady(content)
    }
    expect(failures, 'every deep link must load real JavaScript/CSS without a blank page').toEqual([])
  })
}
