import { expect, test, type Page } from '@playwright/test'
import { csrfToken, login, mutationHeaders } from './helpers'

async function mutate(page: Page, action: string, data: unknown, success = true) {
  const response = await page.request.post('api/save', {
    headers: mutationHeaders(await csrfToken(page)),
    form: { object: 'inbounds', action, data: JSON.stringify(data) },
  })
  const body = await response.json()
  expect(response.ok(), body.msg).toBe(true)
  expect(body.success, body.msg).toBe(success)
}

async function effective(page: Page) {
  await expect.poll(async () => (await (await page.request.get('api/runtime/status')).json()).obj?.status?.state).toBe('running')
  return (await page.request.get('api/singbox-config')).json()
}

for (const mode of ['classic', 'nexus']) {
 for (const [kind, label] of [['bond', 'Bond'], ['core-failover', 'CoreFailover']]) {
  test(`${mode} ${label} edits canonical member references through the shared owner`, async ({ page }, testInfo) => {
    test.setTimeout(90_000)
    await page.addInitScript(layout => window.localStorage.setItem('sui:ui:mode', layout), mode)
    await login(page)
    const capabilities = await (await page.request.get('api/capabilities')).json()
    expect(capabilities.success).toBe(true)
    if (process.env.SUI_E2E_EXPECT_PROVIDER) expect(capabilities.obj.provider.id).toBe(process.env.SUI_E2E_EXPECT_PROVIDER)
    const fact = capabilities.obj.facts.find((row: { category: string; type: string }) => row.category === 'inbounds' && row.type === kind)
    expect(fact?.known).toBe(true)
    await page.goto('hosts/local/inbounds')
    await page.getByRole('button', { name: 'Add', exact: true }).first().click()
    let form = page.getByRole('dialog').filter({ hasText: 'Add Inbound' })
    await form.getByRole('combobox', { name: 'Type', exact: true }).press('Enter')
    if (!fact.available) {
      await expect(page.getByRole('option', { name: label, exact: true })).toHaveCount(0)
      await page.keyboard.press('Escape')
      await form.getByRole('button', { name: 'Close', exact: true }).first().click()
      return
    }
    await page.keyboard.press('Escape')
    await form.getByRole('button', { name: 'Close', exact: true }).first().click()
    const prefix = `r7-${mode}-${kind}-${Date.now()}`
    const tags = [`${prefix}-first`, `${prefix}-second`]
    const port = (mode === 'classic' ? 42110 : 42120) + (kind === 'bond' ? 0 : 100)
    try {
      for (const [index, tag] of tags.entries()) await mutate(page, 'new', { type: 'socks', tag, listen: '127.0.0.1', listen_port: port + index })
      await page.reload()
      await page.getByRole('button', { name: 'Add', exact: true }).first().click()
      form = page.getByRole('dialog').filter({ hasText: 'Add Inbound' })
      await form.getByRole('combobox', { name: 'Type', exact: true }).press('Enter')
      await page.getByRole('option', { name: label, exact: true }).click()
      await form.getByRole('textbox', { name: 'Tag', exact: true }).fill(prefix)
      await expect(form.getByRole('spinbutton', { name: 'Port', exact: true })).toHaveCount(0)
      await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
      await form.getByRole('combobox', { name: 'Member inbounds', exact: true }).press('Enter')
      for (const tag of tags) await page.getByRole('option', { name: `${tag} (socks)`, exact: true }).click()
      await page.keyboard.press('Escape')
      await page.screenshot({ path: testInfo.outputPath(`${mode}-${kind}-members.png`), fullPage: true })
      const [response] = await Promise.all([
        page.waitForResponse(response => response.request().method() === 'POST' && response.url().endsWith('/api/save')),
        form.getByRole('button', { name: 'Save', exact: true }).click(),
      ])
      const saved = await response.json()
      expect(saved.success, saved.msg).toBe(true)
      const submitted = JSON.parse(new URLSearchParams(response.request().postData() ?? '').get('data') ?? '{}')
      expect(submitted.inbounds).toEqual(tags)
      expect(submitted).not.toHaveProperty('listen_port')
      expect(submitted).not.toHaveProperty('users')
      let config = await effective(page)
      let group = config.inbounds.find((row: { tag: string }) => row.tag === prefix)
      expect(group.type).toBe(kind === 'core-failover' ? 'failover' : kind)
      expect(group.inbounds.map((row: { tag: string }) => row.tag)).toEqual(tags)
      expect(config.inbounds.some((row: { tag: string }) => tags.includes(row.tag))).toBe(false)
      const rows = (await (await page.request.get('api/inbounds')).json()).obj.inbounds
      const first = rows.find((row: { tag: string }) => row.tag === tags[0])
      const captured = (await (await page.request.get(`api/inbounds?id=${first.id}`)).json()).obj.inbounds[0]
      await mutate(page, 'edit', { ...captured, listen_port: port + 2 })
      config = await effective(page)
      group = config.inbounds.find((row: { tag: string }) => row.tag === prefix)
      expect(group.inbounds[0].listen_port).toBe(port + 2)
      await mutate(page, 'del', tags[0], false)
      await mutate(page, 'del', prefix)
      config = await effective(page)
      expect(config.inbounds.filter((row: { tag: string }) => tags.includes(row.tag))).toHaveLength(2)
    } finally {
      const rows = (await (await page.request.get('api/inbounds')).json()).obj.inbounds
      for (const tag of [prefix, ...tags]) if (rows.some((row: { tag: string }) => row.tag === tag)) await mutate(page, 'del', tag)
    }
  })
 }
}
