import { expect, test, type Locator, type Page } from '@playwright/test'
import fs from 'node:fs'
import path from 'node:path'
import { generateKeyPairSync, randomBytes } from 'node:crypto'
import { csrfToken, login, mutationHeaders } from './helpers'
import { managedE2EResultDir } from './server-lifecycle'

test.use({ actionTimeout: 15_000 })

for (const mode of ['classic', 'nexus']) {
  test(`${mode} realm inbound keeps the serving token in Inbounds`, async ({ page }) => {
    test.setTimeout(120_000)
    await page.addInitScript(layout => window.localStorage.setItem('sui:ui:mode', layout), mode)
    await login(page)
    const contract = await (await page.request.get('api/editor-contract')).json()
    expect(contract.success).toBe(true)
    const available = contract.obj?.protocol?.criticalFields?.['hysteria2/in']?.realm?.available === true
    if (process.env.SUI_E2E_EXPECT_PROVIDER === 'shtorm-extended') expect(available).toBe(true)
    const tag = `r7-${mode}-realm-inbound-${Date.now()}`
    const token = `synthetic-${mode}-realm-serving-token`
    const read = async () => {
      const row = (await (await page.request.get('api/inbounds')).json()).obj.inbounds.find((row: { tag: string }) => row.tag === tag)
      if (!row) return undefined
      return (await (await page.request.get(`api/inbounds?id=${row.id}`)).json()).obj.inbounds[0]
    }
    try {
      if (available) {
        const certPath = process.env.SUI_E2E_TLS_CERT_PATH ?? path.join(managedE2EResultDir, 'e2e-db', 'native-serving-tls.cert.pem')
        const keyPath = process.env.SUI_E2E_TLS_KEY_PATH ?? path.join(managedE2EResultDir, 'e2e-db', 'native-serving-tls.key.pem')
        const tls = await page.request.post('api/save', {
          headers: mutationHeaders(await csrfToken(page)),
          form: { object: 'tls', action: 'new', data: JSON.stringify({ name: `${tag}-tls`,
            server: { enabled: true, certificate: [fs.readFileSync(certPath, 'utf8')], key: [fs.readFileSync(keyPath, 'utf8')] }, client: {} }) },
        })
        expect((await tls.json()).success).toBe(true)
      }
      await page.goto('hosts/local/inbounds')
      await page.getByRole('button', { name: 'Add', exact: true }).first().click()
      let form = page.getByRole('dialog').filter({ hasText: 'Add Inbound' })
      if (!await selectAvailableHysteria(page, form, 'inbounds')) return
      if (!available) {
        await expect(form.getByLabel('Use Realm rendezvous', { exact: true })).toHaveCount(0)
        await form.getByRole('button', { name: 'Close', exact: true }).first().click()
        return
      }
      await form.getByRole('textbox', { name: 'Tag', exact: true }).fill(tag)
      await form.getByRole('textbox', { name: 'Address', exact: true }).fill('127.0.0.1')
      await form.getByRole('spinbutton', { name: 'Port', exact: true }).fill(mode === 'classic' ? '42100' : '42120')
      await select(page, form, 'Template', `${tag}-tls`)
      await form.getByLabel('Use Realm rendezvous', { exact: true }).check()
      await form.getByRole('textbox', { name: 'Realm server URL', exact: true }).fill('https://127.0.0.1:8443')
      await form.getByRole('textbox', { name: 'Realm ID', exact: true }).fill('serving-peer')
      await form.getByRole('textbox', { name: 'STUN servers (one per line)', exact: true }).fill('127.0.0.1:3478')
      await select(page, form, 'Realm access token', 'Replace credential')
      await form.getByLabel('New credential', { exact: true }).fill(token)
      const created = await save(page, form)
      expect(created.realm.token).toEqual({ intent: 'replace', value: token })
      expect(created).not.toHaveProperty('users')
      const stored = await read()
      expect(stored.realm.token.present).toBe(true)
      expect(stored.realm.token.reference.owner).toBe('inbounds')
      expect(!JSON.stringify(stored).includes(token)).toBe(true)
      await page.goto('hosts/local/inbounds')
      const row = mode === 'nexus' ? page.getByRole('row').filter({ hasText: tag }) : page.locator('.inbounds__card').filter({ hasText: tag })
      await row.getByRole('button', { name: 'Edit', exact: true }).click()
      form = page.getByRole('dialog').filter({ hasText: 'Edit Inbound' })
      await expect(form.getByLabel('New credential', { exact: true })).toHaveCount(0)
      await form.getByRole('textbox', { name: 'Realm ID', exact: true }).fill('edited-serving-peer')
      expect((await save(page, form)).realm.token.intent).toBe('unchanged')
      expect((await read()).realm.token).toEqual(stored.realm.token)
      expect((await read()).realm.realm_id).toBe('edited-serving-peer')
    } finally {
      await removeFixture(page, tag)
    }
  })

  test(`${mode} realm client keeps rendezvous addressing and its owned token`, async ({ page }) => {
    test.setTimeout(120_000)
    await page.addInitScript(layout => window.localStorage.setItem('sui:ui:mode', layout), mode)
    await login(page)
    const contract = await (await page.request.get('api/editor-contract')).json()
    expect(contract.success).toBe(true)
    const available = contract.obj?.protocol?.criticalFields?.['hysteria2/out']?.realm?.available === true
    if (process.env.SUI_E2E_EXPECT_PROVIDER === 'shtorm-extended') expect(available).toBe(true)
    const tag = `r7-${mode}-realm-client-${Date.now()}`
    const token = `synthetic-${mode}-realm-access-token`
    const read = async () => ((await (await page.request.get('api/outbounds')).json()).obj.outbounds ?? []).find((row: { tag: string }) => row.tag === tag)
    try {
      await page.goto('hosts/local/outbounds')
      await page.getByRole('button', { name: 'Add', exact: true }).first().click()
      let form = page.getByRole('dialog').filter({ hasText: 'Add Outbound' })
      if (!await selectAvailableHysteria(page, form, 'outbounds')) return
      if (!available) {
        await expect(form.getByLabel('Use Realm rendezvous', { exact: true })).toHaveCount(0)
        await form.getByRole('button', { name: 'Close', exact: true }).click()
        return
      }
      await form.getByRole('textbox', { name: 'Tag', exact: true }).fill(tag)
      await form.getByRole('textbox', { name: 'Password', exact: true }).fill('protocol-password')
      await form.getByLabel('Use Realm rendezvous', { exact: true }).check()
      const confirmation = page.getByRole('dialog').filter({ hasText: 'Realm rendezvous replaces' })
      if (await confirmation.count()) await confirmation.getByRole('button', { name: 'Confirm', exact: true }).click()
      await form.getByRole('textbox', { name: 'Realm server URL', exact: true }).fill('https://127.0.0.1:8443')
      await form.getByRole('textbox', { name: 'Realm ID', exact: true }).fill('browser-peer')
      await form.getByRole('textbox', { name: 'STUN servers (one per line)', exact: true }).fill('127.0.0.1:3478\n127.0.0.1:3479')
      await select(page, form, 'Realm access token', 'Replace credential')
      await form.getByLabel('New credential', { exact: true }).fill(token)
      const created = await save(page, form)
      expect(created).not.toHaveProperty('server')
      expect(created).not.toHaveProperty('server_port')
      expect(created.realm.token).toEqual({ intent: 'replace', value: token })
      if (mode === 'nexus') await expect(form).toHaveAttribute('inert', '')
      else await expect(form).not.toBeVisible()
      const stored = await read()
      expect(stored.realm.token.present).toBe(true)
      expect(stored.realm.token.reference.owner).toBe('outbounds')
      expect(!JSON.stringify(stored).includes(token)).toBe(true)
      await page.goto('hosts/local/outbounds')
      const row = mode === 'nexus' ? page.getByRole('row').filter({ hasText: tag }) : page.locator('.outbounds__card').filter({ hasText: tag })
      await row.getByRole('button', { name: 'Edit', exact: true }).click()
      form = page.getByRole('dialog').filter({ hasText: 'Edit Outbound' })
      await expect(form.getByLabel('New credential', { exact: true })).toHaveCount(0)
      await expect(form.getByRole('textbox', { name: 'Realm server URL', exact: true })).toHaveValue('https://127.0.0.1:8443')
      await form.getByRole('textbox', { name: 'Realm ID', exact: true }).fill('browser-peer-edited')
      const kept = await save(page, form)
      expect(kept.realm.token.intent).toBe('unchanged')
      const edited = await read()
      expect(edited.realm.realm_id).toBe('browser-peer-edited')
      expect(edited.realm.token).toEqual(stored.realm.token)
      expect(edited.realm.stun_servers).toEqual(['127.0.0.1:3478', '127.0.0.1:3479'])
    } finally {
      if (await read()) {
        const response = await page.request.post('api/save', { headers: mutationHeaders(await csrfToken(page)), form: { object: 'outbounds', action: 'del', data: JSON.stringify(tag) } })
        expect((await response.json()).success).toBe(true)
      }
    }
  })
}

