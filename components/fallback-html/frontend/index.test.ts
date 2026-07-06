import { afterEach, describe, expect, it } from 'vitest'
import { componentRoutes, contributionFor, navItems, unregisterComponent } from '@/componentSystem/registry'
import { register } from './index'

const componentId = 'fallback-html'

afterEach(() => {
  unregisterComponent(componentId)
})

describe('fallback-html frontend contribution', () => {
  it('registers route, navigation and locale loaders through the shared registry', () => {
    unregisterComponent(componentId)
    register()

    const contribution = contributionFor(componentId)
    expect(contribution?.apiVersion).toBe('1.0')
    expect(Object.keys(contribution?.locales ?? {}).sort()).toEqual(['en', 'ru'])

    expect(componentRoutes.value).toEqual(expect.arrayContaining([
      expect.objectContaining({
        path: '/fallback-html',
        name: 'component.fallback-html',
        meta: expect.objectContaining({ componentId }),
      }),
    ]))

    expect(navItems.value).toEqual(expect.arrayContaining([
      expect.objectContaining({
        title: 'fallbackHtml.title',
        path: '/fallback-html',
        section: 'integrations',
      }),
    ]))
  })
})
