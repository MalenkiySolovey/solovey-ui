import { expect, test } from '@playwright/test'
import { login } from './helpers'

for (const mode of ['classic', 'nexus']) test(`${mode} TLS credential choices preserve the draft and save only the chosen mode`, async ({ page }) => {
  test.setTimeout(60_000)
  await page.addInitScript(layout => window.localStorage.setItem('sui:ui:mode', layout), mode)
  await login(page)
  await page.goto('tls')
  await page.getByRole('button', { name: 'Add', exact: true }).first().click()
  const form = page.getByRole('dialog').last()
  const name = `w2-${mode}-${Date.now()}`
  await form.getByRole('textbox', { name: 'Name', exact: true }).fill(name)
  const certificate = form.getByRole('textbox', { name: 'Certificate File Path', exact: true })
  const key = form.getByRole('textbox', { name: 'Key File Path', exact: true })
  await certificate.fill('fixture-certificate.pem')
  await key.fill('fixture-key.pem')
  await form.getByRole('button', { name: 'Use Text', exact: true }).first().click()
  await form.getByRole('button', { name: 'Use Path', exact: true }).first().click()
  await expect(certificate).toHaveValue('fixture-certificate.pem')
  await expect(key).toHaveValue('fixture-key.pem')
  await form.getByRole('button', { name: 'Reality', exact: true }).click()
  await form.getByRole('button', { name: 'TLS', exact: true }).click()
  await expect(certificate).toHaveValue('fixture-certificate.pem')
  const submitted = page.waitForRequest(request => request.method() === 'POST' && request.url().endsWith('/api/save'))
  await form.getByRole('button', { name: 'Save', exact: true }).click()
  const request = await submitted
  const params = new URLSearchParams(request.postData() ?? '')
  const profile = JSON.parse(params.get('data') ?? '{}')
  expect(profile.server.certificate_path).toBe('fixture-certificate.pem')
  expect(profile.server.key_path).toBe('fixture-key.pem')
  expect(profile.server).not.toHaveProperty('certificate')
  expect(profile.server).not.toHaveProperty('key')
  await expect(page.getByText(name, { exact: true }).first()).toBeVisible()
})