const kindLabels: Record<string, string> = { mieru: 'Mieru', trusttunnel: 'TrustTunnel', ssh: 'SSH', mtproxy: 'MTProxy' }
const portOffsets: Record<string, number> = { mieru: 0, trusttunnel: 2, ssh: 4, mtproxy: 6 }
function nativeCredential(kind: string, mode: string, revision: string) {
  return kind === 'mtproxy'
    ? `ee${randomBytes(16).toString('hex')}${Buffer.from('mtproxy-fixture.invalid').toString('hex')}`
    : `synthetic-${mode}-${kind}-${revision}`
}

for (const mode of ['classic', 'nexus']) {
  test(`${mode} sudoku keeps serving identity and delivery credentials in their canonical owners`, async ({ page }, testInfo) => {
    test.setTimeout(120_000)
    await page.addInitScript(layout => window.localStorage.setItem('sui:ui:mode', layout), mode)
    await login(page)
    const capabilities = await (await page.request.get('api/capabilities')).json()
    expect(capabilities.success).toBe(true)
    if (process.env.SUI_E2E_EXPECT_PROVIDER) expect(capabilities.obj.provider.id).toBe(process.env.SUI_E2E_EXPECT_PROVIDER)
    const fact = capabilities.obj.facts.find((row: { category: string; type: string }) => row.category === 'inbounds' && row.type === 'sudoku')
    expect(fact?.known).toBe(true)
    if (!fact.available) {
      await page.goto('hosts/local/inbounds')
      await page.getByRole('button', { name: 'Add', exact: true }).first().click()
      const form = page.getByRole('dialog').filter({ hasText: 'Add Inbound' })
      await form.getByRole('combobox', { name: 'Type', exact: true }).press('Enter')
      await expect(page.getByRole('option', { name: 'Sudoku', exact: true })).toHaveCount(0)
      await page.keyboard.press('Escape')
      await form.getByRole('button', { name: 'Close', exact: true }).first().click()
      return
    }
    const tag = `r7-${mode}-sudoku-${Date.now()}`
    const master = () => randomBytes(31).toString('hex') + '00'
    const firstMaster = master()
    const port = mode === 'classic' ? 42008 : 42028
    const editInbound = async () => {
      await page.goto('hosts/local/inbounds')
      if (mode === 'nexus') await page.getByRole('row').filter({ hasText: tag }).getByRole('button', { name: 'Edit', exact: true }).click()
      else await page.locator('.inbounds__card').filter({ hasText: tag }).getByRole('button', { name: 'Edit', exact: true }).click()
      return page.getByRole('dialog').filter({ hasText: 'Edit Inbound' })
    }
    try {
      await page.goto('hosts/local/clients')
      await page.getByRole('button', { name: 'Add', exact: true }).first().click()
      let form = page.getByRole('dialog').filter({ hasText: 'Add Client' })
      await form.getByRole('textbox', { name: 'Name', exact: true }).fill(`${tag}-user`)
      await save(page, form)

      await page.goto('hosts/local/inbounds')
      await page.getByRole('button', { name: 'Add', exact: true }).first().click()
      form = page.getByRole('dialog').filter({ hasText: 'Add Inbound' })
      await select(page, form, 'Type', 'Sudoku')
      await form.getByRole('textbox', { name: 'Tag', exact: true }).fill(tag)
      await form.getByRole('textbox', { name: 'Address', exact: true }).fill('127.0.0.1')
      await form.getByRole('spinbutton', { name: 'Port', exact: true }).fill(String(port))
      await expect(form).toContainText('individual revocation are unavailable')
      await select(page, form, 'Sudoku private master key', 'Replace credential')
      await form.getByLabel('New credential', { exact: true }).fill(firstMaster)
      await select(page, form, 'AEAD method', 'none')
      await select(page, form, 'Table layout', 'up_ascii_down_entropy')
      for (const label of ['Minimum padding (%)', 'Maximum padding (%)', 'Handshake timeout (seconds)']) {
        await form.getByRole('spinbutton', { name: label, exact: true }).fill('0')
      }
      await select(page, form, 'Pure downlink', 'Disable')
      await form.getByRole('textbox', { name: 'Custom table pattern', exact: true }).fill('xpxvvpvv')
      const tables = form.getByRole('combobox', { name: 'Rotating table patterns', exact: true })
      await tables.fill('xpxvvpvv')
      await tables.press('Enter')
      await form.getByLabel('Disable HTTP masking', { exact: true }).check()
      await form.getByLabel('Disable HTTP masking', { exact: true }).uncheck()
      await select(page, form, 'HTTP mask mode', 'ws')
      await form.getByRole('textbox', { name: 'HTTP path root segment', exact: true }).fill('sudoku-browser')
      await form.getByRole('textbox', { name: 'Fallback host:port', exact: true }).fill('127.0.0.1:9')
      const users = form.locator('.v-card').filter({ has: page.locator('.v-card-subtitle').filter({ hasText: /^Clients$/ }) }).last()
      await users.locator('input[role="combobox"]').first().press('Enter')
      await page.getByRole('option', { name: 'Clients', exact: true }).click()
      await select(page, users, 'Clients', `${tag}-user`)
      await page.keyboard.press('Escape')
      const created = await save(page, form)
      expect(created.master_key.intent === 'replace' && created.master_key.value === firstMaster).toBe(true)
      expect(created).not.toHaveProperty('users')
      expect(created).not.toHaveProperty('out_json')
      const listed = await (await page.request.get('api/inbounds')).json()
      const inboundID = listed.obj.inbounds.find((row: { tag: string }) => row.tag === tag).id
      const readInbound = async () => (await (await page.request.get(`api/inbounds?id=${inboundID}`)).json()).obj.inbounds[0]
      const stored = await readInbound()
      expect(stored.master_key.present).toBe(true)
      expect(stored.master_key.reference.owner).toBe('inbounds')
      expect(stored.key).toMatch(/^[a-f0-9]{64}$/)
      expect(!JSON.stringify(stored).includes(firstMaster)).toBe(true)
      let client = await readClient(page, `${tag}-user`)
      let delivery = client.config.sudoku.keys
      expect(client.inbounds).toEqual([inboundID])
      expect(client.config.sudoku).not.toHaveProperty('name')
      expect(delivery.present).toBe(true)
      expect(delivery.reference.owner).toBe('users')
      const servingKey = stored.key

      form = await editInbound()
      await expect(form.getByRole('textbox', { name: 'Serving public key', exact: true })).toHaveValue(servingKey)
      await expect(form.getByLabel('New credential', { exact: true })).toHaveCount(0)
      await expect(form.getByRole('spinbutton', { name: 'Handshake timeout (seconds)', exact: true })).toHaveValue('0')
      await expect(form.getByRole('combobox', { name: 'Pure downlink', exact: true })).toHaveValue('Disable')
      await form.getByRole('textbox', { name: 'HTTP path root segment', exact: true }).fill('sudoku-edited')
      const kept = await save(page, form)
      expect(kept.master_key.intent).toBe('unchanged')
      expect((await readInbound()).master_key).toEqual(stored.master_key)
      const effective = await checkEffective(page, tag, 'sudoku')
      expect(effective).toMatchObject({ key: servingKey, aead_method: 'none', table_type: 'up_ascii_down_entropy',
        padding_min: 0, padding_max: 0, handshake_timeout: 0, enable_pure_downlink: false, custom_table: 'xpxvvpvv',
        disable_http_mask: false, http_mask_mode: 'ws', path_root: 'sudoku-edited', fallback: '127.0.0.1:9' })
      expect([effective.custom_tables].flat()).toEqual(['xpxvvpvv'])
      expect(effective).not.toHaveProperty('master_key')

      await page.goto('hosts/local/clients')
      form = await editClient(page, `${tag}-user`)
      await form.getByRole('textbox', { name: 'Description', exact: true }).fill('keep the delivery bundle')
      await form.getByRole('tab', { name: 'Config', exact: true }).click()
      let credential = form.locator('.v-row').filter({ has: page.getByText('sudoku', { exact: true }) })
      await expect(credential).toContainText('Its value is hidden.')
      await expect(credential).toContainText('do not revoke previously delivered keys')
      await credential.scrollIntoViewIfNeeded()
      await page.screenshot({ path: testInfo.outputPath(`${mode}-sudoku-masked.png`), fullPage: true })
      expect((await save(page, form)).config.sudoku.keys.intent).toBe('unchanged')
      expect((await readClient(page, `${tag}-user`)).config.sudoku.keys).toEqual(delivery)
      for (const enabled of [false, true]) {
        form = await editClient(page, `${tag}-user`)
        await form.getByLabel('Enable', { exact: true }).setChecked(enabled)
        await save(page, form)
        client = await readClient(page, `${tag}-user`)
        expect(client.enable).toBe(enabled)
        expect(client.config.sudoku.keys).toEqual(delivery)
        expect((await checkEffective(page, tag, 'sudoku')).key).toBe(servingKey)
      }
      form = await editClient(page, `${tag}-user`)
      await form.getByRole('tab', { name: 'Config', exact: true }).click()
      credential = form.locator('.v-row').filter({ has: page.getByText('sudoku', { exact: true }) })
      await select(page, credential, 'Sudoku delivery keys by inbound ID (JSON)', 'Clear stored credential')
      await save(page, form)
      expect((await readClient(page, `${tag}-user`)).config.sudoku.keys.present).toBe(false)
      expect((await checkEffective(page, tag, 'sudoku')).key).toBe(servingKey)
      form = await editClient(page, `${tag}-user`)
      await form.getByRole('tab', { name: 'Config', exact: true }).click()
      credential = form.locator('.v-row').filter({ has: page.getByText('sudoku', { exact: true }) })
      await select(page, credential, 'Sudoku delivery keys by inbound ID (JSON)', 'Replace credential')
      await credential.getByLabel('New credential', { exact: true }).fill('{}')
      await save(page, form)
      delivery = (await readClient(page, `${tag}-user`)).config.sudoku.keys
      expect(delivery.present).toBe(true)

      form = await editInbound()
      await select(page, form, 'Sudoku private master key', 'Replace credential')
      await form.getByLabel('New credential', { exact: true }).fill(master())
      await save(page, form)
      const rotated = await readInbound()
      expect(rotated.key).not.toBe(servingKey)
      expect((await readClient(page, `${tag}-user`)).config.sudoku.keys.rotationRevision).not.toBe(delivery.rotationRevision)
      expect((await checkEffective(page, tag, 'sudoku')).key).toBe(rotated.key)
      form = await editInbound()
      await select(page, form, 'Sudoku private master key', 'Clear stored credential')
      await save(page, form)
      expect((await readInbound()).master_key.present).toBe(false)
      expect((await checkEffective(page, tag, 'sudoku')).key).toBe(rotated.key)
    } finally {
      await removeFixture(page, tag)
    }
  })
}

