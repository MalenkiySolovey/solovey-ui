import { expect, test, type Locator, type Page } from '@playwright/test'
import fs from 'node:fs'
import { csrfToken, login, mutationHeaders } from './helpers'

test.use({ actionTimeout: 15_000 })

async function select(page: Page, form: Locator, label: string, value: string) {
  await form.getByRole('combobox', { name: label, exact: true }).press('Enter')
  await page.getByRole('option', { name: value, exact: true }).click()
}

async function save(page: Page, form: Locator, success = true) {
  const [response] = await Promise.all([
    page.waitForResponse(response => response.request().method() === 'POST' && response.url().endsWith('/api/save')),
    form.getByRole('button', { name: 'Save', exact: true }).click(),
  ])
  const body = await response.json()
  expect(response.ok()).toBe(true)
  expect(body.success, body.msg).toBe(success)
  return JSON.parse(new URLSearchParams(response.request().postData() ?? '').get('data') ?? '{}')
}

async function readClient(page: Page, name: string) {
  const list = await (await page.request.get('api/clients')).json()
  const id = list.obj.clients.find((client: { name: string }) => client.name === name)?.id
  expect(id).toBeGreaterThan(0)
  const body = await (await page.request.get(`api/clients?id=${id}`)).json()
  expect(body.success).toBe(true)
  return body.obj.clients[0]
}

async function editClient(page: Page, name: string) {
  const row = page.getByRole('row').filter({ hasText: name }).first()
  await row.getByRole('button', { name: 'Edit', exact: true }).click()
  return page.getByRole('dialog').filter({ hasText: 'Edit Client' })
}

async function removeFixture(page: Page, tag: string) {
  // Use the canonical owners and exact run-local identities; never clear a DB.
  for (const object of ['inbounds', 'clients', 'tls']) {
    const list = await (await page.request.get(`api/${object}`)).json()
    expect(list.success).toBe(true)
    const row = list.obj[object].find((row: { tag?: string; name?: string }) => object === 'inbounds'
      ? row.tag === tag : row.name === `${tag}-${object === 'tls' ? 'tls' : 'user'}`)
    if (!row) continue
    const response = await page.request.post('api/save', {
      headers: mutationHeaders(await csrfToken(page)),
      form: { object, action: 'del', data: JSON.stringify(object === 'inbounds' ? row.tag : row.id) },
    })
    expect((await response.json()).success).toBe(true)
  }
}

async function checkEffective(page: Page, tag: string, kind: string, password: string) {
  await expect.poll(async () => {
    const body = await (await page.request.get('api/runtime/status')).json()
    return body.obj?.status?.state
  }).toBe('running')
  const response = await page.request.get('api/singbox-config')
  expect(response.ok()).toBe(true)
  const root = await response.json()
  const inbound = root.inbounds.find((row: { tag: string }) => row.tag === tag)
  expect(inbound.type).toBe(kind)
  expect(inbound.users).toEqual([{ name: `${tag}-user`, password }])
  return inbound
}

