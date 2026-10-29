import { afterEach, describe, expect, it, vi } from 'vitest'
import { createRenderer, createSSRApp, defineComponent, h, nextTick, reactive, ref } from 'vue'
import { renderToString } from '@vue/server-renderer'
import { createI18n } from 'vue-i18n'
import DnsEditor from '@/layouts/modals/Dns.vue'
import { readRuntimeCapabilities } from '@/types/runtimeCapabilities'
import { createDnsServer } from '@/types/dns'

const fixture = vi.hoisted(() => ({ store: null as any, mode: null as any }))
vi.mock('@/uiMode/useUiMode', () => ({ useUiMode: () => ({ mode: fixture.mode }) }))
vi.mock('@/store/modules/data', () => ({ default: () => fixture.store }))
vi.mock('@/plugins/randomUtil', () => ({ default: { randomSeq: () => 'fixture' } }))
const renderer = createRenderer<any, any>({ createElement: () => ({}), createText: () => ({}), createComment: () => ({}), insert: () => {}, remove: () => {}, setText: () => {}, setElementText: () => {}, parentNode: () => null, nextSibling: () => null, patchProp: () => {} })
const apps: { unmount: () => void }[] = []
afterEach(() => { apps.splice(0).forEach(app => app.unmount()) })

function mount(data: Record<string, any>, mode: string, native = false) {
  fixture.mode = ref(mode)
  fixture.store = reactive({ capabilities: readRuntimeCapabilities({
    schema: 'solovey-ui/entity-capabilities/v1', componentProfile: 'full', multicastInterfaces: ['fixture0'], facts: [
      { category: 'dns', type: 'mdns', runtimeType: 'mdns', known: true, contextSupported: true, registered: true, compiled: true, available: true, runtimeDependency: 'multicast', runtimeEligible: true, platformDependencyAvailable: true },
      { category: 'dns', type: 'resolved', runtimeType: 'resolved', known: true, contextSupported: true, registered: true, compiled: true, available: false, runtimeDependency: 'resolve1-system-bus', runtimeEligible: false, platformDependencyAvailable: true, reason: 'DNS_RESOLVED_BUS_NAME_OCCUPIED' },
      ...['sdns', 'fallback', 'openconnect'].map(type => ({ category: 'dns', type, runtimeType: type, known: true, contextSupported: true, registered: native, compiled: native, available: native, buildTag: 'shtorm_extended', requiredProvider: 'Shtorm Extended' })),
      { category: 'endpoints', type: 'openconnect', runtimeType: 'openconnect', known: true, contextSupported: true, registered: native, compiled: native, available: native },
    ],
  }), endpoints: [{ type: 'openconnect', tag: 'native-vpn' }, { type: 'wireguard', tag: 'wg' }] })
  const saved = vi.fn()
  const app = renderer.createApp(defineComponent({ extends: DnsEditor, render: () => h('div') }), { visible: true, data: JSON.stringify(data), index: 0, rslvdTags: ['resolver'], serverTags: ['later-a', 'later-b', 'group'], onSave: saved })
  app.config.globalProperties.$t = (key: string) => key
  app.config.warnHandler = () => {}
  apps.push(app)
  const vm = app.mount({}) as any
  vm.updateData()
  return { vm, saved }
}