async function select(page: Page, form: Locator, label: string, value: string) {
  await form.getByRole('combobox', { name: label, exact: true }).press('Enter')
  await page.getByRole('option', { name: value, exact: true }).click()
}

async function selectAvailableHysteria(page: Page, form: Locator, category: string) {
  const capabilities = await (await page.request.get('api/capabilities')).json()
  const available = capabilities.obj?.facts?.some((fact: { category: string; type: string; available: boolean }) => fact.category === category && fact.type === 'hysteria2' && fact.available)
  if (available) {
    await select(page, form, 'Type', 'Hysteria2')
    return true
  }
  await form.getByRole('combobox', { name: 'Type', exact: true }).press('Enter')
  await expect(page.getByRole('option', { name: 'Hysteria2', exact: true })).toHaveCount(0)
  await page.keyboard.press('Escape')
  await form.getByRole('button', { name: 'Close', exact: true }).first().click()
  return false
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
    const row = (list.obj[object] ?? []).find((row: { tag?: string; name?: string }) => object === 'inbounds'
      ? row.tag === tag : row.name === `${tag}-${object === 'tls' ? 'tls' : 'user'}`)
    if (!row) continue
    const response = await page.request.post('api/save', {
      headers: mutationHeaders(await csrfToken(page)),
      form: { object, action: 'del', data: JSON.stringify(object === 'inbounds' ? row.tag : row.id) },
    })
    expect((await response.json()).success).toBe(true)
  }
}

