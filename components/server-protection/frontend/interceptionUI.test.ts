import { describe, expect, it } from 'vitest'
import viewSource from './views/InterceptionGuard.vue?raw'
import en from './locales/en'
import ru from './locales/ru'

const leafKeys = (value: object, prefix = ''): string[] =>
  Object.entries(value).flatMap(([key, item]) => {
    const path = prefix ? `${prefix}.${key}` : key
    return item && typeof item === 'object' ? leafKeys(item as object, path) : [path]
  })

describe('forwarded-ingress interception UI', () => {
  it('remains read-only, explicit, and accessible', () => {
    expect(viewSource).toContain('aria-live="polite"')
    expect(viewSource).toContain('interceptionStatus')
    expect(viewSource).toContain('interceptionPreview')
    expect(viewSource).not.toMatch(/interception(?:Prepare|Apply|Disable|Rollback)/)
    expect(viewSource).not.toMatch(/v-(?:text-field|select)[^>]+(?:interface|mark|mask|table|priority|route|cidr|port|nft|command)/i)
    expect(viewSource).toContain('status.profileMatrix')
    expect(viewSource).toContain('status?.ingressScopes')
    expect(viewSource).toContain('resource.reasonCodes')
  })

  it('keeps English and Russian interception locale surfaces complete', () => {
    expect(leafKeys(ru.serverProtection.interception).sort()).toEqual(
      leafKeys(en.serverProtection.interception).sort(),
    )
    expect(en.serverProtection.tabs.interception).toBeTruthy()
    expect(ru.serverProtection.tabs.interception).toBeTruthy()
  })
})
