import { expect, test } from '@playwright/test'
import { enableComponent, login } from './helpers'

for (const mode of ['classic', 'nexus']) {
  test(`${mode} creates an inline source and edits only its masked reference`, async ({ page }) => {
    await page.addInitScript(selected => window.localStorage.setItem('sui:ui:mode', selected), mode)
    await login(page)
    await enableComponent(page, 'remote-outbound-subscriptions')
    const sources: Record<string, unknown>[] = []
    const mutations: Record<string, unknown>[] = []
    const content = { reference: { owner: 'remote-outbound-subscriptions', id: 'record:1:content', policy: 'OWNER_USE_AND_PRESENCE', projection: 'MASKED' }, present: true, rotationRevision: 'a'.repeat(64) }
    await page.route('**/api/remote-outbound-subscriptions', route => route.fulfill({ json: { success: true, msg: '', obj: sources } }))
    await page.route('**/api/remote-outbound-subscriptions/save', async route => {
      const payload = JSON.parse(new URLSearchParams(route.request().postData() ?? '').get('data') ?? '{}')
      mutations.push(payload)
      sources.splice(0, sources.length, { ...payload, id: 1, content, groups: [], connections: [], lastUpdated: 0, lastError: '' })
      await route.fulfill({ json: { success: true, msg: '', obj: null } })
    })
    await page.goto('hosts/local/remote-subscriptions')
    await page.getByRole('combobox', { name: 'Source type', exact: true }).press('ArrowDown')
    await page.getByRole('option', { name: 'Inline content', exact: true }).click()
    await expect(page.getByRole('textbox', { name: 'Subscription URL', exact: true })).toHaveCount(0)
    await expect(page.getByRole('switch', { name: 'Auto refresh', exact: true })).toHaveCount(0)
    await page.getByRole('textbox', { name: 'Name', exact: true }).fill('Owned inline source')
    await page.getByRole('combobox', { name: 'Source content', exact: true }).press('ArrowDown')
    await page.getByRole('option', { name: 'Replace credential', exact: true }).click()
    const privateSource = JSON.stringify({ outbounds: [{ type: 'shadowsocks', tag: 'inline', server: 'source.example', server_port: 443, method: 'aes-128-gcm', password: 'inline-secret-fixture' }] })
    await page.getByRole('textbox', { name: 'New credential', exact: true }).fill(privateSource)
    await page.locator('form').getByRole('button', { name: 'Add', exact: true }).click()
    await expect.poll(() => mutations.length).toBe(1)
    expect(mutations[0]).toMatchObject({ sourceType: 'inline', url: '', autoUpdate: false, content: { intent: 'replace', value: privateSource } })
    await page.getByRole('button', { name: /Owned inline source/ }).click()
    await page.getByRole('button', { name: 'Edit', exact: true }).click()
    await expect(page.getByRole('combobox', { name: 'Source type', exact: true })).toBeDisabled()
    await expect(page.getByText('A credential is stored. Its value is hidden.', { exact: true })).toBeVisible()
    await expect(page.getByRole('textbox', { name: 'New credential', exact: true })).toHaveCount(0)
    await expect(page.getByText('inline-secret-fixture', { exact: false })).toHaveCount(0)
    await page.locator('form').getByRole('button', { name: 'Update', exact: true }).click()
    await expect.poll(() => mutations.length).toBe(2)
    expect(mutations[1].content).toMatchObject({ intent: 'unchanged', reference: content.reference, rotationRevision: content.rotationRevision })
    expect(JSON.stringify(mutations[1])).not.toContain('inline-secret-fixture')
  })

  test(`${mode} shows preserved endpoint mapping failures and their diagnostics`, async ({ page }) => {
    await page.addInitScript(selected => window.localStorage.setItem('sui:ui:mode', selected), mode)
    await login(page)
    await enableComponent(page, 'remote-outbound-subscriptions')
    await page.route('**/api/remote-outbound-subscriptions', async route => route.fulfill({
      json: {
        success: true,
        msg: '',
        obj: [{
          id: 1,
          name: 'Endpoint fixture',
          url: 'https://source.example/subscription',
          enabled: true,
          tagPrefix: 'fixture',
          autoUpdate: false,
          updateInterval: 86400,
          lastUpdated: 0,
          lastError: '',
          groups: [],
          connections: [
            { id: 11, name: 'needs-mapping', projectionStatus: 'MANUAL_MAPPING_REQUIRED' },
            { id: 12, name: 'unsupported-transport', projectionStatus: 'UNSUPPORTED' },
          ].map(connection => ({
            ...connection,
            subscriptionId: 1,
            type: 'vless',
            outboundTag: `fixture-${connection.id}`,
            enabled: true,
            synced: false,
            sortOrder: connection.id,
            projectionDiagnostics: [{ code: 'transport_substitution_forbidden', field: 'transport' }],
          })),
        }],
      },
    }))

    await page.goto('hosts/local/remote-subscriptions')
    await expect(page.getByTestId('host-context')).toContainText('Host: Local')
    await page.getByRole('button', { name: /Endpoint fixture/ }).click()
    const manual = page.getByRole('row').filter({ hasText: 'needs-mapping' })
    await expect(manual.getByText('Manual mapping required', { exact: true })).toBeVisible()
    const unsupported = page.getByRole('row').filter({ hasText: 'unsupported-transport' })
    await expect(unsupported.getByText('Unsupported', { exact: true })).toBeVisible()
    await unsupported.getByText('Unsupported', { exact: true }).hover()
    await expect(page.getByRole('tooltip', { name: 'transport_substitution_forbidden: transport', exact: true })).toBeVisible()
  })
}
