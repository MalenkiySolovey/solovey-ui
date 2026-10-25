import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createRenderer, defineComponent, h, reactive, ref } from 'vue'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import Client from '@/layouts/modals/Client.logic'
import Inbound from '@/features/useInboundEditor'
import { createInbound } from '@/types/inbounds'
import { updateConfigs } from '@/types/clients'
import { clientCredentialDraftInvalid, initializeClientCredentialDrafts, serializeClientCredentialDrafts, shuffleManagedClientCredentials } from '@/features/clientCredentialDraft'

const fixture = vi.hoisted(() => ({ mode: undefined as any, store: undefined as any }))
vi.mock('@/store/modules/data', () => ({ default: () => fixture.store }))
vi.mock('@/uiMode/useUiMode', () => ({ useUiMode: () => ({ mode: fixture.mode }) }))
vi.mock('@/locales', () => ({ i18n: { global: { t: (key: string) => key } }, dateLocale: () => 'en-US' }))
vi.mock('notivue', () => ({ push: { success: vi.fn(), error: vi.fn() } }))

const renderer = createRenderer<any, any>({
  createElement: () => ({}), createText: () => ({}), createComment: () => ({}), insert: () => {}, remove: () => {},
  setText: () => {}, setElementText: () => {}, parentNode: () => null, nextSibling: () => null, patchProp: () => {},
})
const apps: Array<{ unmount: () => void }> = []
function mount(feature: any, id: number) {
  const app = renderer.createApp(defineComponent({ extends: feature, render: () => h('div') }), { visible: true, id, inboundTags: [], inTags: [], tlsConfigs: [], groups: [] })
  app.config.warnHandler = () => {}
  apps.push(app)
  return app.mount({}) as any
}
function capabilities(available: boolean) {
  return { schema: 'solovey-ui/entity-capabilities/v1' as const, componentProfile: 'full' as const,
    facts: ['mieru', 'trusttunnel'].map(type => ({ category: 'inbounds', type, runtimeType: type,
      known: true, contextSupported: true, registered: available, compiled: available, available })) }
}
const storedView = () => ({ reference: { owner: 'users', id: 'record:7:mieru.password', policy: 'OWNER_USE_AND_PRESENCE' as const, projection: 'MASKED' as const }, present: true, rotationRevision: 'a'.repeat(64), correlation: { owner: 'users', private: 'metadata-only' } })

beforeEach(() => {
  fixture.mode = ref('classic')
  fixture.store = reactive({ capabilities: capabilities(true), clients: [{ id: 7, name: 'alice' }],
    checkClientName: () => false, checkTag: () => false, save: vi.fn().mockResolvedValue(false),
    loadClients: async () => ({ id: 7, name: 'alice', config: { mieru: { name: 'alice', password: storedView() } }, inbounds: [1], links: [] }) })
})
afterEach(() => { apps.splice(0).forEach(app => app.unmount()) })