async function checkEffective(page: Page, tag: string, kind: string, password?: string) {
  await expect.poll(async () => {
    const body = await (await page.request.get('api/runtime/status')).json()
    return body.obj?.status?.state
  }).toBe('running')
  const response = await page.request.get('api/singbox-config')
  expect(response.ok()).toBe(true)
  const root = await response.json()
  const inbound = root.inbounds.find((row: { tag: string }) => row.tag === tag)
  expect(inbound.type).toBe(kind)
  const field = kind === 'mtproxy' ? 'secret' : 'password'
  expect(inbound.users ?? []).toEqual(password === undefined ? [] : [{ name: `${tag}-user`, [field]: password }])
  return inbound
}

for (const mode of ['classic', 'nexus']) for (const kind of ['mieru', 'trusttunnel', 'ssh', 'mtproxy']) {
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
      const option = page.getByRole('option', { name: kindLabels[kind], exact: true })
      await expect(option).toHaveCount(0)
      await page.keyboard.press('Escape')
      await expect(form.getByRole('textbox', { name: 'Tag', exact: true })).toBeVisible()
      await form.getByRole('button', { name: 'Close', exact: true }).first().click()
      return
    }
    const tag = `r7-${mode}-${kind}-${Date.now()}`
    try {
      const port = 42000 + (mode === 'nexus' ? 20 : 0) + portOffsets[kind]
      const credentialField = kind === 'mtproxy' ? 'secret' : 'password'
      const firstPassword = nativeCredential(kind, mode, 'first')
      await page.goto('hosts/local/clients')
      await page.getByRole('button', { name: 'Add', exact: true }).first().click()
      let form = page.getByRole('dialog').filter({ hasText: 'Add Client' })
      await form.getByRole('textbox', { name: 'Name', exact: true }).fill(`${tag}-user`)
      if (mode === 'nexus') await expect(form.getByText('Unsaved changes')).toBeVisible()
      await form.getByRole('tab', { name: 'Config', exact: true }).click()
      let credential = form.locator('.v-row').filter({ has: page.getByText(kind, { exact: true }) })
      await select(page, credential, credentialField, 'Replace credential')
      await credential.getByLabel('New credential', { exact: true }).fill(firstPassword)
      const clientSubmit = await save(page, form)
      expect(clientSubmit.inbounds).toEqual([])
      expect(clientSubmit.config[kind][credentialField]).toEqual({ intent: 'replace', value: firstPassword })
      await expect(page.getByText(`${tag}-user`, { exact: true }).first()).toBeVisible()
      const unassigned = await readClient(page, `${tag}-user`)
      const reference = unassigned.config[kind][credentialField]
      await page.goto('hosts/local/inbounds?source=r7-native#editor')
      await page.reload()
      await expect(page).toHaveURL(/\/hosts\/local\/inbounds\?source=r7-native#editor$/)
      await page.getByRole('button', { name: 'Add', exact: true }).first().click()
      form = page.getByRole('dialog').filter({ hasText: 'Add Inbound' })
      await select(page, form, 'Type', kindLabels[kind])
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
      } else if (kind === 'trusttunnel') {
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
      } else if (kind === 'ssh') {
        await form.getByRole('textbox', { name: 'SSH server identification', exact: true }).fill('SSH-2.0-R7BrowserFixture')
        await form.getByRole('spinbutton', { name: 'Maximum authentication attempts', exact: true }).fill('3')
        await select(page, form, 'SSH host private-key bundle', 'Replace credential')
        await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
        const key = generateKeyPairSync('ed25519').privateKey.export({ type: 'pkcs8', format: 'pem' }).toString()
        await form.getByLabel('New credential', { exact: true }).fill(key)
      } else {
        await select(page, form, 'IP preference', 'only-ipv4')
        await form.getByRole('spinbutton', { name: 'Concurrency', exact: true }).fill('4')
        await form.getByLabel('Update Telegram data-center addresses', { exact: true }).check()
        await form.getByLabel('Update Telegram data-center addresses', { exact: true }).uncheck()
        await form.getByRole('textbox', { name: 'Handshake timeout', exact: true }).fill('3s')
        await form.getByRole('textbox', { name: 'Doppelganger interval', exact: true }).fill('2h')
        const urls = form.getByRole('combobox', { name: 'Doppelganger URLs', exact: true })
        await urls.fill('https://mtproxy-fixture.invalid/')
        await urls.press('Enter')
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
      const detail = await (await page.request.get(`api/inbounds?id=${inboundID}`)).json()
      const savedInbound = detail.obj.inbounds[0]
      if (kind === 'ssh') {
        expect(savedInbound.host_key.present).toBe(true)
        expect(savedInbound.host_key.reference.owner).toBe('inbounds')
        expect(JSON.stringify(savedInbound)).not.toContain('PRIVATE KEY')
      }
      await page.reload()
      if (mode === 'nexus') await page.getByRole('row').filter({ hasText: tag }).getByRole('button', { name: 'Edit', exact: true }).click()
      else await page.locator('.inbounds__card').filter({ hasText: tag }).getByRole('button', { name: 'Edit', exact: true }).click()
      form = page.getByRole('dialog').filter({ hasText: 'Edit Inbound' })
      await expect(form.getByRole('textbox', { name: 'Address', exact: true })).toHaveValue('127.0.0.1')
      await expect(form.getByRole('spinbutton', { name: 'Port', exact: true })).toHaveValue(String(port))
      if (kind === 'mieru') {
        await expect(form.getByRole('spinbutton', { name: 'MTU', exact: true })).toHaveValue('1400')
        await form.getByRole('spinbutton', { name: 'MTU', exact: true }).fill('1420')
      } else if (kind === 'trusttunnel') {
        await expect(form.getByRole('spinbutton', { name: 'CWND', exact: true })).toHaveValue('48')
        await form.getByRole('spinbutton', { name: 'CWND', exact: true }).fill('49')
      } else if (kind === 'ssh') {
        await expect(form.getByRole('textbox', { name: 'SSH server identification', exact: true })).toHaveValue('SSH-2.0-R7BrowserFixture')
        await expect(form.getByLabel('New credential', { exact: true })).toHaveCount(0)
        await expect(form.getByText('Its value is hidden.', { exact: false })).toBeVisible()
        await form.getByRole('spinbutton', { name: 'Maximum authentication attempts', exact: true }).fill('4')
      } else {
        await expect(form.getByRole('spinbutton', { name: 'Concurrency', exact: true })).toHaveValue('4')
        await form.getByRole('spinbutton', { name: 'Concurrency', exact: true }).fill('5')
      }
      if (mode === 'nexus') await expect(form.getByText('Unsaved changes')).toBeVisible()
      await page.screenshot({ path: testInfo.outputPath(`${mode}-${kind}-server-edit.png`), fullPage: true })
      const editedInbound = await save(page, form)
      if (kind === 'ssh') {
        expect(editedInbound.host_key.intent).toBe('unchanged')
        const refreshed = await (await page.request.get(`api/inbounds?id=${inboundID}`)).json()
        expect(refreshed.obj.inbounds[0].host_key).toEqual(savedInbound.host_key)
      }

      await page.goto('hosts/local/clients')
      let client = await readClient(page, `${tag}-user`)
      expect(client.inbounds).toEqual([inboundID])
      expect(client.config[kind][credentialField]).toEqual(reference)
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
      } else if (kind === 'trusttunnel') {
        expect([effective.network].flat()).toEqual(['tcp', 'udp'])
        expect(effective.congestion_controller).toBe('cubic')
        expect(effective.cwnd).toBe(49)
        expect(effective.disable_path_mtu_discovery).toBe(true)
        expect(effective.tls.enabled).toBe(true)
      } else if (kind === 'ssh') {
        expect(effective.server_version).toBe('SSH-2.0-R7BrowserFixture')
        expect(effective.max_auth_tries).toBe(4)
        expect(effective.host_key).toHaveLength(1)
      } else {
        expect(effective.prefer_ip).toBe('only-ipv4')
        expect(effective.concurrency).toBe(5)
        expect(effective.auto_update).toBe(false)
        expect(effective.handshake_timeout).toBe('3s')
        expect(effective.doppelganger_each).toBe('2h')
        expect([effective.doppelganger_urls].flat()).toEqual(['https://mtproxy-fixture.invalid/'])
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
      expect(retained.config[kind][credentialField].intent).toBe('unchanged')
      expect(retained.config[kind][credentialField]).not.toHaveProperty('value')
      client = await readClient(page, `${tag}-user`)
      expect(client.config[kind][credentialField]).toEqual(reference)

      form = await editClient(page, `${tag}-user`)
      await form.getByRole('tab', { name: 'Config', exact: true }).click()
      credential = form.locator('.v-row').filter({ has: page.getByText(kind, { exact: true }) })
      await select(page, credential, credentialField, 'Replace credential')
      await expect(form.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()
      const secondPassword = nativeCredential(kind, mode, 'second')
      await credential.getByLabel('New credential', { exact: true }).fill(secondPassword)
      await save(page, form)
      client = await readClient(page, `${tag}-user`)
      expect(client.config[kind][credentialField].rotationRevision).not.toBe(reference.rotationRevision)
      effective = await checkEffective(page, tag, kind, secondPassword)
      expect(effective.users[0].name).toBe(`${tag}-user`)
      const rotatedReference = client.config[kind][credentialField]
      const membership = client.inbounds
      for (const enabled of [false, true]) {
        form = await editClient(page, `${tag}-user`)
        await form.getByLabel('Enable', { exact: true }).setChecked(enabled)
        const changed = await save(page, form)
        expect(changed.enable).toBe(enabled)
        expect(changed.config[kind][credentialField].intent).toBe('unchanged')
        client = await readClient(page, `${tag}-user`)
        expect(client.enable).toBe(enabled)
        expect(client.config[kind][credentialField]).toEqual(rotatedReference)
        expect(client.inbounds).toEqual(membership)
        await checkEffective(page, tag, kind, enabled ? secondPassword : undefined)
      }
    } finally {
      await removeFixture(page, tag)
    }
  })
}
