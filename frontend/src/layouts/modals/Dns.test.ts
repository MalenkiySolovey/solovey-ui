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

function mount(data: Record<string, any>, mode: string) {
  fixture.mode = ref(mode)
  fixture.store = reactive({ capabilities: readRuntimeCapabilities({
    schema: 'solovey-ui/entity-capabilities/v1', componentProfile: 'full', multicastInterfaces: ['fixture0'], facts: [
      { category: 'dns', type: 'mdns', runtimeType: 'mdns', known: true, contextSupported: true, registered: true, compiled: true, available: true, runtimeDependency: 'multicast', runtimeEligible: true, platformDependencyAvailable: true },
      { category: 'dns', type: 'resolved', runtimeType: 'resolved', known: true, contextSupported: true, registered: true, compiled: true, available: false, runtimeDependency: 'resolve1-system-bus', runtimeEligible: false, platformDependencyAvailable: true, reason: 'DNS_RESOLVED_BUS_NAME_OCCUPIED' },
    ],
  }) })
  const saved = vi.fn()
  const app = renderer.createApp(defineComponent({ extends: DnsEditor, render: () => h('div') }), { visible: true, data: JSON.stringify(data), index: 0, rslvdTags: ['resolver'], onSave: saved })
  app.config.globalProperties.$t = (key: string) => key
  app.config.warnHandler = () => {}
  apps.push(app)
  const vm = app.mount({}) as any
  vm.updateData()
  return { vm, saved }
}

describe('shared DNS editor for Classic and Nexus', () => {
  for (const mode of ['classic', 'nexus']) {
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
      for (const name of ['v-row', 'v-col', 'v-select', 'v-text-field', 'v-alert', 'v-card', 'v-card-title', 'v-card-text', 'v-card-actions', 'v-divider', 'v-spacer', 'v-btn', 'v-dialog', 'v-navigation-drawer', 'v-toolbar', 'v-toolbar-title', 'v-icon']) {
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
      expect(vm.dnsTypes.find((item: any) => item.value === 'resolved').props.disabled).toBe(true)
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
