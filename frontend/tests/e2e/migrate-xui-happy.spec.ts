import { expect, test, type Page } from '@playwright/test'

const mockAuthenticatedShell = async (page: Page) => {
  await page.addInitScript(() => {
    window.localStorage.setItem('locale', 'en')
  })
  await page.route('**/api/load**', async route => route.fulfill({
    json: {
      success: true,
      msg: '',
      obj: {
        onlines: { inbound: [], outbound: [], user: [] },
        config: {},
        inbounds: [],
        outbounds: [],
        services: [],
        endpoints: [],
        clients: [],
        tls: [],
      },
    },
  }))
  await page.route('**/api/csrf', async route => route.fulfill({
    json: { success: true, msg: '', obj: { token: 'cluster-h-csrf' } },
  }))
  await page.route('**/api/realtime/ws-token', async route => route.fulfill({
    json: { success: true, msg: '', obj: { token: 'cluster-h-ws-token' } },
  }))
  await page.route('**/api/logout', async route => route.fulfill({
    json: { success: true, msg: '', obj: null },
  }))
}

// XFAIL: пункты 43, 44, 45, 46 реестра; полный happy path требует test-db/x-ui.db и test-db/s-ui.db.
test.skip('upload synthetic db, build plan, apply, download JSON/Markdown report, and rollback', async () => {})

test('Issue43 shows inline apply failure on review step', async ({ page }) => {
  await mockAuthenticatedShell(page)
  await page.route('**/api/import-xui/plan', async route => route.fulfill({
    json: {
      success: true,
      msg: '',
      obj: {
        source: { hash: 'issue43-hash' },
        defaults: {},
        items: [
          {
            kind: 'inbound',
            srcId: '1',
            srcTag: 'demo-inbound',
            dstTag: 'demo-inbound',
            action: 'create',
            conflict: false,
            previewJson: { tag: 'demo-inbound' },
          },
        ],
      },
    },
  }))
  await page.route('**/api/import-xui/apply', async route => route.fulfill({
    json: { success: false, msg: 'synthetic apply failed', obj: null },
  }))

  await page.goto('migrate-xui')
  await expect(page).toHaveURL(/\/migrate-xui$/)
  await expect(page.getByText('Migrate from 3x-ui')).toBeVisible()
  await page.locator('input[type="file"]').setInputFiles({
    name: 'x-ui.db',
    mimeType: 'application/octet-stream',
    buffer: Buffer.from('SQLite format 3\0'),
  })
  await page.getByRole('button', { name: 'Build plan' }).click()
  await page.getByRole('button', { name: 'Apply plan' }).click()

  await expect(page.getByTestId('migrate-xui-apply-error')).toBeVisible()
  await expect(page.getByTestId('migrate-xui-apply-error')).toContainText('synthetic apply failed')
  await expect(page.getByText('Review migration plan')).toBeVisible()
})

// XFAIL: пункт 45 реестра; generated admin password должен быть скрыт до явного reveal.
test.skip('generated admin password is shown once via reveal pattern, not raw JSON in DOM', async () => {})

// XFAIL: пункт 46 реестра; reset_required пока не имеет backend force-reset semantics.
test.skip('adminMode reset_required is disabled or warns until backend contract exists', async () => {})
