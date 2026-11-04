import { expect, test, type Locator, type Page } from '@playwright/test'
import { csrfToken, login, mutationHeaders } from './helpers'

async function select(page: Page, form: Locator, label: string, value: string) {
  await form.getByRole('combobox', { name: label, exact: true }).press('Enter')
  await page.getByRole('option', { name: value, exact: true }).click()
}

for (const mode of ['classic', 'nexus']) {
  test(mode + ' failover policy saves through the shared owner without a hidden Direct fallback', async ({ page }) => {
    test.setTimeout(90_000)
    await page.addInitScript(layout => window.localStorage.setItem('sui:ui:mode', layout), mode)
    await login(page)
    const tag = 'r7-' + mode + '-all-down-' + Date.now()
    const member = tag + '-member'
    const mutate = async (action: string, data: unknown) => {
      const response = await page.request.post('api/save', {
        headers: mutationHeaders(await csrfToken(page)),
        form: { object: 'outbounds', action, data: JSON.stringify(data) },
      })
      const result = await response.json()
      expect(result.success, result.msg).toBe(true)
    }
    const read = async () => ((await (await page.request.get('api/outbounds')).json()).obj.outbounds ?? []).find((row: { tag: string }) => row.tag === tag)
    const save = async (form: Locator) => {
      const response = page.waitForResponse(r => r.url().endsWith('/api/save') && r.request().method() === 'POST')
      await form.getByRole('button', { name: 'Save', exact: true }).click()
      const result = await (await response).json()
      expect(result.success, result.msg).toBe(true)
    }
    try {
      await mutate('new', { type: 'socks', tag: member, server: '127.0.0.1', server_port: 9 })
      await page.goto('hosts/local/outbounds')
      await page.getByRole('button', { name: 'Add', exact: true }).first().click()
      let form = page.getByRole('dialog').filter({ hasText: 'Add Outbound' })
      await select(page, form, 'Type', 'Failover')
      await expect(form).toContainText('Failover Group')
      await expect(form.getByLabel('Server Address')).toHaveCount(0)
      await expect(form.getByLabel('Server Port')).toHaveCount(0)
      await expect(form.getByRole('combobox', { name: 'When all members are down', exact: true })).toHaveValue('Keep current member')
      await form.getByRole('textbox', { name: 'Tag', exact: true }).fill(tag)
      await form.getByRole('button', { name: 'Add Outbound', exact: true }).click()
      await select(page, form, 'Primary', member)
      await form.getByLabel('Automatic Switching', { exact: true }).uncheck()
      await save(form)
      expect((await read()).failover.all_down_policy).toBe('hold_current')
      for (const [label, policy] of [['Block new connections', 'block'], ['Use direct explicitly', 'direct'], ['Keep current member', 'hold_current']]) {
        await page.reload()
        const row = mode === 'nexus' ? page.getByRole('row').filter({ has: page.getByText(tag, { exact: true }) }) : page.locator('.outbounds__card').filter({ has: page.getByText(tag, { exact: true }) })
        await row.getByRole('button', { name: 'Edit', exact: true }).click()
        form = page.getByRole('dialog').filter({ hasText: 'Edit Outbound' })
        await select(page, form, 'When all members are down', label)
        await save(form)
        expect((await read()).failover.all_down_policy).toBe(policy)
        const status = await page.request.get('api/failover-status')
        expect(status.ok()).toBe(true)
        expect((await status.json()).success).toBe(true)
        const configResponse = await page.request.get('api/singbox-config')
        expect(configResponse.ok()).toBe(true)
        const config = await configResponse.json()
        const group = config.outbounds.find((row: { tag: string }) => row.tag === tag)
        const expected = policy === 'hold_current' ? [member] : [member, policy === 'block' ? '__solovey_failover_reject' : 'direct']
        expect(group.type).toBe('selector')
        expect(group.outbounds).toEqual(expected)
        expect(group).not.toHaveProperty('failover')
      }
    } finally {
      if (await read()) await mutate('del', tag)
      await mutate('del', member)
    }
  })
}
