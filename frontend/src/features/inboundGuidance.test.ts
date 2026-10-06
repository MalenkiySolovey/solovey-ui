import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { inboundGuidanceKeys } from './inboundGuidance'
import type { RuntimeCapability } from '@/types/runtimeCapabilities'
const fact = (type: string): RuntimeCapability => ({ category: 'inbounds', type, runtimeType: type, known: true, contextSupported: true, registered: true, compiled: true, available: true })
describe('bounded informational inbound guidance', () => {
  for (const type of ['vless', 'vmess', 'trojan', 'shadowsocks', 'socks', 'http']) {
    it('projects ' + type + ' without changing protocol facts', () => {
      const input = fact(type)
      const original = structuredClone(input)
      const keys = inboundGuidanceKeys(input, { hasTls: true, tlsSelected: true, transport: 'ws' })
      expect(keys).toEqual(['inboundGuidance.' + type, 'inboundGuidance.tlsSelected', 'inboundGuidance.transport'])
      expect(input).toEqual(original)
    })
  }
  it('omits unknown/unavailable/context-unsupported and irrelevant transports', () => {
    for (const input of [undefined, fact('unknown'), { ...fact('vless'), available: false }, { ...fact('vless'), contextSupported: false }, { ...fact('vless'), category: 'outbounds' }]) expect(inboundGuidanceKeys(input, { hasTls: true, tlsSelected: true })).toEqual([])
    expect(inboundGuidanceKeys(fact('http'), { hasTls: true, tlsSelected: false, transport: 'unknown' })).toEqual(['inboundGuidance.http', 'inboundGuidance.tlsOptional'])
    expect(inboundGuidanceKeys(fact('shadowsocks'), { hasTls: false, tlsSelected: false })).toEqual(['inboundGuidance.shadowsocks'])
  })
  it('both editors consume the same presentation with native keyboard disclosure', () => {
    for (const file of ['../layouts/modals/Inbound.vue', '../components/nexus/drawers/InboundDrawer.vue']) expect(readFileSync(fileURLToPath(new URL(file, import.meta.url)), 'utf8')).toContain('<InboundGuidance :hints="guidanceHints" />')
    const component = readFileSync(fileURLToPath(new URL('../components/fields/InboundGuidance.vue', import.meta.url)), 'utf8')
    expect(component).toContain('<details')
    expect(component).toContain('<summary>')
    expect(component).not.toContain('$emit')
  })
})
