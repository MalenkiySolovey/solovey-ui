import { expect, test } from '@playwright/test'
import { enableComponent, login } from './helpers'

for (const mode of ['classic', 'nexus']) {
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
