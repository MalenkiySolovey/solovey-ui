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
    await page.getByRole('textbox', { name: 'Include names (regular expression)', exact: true }).fill('^inline')
    await page.getByRole('textbox', { name: 'Exclude names (regular expression)', exact: true }).fill('-test$')
    await page.getByLabel('Remove emoji from imported names', { exact: true }).check()
    await page.getByRole('button', { name: 'Imported outbound dial options', exact: true }).click()
    await page.getByRole('button', { name: 'Dial Options', exact: true }).click()
    await page.getByRole('checkbox', { name: 'Connection Timeout', exact: true }).check()
    await page.keyboard.press('Escape')
    await page.getByRole('spinbutton', { name: 'Connection Timeout', exact: true }).fill('3')
    const sourceFile = page.locator('form input[type="file"]')
    await sourceFile.setInputFiles({ name: 'oversize.txt', mimeType: 'text/plain', buffer: Buffer.alloc((512 << 10) + 1, 'x') })
    await expect(page.getByText('Choose a non-empty file up to 512 KiB.', { exact: true })).toBeVisible()
    await expect(page.locator('form').getByRole('button', { name: 'Add', exact: true })).toBeDisabled()
    const privateSource = JSON.stringify({ outbounds: [{ type: 'shadowsocks', tag: 'inline', server: 'source.example', server_port: 443, method: 'aes-128-gcm', password: 'inline-secret-fixture' }] }) + '\n' + ' '.repeat(1024)
    await sourceFile.setInputFiles({ name: 'source.json', mimeType: 'application/json', buffer: Buffer.from(privateSource) })
    await expect(page.getByRole('textbox', { name: 'New credential', exact: true })).toHaveValue(privateSource)
    expect(mutations).toHaveLength(0)
    await page.locator('form').getByRole('button', { name: 'Add', exact: true }).click()
    await expect.poll(() => mutations.length).toBe(1)
    expect(mutations[0]).toMatchObject({ sourceType: 'inline', url: '', autoUpdate: false, include: '^inline', exclude: '-test$', removeEmojis: true, dialerOverride: { connect_timeout: '3s' }, content: { intent: 'replace', value: privateSource } })
    await page.getByRole('button', { name: /Owned inline source/ }).click()
    await page.getByRole('button', { name: 'Edit', exact: true }).click()
    await expect(page.getByRole('combobox', { name: 'Source type', exact: true })).toBeDisabled()
    await expect(page.getByRole('textbox', { name: 'Include names (regular expression)', exact: true })).toHaveValue('^inline')
    await expect(page.getByRole('textbox', { name: 'Exclude names (regular expression)', exact: true })).toHaveValue('-test$')
    await expect(page.getByLabel('Remove emoji from imported names', { exact: true })).toBeChecked()
    const dialPanel = page.getByRole('button', { name: 'Imported outbound dial options', exact: true })
    if (await dialPanel.getAttribute('aria-expanded') !== 'true') await dialPanel.click()
    await expect(page.getByRole('spinbutton', { name: 'Connection Timeout', exact: true })).toHaveValue('3')
    await expect(page.getByText('A credential is stored. Its value is hidden.', { exact: true })).toBeVisible()
    await expect(page.getByRole('textbox', { name: 'New credential', exact: true })).toHaveCount(0)
    await expect(page.getByText('inline-secret-fixture', { exact: false })).toHaveCount(0)
    await page.locator('form').getByRole('button', { name: 'Update', exact: true }).click()
    await expect.poll(() => mutations.length).toBe(2)
    expect(mutations[1].content).toMatchObject({ intent: 'unchanged', reference: content.reference, rotationRevision: content.rotationRevision })
    expect(JSON.stringify(mutations[1])).not.toContain('inline-secret-fixture')
  })

  test(`${mode} creates and edits a watched host file source`, async ({ page }) => {
    await page.addInitScript(selected => window.localStorage.setItem('sui:ui:mode', selected), mode)
    await login(page)
    await enableComponent(page, 'remote-outbound-subscriptions')
    const sources: Record<string, unknown>[] = []
    const mutations: Record<string, unknown>[] = []
    await page.route('**/api/remote-outbound-subscriptions', route => route.fulfill({ json: { success: true, msg: '', obj: sources } }))
    await page.route('**/api/remote-outbound-subscriptions/save', async route => {
      const payload = JSON.parse(new URLSearchParams(route.request().postData() ?? '').get('data') ?? '{}')
      mutations.push(payload)
      sources.splice(0, sources.length, { ...payload, id: 1, groups: [], connections: [], lastUpdated: 0, lastError: '' })
      await route.fulfill({ json: { success: true, msg: '', obj: null } })
    })
    await page.goto('hosts/local/remote-subscriptions')
    await page.getByRole('combobox', { name: 'Source type', exact: true }).press('ArrowDown')
    await page.getByRole('option', { name: 'Host file', exact: true }).click()
    await expect(page.getByRole('textbox', { name: 'Subscription URL', exact: true })).toHaveCount(0)
    await expect(page.getByRole('textbox', { name: 'Source User-Agent', exact: true })).toHaveCount(0)
    await expect(page.locator('form input[type="file"]')).toHaveCount(0)
    await expect(page.getByRole('textbox', { name: 'New credential', exact: true })).toHaveCount(0)
    await page.getByRole('textbox', { name: 'Name', exact: true }).fill('Host file source')
    await page.getByRole('textbox', { name: 'Source path on the Panel host', exact: true }).fill('/var/lib/solovey-ui/source.json')
    await page.getByLabel('Auto refresh', { exact: true }).check()
    await page.getByRole('spinbutton', { name: 'Interval', exact: true }).fill('10')
    await page.locator('form').getByRole('button', { name: 'Add', exact: true }).click()
    await expect.poll(() => mutations.length).toBe(1)
    expect(mutations[0]).toMatchObject({ sourceType: 'file', sourcePath: '/var/lib/solovey-ui/source.json', url: '', userAgent: '', autoUpdate: true, updateInterval: 600 })
    expect(mutations[0]).not.toHaveProperty('content')
    await page.getByRole('button', { name: /Host file source/ }).click()
    await expect(page.getByText('/var/lib/solovey-ui/source.json', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Edit', exact: true }).click()
    await expect(page.getByRole('combobox', { name: 'Source type', exact: true })).toBeDisabled()
    await expect(page.getByLabel('Auto refresh', { exact: true })).toBeChecked()
    await expect(page.getByRole('spinbutton', { name: 'Interval', exact: true })).toHaveValue('10')
    await page.getByRole('textbox', { name: 'Source path on the Panel host', exact: true }).fill('/var/lib/solovey-ui/replacement.json')
    await page.locator('form').getByRole('button', { name: 'Update', exact: true }).click()
    await expect.poll(() => mutations.length).toBe(2)
    expect(mutations[1]).toMatchObject({ id: 1, sourceType: 'file', sourcePath: '/var/lib/solovey-ui/replacement.json', autoUpdate: true })
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
