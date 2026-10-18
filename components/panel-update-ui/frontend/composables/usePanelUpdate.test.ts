import { computed, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { ComponentCatalogStatus } from '../types'

const mocks = vi.hoisted(() => ({ post: vi.fn(), get: vi.fn(), grant: vi.fn(), loadData: vi.fn() }))
vi.mock('@/plugins/httputil', () => ({ default: { post: mocks.post, get: mocks.get } }))
vi.mock('@/shared/composables/useSecurityOperations', () => ({ acquireStepUpToken: mocks.grant }))
vi.mock('@/store/modules/data', () => ({ default: () => ({ components: [], loadData: mocks.loadData }) }))
import { usePanelUpdate } from './usePanelUpdate'

beforeEach(() => {
  vi.resetAllMocks()
  vi.stubGlobal('ref', ref)
  vi.stubGlobal('computed', computed)
  vi.stubGlobal('watch', vi.fn())
  vi.stubGlobal('onMounted', vi.fn())
  vi.stubGlobal('onUnmounted', vi.fn())
  mocks.post.mockResolvedValue({ success: true })
  mocks.get.mockResolvedValue({ success: false })
})
afterEach(() => vi.unstubAllGlobals())

describe('component removal assurance', () => {
  const component = { id: 'telegram', name: 'Telegram' } as ComponentCatalogStatus

  it('sends the grant for the exact owner without forwarding the credential', async () => {
    mocks.grant.mockResolvedValue({ token: 'one-use-grant', response: { success: true } })
    const ui = usePanelUpdate()
    ui.openComponentRemoveConfirm(component)
    ui.componentRemovePassword.value = 'private credential'
    await ui.removeComponent()
    expect(mocks.grant).toHaveBeenCalledWith('component.remove', 'component:telegram', 'private credential')
    expect(mocks.post).toHaveBeenCalledWith('api/update/components/telegram/remove', {}, {
      headers: { 'X-Step-Up-Token': 'one-use-grant' },
    })
    expect(ui.componentRemovePassword.value).toBe('')
    expect(ui.componentRemoveConfirm.value).toBe(false)
  })

  it('keeps the dialog open and clears a consumed MFA credential when issuance fails', async () => {
    mocks.grant.mockResolvedValue({ token: null, response: { success: false } })
    const ui = usePanelUpdate()
    ui.openComponentRemoveConfirm(component)
    ui.componentRemovePassword.value = '123456'
    await ui.removeComponent()
    expect(mocks.post).not.toHaveBeenCalled()
    expect(ui.componentRemovePassword.value).toBe('')
    expect(ui.componentRemoveConfirm.value).toBe(true)
    expect(ui.componentAction.value).toBe('')
  })
})
