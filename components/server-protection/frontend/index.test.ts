import { afterEach, describe, expect, it, vi } from 'vitest'
import { componentRoutes, contributionFor, navItems, slotEntries, unregisterComponent } from '@/componentSystem/registry'
import { register } from './index'
import { stateColor, useServerProtection } from './useServerProtection'

const apiMocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() }))

vi.mock('./api', () => ({ protectionAPI: apiMocks }))

const componentId = 'server-protection'

afterEach(() => {
  unregisterComponent(componentId)
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  apiMocks.get.mockReset()
  apiMocks.post.mockReset()
})

describe('server-protection frontend contribution', () => {
  it('registers only through the component system', () => {
    register()
    const contribution = contributionFor(componentId)
    expect(contribution?.apiVersion).toBe('1.0')
    expect(Object.keys(contribution?.locales ?? {}).sort()).toEqual(['en', 'ru'])
    expect(componentRoutes.value).toEqual(expect.arrayContaining([
      expect.objectContaining({ path: '/server-protection', name: 'component.server-protection' }),
    ]))
    expect(navItems.value).toEqual(expect.arrayContaining([
      expect.objectContaining({ path: '/server-protection', section: 'system' }),
    ]))
    expect(slotEntries('inbound:editor')).toHaveLength(1)
    expect(slotEntries('inbound:status-detail')).toHaveLength(1)
  })

  it('removes inbound contributions when the component is disabled', async () => {
    const { unregister } = await import('./index')
    register()
    unregister()
    expect(slotEntries('inbound:editor')).toHaveLength(0)
    expect(slotEntries('inbound:status-detail')).toHaveLength(0)
  })

  it('renders degraded and missing capabilities distinctly', () => {
    expect(stateColor('supported')).toBe('success')
    expect(stateColor('degraded')).toBe('warning')
    expect(stateColor('unsupported')).toBe('error')
    expect(stateColor('missing_capability')).toBe('info')
  })

	it('keeps legacy free-form fronting actions out of the page model', () => {
		vi.spyOn(console, 'warn').mockImplementation(() => undefined)
		const model = useServerProtection()
		expect('frontingDraft' in model).toBe(false)
		expect('syncFronting' in model).toBe(false)
		expect('applyFronting' in model).toBe(false)
		expect(apiMocks.post).not.toHaveBeenCalled()
		expect(stateColor('PREPARED')).toBe('warning')
		expect(stateColor('APPLIED')).toBe('success')
		expect(stateColor('RECONCILE_REQUIRED')).toBe('error')
	})
})
