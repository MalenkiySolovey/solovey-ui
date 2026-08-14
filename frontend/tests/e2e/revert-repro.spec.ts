import { test, expect } from '@playwright/test'
import fs from 'node:fs'
import path from 'node:path'
import { login, mutationHeaders, readServerState } from './helpers'

// REGRESSION GUARD for the "unsaved edits revert before save" fix.
//
// Before the fix, Basics/DNS/Rules bound v-model to Data().config BY REFERENCE, so a background
// reload (10s poll / WS) that ran setNewData() (this.config = data.config) wiped unsaved edits.
// The fix makes these pages edit a LOCAL CLONE of the config. This test proves the unsaved edit
// now SURVIVES a real server-side config change, while that change still reaches the store, and
// that Save still works afterwards.

const OUT = path.join(process.cwd(), '..', 'tests', 'baseline', 'e2e', 'revert-repro')
const xrw = { 'X-Requested-With': 'XMLHttpRequest' }

test('unsaved Basics edit SURVIVES a background server-side config change (fix regression)', async ({ page, browser }) => {
  test.setTimeout(60_000)
  const { baseURL } = readServerState()
  fs.mkdirSync(OUT, { recursive: true })
  const summary: Record<string, unknown> = {}

  await page.addInitScript(() => {
    window.localStorage.setItem('sui:ui:mode', 'classic')
  })
  await login(page)
  await page.goto('basics', { waitUntil: 'domcontentloaded' })
  await page.locator('.v-expansion-panel-title', { hasText: 'Logs' }).first().click()
  const outputField = page.getByLabel('Output', { exact: true })
  await expect(outputField).toBeVisible()

  const marker = `UNSAVED-EDIT-${Date.now()}`
  await outputField.fill(marker)
  await outputField.blur()
  await expect(outputField).toHaveValue(marker)
  summary.unsavedMarker = marker

  // A second admin session changes the config on the server (the trigger that used to revert).
  const serverValue = `SERVER-CHANGED-${Date.now()}`
  const ctxB = await browser.newContext({ baseURL })
  const pageB = await ctxB.newPage()
  await login(pageB)
  const req = pageB.request
  const token = (await (await req.get('api/csrf', { headers: xrw })).json())?.obj?.token
  const cfg = (await (await req.get('api/load', { headers: xrw })).json())?.obj?.config
  cfg.log = cfg.log ?? {}
  cfg.log.output = serverValue
  const saveJson = await (await req.post('api/save', {
    headers: mutationHeaders(token),
    form: { object: 'config', action: 'set', data: JSON.stringify(cfg) },
  })).json()
  expect(saveJson.success, `ctxB save: ${JSON.stringify(saveJson)}`).toBe(true)
  summary.serverValue = serverValue

  // Wait 15s: guarantees at least one 10s background poll on context A.
  await page.waitForTimeout(15_000)

  // The background change DID reach the server/store...
  const serverNow = (await (await page.request.get('api/load', { headers: xrw })).json())?.obj?.config?.log?.output
  summary.serverLogOutputAfter = serverNow
  // ...but the form kept the unsaved edit (isolated from the live store).
  const finalValue = await outputField.inputValue()
  summary.finalValueInUI = finalValue
  await page.screenshot({ path: path.join(OUT, '3-fixed-edit-survives.png'), fullPage: true })

  expect(serverNow, 'background config change must have reached the server').toBe(serverValue)
  expect(finalValue, 'unsaved edit must SURVIVE the background reload (fix)').toBe(marker)

  // And Save still works: it commits the form clone (last-write-wins over the concurrent change).
  await page.getByRole('button', { name: 'Save' }).click()
  await expect.poll(async () => {
    const v = (await (await page.request.get('api/load', { headers: xrw })).json())?.obj?.config?.log?.output
    return v
  }, { timeout: 10_000 }).toBe(marker)
  summary.savedOk = true
  fs.writeFileSync(path.join(OUT, 'summary.json'), JSON.stringify(summary, null, 2))

  await ctxB.close()
})

test('Basics, DNS and Rules render without errors after the local-clone refactor', async ({ page }) => {
  test.setTimeout(45_000)
  const errors: string[] = []
  page.on('pageerror', (e) => errors.push(String(e)))
  await login(page)
  for (const route of ['basics', 'dns', 'rules']) {
    await page.goto(route, { waitUntil: 'domcontentloaded' })
    await expect(page.getByRole('button', { name: 'Save' }).first(), `Save button on /${route}`).toBeVisible({ timeout: 10_000 })
  }
  expect(errors, `uncaught page errors: ${errors.join(' | ')}`).toEqual([])
})
