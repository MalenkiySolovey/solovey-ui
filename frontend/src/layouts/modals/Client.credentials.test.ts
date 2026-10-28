import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createRenderer, defineComponent, h, reactive, ref } from 'vue'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import Client from '@/layouts/modals/Client.logic'
import Inbound from '@/features/useInboundEditor'
import { createInbound } from '@/types/inbounds'
import { updateConfigs } from '@/types/clients'
import { clientCredentialDraftInvalid, initializeClientCredentialDrafts, serializeClientCredentialDrafts, shuffleManagedClientCredentials } from '@/features/clientCredentialDraft'
import { inboundCredentialDraftInvalid, serializeInboundCredentialDraft } from '@/features/inboundCredentialDraft'

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
    facts: ['mieru', 'trusttunnel', 'ssh', 'mtproxy', 'sudoku'].map(type => ({ category: 'inbounds', type, runtimeType: type,
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
    it(`${mode} retains the Sudoku public identity and explicit false/zero fields while submitting only the master mutation`, async () => {
      fixture.mode.value = mode
      const view = { ...storedView(), reference: { ...storedView().reference, owner: 'inbounds', id: 'record:9:master_key' } }
      const shape = { id: 9, tag: 'sudoku-owned', listen: '127.0.0.1', listen_port: 23456, key: 'public-fixture', master_key: view,
        aead_method: 'aes-128-gcm', padding_min: 0, padding_max: 30, table_type: 'up_ascii_down_entropy', handshake_timeout: 0,
        enable_pure_downlink: false, custom_table: 'xpxvvpvv', custom_tables: ['xpxvvpvv'], disable_http_mask: false,
        http_mask_mode: 'poll', path_root: 'owned', fallback: 'fixture.invalid:8080' }
      fixture.store.loadInbounds = async () => [createInbound('sudoku', shape)]
      const vm = mount(Inbound, 9)
      await vm.loadData(9)
      expect(vm.dirty).toBe(false)
      expect(vm.HasTls).not.toContain('sudoku')
      expect(vm.hasGenericTransport).toBe(false)
      await vm.saveChanges()
      expect(fixture.store.save).toHaveBeenCalledWith('inbounds', 'edit', expect.objectContaining({ ...shape,
        master_key: { intent: 'unchanged', reference: view.reference, present: true, rotationRevision: 'a'.repeat(64) } }), [])
      fixture.store.save.mockClear()
      vm.inbound.master_key = { intent: 'replace', value: '' }
      expect(vm.saveReasons).toContain('secretCredential.required')
      await vm.saveChanges()
      expect(fixture.store.save).not.toHaveBeenCalled()
      await vm.loadData(9)
      fixture.store.capabilities = capabilities(false)
      expect(vm.capabilityState.allowed).toBe(false)
      expect(vm.capabilityState.message).toBe('capability.historical')
    })

    it(`${mode} keeps Sudoku delivery keys in the shared Users editor without inventing a principal`, async () => {
      fixture.mode.value = mode
      const view = { ...storedView(), reference: { ...storedView().reference, id: 'record:7:sudoku.keys' } }
      fixture.store.loadClients = async () => ({ id: 7, name: 'alice', config: { sudoku: { keys: view } }, inbounds: [1], links: [] })
      const vm = mount(Client, 7)
      await vm.updateData(7)
      expect(vm.clientConfig.sudoku.name).toBeUndefined()
      expect(vm.managedSecretField('sudoku')).toBe('keys')
      await vm.saveChanges()
      expect(fixture.store.save).toHaveBeenCalledWith('clients', 'edit', expect.objectContaining({ config: expect.objectContaining({
        sudoku: { keys: { intent: 'unchanged', reference: view.reference, present: true, rotationRevision: 'a'.repeat(64) } },
      }) }))
      fixture.store.save.mockClear()
      vm.shuffle('sudoku')
      expect(vm.clientConfig.sudoku.keys).toEqual({ intent: 'replace', value: '{}' })
      await vm.updateData(7)
      fixture.store.capabilities = capabilities(false)
      vm.client.name = 'renamed'
      vm.shuffle('sudoku')
      await vm.saveChanges()
      expect(fixture.store.save).toHaveBeenCalledWith('clients', 'edit', expect.objectContaining({ name: 'renamed', config: expect.objectContaining({
        sudoku: { keys: { intent: 'unchanged', reference: view.reference, present: true, rotationRevision: 'a'.repeat(64) } },
      }) }))
    })

    it(`${mode} retains masked SSH host keys and submits explicit host-key mutations through Inbounds`, async () => {
      fixture.mode.value = mode
      const view = { ...storedView(), reference: { ...storedView().reference, owner: 'inbounds', id: 'record:9:host_key' } }
      fixture.store.loadInbounds = async () => [createInbound('ssh', { id: 9, tag: 'ssh-owned', listen_port: 23456, host_key: view })]
      const vm = mount(Inbound, 9)
      await vm.loadData(9)
      expect(vm.dirty).toBe(false)
      await vm.saveChanges()
      expect(fixture.store.save).toHaveBeenCalledWith('inbounds', 'edit', expect.objectContaining({ host_key: { intent: 'unchanged', reference: view.reference, present: true, rotationRevision: 'a'.repeat(64) } }), [])
      fixture.store.save.mockClear()
      vm.inbound.host_key = { intent: 'replace', value: '' }
      expect(vm.saveReasons).toContain('secretCredential.required')
      await vm.saveChanges()
      expect(fixture.store.save).not.toHaveBeenCalled()
      vm.inbound.host_key = { intent: 'replace', value: 'runtime-only-private-key' }
      await vm.saveChanges()
      expect(fixture.store.save).toHaveBeenCalledWith('inbounds', 'edit', expect.objectContaining({ host_key: { intent: 'replace', value: 'runtime-only-private-key' } }), [])
      await vm.loadData(9)
      expect(vm.dirty).toBe(false)
      expect(JSON.stringify(vm.inbound)).not.toContain('runtime-only-private-key')
      fixture.store.capabilities = capabilities(false)
      expect(vm.capabilityState.allowed).toBe(false)
      expect(vm.capabilityState.message).toBe('capability.historical')
    })

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
      expect(vm.capabilityState.allowed).toBe(true)
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
      expect(vm.capabilityState.allowed).toBe(false)
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
      expect(source).toContain('<SshInbound v-if="inbound.type == inTypes.SSH" :data="inbound" />')
      expect(source).toContain('<MTProxy v-if="inbound.type == inTypes.MTProxy" :data="inbound" />')
      expect(source).toContain('<Sudoku v-if="inbound.type == inTypes.Sudoku" :data="inbound" />')
      expect(source).toContain('<Transport v-if="hasGenericTransport"')
      expect(source).toContain('<CapabilitySurface :state="capabilityState" :type="inbound.type">')
    }
  })

  it('guards raw host keys and generates a complete MTProxy fake-TLS credential', () => {
    for (const master_key of ['private-plaintext', { intent: 'replace', value: '' }, { intent: 'clear', value: 'extra' }]) {
      expect(inboundCredentialDraftInvalid({ type: 'sudoku', master_key })).toBe(true)
    }
    for (const host_key of ['private-plaintext', { intent: 'replace', value: '' }, { intent: 'clear', value: 'extra' }]) {
      expect(inboundCredentialDraftInvalid({ type: 'ssh', host_key })).toBe(true)
    }
    const input = { type: 'ssh', host_key: { ...storedView(), reference: { ...storedView().reference, owner: 'inbounds' } } }
    const before = JSON.stringify(input)
    expect(serializeInboundCredentialDraft(input).host_key).toEqual(expect.objectContaining({ intent: 'unchanged' }))
    expect(JSON.stringify(input)).toBe(before)
    const config: any = {}
    initializeClientCredentialDrafts(config, capabilities(true), 'alice')
    shuffleManagedClientCredentials(config, capabilities(true), 'mtproxy')
    expect(config.mtproxy.secret).toEqual({ intent: 'replace', value: expect.stringMatching(/^ee[0-9a-f]{32}7777772e676f6f676c652e636f6d$/) })
    expect(config.ssh.password).toBeUndefined()
    expect(config.sudoku).toEqual({})
    shuffleManagedClientCredentials(config, capabilities(true), 'sudoku')
    expect(config.sudoku).toEqual({ keys: { intent: 'replace', value: '{}' } })
  })
})
