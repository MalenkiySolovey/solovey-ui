import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createRenderer, defineComponent, h, nextTick, reactive } from 'vue'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import Inbound from './useInboundEditor'
import Outbound from './useOutboundEditor'
import Service from './useServiceEditor'
import Tls from './useTlsEditor'
import { entitySaveReasons } from './entitySaveReview'

const store = vi.hoisted(() => ({ capabilities: undefined as any, clients: [], checkTag: vi.fn(() => false), save: vi.fn(), applyInboundDraft: vi.fn() }))
vi.mock('@/store/modules/data', () => ({ default: () => store }))
vi.mock('notivue', () => ({ push: { success: vi.fn(), error: vi.fn() } }))
vi.mock('@/locales', () => ({ i18n: { global: { t: (key: string) => key } } }))

const renderer = createRenderer<any, any>({
  createElement: () => ({}), createText: () => ({}), createComment: () => ({}), insert: () => {}, remove: () => {},
  setText: () => {}, setElementText: () => {}, parentNode: () => null, nextSibling: () => null, patchProp: () => {},
})
const apps: Array<{ unmount: () => void }> = []
const mount = (feature: any, props: Record<string, unknown> = {}) => {
  const app = renderer.createApp(defineComponent({ extends: feature, render: () => h('div') }), { visible: true, id: 0, data: '{}', ...props })
  apps.push(app)
  return app.mount({}) as any
}
const deferred = () => { let resolve!: (value: boolean) => void; return { promise: new Promise<boolean>(done => { resolve = done }), resolve: (value: boolean) => resolve(value) } }
beforeEach(() => {
  vi.clearAllMocks()
  store.save.mockResolvedValue(false)
  store.capabilities = reactive({ schema: 'solovey-ui/entity-capabilities/v1', componentProfile: 'full', facts: ['inbounds', 'outbounds', 'services'].flatMap(category => ['direct', 'hysteria2', 'tun', 'derp', 'oom-killer'].map(type => ({ category, type, runtimeType: type, known: true, contextSupported: true, registered: true, compiled: true, available: true }))) })
})
afterEach(() => { apps.splice(0).forEach(app => app.unmount()) })

