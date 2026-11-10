import { expect, test } from '@playwright/test'
import { csrfToken, enableComponent, login, mutationHeaders, readServerState } from './helpers'

for (const mode of ['classic', 'nexus']) {
  test(`${mode} preserves Telegram settings, bounded actions and optional contributions`, async ({ page }) => {
    test.setTimeout(120_000)
    await page.addInitScript(selected => window.localStorage.setItem('sui:ui:mode', selected), mode)
    await login(page)
    await enableComponent(page, 'telegram')
    const original = (await (await page.request.get('api/settings')).json()).obj
    const base = readServerState().baseURL
    const settingsURL = new URL('api/settings', base).toString()
    const discoveryURL = new URL('api/telegram/detect-chat', base).toString()
    const testURL = new URL('api/telegram/test', base).toString()
    const postureURL = new URL('api/v1/security/posture', base).toString()
    try {
      await page.route(settingsURL, route => route.fulfill({ json: { success: false, msg: 'fixture settings unavailable', obj: null } }))
      await page.goto('telegram')
      await expect(page.locator('html')).toHaveAttribute('data-ui-mode', mode)
      await expect(page.locator('a[href$="/telegram"]').first()).toBeVisible()
      await expect(page.getByText('Saved settings are unavailable.', { exact: false })).toBeVisible()
      await expect(page.getByRole('button', { name: 'Test', exact: true })).toBeDisabled()
      await page.unroute(settingsURL)
      await page.getByRole('button', { name: 'Reload settings', exact: true }).click()
      const threshold = page.getByRole('spinbutton', { name: 'CPU Threshold' })
      await expect(threshold).toBeEnabled()
      const value = original.telegramCpuThreshold === '87' ? '86' : '87'
      await threshold.fill(value)
      const saved = page.waitForResponse(response => response.request().method() === 'POST' && response.url().endsWith('/api/save'))
      await page.getByRole('button', { name: 'Save', exact: true }).click()
      const body = await (await saved).json()
      expect(body.success, body.msg).toBe(true)
      expect(body.obj.settings.telegramCpuThreshold).toBe(value)

      let releaseDiscovery!: () => void
      let observedDiscovery!: () => void
      const responseReady = new Promise<void>(resolve => { releaseDiscovery = resolve })
      const requestSeen = new Promise<void>(resolve => { observedDiscovery = resolve })
      let discoveryCalls = 0
      await page.route(discoveryURL, async route => {
        discoveryCalls++
        observedDiscovery()
        await responseReady
        await route.fulfill({ json: { success: true, msg: '', obj: { success: true, chatId: '-9007199254740993' } } })
      })
      await page.getByRole('button', { name: 'Detect chat', exact: true }).click()
      await requestSeen
      await expect(threshold).toBeDisabled()
      await expect(page.getByRole('button', { name: 'Test', exact: true })).toBeDisabled()
      releaseDiscovery()
      await expect(page.getByRole('textbox', { name: 'Chat ID', exact: true })).toHaveValue('-9007199254740993')
      await expect(threshold).toBeEnabled()
      expect(discoveryCalls).toBe(1)
      await page.unroute(discoveryURL)
      await page.route(discoveryURL, route => route.fulfill({ json: { success: true, msg: '', obj: { success: false, errorClass: 'no_chat' } } }))
      await page.getByRole('button', { name: 'Detect chat', exact: true }).click()
      await expect(page.getByText('No supported chat was found', { exact: false })).toBeVisible()

      await page.route(testURL, route => route.fulfill({ json: { success: true, msg: '', obj: { success: false, errorClass: 'unauthorized' } } }))
      await page.getByRole('button', { name: 'Test', exact: true }).click()
      await expect(page.getByText('unauthorized', { exact: true })).toBeVisible()
      expect((await (await page.request.get('api/settings')).json()).obj.telegramChatID).toBe('-9007199254740993')
      await page.goto('settings?tab=t6')
      const encryption = page.getByRole('checkbox', { name: 'Encrypt with Telegram backup passphrase', exact: true })
      await expect(encryption).toBeVisible()
      if (original.telegramBackupPassphraseHasSecret === 'true') await expect(encryption).toBeEnabled()
      else await expect(encryption).toBeDisabled()

      await page.route(postureURL, async route => {
        const response = await route.fetch()
        const body = await response.json()
        body.obj.componentUIReadPermissions = ['unknown.permission']
        await route.fulfill({ response, json: body })
      })
      await page.reload()
      await expect(page.locator('a[href$="/telegram"]')).toHaveCount(0)
      await expect(encryption).toHaveCount(0)
      await page.unroute(postureURL)
      const disabled = await page.request.post('api/update/components/telegram/disable', { headers: mutationHeaders(await csrfToken(page)) })
      expect((await disabled.json()).success).toBe(true)
      await page.goto('telegram')
      await expect(page.getByText('This feature is unavailable on this Host. Check the installed Components in Settings.')).toBeVisible()
      await expect(page.locator('a[href$="/telegram"]')).toHaveCount(0)
    } finally {
      await page.unroute(settingsURL)
      await page.unroute(discoveryURL)
      await page.unroute(testURL)
      await page.unroute(postureURL)
      await enableComponent(page, 'telegram')
      const restored = await page.request.post('api/save', {
        headers: mutationHeaders(await csrfToken(page)),
        form: { object: 'settings', action: 'set', data: JSON.stringify({ telegramCpuThreshold: original.telegramCpuThreshold, telegramChatID: original.telegramChatID }) },
      })
      expect((await restored.json()).success).toBe(true)
    }
  })
}
