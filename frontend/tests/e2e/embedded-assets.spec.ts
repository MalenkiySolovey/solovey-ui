import { expect, test } from '@playwright/test'
import { login, readServerState } from './helpers'

for (const mode of ['classic', 'nexus']) {
  test(`${mode} loads modules and styles after direct deep links and refresh`, async ({ page }) => {
    await page.addInitScript(selected => window.localStorage.setItem('sui:ui:mode', selected), mode)
    const failures: string[] = []
    page.on('pageerror', error => failures.push(error.message))
    page.on('requestfailed', request => {
      if (/\/assets\/.*\.(?:js|css)(?:\?|$)/.test(request.url())) {
        failures.push(`${request.url()}: ${request.failure()?.errorText}`)
      }
    })
    page.on('response', response => {
      if (/\/assets\/.*\.(?:js|css)(?:\?|$)/.test(response.url()) &&
        (!response.ok() || response.headers()['content-type']?.includes('text/html'))) {
        failures.push(`${response.status()} ${response.headers()['content-type']} ${response.url()}`)
      }
    })
    await login(page)
    for (const route of ['hosts/local/clients', 'hosts/local/dns', 'hosts/local/settings']) {
      await page.goto(route)
      await expect(page.getByTestId('host-context')).toContainText('Host: Local')
      expect(await page.evaluate(() => document.baseURI)).toBe(readServerState().baseURL)
      await page.reload()
      await expect(page.getByTestId('host-context')).toContainText('Host: Local')
    }
    expect(failures, 'every deep link must load real JavaScript/CSS without a blank page').toEqual([])
  })
}