describe('shared editor save review', () => {
  it('projects whitespace identity, absent/invalid port and TLS/pending without mutating input', () => {
    const facts = { identity: '  ', port: '', requiresPort: true, missingRequiredTls: true, pending: true }
    const original = { ...facts }
    expect(entitySaveReasons(facts)).toEqual(['form.saveIdentity', 'form.savePortRequired', 'form.saveTls', 'form.savePending'])
    expect(facts).toEqual(original)
    for (const port of [0, 65536, 1.5, NaN, Infinity, 'bad', true, {}]) expect(entitySaveReasons({ identity: 'ok', port, requiresPort: true, pending: false })).toContain('form.savePortInvalid')
    for (const port of [1, 65535, '443']) expect(entitySaveReasons({ identity: 'ok', port, requiresPort: true, pending: false })).toEqual([])
    expect(entitySaveReasons({ identity: 'tun', requiresPort: false, pending: false })).toEqual([])
  })

  for (const [feature, field, name] of [[Inbound, 'inbound', 'Inbound'], [Outbound, 'outbound', 'Outbound'], [Service, 'srv', 'Service'], [Tls, 'tls', 'Tls']] as const) {
    it(name + ' actual feature method blocks blank identity and pending submission', async () => {
      const onSave = vi.fn()
      const vm = mount(feature, { saving: name === 'Tls', onSave })
      vm[field][name === 'Tls' ? 'name' : 'tag'] = '  '
      expect(vm.saveReasons).toContain('form.saveIdentity')
      await vm.saveChanges()
      expect(store.save).not.toHaveBeenCalled()
      vm[field][name === 'Tls' ? 'name' : 'tag'] = 'valid'
      if (name === 'Tls') {
        await vm.saveChanges()
        expect(vm.saveReasons).toContain('form.savePending')
        expect(onSave).not.toHaveBeenCalled()
        expect(store.save).not.toHaveBeenCalled()
      } else {
        vm.loading = true
        await vm.saveChanges()
        expect(vm.saveReasons).toContain('form.savePending')
        expect(store.save).not.toHaveBeenCalled()
      }
    })
    it(name + ' both layouts bind the same reasons and guarded Save', () => {
      for (const file of ['../layouts/modals/' + name + '.vue', '../components/nexus/drawers/' + name + 'Drawer.vue']) {
        const source = readFileSync(fileURLToPath(new URL(file, import.meta.url)), 'utf8')
        expect(source).toContain('<SaveGuardNotice :reasons="saveReasons" />')
        expect(source).toContain('saveReasons.length > 0')
        expect(source).toMatch(/@(click|save)="saveChanges"/)
      }
    })
  }

  it('inbound explains required port/TLS and leaves Tun portless', async () => {
    const vm = mount(Inbound)
    vm.inbound.tag = 'in'
    vm.inbound.type = 'hysteria2'
    vm.inbound.listen_port = undefined
    vm.inbound.tls_id = 0
    expect(vm.saveReasons).toEqual(['form.savePortRequired', 'form.saveTls'])
    await vm.saveChanges()
    expect(store.save).not.toHaveBeenCalled()
    vm.inbound.type = 'tun'
    expect(vm.saveReasons).toEqual([])
  })

  it('outbound and service ports follow their existing rendered fields', async () => {
    const out = mount(Outbound)
    out.outbound.tag = 'out'
    out.outbound.type = 'direct'
    expect(out.saveReasons).toEqual([])
    const srv = mount(Service)
    srv.srv.tag = 'srv'
    srv.srv.type = 'derp'
    srv.srv.listen_port = 70000
    expect(srv.saveReasons).toEqual(['form.savePortInvalid'])
    await srv.saveChanges()
    expect(store.save).not.toHaveBeenCalled()
    srv.srv.type = 'oom-killer'
    expect(srv.saveReasons).toEqual([])
  })

  it('TLS explains the rendered Reality handshake port without rewriting the draft', async () => {
    const onSave = vi.fn()
    const vm = mount(Tls, { onSave })
    vm.tls.name = 'reality'
    vm.tls.server.reality = { enabled: true, handshake: { server_port: 70000 } }
    const before = JSON.stringify(vm.tls)
    expect(vm.saveReasons).toEqual(['form.savePortInvalid'])
    await vm.saveChanges()
    expect(onSave).not.toHaveBeenCalled()
    expect(JSON.stringify(vm.tls)).toBe(before)
    vm.tls.server.reality.handshake.server_port = 443
    await vm.saveChanges()
    expect(onSave).toHaveBeenCalledOnce()
  })

  it('distinguishes backend unavailable, unknown and context facts and retains historical edit allowance', async () => {
    const vm = mount(Outbound)
    vm.outbound.tag = 'old'
    const fact = store.capabilities.facts.find((f: any) => f.category === 'outbounds' && f.type === 'direct')
    fact.available = false; fact.registered = false; fact.compiled = false
    expect(vm.saveReasons).toEqual(['capability.unavailable'])
    await vm.saveChanges()
    expect(store.save).not.toHaveBeenCalled()
    vm.storedCapabilityType = 'direct'
    expect(vm.capabilityState.message).toBe('capability.historical')
    expect(vm.saveReasons).toEqual([])
    vm.outbound.type = 'unrecognized'
    expect(vm.saveReasons).toContain('capability.unknown')
    vm.outbound.type = 'tun'
    store.capabilities.facts.find((f: any) => f.category === 'outbounds' && f.type === 'tun').contextSupported = false
    store.capabilities.facts.find((f: any) => f.category === 'outbounds' && f.type === 'tun').available = false
    expect(vm.saveReasons).toContain('capability.contextUnsupported')
  })

  it('awaits backend rejection, preserves draft, and prevents repeated saves', async () => {
    const vm = mount(Inbound)
    vm.inbound.tag = 'valid'
    vm.inbound.listen_port = 443
    const pending = deferred()
    store.save.mockReturnValue(pending.promise)
    const first = vm.saveChanges()
    await vm.saveChanges()
    expect(store.save).toHaveBeenCalledTimes(1)
    expect(vm.saveReasons).toContain('form.savePending')
    pending.resolve(false)
    await first
    await nextTick()
    expect(vm.loading).toBe(false)
    expect(vm.inbound.tag).toBe('valid')
  })
})
