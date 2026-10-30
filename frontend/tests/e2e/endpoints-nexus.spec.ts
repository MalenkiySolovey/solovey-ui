import { expect, test } from '@playwright/test'

import { login } from './helpers'

// Verifies the FormShell path: the Endpoint modal renders as a right-side
// drawer (role=dialog) in Nexus mode, opened from the new list's Add button.
// NOTE: actually saving a WireGuard/Warp/Tailscale endpoint needs the sing-box
// core / external connectivity (api/keypairs key-gen, Cloudflare, auth keys),
// which the test stack disables (SUI_DISABLE_CORE/XUI_DISABLE_REMOTE) — the same
// in Classic, so it is an env limitation, not a redesign regression.
test('nexus endpoints list opens the FormShell drawer', async ({ page }) => {
  test.setTimeout(60_000)

  await login(page)
  await page.goto('endpoints')

  await page.getByRole('button', { name: 'Add', exact: true }).first().click()
  const drawer = page.getByRole('dialog')
  await expect(drawer).toContainText('Add Endpoint')
})

for (const mode of ['classic', 'nexus']) {
  test(`${mode} uses the canonical OpenVPN serving form and keeps its key masked`, async ({ page }) => {
    test.setTimeout(90_000)
    await page.addInitScript(selected => window.localStorage.setItem('sui:ui:mode', selected), mode)
    await login(page)
    const capabilities = await (await page.request.get('api/capabilities')).json()
    expect(capabilities.success).toBe(true)
    const fact = capabilities.obj.facts.find((item: { category: string; type: string }) => item.category === 'endpoints' && item.type === 'openvpn-server')
    expect(fact?.known).toBe(true)
    await page.goto('hosts/local/endpoints')
    await page.getByRole('button', { name: 'Add', exact: true }).first().click()
    let form = page.getByRole('dialog').filter({ hasText: 'Add Endpoint' })
    await form.getByRole('combobox', { name: 'Type', exact: true }).press('Enter')
    if (!fact.available) {
      await expect(page.getByRole('option', { name: 'OpenVPNServer', exact: true })).toHaveCount(0)
      await expect(page.getByRole('option', { name: 'openvpn-server', exact: true })).toHaveCount(0)
      return
    }
    await page.getByRole('option', { name: 'OpenVPNServer', exact: true }).click()
    const tag = `serving-vpn-${mode}-${Date.now()}`
    await form.getByRole('textbox', { name: 'Tag', exact: true }).fill(tag)
    await form.getByRole('spinbutton', { name: 'Port', exact: true }).fill(mode === 'classic' ? '43801' : '43802')
    await form.getByRole('combobox', { name: 'VPN mode', exact: true }).press('Enter')
    await page.getByRole('option', { name: 'static_key', exact: true }).click()
    const confirmation = page.getByRole('dialog').filter({ hasText: 'Change VPN mode' })
    await confirmation.getByRole('button', { name: 'Change VPN mode', exact: true }).click()
    await form.getByRole('combobox', { name: 'Network transport', exact: true }).press('Enter')
    await page.getByRole('option', { name: 'tcp', exact: true }).click()
    await form.getByRole('button', { name: 'Static private key', exact: true }).click()
    await form.getByRole('combobox', { name: 'Static private key', exact: true }).press('Enter')
    await page.getByRole('option', { name: 'Replace credential', exact: true }).click()
    const key = '-----BEGIN OpenVPN Static key V1-----\n' + '01'.repeat(256) + '\n-----END OpenVPN Static key V1-----'
    await form.getByRole('textbox', { name: 'New credential', exact: true }).fill(key)
    await form.getByRole('button', { name: 'Connection and packets', exact: true }).click()
    await form.getByRole('textbox', { name: 'Data cipher / TLS cipher list', exact: true }).fill('AES-256-CBC')
    await form.getByRole('button', { name: 'Tunnel addresses and routing', exact: true }).click()
    await form.getByRole('textbox', { name: 'Peer IPv4 address', exact: true }).fill('10.8.0.2')
    await form.getByRole('button', { name: /^(Save|Add)$/ }).last().click()
    if (mode === 'nexus') await expect(form).toHaveAttribute('inert', '')
    else await expect(form).toBeHidden()
    const row = mode === 'nexus' ? page.getByRole('row').filter({ hasText: tag }) : page.locator('.endpoints__card').filter({ hasText: tag })
    await row.getByRole('button', { name: 'Edit', exact: true }).click()
    form = page.getByRole('dialog').filter({ hasText: 'Edit Endpoint' })
    await form.getByRole('button', { name: 'Static private key', exact: true }).click()
    await expect(form.getByText('A credential is stored. Its value is hidden.', { exact: true })).toBeVisible()
    await expect(form.getByRole('textbox', { name: 'New credential', exact: true })).toHaveCount(0)
  })
}
