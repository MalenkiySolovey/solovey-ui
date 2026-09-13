import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { Component } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Data from '@/store/modules/data'
import { componentSystemBundledIDs, syncEnabledComponents } from './loader'
import { contributionFor, registerComponent, registeredComponents, unregisterComponent } from './registry'
import type { ComponentStatus } from '@/store/modules/data'

const entries = vi.hoisted(() => ({
  loadAlpha: vi.fn(),
  loadBeta: vi.fn(),
  loadBroken: vi.fn(),
  unregisterAlpha: vi.fn(),
}))

vi.mock('virtual:solovey-component-entries', () => ({
  componentEntries: {
    'fixture-alpha': entries.loadAlpha,
    'fixture-beta': entries.loadBeta,
    'fixture-broken': entries.loadBroken,
  },
}))

beforeEach(() => {
  setActivePinia(createPinia())
  for (const component of registeredComponents.value) unregisterComponent(component.id)
  entries.loadAlpha.mockReset().mockResolvedValue({
    register: () => registerComponent({
      id: 'fixture-alpha',
      apiVersion: '1.0',
      routes: [{
        path: '/fixture-alpha',
        name: 'fixture.alpha',
        component: async () => ({ default: {} as Component }),
      }],
    }),
    unregister: () => {
      entries.unregisterAlpha()
      unregisterComponent('fixture-alpha')
    },
  })
  entries.loadBeta.mockReset().mockResolvedValue({
    register: () => registerComponent({
      id: 'fixture-beta',
      apiVersion: '1.0',
    }),
    unregister: () => unregisterComponent('fixture-beta'),
  })
  entries.loadBroken.mockReset().mockRejectedValue(new Error('fixture load failed'))
  entries.unregisterAlpha.mockReset()
})

afterEach(async () => {
  const data = Data()
  data.components = []
  await syncEnabledComponents(createHostRouter())
  for (const component of registeredComponents.value) unregisterComponent(component.id)
})

describe('component frontend loader', () => {
  it('loads only the installed active projection and ignores unknown backend entries', async () => {
    const data = Data()
    data.componentsLoaded = true
    data.components = [
      componentStatus('fixture-alpha'),
      componentStatus('fixture-beta', { installed: false }),
      componentStatus('fixture-broken', { active: false }),
      componentStatus('fixture-not-bundled'),
    ]
    const router = createHostRouter()

    await syncEnabledComponents(router)

    expect(entries.loadAlpha).toHaveBeenCalledOnce()
    expect(entries.loadBeta).not.toHaveBeenCalled()
    expect(entries.loadBroken).not.toHaveBeenCalled()
    expect(registeredComponents.value.map(component => component.id)).toEqual(['fixture-alpha'])
    expect(router.resolve('/fixture-alpha').matched.some(route => route.meta.componentId === 'fixture-alpha')).toBe(true)
  })

  it('unloads a component and its routes when the installed projection disables it', async () => {
    const data = Data()
    data.componentsLoaded = true
    data.components = [componentStatus('fixture-alpha')]
    const router = createHostRouter()

    await syncEnabledComponents(router)
    data.components = [componentStatus('fixture-alpha', { active: false })]
    await syncEnabledComponents(router)

    expect(entries.unregisterAlpha).toHaveBeenCalledOnce()
    expect(contributionFor('fixture-alpha')).toBeUndefined()
    expect(router.hasRoute('fixture.alpha')).toBe(false)
  })

  it('isolates one optional component failure without changing the bundled catalog', async () => {
    const warning = vi.spyOn(console, 'warn').mockImplementation(() => undefined)
    const data = Data()
    data.componentsLoaded = true
    data.components = [componentStatus('fixture-alpha'), componentStatus('fixture-broken')]

    await syncEnabledComponents(createHostRouter())

    expect(contributionFor('fixture-alpha')).toBeDefined()
    expect(contributionFor('fixture-broken')).toBeUndefined()
    expect(componentSystemBundledIDs()).toContain('fixture-broken')
    expect(warning).toHaveBeenCalledWith(
      '[componentSystem] failed to load fixture-broken',
      expect.any(Error),
    )
  })
})

function componentStatus(id: string, overrides: Partial<ComponentStatus> = {}): ComponentStatus {
  return {
    id,
    name: id,
    version: '1',
    delivery: 'in-process',
    defaultEnabled: true,
    installed: true,
    enabled: true,
    active: true,
    ...overrides,
  }
}

function createHostRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [{
      path: '/',
      name: 'main',
      component: { template: '<router-view />' },
      children: [],
    }],
  })
}
