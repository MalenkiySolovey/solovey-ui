import { expect, test, type APIResponse, type Page } from '@playwright/test'
import { createHash } from 'node:crypto'
import { csrfToken, enableComponent, login, readServerState, writeJSONArtifact } from './helpers'

type APIEnvelope<T> = {
  success: boolean
  obj?: T
  msg?: string
}

type FallbackSite = {
  id: number
  name: string
  pages?: Array<{ path: string; title: string }>
}

type PublishView = {
  version: string
  active: boolean
}

const componentAPI = 'api/components/fallback-html'

const assertAPIObj = async <T>(response: APIResponse) => {
  const text = await response.text()
  let body: APIEnvelope<T>
  try {
    body = JSON.parse(text) as APIEnvelope<T>
  } catch (error) {
    throw new Error(`Expected JSON API envelope from ${response.url()}, got ${response.status()} ${response.statusText()}: ${text.slice(0, 240)}`, { cause: error })
  }
  expect(response.ok(), body.msg).toBeTruthy()
  expect(body.success, body.msg).toBe(true)
  expect(body.obj).toBeDefined()
  return body.obj as T
}

const componentPost = async <T>(page: Page, path: string, data?: unknown) => {
  const token = await csrfToken(page)
  return assertAPIObj<T>(await page.request.post(`${componentAPI}${path}`, {
    headers: { 'X-CSRF-Token': token },
    data: data ?? {},
  }))
}

const componentGet = async <T>(page: Page, path: string) => (
  assertAPIObj<T>(await page.request.get(`${componentAPI}${path}`))
)

const cleanupSite = async (page: Page, siteID?: number) => {
  if (!siteID) return
  const token = await csrfToken(page).catch(() => '')
  if (!token) return
  const headers = { 'X-CSRF-Token': token }
  await page.request.post(`${componentAPI}/sites/${siteID}/unpublish`, { headers }).catch(() => undefined)
  await page.request.delete(`${componentAPI}/sites/${siteID}`, { headers }).catch(() => undefined)
}

const cleanupPreviousE2ESites = async (page: Page) => {
  const sites = await componentGet<FallbackSite[]>(page, '/sites')
  for (const site of sites) {
    if (site.name.startsWith('E2E public site ')) {
      await cleanupSite(page, site.id)
    }
  }
}

test('fallback-html creates, publishes and exposes a node-safe public site', async ({ page }) => {
  test.setTimeout(90_000)

  await login(page)
  await enableComponent(page, 'fallback-html')
  await cleanupPreviousE2ESites(page)

  let site: FallbackSite | undefined
  try {
    const siteName = `E2E public site ${Date.now()}`
    site = await componentPost<FallbackSite>(page, '/sites', {
      name: siteName,
      enabled: true,
      templateId: 'generated-portal',
    })
    expect(site.id).toBeGreaterThan(0)
    expect(site.pages?.some(item => item.path === '/')).toBe(true)

    await componentPost(page, `/sites/${site.id}/publish`)
    const publishes = await componentGet<PublishView[]>(page, `/sites/${site.id}/publishes`)
    const activePublish = publishes.find(item => item.active)
    expect(activePublish?.version).toEqual(expect.any(String))

    const artifact = await page.request.get(`${componentAPI}/sites/${site.id}/artifact/${activePublish!.version}`)
    if (!artifact.ok()) {
      expect(artifact.ok(), await artifact.text()).toBe(true)
    }
    expect(artifact.headers()['content-type']).toContain('application/gzip')
    expect(artifact.headers()['content-disposition']).toContain('.tar.gz')
    const artifactBody = await artifact.body()
    const artifactSummary = {
      version: activePublish!.version,
      contentType: artifact.headers()['content-type'],
      sizeBytes: artifactBody.length,
      sha256: createHash('sha256').update(artifactBody).digest('hex'),
    }
    expect(artifactSummary.sizeBytes).toBeGreaterThan(0)
    expect(artifactSummary.sha256).toMatch(/^[a-f0-9]{64}$/)
    writeJSONArtifact('fallback-html/artifact.json', artifactSummary)

    await page.goto('fallback-html')
    const card = page.locator('.v-card').filter({ hasText: siteName }).first()
    await expect(card.getByText(siteName)).toBeVisible()
    await expect(card.getByText('Published versions')).toBeVisible()

    const previewRequest = page.waitForResponse(response => (
      response.url().includes(`/sites/${site!.id}/preview`) &&
      response.request().method() === 'POST'
    ))
    await card.getByRole('button', { name: 'Preview' }).evaluate((button: HTMLButtonElement) => button.click())
    await previewRequest
    const preview = page.getByRole('dialog').filter({ hasText: 'Preview /' })
    await expect(preview.locator('iframe[title="Preview"]')).toBeVisible()
    await page.keyboard.press('Escape')

    const backendURL = new URL(readServerState().backendURL)
    const publicHome = await page.request.get(`${backendURL.origin}/`)
    expect(publicHome.status()).toBe(200)
    expect(publicHome.headers()['content-type']).toContain('text/html')
    expect(await publicHome.text()).toContain(siteName)
  } finally {
    await cleanupSite(page, site?.id)
  }
})