describe('shared DNS editor for Classic and Nexus', () => {
  for (const mode of ['classic', 'nexus']) {
    it(`${mode} selects an available native VPN DNS endpoint and retains resolver policy`, () => {
      const source = { type: 'openconnect', tag: 'vpn-dns', endpoint: 'native-vpn', accept_default_resolvers: true, accept_search_domain: true }
      const { vm, saved } = mount(source, mode, true)
      expect(vm.openConnectEndpointTags).toEqual(['native-vpn'])
      expect(vm.missingService).toBe(false)
      expect(vm.WithoutDial).toContain('openconnect')
      vm.save()
      expect(saved).toHaveBeenCalledWith(source)
      const historical = mount(source, mode, false)
      expect(historical.vm.openConnectEndpointTags).toEqual([])
      expect(historical.vm.dnsTypes.some((item: any) => item.value === 'openconnect')).toBe(false)
      expect(historical.vm.dnsServer).toEqual(source)
      expect(historical.vm.capabilityState.historical).toBe(true)
      historical.vm.save()
      expect(historical.saved).not.toHaveBeenCalled()
    })
    it(`${mode} offers native DNS only when available and edits one canonical fallback value`, () => {
      const source = { type: 'fallback', tag: 'group', servers: ['later-b', 'later-a'], strategy: 'parallel', timeout: '3s' }
      const { vm, saved } = mount(source, mode, true)
      expect(vm.dnsTypes.map((item: any) => item.value)).toEqual(expect.arrayContaining(['sdns', 'fallback']))
      expect(vm.fallbackServers).toEqual(['later-a', 'later-b'])
      expect(vm.WithoutDial).toContain('fallback')
      expect(vm.WithoutDial).not.toContain('sdns')
      vm.dnsServer.strategy = 'sequential'
      vm.save()
      expect(saved).toHaveBeenCalledWith({ ...source, strategy: 'sequential' })
      expect(source.strategy).toBe('parallel')
      expect(createDnsServer('fallback')).toEqual({ type: 'fallback', servers: [], strategy: 'sequential' })
      expect(createDnsServer('sdns')).toEqual({ type: 'sdns', stamp: '' })
    })
    it(`${mode} preserves unavailable native DNS history outside creation choices`, () => {
      for (const source of [
        { type: 'sdns', tag: 'private-dns', stamp: 'sdns://preserved' },
        { type: 'fallback', tag: 'group', servers: ['later-b', 'later-a'], strategy: 'parallel', timeout: '3s' },
      ]) {
        const { vm, saved } = mount(source, mode)
        expect(vm.dnsTypes.some((item: any) => ['sdns', 'fallback'].includes(item.value))).toBe(false)
        expect(vm.capabilityState).toMatchObject({ allowed: false, historical: true })
        expect(vm.dnsServer).toEqual(source)
        vm.save()
        expect(saved).not.toHaveBeenCalled()
        fixture.store.capabilities.facts.find((fact: any) => fact.type === source.type).available = true
        vm.save()
        expect(saved).toHaveBeenCalledWith(source)
      }
    })
    it(`${mode} restores list/scalar interfaces and preserves the source until explicit save`, async () => {
      const source = { type: 'mdns', tag: 'local', interface: 'fixture0' }
      const { vm, saved } = mount(source, mode)
      expect(vm.multicastInterfaces).toBe('fixture0')
      expect(vm.observedInterfaces).toEqual(['fixture0'])
      vm.multicastInterfaces = 'fixture0,other'
      await nextTick()
      expect(vm.dnsServer.interface).toEqual(['fixture0', 'other'])
      expect(source.interface).toBe('fixture0')
      expect(vm.dirty).toBe(true)
      vm.multicastInterfaces = ''
      expect(vm.dnsServer).not.toHaveProperty('interface')
      vm.save()
      expect(saved).toHaveBeenCalledWith({ type: 'mdns', tag: 'local' })
      expect(createDnsServer('mdns')).toEqual({ type: 'mdns' })
      expect(vm.WithoutDial).toContain('mdns')
    })
    it(`${mode} renders the shared mDNS fields in its actual form shell`, async () => {
      mount({ type: 'mdns', tag: 'multicast', interface: ['fixture0'] }, mode)
      const app = createSSRApp(defineComponent({ ...DnsEditor, created() { (this as any).updateData() } }), { visible: true, modelValue: true, index: 0, data: JSON.stringify({ type: 'mdns', tag: 'multicast', interface: ['fixture0'] }) })
      app.use(createI18n({ legacy: false, locale: 'en', messages: { en: {} }, missingWarn: false, fallbackWarn: false }))
      app.config.globalProperties.$t = (key: string) => key
      app.config.warnHandler = () => {}
      for (const name of ['v-row', 'v-col', 'v-select', 'v-text-field', 'v-alert', 'v-card', 'v-card-title', 'v-card-text', 'v-card-actions', 'v-divider', 'v-spacer', 'v-btn', 'v-dialog', 'v-navigation-drawer', 'v-toolbar', 'v-toolbar-title', 'v-icon', 'v-defaults-provider']) {
        app.component(name, defineComponent({ inheritAttrs: false, setup: (_, context) => () => h('div', context.attrs, context.slots.default?.()) }))
      }
      const markup = await renderToString(app)
      expect(markup).toContain('dns.mdnsInterfaces')
      expect(markup).toContain('dns.mdnsSemantics')
      expect(markup).toContain('fixture0')
    })
    it(`${mode} explains unavailable resolved history and refuses a missing service`, () => {
      const { vm, saved } = mount({ type: 'resolved', tag: 'resolved', service: 'missing', accept_default_resolvers: true }, mode)
      expect(vm.selectedCapability.reason).toBe('DNS_RESOLVED_BUS_NAME_OCCUPIED')
      expect(vm.dnsTypes.some((item: any) => item.value === 'resolved')).toBe(false)
      expect(vm.capabilityState).toMatchObject({ allowed: false, historical: true })
      expect(vm.missingService).toBe(true)
      vm.save()
      expect(saved).not.toHaveBeenCalled()
      vm.dnsServer.service = 'resolver'
      fixture.store.capabilities.facts[1].runtimeEligible = true
      fixture.store.capabilities.facts[1].available = true
      vm.save()
      expect(saved).toHaveBeenCalledWith(expect.objectContaining({ service: 'resolver', accept_default_resolvers: true }))
    })
  }
})
