import { expect, test, type APIResponse, type Page } from '@playwright/test'
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

type NodePublishPlan = {
  schema: string
  nodeId: string
  version: string
  artifact: {
    filename: string
    sha256: string
    sizeBytes: number
  }
  signature: {
    mode: string
    required: boolean
  }
  apply: {
    stagingRequired: boolean
    atomicSwap: boolean
    rollbackOnFailure: boolean
  }
}

const componentAPI = 'api/components/fallback-html'

const assertAPIObj = async <T>(response: APIResponse) => {
  const body = await response.json() as APIEnvelope<T>
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

test('fallback-html creates, publishes and exposes a node-safe public site', async ({ page }) => {
  test.setTimeout(90_000)

  await login(page)
  await enableComponent(page, 'fallback-html')

  const siteName = `E2E public site ${Date.now()}`
  const site = await componentPost<FallbackSite>(page, '/sites', {
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

  const plan = await componentGet<NodePublishPlan>(
    page,
    `/sites/${site.id}/node-plan/${activePublish!.version}?nodeId=e2e-node-1`,
  )
  expect(plan).toMatchObject({
    schema: 'solovey-ui/fallback-html-node-publish-plan/v1',
    nodeId: 'e2e-node-1',
    version: activePublish!.version,
    signature: { mode: 'orchestrator-channel', required: true },
    apply: { stagingRequired: true, atomicSwap: true, rollbackOnFailure: true },
  })
  expect(plan.artifact.filename).toContain('.tar.gz')
  expect(plan.artifact.sha256).toMatch(/^[a-f0-9]{64}$/)
  expect(plan.artifact.sizeBytes).toBeGreaterThan(0)
  writeJSONArtifact('fallback-html/node-plan.json', plan)

  await page.goto('fallback-html')
  const card = page.locator('.v-card').filter({ hasText: siteName }).first()
  await expect(card.getByText(siteName)).toBeVisible()
  await expect(card.getByText('Published versions')).toBeVisible()
  await expect(card.getByRole('button', { name: 'Node plan' })).toBeVisible()

  const previewRequest = page.waitForResponse(response => (
    response.url().includes(`/sites/${site.id}/preview`) &&
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
})
