import { describe, expect, it } from 'vitest'
import DnsRuleModal from '@/layouts/modals/DnsRule.vue'
import { createDnsServer } from './dns'
import { serializeDNSRule, type CoreConfigContract } from './coreConfigContract'

const contract: CoreConfigContract = { dnsActions: { route: ['server', 'disable_cache', 'rewrite_ttl', 'race'], evaluate: ['server', 'tag', 'race'], respond: ['race'], reject: ['no_drop', 'race'] }, dnsConditions: ['match_response'], tunDnsModes: ['disabled', 'native', 'hijack'], maxRuleDepth: 64, maxRuleNodes: 4096 }

describe('DNS editor round trips', () => {
  it('keeps explicit false, zero, empty values and unknown valid fields', () => {
    const draft = { type: 'simple', mode: 'and', rules: [{ query_type: ['A'], match_response: false }], action: 'route', invert: false, server: 'dns', disable_cache: false, rewrite_ttl: 0, custom: '', extraRef: 'owned' }
    const result = serializeDNSRule(draft, 'route', contract)
    expect(result).toEqual({ query_type: ['A'], match_response: false, action: 'route', invert: false, server: 'dns', disable_cache: false, rewrite_ttl: 0, custom: '', extraRef: 'owned' })
    expect(draft.type).toBe('simple')
  })
  it('changes only action-owned fields after an explicit action switch', () => {
    const result = serializeDNSRule({ type: 'logical', mode: 'and', rules: [{ match_response: 'r' }], action: 'respond', server: 'dns', tag: 'r', race: false, custom: 'keep' }, 'evaluate', contract)
    expect(result).toEqual({ type: 'logical', mode: 'and', rules: [{ match_response: 'r' }], action: 'respond', race: false, custom: 'keep' })
  })
  it('does not guess field authority when the contract request fails', () => {
    expect(serializeDNSRule({ action: 'respond', server: 'dns', tag: 'r' }, 'evaluate', undefined)).toEqual({ action: 'respond', server: 'dns', tag: 'r' })
  })
  it('factory defaults and imported nested TLS state are isolated across reopen', () => {
    const first = createDnsServer('tls')
    first.tls.insecure = true
    expect(createDnsServer('tls').tls).toEqual({})
    const source = { type: 'https', tls: { insecure: false }, headers: { empty: [] } }
    const draft = createDnsServer('https', source)
    draft.tls.insecure = true
    expect(source.tls.insecure).toBe(false)
    expect(createDnsServer('https', source).headers.empty).toEqual([])
  })
  it('real modal open/save/close/reopen preserves typed default rules and false presence', () => {
    const modal = DnsRuleModal as any
    const source = { type: 'default', action: 'route', invert: false, domain_suffix: [], server: 'dns', disable_cache: false, rewrite_ttl: 0, client_subnet: '' }
    let saved: any
    let closed = false
    const state = { ...modal.data(), $props: { index: 0, data: JSON.stringify(source), serverTags: ['dns'] }, $emit: (event: string, value: any) => { if (event === 'save') saved = value; if (event === 'close') closed = true } }
    modal.methods.updateData.call(state)
    expect(state.ruleData.rules[0].domain_suffix).toEqual([])
    modal.methods.saveChanges.call(state)
    expect(saved).toEqual(source)
    modal.methods.closeModal.call(state)
    expect(closed).toBe(true)
    state.ruleData.disable_cache = true
    modal.methods.updateData.call(state)
    modal.methods.saveChanges.call(state)
    expect(saved.disable_cache).toBe(false)
    expect(source.disable_cache).toBe(false)
  })
})
