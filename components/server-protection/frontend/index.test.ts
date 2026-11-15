import { afterEach, describe, expect, it, vi } from 'vitest'
import { register, unregister } from './index'
import { stateColor, useServerProtection } from './useServerProtection'
const apiMocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() }))
const contributionMocks = vi.hoisted(() => ({ registerComponent: vi.fn(), unregisterComponent: vi.fn() }))
vi.mock('./api', () => ({ protectionAPI: apiMocks }))
vi.mock('@solovey/component-ui/contributions', () => contributionMocks)

afterEach(() => { vi.restoreAllMocks(); vi.clearAllMocks(); vi.unstubAllGlobals() })
describe('Server Protection public UI contribution', () => {
  it('declares the existing route, theme icons, inbound slots and locales through the public owner', () => {
    register()
    expect(contributionMocks.registerComponent).toHaveBeenCalledOnce()
    const contribution = contributionMocks.registerComponent.mock.calls[0]?.[0]
    expect(contribution).toMatchObject({ id: 'server-protection', apiVersion: '1.0', routes: [{path: '/server-protection',name: 'component.server-protection'}],
      nav: [{path: '/server-protection',order: 123,section: 'system',icon: 'mdi-shield-lock-outline',nexusIcon: 'lucide:shield-check'}],
      slots: [{slot: 'inbound:editor',order: 220},{slot: 'inbound:status-detail',order: 220}] })
    expect(Object.keys(contribution.locales).sort()).toEqual(['en','ru'])
    expect(typeof contribution.routes[0].component).toBe('function')
    expect(contribution.slots.every((entry: {component: unknown}) => typeof entry.component === 'function')).toBe(true)
  })
  it('unregisters through the same public contribution owner', () => {
    unregister()
    expect(contributionMocks.unregisterComponent).toHaveBeenCalledExactlyOnceWith('server-protection')
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
