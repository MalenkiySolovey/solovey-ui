import { describe, expect, it, vi } from 'vitest'
import { mergeTemplateRuleSets } from './templateRuleSets'
import SubJsonExt from './SubJsonExt.vue'
import { type CoreConfigContract } from '@/types/coreConfigContract'
vi.mock('@/types/coreConfigContract', () => ({ coreConfigContract: { value: undefined }, loadCoreConfigContract: vi.fn() }))

const contract: CoreConfigContract = { dnsActions: {}, dnsConditions: [], tunDnsModes: [], maxRuleDepth: 64, maxRuleNodes: 4096, directHttpClient: { engine: 'go', version: 2 } }
const stock = { tag: 'stock', type: 'remote', format: 'binary', url: 'https://example.invalid/stock.srs', download_detour: 'direct' }

describe('subscription rule-set identity and round trips', () => {
  it('keeps operator-edited presets and unrelated custom sets across repeated edit/save', () => {
    const custom = { ...stock, url: 'https://example.invalid/custom.srs', http_client: 'chosen', update_interval: '1h', extra: false }
    const unrelated = { tag: ['alpha', 'beta'], type: 'remote', url: 'https://example.invalid/{tag}.srs', http_client: { headers: { empty: [] } } }
    const template: Record<string, any> = { rules: [{ type: 'logical', rules: [{ rule_set: 'stock' }] }], rule_set: [custom, unrelated], http_clients: [{ tag: 'chosen', engine: 'go', version: 2 }], default_domain_resolver: { server: 'dns', disable_cache: false, rewrite_ttl: 0 } }
    const original = structuredClone(template)
    template.rule_set = mergeTemplateRuleSets(template, [stock], contract)
    const reimported = JSON.parse(JSON.stringify(template))
    expect(mergeTemplateRuleSets(reimported, [stock], contract)).toEqual([custom, unrelated])
    expect(template).toEqual(original)
    expect(mergeTemplateRuleSets({ rules: [], rule_set: [custom, unrelated] }, [stock], contract)).toEqual([custom, unrelated])
  })
  it('uses the backend direct factory only for a newly added default and never splits a tag list', () => {
    const custom = { tag: ['stock', 'alias'], type: 'remote', url: 'https://example.invalid/{tag}.srs', download_detour: 'proxy' }
    expect(mergeTemplateRuleSets({ rules: [{ rule_set: 'alias' }], rule_set: [custom] }, [stock], contract)).toEqual([custom])
    expect(mergeTemplateRuleSets({ rules: [{ rule_set: 'stock' }] }, [stock], contract)).toEqual([{ tag: 'stock', type: 'remote', format: 'binary', url: stock.url, http_client: { engine: 'go', version: 2 } }])
    expect(stock.download_detour).toBe('direct')
  })
  it('preserves the original when the owner contract is unavailable or traversal exceeds budget', () => {
    const draft = { rules: [{ rule_set: 'stock' }], rule_set: [stock] }
    expect(() => mergeTemplateRuleSets(draft, [stock], {} as CoreConfigContract)).toThrow()
    expect(() => mergeTemplateRuleSets(draft, [stock], { ...contract, maxRuleNodes: 1 } )).not.toThrow()
    expect(() => mergeTemplateRuleSets({ ...draft, rules: [{ rules: [{ rule_set: 'stock' }] }] }, [stock], { ...contract, maxRuleNodes: 1 })).toThrow()
    expect(draft.rule_set).toEqual([stock])
  })
  it('real form open and reopen preserve object resolver, empty fields and custom references', () => {
    const component = SubJsonExt as any
    const source = { default_domain_resolver: { server: 'selected', disable_cache: false, rewrite_ttl: 0 }, rule_set: [stock], custom: '' }
    const state: any = { ...component.data(), $props: { settings: { subJsonExt: JSON.stringify(source) } } }
    component.methods.loadData.call(state)
    expect(state.subJsonExt).toEqual(source)
    state.subJsonExt.default_domain_resolver.disable_cache = true
    component.methods.loadData.call(state)
    expect(state.subJsonExt).toEqual(source)
    component.computed.enableInb.set.call(state, true)
    state.subJsonExt.inbounds[0].strict_route = true
    component.computed.enableInb.set.call(state, false)
    component.computed.enableInb.set.call(state, true)
    expect(state.subJsonExt.inbounds[0].strict_route).toBe(false)
    expect(state.subJsonExt.inbounds[0].dns_mode).toBe('disabled')
  })
  it('blocked and stale preview cannot change the draft', () => {
    const component = SubJsonExt as any
    const state = { subJsonExt: { custom: false }, previewSource: '{"custom":false}', compatibilityPreview: { blocked: true, subscriptionTemplate: { custom: true } } }
    component.methods.applyCompatibilityPreview.call(state)
    expect(state.subJsonExt.custom).toBe(false)
    state.compatibilityPreview.blocked = false
    state.previewSource = '{}'
    component.methods.applyCompatibilityPreview.call(state)
    expect(state.subJsonExt.custom).toBe(false)
  })
})
