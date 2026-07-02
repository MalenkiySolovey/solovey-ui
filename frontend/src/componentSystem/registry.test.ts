import { afterEach, describe, expect, it } from 'vitest'
import { componentRoutes, navItems, registerComponent, slotEntries, unregisterComponent } from './registry'

const ids = ['test-alpha', 'test-beta']

afterEach(() => {
  for (const id of ids) unregisterComponent(id)
})

describe('componentSystem registry', () => {
  it('registers declarative routes, navigation items and slots by component id', () => {
    registerComponent({
      id: 'test-alpha',
      apiVersion: '1.0',
      routes: [{ path: '/alpha', name: 'alpha', component: async () => ({ default: {} as any }) }],
      nav: [{ title: 'alpha', icon: 'mdi-test', path: '/alpha', order: 20 }],
      slots: [{ slot: 'dashboard:widgets', component: async () => ({ default: {} as any }), order: 10 }],
    })

    expect(componentRoutes.value.map(route => [route.path, route.meta?.componentId])).toContainEqual(['/alpha', 'test-alpha'])
    expect(navItems.value.map(item => item.path)).toContain('/alpha')
    expect(slotEntries('dashboard:widgets')).toHaveLength(1)
  })

  it('replaces duplicate registrations and unregisters every contribution', () => {
    registerComponent({
      id: 'test-beta',
      apiVersion: '1.0',
      routes: [{ path: '/old', name: 'old', component: async () => ({ default: {} as any }) }],
    })
    registerComponent({
      id: 'test-beta',
      apiVersion: '1.0',
      routes: [{ path: '/new', name: 'new', component: async () => ({ default: {} as any }) }],
    })

    expect(componentRoutes.value.map(route => route.path)).not.toContain('/old')
    expect(componentRoutes.value.map(route => route.path)).toContain('/new')

    unregisterComponent('test-beta')
    expect(componentRoutes.value.map(route => route.path)).not.toContain('/new')
  })
})
