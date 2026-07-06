import { describe, expect, it } from 'vitest'
import {
  endpointChipColor,
  endpointChipTitle,
  isExternalURL,
  endpointLabel,
  endpointStatusText,
  isReservedPublicPath,
  normalizePublicPathPreview,
} from './publicSiteLogic'

describe('fallback-html public site UI logic', () => {
  it('renders target labels and blocked reasons explicitly', () => {
    const endpoint = {
      runtime: 'gin',
      listen: '',
      port: 2096,
      status: 'blocked',
      reason: 'port is owned by an inbound',
    }

    expect(endpointLabel(endpoint)).toBe('gin / 0.0.0.0:2096')
    expect(endpointStatusText(endpoint)).toBe('blocked: port is owned by an inbound')
    expect(endpointChipTitle(endpoint)).toBe('gin / 0.0.0.0:2096 - blocked: port is owned by an inbound')
    expect(endpointChipColor(endpoint.status)).toBe('error')
    expect(endpointChipColor('available')).toBe('success')
    expect(endpointChipColor('stale')).toBe('warning')
    expect(endpointChipColor('blocked-inbound')).toBe('error')
    expect(endpointChipColor('blocked-external')).toBe('error')
    expect(endpointChipColor('loopback-fallback')).toBe('error')
    expect(endpointChipColor('managed')).toBe('info')
    expect(endpointChipColor('free')).toBe('info')
    expect(endpointChipColor('active')).toBe('success')
    expect(endpointChipColor('planned')).toBe('warning')
    expect(endpointChipColor('applying')).toBe('warning')
    expect(endpointChipColor('failed')).toBe('error')
    expect(endpointChipColor('not-targeted')).toBe('info')
  })

  it('normalizes public path previews without hiding invalid Windows separators', () => {
    expect(normalizePublicPathPreview('')).toBe('/')
    expect(normalizePublicPathPreview('docs')).toBe('/docs/')
    expect(normalizePublicPathPreview('/docs//intro')).toBe('/docs/intro/')
    expect(normalizePublicPathPreview('/robots.txt')).toBe('/robots.txt')
    expect(normalizePublicPathPreview('bad\\path')).toBe('bad\\path')
  })

  it('flags reserved public prefixes before the server rejects them', () => {
    expect(isReservedPublicPath('/api')).toBe(true)
    expect(isReservedPublicPath('/apiv2/users')).toBe(true)
    expect(isReservedPublicPath('/assets/app.js')).toBe(true)
    expect(isReservedPublicPath('/sub/client')).toBe(true)
    expect(isReservedPublicPath('/.well-known/acme-challenge/token')).toBe(true)
    expect(isReservedPublicPath('/secret-panel/login', ['/secret-panel/'])).toBe(true)
    expect(isReservedPublicPath('https://example.com/api')).toBe(false)
    expect(isReservedPublicPath('/about')).toBe(false)
    expect(isExternalURL('https://example.com/')).toBe(true)
    expect(isExternalURL('/local/')).toBe(false)
  })
})
