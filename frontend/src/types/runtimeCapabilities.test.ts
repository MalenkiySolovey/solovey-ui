import { describe, expect, it } from 'vitest'
import { readRuntimeCapabilities, runtimeCapability } from './runtimeCapabilities'

const projection = () => ({
  schema: 'solovey-ui/entity-capabilities/v1', componentProfile: 'minimal',
  facts: [{ category: 'endpoints', type: 'warp', runtimeType: 'wireguard', known: true, contextSupported: true, registered: true, compiled: true, available: true },
    { category: 'endpoints', type: 'tailscale', runtimeType: 'tailscale', known: true, contextSupported: true, registered: true, compiled: false, available: false, buildTag: 'with_tailscale,with_gvisor', reason: 'KNOWN_BUT_NOT_COMPILED' }],
})

describe('backend runtime capability projection', () => {
  it('keeps semantic identity, actual build facts and unknown context distinct', () => {
    const source = projection()
    const value = readRuntimeCapabilities(source)
    expect(runtimeCapability(value, 'endpoints', 'warp')?.runtimeType).toBe('wireguard')
    expect(runtimeCapability(value, 'endpoints', 'tailscale')?.available).toBe(false)
    expect(runtimeCapability(value, 'outbounds', 'warp')).toBeUndefined()
    source.facts[0]!.type = 'changed'
    expect(runtimeCapability(value, 'endpoints', 'warp')?.available).toBe(true)
  })
  it('fails closed on missing, malformed, conflicting and duplicate facts', () => {
    for (const bad of [undefined, {}, { ...projection(), schema: 'future' },
      { ...projection(), facts: [{ ...projection().facts[1], available: true }] },
      { ...projection(), facts: [projection().facts[0], projection().facts[0]] },
      { ...projection(), facts: [{ ...projection().facts[0], compiled: 'yes' }] }]) {
      expect(readRuntimeCapabilities(bad)).toBeUndefined()
    }
  })
  it('keeps compiled dependencies unavailable without contradicting the snapshot', () => {
    const fact = { ...projection().facts[0], category: 'dns', type: 'resolved', runtimeType: 'resolved', runtimeDependency: 'resolve1-system-bus', runtimeEligible: false, platformDependencyAvailable: true, available: false }
    expect(readRuntimeCapabilities({ ...projection(), facts: [fact] })?.facts[0]?.available).toBe(false)
    for (const bad of [{ ...fact, available: true }, { ...fact, runtimeEligible: undefined }, { ...fact, platformDependencyAvailable: 'yes' }]) {
      expect(readRuntimeCapabilities({ ...projection(), facts: [bad] })).toBeUndefined()
    }
    expect(readRuntimeCapabilities({ ...projection(), facts: [{ ...projection().facts[0], supportedByProduct: false }] })).toBeUndefined()
  })
})
