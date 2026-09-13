/**
 * Owner-local production browser qualification for Server Protection.
 * The neutral panel runner discovers this contribution through component.json.
 */
export async function run({ component, page, expect, successfulObject }) {
  const apiBase = `api/components/${component.id}`
  const observedResponses = []
  const pageErrors = []
  const observeResponse = response => {
    if (response.url().includes(`/api/components/${component.id}/`)) {
      observedResponses.push({ status: response.status(), url: response.url() })
    }
  }
  page.on('response', observeResponse)
  const observePageError = error => pageErrors.push(String(error))
  page.on('pageerror', observePageError)

  try {
    await page.goto('server-protection')
    await expect(page.getByRole('heading', { name: 'Server Protection' })).toBeVisible()
    await expect(page.getByText('Safe preview with optional apply beta')).toBeVisible()

    const refreshInventory = page.getByRole('button', { name: 'Refresh inventory', exact: true })
    for (let attempt = 0; attempt < 2; attempt += 1) {
      await expect(refreshInventory).toBeEnabled()
      const refreshResponse = page.waitForResponse(response => (
        response.request().method() === 'GET' && response.url().includes(`/api/components/${component.id}/resources`)
      ))
      await refreshInventory.click()
      expect((await refreshResponse).status()).toBe(200)
    }

    await successfulObject(await page.request.get(`${apiBase}/status`))
    await successfulObject(await page.request.get(`${apiBase}/resources`))
    await successfulObject(await page.request.get(`${apiBase}/host-surfaces?refresh=true`))
    await successfulObject(await page.request.get(`${apiBase}/posture`))

    const resourcePage = await successfulObject(
      await page.request.get(`${apiBase}/resources?page=1&limit=1`),
    )
    expect(resourcePage.page).toBe(1)
    expect(resourcePage.limit).toBe(1)
    expect(resourcePage.items).toHaveLength(Math.min(1, resourcePage.total))
    if (resourcePage.total > 1) {
      const nextResourcePage = await successfulObject(
        await page.request.get(`${apiBase}/resources?page=2&limit=1`),
      )
      expect(nextResourcePage).toMatchObject({ page: 2, limit: 1, total: resourcePage.total })
    }

    const targetPage = await successfulObject(
      await page.request.get(`${apiBase}/target-capabilities?page=1&limit=1`),
    )
    expect(targetPage.page).toBe(1)
    expect(targetPage.limit).toBe(1)
    expect(targetPage.items).toHaveLength(Math.min(1, targetPage.total))
    expect(targetPage.targetsV2).toHaveLength(Math.min(1, targetPage.totalV2))
    if (Math.max(targetPage.total, targetPage.totalV2) > 1) {
      const nextTargetPage = await successfulObject(
        await page.request.get(`${apiBase}/target-capabilities?page=2&limit=1`),
      )
      expect(nextTargetPage).toMatchObject({
        page: 2,
        limit: 1,
        total: targetPage.total,
        totalV2: targetPage.totalV2,
      })
    }

    await page.getByRole('tab', { name: 'Surfaces & contracts', exact: true }).click()
    await expect(page.getByText('Read-only facts and contracts.', { exact: false })).toBeVisible()
    await expect(page.getByText('Management and recovery posture', { exact: true })).toBeVisible()

    const baselineResponse = await page.request.get(`${apiBase}/firewall-baseline`)
    const baselineBody = await baselineResponse.json()
    expect(baselineResponse.ok(), baselineBody.msg).toBeTruthy()
    expect(baselineBody.msg).not.toContain('validation_error')

    const previewResponse = page.waitForResponse(response => (
      response.request().method() === 'POST' && response.url().endsWith(`/api/components/${component.id}/firewall/preview`)
    ))
    await page.getByRole('tab', { name: 'Firewall Preview', exact: true }).click()
    await page.getByRole('button', { name: 'Generate preview', exact: true }).click()
    const previewNetworkResponse = await previewResponse
    const previewBody = await previewNetworkResponse.json()
    expect(await previewNetworkResponse.request().headerValue('content-type')).toContain('application/json')
    expect(previewNetworkResponse.request().postData()).toBe('{"includeGeneratedNft":true}')
    expect(previewNetworkResponse.ok(), previewBody.msg).toBeTruthy()
    expect(previewBody.msg).not.toContain('validation_error')
    if (previewBody.success) {
      const preview = previewBody.obj
      expect(preview.revision).toMatch(/^[0-9a-f]{64}$/)
      expect(preview.inputRevision).toMatch(/^[0-9a-f]{64}$/)
      for (const field of ['wouldKeep', 'wouldOpen', 'wouldWarn', 'wouldBlock', 'warnings', 'protectedKeep']) {
        expect(Array.isArray(preview[field]), `${field} must be a JSON array`).toBeTruthy()
      }
      expect(preview.wouldKeep.length).toBeGreaterThan(0)
      expect(preview.wouldOpen.length).toBeGreaterThanOrEqual(0)
    } else {
      // The Windows api-only host has no production listener contributors.
      // Its honest fail-closed result is not the form-body binding regression.
      expect(previewBody.msg).toContain('protectable_resource_inventory_incomplete')
    }
    await expect(page.getByText('validation_error', { exact: false })).toHaveCount(0)
    await expect(refreshInventory).toBeEnabled()

    expect(observedResponses.length).toBeGreaterThan(0)
    expect(observedResponses.filter(response => response.status === 400)).toEqual([])
    expect(observedResponses.filter(response => response.status === 429)).toEqual([])
    expect(observedResponses.filter(response => response.status >= 500)).toEqual([])
    expect(pageErrors).toEqual([])
  } finally {
    page.off('response', observeResponse)
    page.off('pageerror', observePageError)
  }
}