describe('native credential drafts in the existing Users editor', () => {
  it('uses explicit owner mutations and retains masked metadata without mutation', () => {
    const input = { mieru: { name: 'alice', password: storedView() } }
    const before = JSON.stringify(input)
    const payload = serializeClientCredentialDrafts(input)
    expect(payload.mieru.password).toEqual({ intent: 'unchanged', reference: storedView().reference, present: true, rotationRevision: 'a'.repeat(64) })
    expect(JSON.stringify(input)).toBe(before)
    expect(JSON.stringify(payload)).not.toContain('metadata-only')
    for (const password of ['raw-secret', { intent: 'replace', value: '' }, { intent: 'replace', value: 'sbox:v1:untrusted' }, { intent: 'replace', value: 'new', reference: storedView().reference },
      { ...storedView(), value: 'ambiguous' }, { ...storedView(), intent: 'unchanged' }]) {
      expect(clientCredentialDraftInvalid({ mieru: { password } })).toBe(true)
    }
    expect(serializeClientCredentialDrafts({ mieru: { password: { intent: 'clear' } } }).mieru.password).toEqual({ intent: 'clear' })
  })

  it('initializes and shuffles only available serving credentials and preserves unavailable principals', () => {
    const config: any = {}
    initializeClientCredentialDrafts(config, capabilities(false), 'alice')
    expect(config).toEqual({})
    initializeClientCredentialDrafts(config, capabilities(true), 'alice')
    expect(config.mieru).toEqual({ name: 'alice' })
    expect(config.trusttunnel).toEqual({ name: 'alice' })
    config.mieru.password = storedView()
    const before = JSON.stringify(config)
    shuffleManagedClientCredentials(config, capabilities(false))
    updateConfigs(config, 'renamed', capabilities(false))
    expect(JSON.stringify(config)).toBe(before)
    shuffleManagedClientCredentials(config, capabilities(true), 'mieru')
    expect(config.mieru.password.intent).toBe('replace')
    expect(config.mieru.password.value).toHaveLength(32)
    expect(config.trusttunnel.password).toBeUndefined()
  })

  for (const mode of ['classic', 'nexus']) {
    it(`${mode} loads references, guards an empty replacement, saves and reloads through the same Users API`, async () => {
      fixture.mode.value = mode
      const vm = mount(Client, 7)
      await vm.updateData(7)
      expect(vm.dirty).toBe(false)
      expect(vm.clientConfig.mieru.password.reference.id).toBe('record:7:mieru.password')
      vm.clientConfig.mieru.password = { intent: 'replace', value: '' }
      await vm.saveChanges()
      expect(fixture.store.save).not.toHaveBeenCalled()
      vm.clientConfig.mieru.password = { intent: 'replace', value: 'synthetic-replacement' }
      expect(vm.dirty).toBe(true)
      await vm.saveChanges()
      expect(fixture.store.save).toHaveBeenCalledWith('clients', 'edit', expect.objectContaining({ config: expect.objectContaining({ mieru: { name: 'alice', password: { intent: 'replace', value: 'synthetic-replacement' } } }) }))
      await vm.updateData(7)
      expect(vm.dirty).toBe(false)
      expect(JSON.stringify(vm.clientConfig)).not.toContain('synthetic-replacement')
    })

    it(`${mode} preserves an unavailable credential and principal on a metadata edit`, async () => {
      fixture.mode.value = mode
      fixture.store.capabilities = capabilities(false)
      const vm = mount(Client, 7)
      await vm.updateData(7)
      vm.client.name = 'renamed'
      vm.client.desc = 'description only'
      vm.shuffle('mieru')
      await vm.saveChanges()
      expect(fixture.store.save).toHaveBeenCalledWith('clients', 'edit', expect.objectContaining({ name: 'renamed', config: expect.objectContaining({ mieru: { name: 'alice', password: { intent: 'unchanged', reference: storedView().reference, present: true, rotationRevision: 'a'.repeat(64) } } }) }))
    })

    it(`${mode} guards Native inbound availability, TLS and range-only Mieru without treating its transport as generic V2Ray transport`, async () => {
      fixture.mode.value = mode
      const vm = mount(Inbound, 0)
      vm.inbound = createInbound('mieru', { id: 0, tag: 'native', listen: '127.0.0.1', listen_port: 0, listen_ports: ['23456-23457'] })
      vm.initUsers = { model: 'client', values: [7] }
      expect(vm.hasGenericTransport).toBe(false)
      expect(vm.nativeOptionsUnavailable).toBe(false)
      expect(vm.hasUser).toBe(true)
      expect(vm.saveReasons).toEqual([])
      await vm.saveChanges()
      expect(fixture.store.save).toHaveBeenCalledWith('inbounds', 'new', expect.objectContaining({ transport: 'TCP', listen_ports: ['23456-23457'] }), [7])
      fixture.store.save.mockClear()
      vm.inbound = createInbound('trusttunnel', { id: 0, tag: 'native', listen: '127.0.0.1', listen_port: 23456 })
      expect(vm.HasTls).toContain('trusttunnel')
      expect(vm.saveReasons).toContain('form.saveTls')
      await vm.saveChanges()
      expect(fixture.store.save).not.toHaveBeenCalled()
      vm.inbound.tls_id = 1
      expect(vm.saveReasons).toEqual([])
      fixture.store.capabilities = capabilities(false)
      expect(vm.nativeOptionsUnavailable).toBe(true)
      expect(vm.saveReasons).toContain('capability.unavailable')
      await vm.saveChanges()
      expect(fixture.store.save).not.toHaveBeenCalled()
    })
  }

  it('wires the same native forms into both actual inbound presentations', () => {
    for (const file of ['./Inbound.vue', '../../components/nexus/drawers/InboundDrawer.vue']) {
      const source = readFileSync(fileURLToPath(new URL(file, import.meta.url)), 'utf8')
      expect(source).toContain('<Mieru v-if="inbound.type == inTypes.Mieru" :data="inbound" />')
      expect(source).toContain('<TrustTunnel v-if="inbound.type == inTypes.TrustTunnel" :data="inbound" />')
      expect(source).toContain('<Transport v-if="hasGenericTransport"')
      expect(source).toContain(':defaults="{ global: { disabled: nativeOptionsUnavailable } }"')
    }
  })
})
