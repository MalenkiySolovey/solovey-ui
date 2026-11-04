import { expect, test } from '@playwright/test'

import { csrfToken, login, mutationHeaders } from './helpers'

test.use({ actionTimeout: 15_000 })

// Verifies the FormShell path: the Endpoint modal renders as a right-side
// drawer (role=dialog) in Nexus mode, opened from the new list's Add button.
// WireGuard key generation is local; provider startup/traffic is proved by the
// existing native owner suite. WARP enrollment still needs its remote authority.
test('nexus endpoints list opens the FormShell drawer', async ({ page }) => {
  test.setTimeout(60_000)

  await login(page)
  await page.goto('endpoints')

  await page.getByRole('button', { name: 'Add', exact: true }).first().click()
  const drawer = page.getByRole('dialog')
  await expect(drawer).toContainText('Add Endpoint')
})

for (const mode of ['classic', 'nexus']) {
  test(`${mode} manages User-owned WireGuard devices without exposing keys`, async ({ page }) => {
    test.setTimeout(120_000)
    await page.addInitScript(selected => window.localStorage.setItem('sui:ui:mode', selected), mode)
    await login(page)
    const capabilities = (await (await page.request.get('api/capabilities')).json()).obj
    const tag = `managed-wg-${mode}-${Date.now()}`
    const save = async (object: string, action: string, data: unknown) => {
      const response = await page.request.post('api/save', { headers: mutationHeaders(await csrfToken(page)), form: { object, action, data: JSON.stringify(data) } })
      const body = await response.json()
      expect(body.success, body.msg).toBe(true)
    }
    await page.goto('hosts/local/endpoints')
    await page.getByRole('button', { name: 'Add', exact: true }).first().click()
    const endpointForm = page.getByRole('dialog').filter({ hasText: 'Add Endpoint' })
    if (capabilities.wireGuardDevices !== true) {
      await expect(endpointForm.getByLabel('Manage devices through Users', { exact: true })).toHaveCount(0)
      return
    }
    await endpointForm.getByRole('textbox', { name: 'Tag', exact: true }).fill(tag)
    await endpointForm.getByRole('textbox', { name: /^Local IPs / }).fill('10.79.0.1/29')
    await endpointForm.getByRole('spinbutton', { name: 'Port', exact: true }).fill(mode === 'classic' ? '42700' : '42701')
    await endpointForm.getByLabel('Manage devices through Users', { exact: true }).check()
    await endpointForm.getByRole('spinbutton', { name: 'Device limit', exact: true }).fill('2')
    await endpointForm.getByRole('textbox', { name: 'Public endpoint', exact: true }).fill('vpn.example:51820')
    await endpointForm.getByRole('button', { name: /^(Save|Add)$/ }).last().click()
    if (mode === 'nexus') await expect(endpointForm).toHaveAttribute('inert', '')
    else await expect(endpointForm).toBeHidden()
    const endpoints = (await (await page.request.get('api/endpoints')).json()).obj.endpoints
    const endpointID = endpoints.find((item: { tag: string }) => item.tag === tag).id
    await save('clients', 'new', { name: tag, enable: true, config: {}, inbounds: [] })
    const clients = (await (await page.request.get('api/clients')).json()).obj.clients
    const clientID = clients.find((item: { name: string }) => item.name === tag).id
    const read = async () => (await (await page.request.get(`api/client-devices?clientId=${clientID}`)).json()).obj
    const clickMutation = async (label: string, dialog: ReturnType<typeof page.getByRole>) => {
      const [response] = await Promise.all([page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/api/save')), dialog.getByRole('button', { name: label, exact: true }).click()])
      const body = await response.json()
      expect(body.success, body.msg).toBe(true)
    }
    try {
      await page.goto('hosts/local/clients')
      await page.getByRole('row').filter({ hasText: tag }).getByRole('button', { name: 'Edit', exact: true }).click()
      const userForm = page.getByRole('dialog').filter({ hasText: 'Edit Client' })
      await userForm.getByRole('button', { name: 'WireGuard devices', exact: true }).click()
      const dialog = page.getByRole('dialog').filter({ has: page.locator('.v-card-title').filter({ hasText: /^WireGuard devices$/ }) })
      await expect(dialog.getByRole('button', { name: 'Grant endpoint access', exact: true })).toBeEnabled()
      await clickMutation('Grant endpoint access', dialog)
      await expect(dialog.getByRole('textbox', { name: 'Device name', exact: true })).toBeVisible()
      await dialog.getByRole('textbox', { name: 'Device name', exact: true }).fill('phone')
      await clickMutation('Create device', dialog)
      await expect(dialog.getByRole('row').filter({ hasText: 'phone' })).toBeVisible()
      const first = (await read()).devices[0]
      expect(first.address).toBe('10.79.0.2/32')
      expect(first).not.toHaveProperty('privateKey')
      expect(first).not.toHaveProperty('presharedKey')
      const downloaded = page.waitForEvent('download')
      await dialog.getByRole('button', { name: 'Download private .conf', exact: true }).click()
      const stream = await (await downloaded).createReadStream()
      const chunks: Buffer[] = []; for await (const chunk of stream!) chunks.push(Buffer.from(chunk))
      const config = Buffer.concat(chunks).toString('utf8')
      expect(config).toContain('Address = 10.79.0.2/32\n')
      expect(config).toContain('Endpoint = vpn.example:51820\n')
      expect(config).toContain('PrivateKey = ')
      expect(config).not.toContain('sbox:v1:')
      await dialog.getByRole('button', { name: 'Rotate keys', exact: true }).click()
      await clickMutation('yes', page.getByRole('dialog', { name: 'Rotate keys', exact: true }))
      await expect.poll(async () => (await read()).devices[0].publicKey).not.toBe(first.publicKey)
      const rotated = (await read()).devices[0]
      expect(rotated.id).toBe(first.id); expect(rotated.address).toBe(first.address)
      await expect(dialog.getByRole('button', { name: 'Revoke', exact: true })).toBeEnabled()
      await dialog.getByRole('button', { name: 'Revoke', exact: true }).click()
      await clickMutation('yes', page.getByRole('dialog', { name: 'Revoke', exact: true }))
      await expect(dialog.getByText('Revoked', { exact: true })).toBeVisible()
      await expect(dialog.getByRole('button', { name: 'Download private .conf', exact: true })).toHaveCount(0)
      const denied = await page.request.post('api/client-device-config', { headers: mutationHeaders(await csrfToken(page)), data: { clientId: clientID, deviceId: first.id, host: 'panel.example' } })
      expect(denied.status()).toBe(409)
      const ordinary = (await (await page.request.get(`api/clients?id=${clientID}`)).json()).obj.clients[0]
      expect(ordinary.config.wireguard.owner).toBe('client-devices')
      expect(JSON.stringify(ordinary)).not.toContain('sbox:v1:')
      expect((await read()).grants[0].endpointId).toBe(endpointID)
    } finally {
      await save('clients', 'del', clientID)
      await save('endpoints', 'del', tag)
    }
  })

  test(`${mode} projects native Amnezia options and preserves masked credentials`, async ({ page }) => {
    test.setTimeout(90_000)
    await page.addInitScript(selected => window.localStorage.setItem('sui:ui:mode', selected), mode)
    await login(page)
    const contract = await (await page.request.get('api/editor-contract')).json()
    expect(contract.success).toBe(true)
    const available = contract.obj.protocol.criticalFields['wireguard/endpoint']?.amnezia?.available === true
    await page.goto('hosts/local/endpoints')
    await page.getByRole('button', { name: 'Add', exact: true }).first().click()
    let form = page.getByRole('dialog').filter({ hasText: 'Add Endpoint' })
    if (!available) {
      await expect(form.getByRole('button', { name: 'AmneziaWG 3.1', exact: true })).toHaveCount(0)
      return
    }
    const tag = `native-awg-${mode}-${Date.now()}`
    await form.getByRole('textbox', { name: 'Tag', exact: true }).fill(tag)
    await form.getByRole('textbox', { name: 'Public endpoint', exact: true }).fill('[2001:db8::1]:63443')
    await form.getByRole('textbox', { name: 'Client routes (AllowedIPs)', exact: true }).fill('192.0.2.0/24\n2001:db8:1::/64')
    await form.getByRole('spinbutton', { name: 'Client keepalive (seconds)', exact: true }).fill('0')
    await form.getByRole('button', { name: 'Add Peer', exact: true }).click()
    await expect(form.getByRole('textbox', { name: 'Peer Public Key', exact: true })).not.toHaveValue('')
    await form.getByRole('button', { name: 'AmneziaWG 3.1', exact: true }).click()
    await form.getByRole('checkbox', { name: 'Use AmneziaWG parameters', exact: true }).check()
    await form.getByRole('textbox', { name: 'Handshake initiation header (H1)', exact: true }).fill('123')
    await form.getByRole('textbox', { name: 'Handshake response header (H2)', exact: true }).fill('200-300')
    for (const label of ['Handshake initiation padding (S1)', 'Handshake response padding (S2)', 'Cookie message padding (S3)', 'Transport message padding (S4)']) {
      await form.getByRole('spinbutton', { name: label, exact: true }).fill('16')
    }
    await form.getByRole('combobox', { name: 'Header protection key (base64)', exact: true }).press('Enter')
    await page.getByRole('option', { name: 'Replace credential', exact: true }).click()
    await form.getByRole('textbox', { name: 'New credential', exact: true }).fill(Buffer.alloc(32, 7).toString('base64'))
    await form.getByRole('button', { name: /^(Save|Add)$/ }).last().click()
    if (mode === 'nexus') await expect(form).toHaveAttribute('inert', '')
    else await expect(form).toBeHidden()
    const row = mode === 'nexus' ? page.getByRole('row').filter({ hasText: tag }) : page.locator('.endpoints__card').filter({ hasText: tag })
    await row.getByRole('button', { name: 'Edit', exact: true }).click()
    form = page.getByRole('dialog').filter({ hasText: 'Edit Endpoint' })
    await expect(form.getByRole('textbox', { name: 'Public endpoint', exact: true })).toHaveValue('[2001:db8::1]:63443')
    await expect(form.getByRole('spinbutton', { name: 'Client keepalive (seconds)', exact: true })).toHaveValue('0')
    await form.getByRole('button', { name: 'AmneziaWG 3.1', exact: true }).click()
    await expect(form.getByRole('textbox', { name: 'Handshake initiation header (H1)', exact: true })).toHaveValue('123')
    await expect(form.getByRole('textbox', { name: 'Handshake response header (H2)', exact: true })).toHaveValue('200-300')
    await expect(form.getByText('A credential is stored. Its value is hidden.', { exact: true })).toBeVisible()
    await expect(form.getByRole('textbox', { name: 'New credential', exact: true })).toHaveCount(0)
    await page.keyboard.press('Escape')
    await row.getByRole('button', { name: mode === 'nexus' ? 'Config' : 'WireGuard QR code', exact: true }).click()
    const qr = page.locator('#qrcode-modal')
    await expect(qr.getByRole('button', { name: 'Download peer configuration 1', exact: true })).toBeVisible()
    const downloaded = page.waitForEvent('download')
    await qr.getByRole('button', { name: 'Download peer configuration 1', exact: true }).click()
    const download = await downloaded
    expect(download.suggestedFilename()).toMatch(/^endpoint_\d+_peer_1\.conf$/)
    const stream = await download.createReadStream()
    expect(stream).not.toBeNull()
    const chunks: Buffer[] = []
    for await (const chunk of stream!) chunks.push(Buffer.from(chunk))
    const config = Buffer.concat(chunks).toString('utf8')
    expect(config).toContain('H2 = 200-300\n')
    expect(config).toContain('Endpoint = [2001:db8::1]:63443\n')
    expect(config).toContain('AllowedIPs = 192.0.2.0/24, 2001:db8:1::/64\n')
    expect(config).not.toContain('PersistentKeepalive')
    expect(config).toContain(`HeaderProtectionKey = ${Buffer.alloc(32, 7).toString('base64')}\n`)
    expect(config).not.toContain('sbox:v1:')
  })
}
