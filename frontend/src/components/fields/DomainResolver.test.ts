import { describe, expect, it } from 'vitest'
import DomainResolver from './DomainResolver.vue'

function form(source: Record<string, any>) {
  const owner = DomainResolver as any
  const state: any = { ...owner.data(), $props: { data: source, field: 'domain_resolver' }, dnsTags: ['unselected'] }
  Object.defineProperty(state, 'value', { get: () => source.domain_resolver })
  Object.defineProperty(state, 'advanced', { get: () => owner.computed.advanced.call(state) })
  return { owner, state }
}

describe('resolver draft intent', () => {
  it('a subscription form supplies its own DNS catalogue instead of the panel catalogue', () => {
    const owner = DomainResolver as any
    expect(owner.computed.dnsTags.call({ $props: { serverTags: ['subscription-dns'] } })).toEqual(['subscription-dns'])
    expect(owner.computed.dnsTags.call({ $props: { serverTags: [] } })).toEqual([])
  })
  it('reading an absent resolver never inserts a DNS choice', () => {
    const source = {}
    const { state } = form(source)
    expect(state.advanced).toEqual({ server: '' })
    expect(source).toEqual({})
  })
  it('preserves advanced options before commit and reuses the selected tag', () => {
    const source = { domain_resolver: { server: 'chosen', strategy: 'ipv6_only', disable_cache: false, rewrite_ttl: 0, client_subnet: '' } }
    const original = structuredClone(source.domain_resolver)
    const { owner, state } = form(source)
    owner.computed.mode.set.call(state, 'advanced')
    expect(source.domain_resolver).toEqual(original)
    owner.computed.mode.set.call(state, 'tag')
    expect(source.domain_resolver).toBe('chosen')
    owner.computed.mode.set.call(state, 'advanced')
    expect(source.domain_resolver).toEqual(original)
  })
  it('an explicit zero TTL remains a valid value', () => {
    const source = { domain_resolver: { server: 'chosen', rewrite_ttl: 60, disable_cache: false } }
    const { owner, state } = form(source)
    owner.computed.rewriteTtl.set.call(state, 0)
    expect(source.domain_resolver.rewrite_ttl).toBe(0)
    expect(source.domain_resolver.disable_cache).toBe(false)
  })
})
