import { afterEach, describe, expect, it } from 'vitest'
import { componentRoutes, contributionFor, navItems, slotEntries, unregisterComponent } from './registry'
import { register, unregister } from '../../../components/server-protection/frontend/index'
const componentId = 'server-protection'
afterEach(() => { unregisterComponent(componentId) })
describe('Server Protection Product contribution integration', () => {
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

  it('removes inbound contributions when the component is disabled', () => {
    register()
    unregister()
    expect(slotEntries('inbound:editor')).toHaveLength(0)
    expect(slotEntries('inbound:status-detail')).toHaveLength(0)
  })


})
