import { expect, test, type Page } from '@playwright/test'

import { login, readServerState } from './helpers'

const syntheticDbFile = {
  name: 'x-ui.db',
  mimeType: 'application/octet-stream',
  buffer: Buffer.from('SQLite format 3\0'),
}

const fillSecurityVerification = async (page: Page) => {
  await page.getByTestId('migrate-xui-security-credential').locator('input').fill(readServerState().password)
}

const uploadSyntheticDb = async (page: Page) => {
  await expect(page.getByTestId('migrate-xui-build-plan')).toBeVisible()
  const fileInput = page.locator('.migrate-xui input[type="file"]').first()
  await expect(fileInput).toBeAttached()
  await fileInput.setInputFiles(syntheticDbFile)
}

const chooseMigrateSelectOption = async (page: Page, testId: string, option: string) => {
  const select = page.getByTestId(testId)
  await select.locator('.v-field').click()
  await page.getByRole('option', { name: option, exact: true }).last().click()
}

const expectImportPage = async (page: Page) => {
  await expect(page).toHaveURL(/\/migrate-xui$/)
  await expect(page.getByRole('heading', { name: 'Panel database import' })).toBeVisible()
}

test('shows inline apply failure on review step', async ({ page }) => {
  await login(page)
  await page.route('**/api/import-xui/plan', async route => route.fulfill({
    json: {
      success: true,
      msg: '',
      obj: {
        source: { hash: 'apply-failure-hash' },
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
  await expectImportPage(page)
  await uploadSyntheticDb(page)
  await page.getByRole('button', { name: 'Build plan' }).click()
  await fillSecurityVerification(page)
  await page.getByRole('button', { name: 'Apply plan' }).click()

  await expect(page.getByTestId('migrate-xui-apply-error')).toBeVisible()
  await expect(page.getByTestId('migrate-xui-apply-error')).toContainText('synthetic apply failed')
  await expect(page.getByText('Review migration plan')).toBeVisible()
})

test('waits for rollback database health before reload', async ({ page }) => {
  let healthCalls = 0
  await login(page)
  await page.route('**/api/import-xui/plan', async route => route.fulfill({
    json: {
      success: true,
      msg: '',
      obj: {
        source: { hash: 'rollback-health-hash' },
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
    json: {
      success: true,
      msg: '',
      obj: {
        backup_path: 's-ui-pre-xui-import-100.db',
        summary: { inbounds: { created: 1 } },
      },
    },
  }))
  await page.route('**/api/import-xui/rollback', async route => route.fulfill({
    json: { success: true, msg: '', obj: null },
  }))
  await page.route('**/api/status**', async route => {
    healthCalls += 1
    await route.fulfill({
      json: { success: true, msg: '', obj: { db: { clients: 0 } } },
    })
  })

  await page.goto('migrate-xui')
  await expectImportPage(page)
  await uploadSyntheticDb(page)
  await page.getByRole('button', { name: 'Build plan' }).click()
  await fillSecurityVerification(page)
  await page.getByRole('button', { name: 'Apply plan' }).click()
  await expect(page.getByText('Migration result')).toBeVisible()
  await fillSecurityVerification(page)
  await page.getByRole('button', { name: 'Restore previous database' }).click()

  await expect.poll(() => healthCalls).toBeGreaterThan(0)
})

test('hides generated admin passwords until reveal and auto-clears them', async ({ page }) => {
  await page.addInitScript(() => {
    const nativeSetTimeout = window.setTimeout
    window.setTimeout = ((handler: TimerHandler, timeout?: number, ...args: any[]) => {
      const adjusted = timeout === 5 * 60 * 1000 ? 5000 : timeout
      return nativeSetTimeout(handler, adjusted, ...args)
    }) as typeof window.setTimeout
  })
  await login(page)
  await page.route('**/api/import-xui/plan', async route => route.fulfill({
    json: {
      success: true,
      msg: '',
      obj: {
        source: { hash: 'generated-admin-hash' },
        defaults: {},
        items: [
          {
            kind: 'admin',
            srcId: '1',
            srcTag: 'migrated-admin',
            dstTag: 'migrated-admin',
            action: 'create',
            conflict: false,
            previewJson: { username: 'migrated-admin' },
          },
        ],
      },
    },
  }))
  await page.route('**/api/import-xui/apply', async route => route.fulfill({
    json: {
      success: true,
      msg: '',
      obj: {
        backup_path: 's-ui-pre-xui-import-101.db',
        summary: { admins: { created: 1 } },
        generated_admins: [
          { username: 'migrated-admin', password: 'generated-secret-password' },
        ],
      },
    },
  }))

  await page.goto('migrate-xui')
  await expectImportPage(page)
  await uploadSyntheticDb(page)
  await page.getByRole('button', { name: 'Build plan' }).click()
  await fillSecurityVerification(page)
  await page.getByRole('button', { name: 'Apply plan' }).click()
  await expect(page.getByText('Migration result')).toBeVisible()
  await expect(page.locator('body')).not.toContainText('generated-secret-password')
  await expect(page.getByTestId('migrate-xui-generated-admins-hidden')).toBeVisible()

  await page.getByRole('button', { name: 'Reveal passwords' }).click()
  await expect(page.locator('body')).toContainText('generated-secret-password')
  await expect(page.locator('body')).not.toContainText('generated-secret-password', { timeout: 7000 })
  await expect(page.getByTestId('migrate-xui-generated-admins')).toBeHidden()
})

test('sends reset_required adminMode when building a plan', async ({ page }) => {
  let planRequestBody = ''
  await login(page)
  await page.route('**/api/import-xui/plan', async route => {
    planRequestBody = route.request().postData() ?? ''
    await route.fulfill({
      json: {
        success: true,
        msg: '',
        obj: {
          source: { hash: 'reset-required-hash' },
          defaults: { adminMode: 'reset_required' },
          items: [
            {
              kind: 'admin',
              srcId: '1',
              srcTag: 'migrated-admin',
              dstTag: 'migrated-admin',
              action: 'create',
              conflict: false,
              adminMode: 'reset_required',
              previewJson: { username: 'migrated-admin', mode: 'reset_required' },
            },
          ],
        },
      },
    })
  })

  await page.goto('migrate-xui')
  await expectImportPage(page)
  await uploadSyntheticDb(page)
  await chooseMigrateSelectOption(page, 'migrate-xui-admin-mode', 'Require password reset')
  await page.getByRole('button', { name: 'Build plan' }).click()

  await expect.poll(() => planRequestBody).toContain('adminMode')
  expect(planRequestBody).toContain('reset_required')
})