for (const mode of ['classic', 'nexus']) for (const kind of ['mieru', 'trusttunnel']) {
  test(`${mode} ${kind} uses the native inbound and Users credential owners`, async ({ page }, testInfo) => {
    test.setTimeout(120_000)
    await page.addInitScript(layout => window.localStorage.setItem('sui:ui:mode', layout), mode)
    await login(page)
    const capabilities = await (await page.request.get('api/capabilities')).json()
    expect(capabilities.success).toBe(true)
    if (process.env.SUI_E2E_EXPECT_PROVIDER) expect(capabilities.obj.provider.id).toBe(process.env.SUI_E2E_EXPECT_PROVIDER)
    const fact = capabilities.obj.facts.find((row: { category: string; type: string }) => row.category === 'inbounds' && row.type === kind)
    expect(fact?.known).toBe(true)
    if (!fact.available) {
      await page.goto('hosts/local/inbounds?source=r7-native#editor')
      await page.reload()
      await expect(page).toHaveURL(/\/hosts\/local\/inbounds\?source=r7-native#editor$/)
      await page.getByRole('button', { name: 'Add', exact: true }).first().click()
      const form = page.getByRole('dialog').filter({ hasText: 'Add Inbound' })
      await form.getByRole('combobox', { name: 'Type', exact: true }).press('Enter')
      const option = page.getByRole('option', { name: kind === 'mieru' ? 'Mieru' : 'TrustTunnel', exact: true })
      await expect(option).toHaveAttribute('aria-disabled', 'true')
      await page.keyboard.press('Escape')
      await expect(form.getByRole('textbox', { name: 'Tag', exact: true })).toBeVisible()
      await form.getByRole('button', { name: 'Close', exact: true }).first().click()
      return
    }
    const tag = `r7-${mode}-${kind}-${Date.now()}`
    try {
      const port = 42000 + (mode === 'nexus' ? 20 : 0) + (kind === 'trusttunnel' ? 2 : 0)
      const firstPassword = `synthetic-${mode}-${kind}-first`
      await page.goto('hosts/local/clients')
      await page.getByRole('button', { name: 'Add', exact: true }).first().click()
      let form = page.getByRole('dialog').filter({ hasText: 'Add Client' })
      await form.getByRole('textbox', { name: 'Name', exact: true }).fill(`${tag}-user`)
      if (mode === 'nexus') await expect(form.getByText('Unsaved changes')).toBeVisible()
      await form.getByRole('tab', { name: 'Config', exact: true }).click()
      let credential = form.locator('.v-row').filter({ has: page.getByText(kind, { exact: true }) })
      await select(page, credential, 'password', 'Replace credential')
      await credential.getByLabel('New credential', { exact: true }).fill(firstPassword)
      const clientSubmit = await save(page, form)
      expect(clientSubmit.inbounds).toEqual([])
      expect(clientSubmit.config[kind].password).toEqual({ intent: 'replace', value: firstPassword })
      await expect(page.getByText(`${tag}-user`, { exact: true }).first()).toBeVisible()
      const unassigned = await readClient(page, `${tag}-user`)
      const reference = unassigned.config[kind].password
      await page.goto('hosts/local/inbounds?source=r7-native#editor')
      await page.reload()
      await expect(page).toHaveURL(/\/hosts\/local\/inbounds\?source=r7-native#editor$/)
      await page.getByRole('button', { name: 'Add', exact: true }).first().click()
      form = page.getByRole('dialog').filter({ hasText: 'Add Inbound' })
      await select(page, form, 'Type', kind === 'mieru' ? 'Mieru' : 'TrustTunnel')
      await form.getByRole('textbox', { name: 'Tag', exact: true }).fill(tag)
      await form.getByRole('textbox', { name: 'Address', exact: true }).fill('127.0.0.1')
      await form.getByRole('spinbutton', { name: 'Port', exact: true }).fill(String(port))
      if (kind === 'mieru') {
        await select(page, form, 'Transport', mode === 'nexus' ? 'UDP' : 'TCP')
        await form.getByLabel('Require user hint', { exact: true }).check()
        await form.getByRole('spinbutton', { name: 'MTU', exact: true }).fill('1400')
        const ranges = form.getByRole('combobox', { name: 'Additional listening ports or ranges', exact: true })
        await ranges.fill(`${port + 100}-${port + 101}`)
        await ranges.press('Enter')
        const pattern = form.getByRole('textbox', { name: 'Traffic pattern', exact: true })
        await pattern.fill('synthetic-invalid-pattern')
      } else {
        await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
        const certPath = process.env.SUI_E2E_TLS_CERT_PATH
        const keyPath = process.env.SUI_E2E_TLS_KEY_PATH
        if (!certPath || !keyPath) throw new Error('Native TrustTunnel qualification requires private synthetic TLS fixture paths')
        const profileName = `${tag}-tls`
        const tls = await page.request.post('api/save', {
          headers: mutationHeaders(await csrfToken(page)),
          form: { object: 'tls', action: 'new', data: JSON.stringify({ name: profileName,
            server: { enabled: true, certificate: [fs.readFileSync(certPath, 'utf8')], key: [fs.readFileSync(keyPath, 'utf8')] }, client: {} }) },
        })
        expect((await tls.json()).success).toBe(true)
        // Reload the editor's canonical TLS list through the same page owner.
        await form.getByRole('button', { name: 'Close', exact: true }).first().click()
        if (mode === 'nexus') await page.getByRole('dialog', { name: 'Discard changes?', exact: true }).getByRole('button', { name: 'Discard', exact: true }).click()
        await page.reload()
        await page.getByRole('button', { name: 'Add', exact: true }).first().click()
        form = page.getByRole('dialog').filter({ hasText: 'Add Inbound' })
        await select(page, form, 'Type', 'TrustTunnel')
        await form.getByRole('textbox', { name: 'Tag', exact: true }).fill(tag)
        await form.getByRole('textbox', { name: 'Address', exact: true }).fill('127.0.0.1')
        await form.getByRole('spinbutton', { name: 'Port', exact: true }).fill(String(port))
        await select(page, form, 'Template', profileName)
        await select(page, form, 'Congestion controller', 'cubic')
        await form.getByRole('spinbutton', { name: 'CWND', exact: true }).fill('48')
        await expect(form).toContainText('does not apply a custom congestion window')
        await form.getByLabel('Disable path MTU discovery', { exact: true }).check()
      }
      const users = form.locator('.v-card').filter({ has: page.locator('.v-card-subtitle').filter({ hasText: /^Clients$/ }) }).last()
      await users.locator('input[role="combobox"]').first().press('Enter')
      await page.getByRole('option', { name: 'Clients', exact: true }).click()
      await select(page, users, 'Clients', `${tag}-user`)
      await page.keyboard.press('Escape')
      if (kind === 'mieru') {
        const pattern = form.getByRole('textbox', { name: 'Traffic pattern', exact: true })
        await save(page, form, false)
        await expect(pattern).toHaveValue('synthetic-invalid-pattern')
        const rejected = page.getByRole('alert').filter({ hasText: 'NATIVE_INBOUND_CANDIDATE_REJECTED' })
        await rejected.click()
        await expect(rejected).toBeHidden()
        await pattern.fill('')
      }
      const submitted = await save(page, form)
      expect(submitted).not.toHaveProperty('users')
      expect(submitted).not.toHaveProperty('out_json')
      await expect(page.getByText(tag, { exact: true }).first()).toBeVisible()
      const stored = await (await page.request.get('api/inbounds')).json()
      const inboundID = stored.obj.inbounds.find((row: { tag: string }) => row.tag === tag).id
      await page.reload()
      if (mode === 'nexus') await page.getByRole('row').filter({ hasText: tag }).getByRole('button', { name: 'Edit', exact: true }).click()
      else await page.locator('.inbounds__card').filter({ hasText: tag }).getByRole('button', { name: 'Edit', exact: true }).click()
      form = page.getByRole('dialog').filter({ hasText: 'Edit Inbound' })
      await expect(form.getByRole('textbox', { name: 'Address', exact: true })).toHaveValue('127.0.0.1')
      await expect(form.getByRole('spinbutton', { name: 'Port', exact: true })).toHaveValue(String(port))
      if (kind === 'mieru') {
        await expect(form.getByRole('spinbutton', { name: 'MTU', exact: true })).toHaveValue('1400')
        await form.getByRole('spinbutton', { name: 'MTU', exact: true }).fill('1420')
      } else {
        await expect(form.getByRole('spinbutton', { name: 'CWND', exact: true })).toHaveValue('48')
        await form.getByRole('spinbutton', { name: 'CWND', exact: true }).fill('49')
      }
      if (mode === 'nexus') await expect(form.getByText('Unsaved changes')).toBeVisible()
      await page.screenshot({ path: testInfo.outputPath(`${mode}-${kind}-server-edit.png`), fullPage: true })
      await save(page, form)

      await page.goto('hosts/local/clients')
      let client = await readClient(page, `${tag}-user`)
      expect(client.inbounds).toEqual([inboundID])
      expect(client.config[kind].password).toEqual(reference)
      expect(reference.present).toBe(true)
      expect(reference.reference.owner).toBe('users')
      expect(reference.rotationRevision).not.toBe('')
      expect(JSON.stringify(client)).not.toContain(firstPassword)
      expect(JSON.stringify(client)).not.toContain('sbox:v1:')
      let effective = await checkEffective(page, tag, kind, firstPassword)
      expect(effective.listen_port).toBe(port)
      if (kind === 'mieru') {
        expect(effective.transport).toBe(mode === 'nexus' ? 'UDP' : 'TCP')
        expect(effective.user_hint_is_mandatory).toBe(true)
        expect(effective.mtu).toBe(1420)
        expect([effective.listen_ports].flat()).toEqual([`${port + 100}-${port + 101}`])
      } else {
        expect([effective.network].flat()).toEqual(['tcp', 'udp'])
        expect(effective.congestion_controller).toBe('cubic')
        expect(effective.cwnd).toBe(49)
        expect(effective.disable_path_mtu_discovery).toBe(true)
        expect(effective.tls.enabled).toBe(true)
      }
      await page.reload()
      form = await editClient(page, `${tag}-user`)
      await form.getByRole('textbox', { name: 'Description', exact: true }).fill('metadata edit keeps the credential')
      await form.getByRole('tab', { name: 'Config', exact: true }).click()
      credential = form.locator('.v-row').filter({ has: page.getByText(kind, { exact: true }) })
      await expect(credential).toContainText('Its value is hidden.')
      await expect(credential.getByLabel('New credential', { exact: true })).toHaveCount(0)
      await credential.scrollIntoViewIfNeeded()
      await page.screenshot({ path: testInfo.outputPath(`${mode}-${kind}-masked.png`), fullPage: true })
      const retained = await save(page, form)
      expect(retained.config[kind].password.intent).toBe('unchanged')
      expect(retained.config[kind].password).not.toHaveProperty('value')
      client = await readClient(page, `${tag}-user`)
      expect(client.config[kind].password).toEqual(reference)

      form = await editClient(page, `${tag}-user`)
      await form.getByRole('tab', { name: 'Config', exact: true }).click()
      credential = form.locator('.v-row').filter({ has: page.getByText(kind, { exact: true }) })
      await select(page, credential, 'password', 'Replace credential')
      await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
      const secondPassword = `synthetic-${mode}-${kind}-second`
      await credential.getByLabel('New credential', { exact: true }).fill(secondPassword)
      await save(page, form)
      client = await readClient(page, `${tag}-user`)
      expect(client.config[kind].password.rotationRevision).not.toBe(reference.rotationRevision)
      effective = await checkEffective(page, tag, kind, secondPassword)
      expect(effective.users[0].name).toBe(`${tag}-user`)
    } finally {
      await removeFixture(page, tag)
    }
  })
}
