import { afterEach, describe, expect, it, vi } from 'vitest'
import { createRenderer, createSSRApp, defineComponent, h, nextTick, reactive } from 'vue'
import { renderToString } from '@vue/server-renderer'
import { useSnellEditor } from './snellEditor'
import Snell from '@/components/protocols/Snell.vue'
import InboundEditor from './useInboundEditor'
import InboundDrawer from '@/components/nexus/drawers/InboundDrawer.logic'
import { coreConfigContract, type CoreConfigContract } from '@/types/coreConfigContract'
import { createClient } from '@/types/clients'

vi.mock('@/plugins/httputil', () => ({ default: { get: vi.fn(async () => ({ success: false })) } }))
vi.mock('notivue', () => ({ push: { error: vi.fn(), success: vi.fn() } }))
vi.mock('@/locales', () => ({ i18n: { global: { t: (key: string) => key } } }))
vi.mock('@/plugins/randomUtil', () => ({ default: { randomSeq: () => 'fixture', randomUUID: () => 'fixture-uuid', randomPassword: () => 'fixture-password', randomShadowsocksPassword: () => 'fixture-key' } }))

const renderer = createRenderer<any, any>({ createElement: () => ({}), createText: () => ({}), createComment: () => ({}), insert: () => {}, remove: () => {}, setText: () => {}, setElementText: () => {}, parentNode: () => null, nextSibling: () => null, patchProp: () => {} })
const apps: { unmount: () => void }[] = []
function mount(component: any, props: Record<string, unknown>) {
  const app = renderer.createApp(defineComponent({ extends: component, render: () => h('div') }), props)
  app.config.globalProperties.$t = (key: string) => key
  app.config.warnHandler = () => {}
  apps.push(app)
  return app.mount({}) as any
}
const contract = (): CoreConfigContract => ({ dnsActions: {}, dnsConditions: [], tunDnsModes: [], maxRuleDepth: 0, maxRuleNodes: 0, protocol: { fields: {}, memoryUnits: {}, snell: {
  in: { versions: [{ version: 5, clientVersion: 4, pskMinBytes: 1, pskMaxBytes: 0, obfuscation: true }, { version: 6, clientVersion: 6, pskMinBytes: 12, pskMaxBytes: 255, obfuscation: false }], obfsModes: ['none', 'http', 'tls'], modes: ['default', 'unshaped', 'unsafe-raw'], userKeyMaxBytes: 255, uri: false },
  out: { versions: [{ version: 4, clientVersion: 4, pskMinBytes: 1, pskMaxBytes: 0, obfuscation: true }, { version: 6, clientVersion: 6, pskMinBytes: 12, pskMaxBytes: 255, obfuscation: false }], obfsModes: ['none', 'http', 'tls'], modes: ['default', 'unshaped', 'unsafe-raw'], userKeyMaxBytes: 255, uri: false },
} } })
afterEach(() => { apps.splice(0).forEach(app => app.unmount()); coreConfigContract.value = undefined })

describe('Snell shared version editor', () => {
  it('uses backend versions, byte limits and masking while preserving credentials on version switches', async () => {
    coreConfigContract.value = contract()
    const data = reactive<Record<string, any>>({ version: 6, psk: 'short', mode: 'unshaped', userkey: 'retained' })
    const state = useSnellEditor(data, 'in')
    expect(state.contract.value!.versions.map(item => item.version)).toEqual([5, 6])
    expect(state.pskError.value).toEqual({ min: 12, max: 255, bytes: 5 })
    const app = createSSRApp(Snell, { data, direction: 'in' })
    app.config.globalProperties.$t = (key: string) => key
    app.config.warnHandler = () => {}
    for (const name of ['v-card', 'v-card-subtitle', 'v-alert', 'v-row', 'v-col', 'v-select', 'v-text-field', 'v-btn', 'v-switch']) {
      app.component(name, defineComponent({ inheritAttrs: false, setup: (_, context) => () => h('div', context.attrs, context.slots.default?.()) }))
    }
    expect(await renderToString(app)).toContain('type="password"')
    data.psk = 'абвгдеж'; await nextTick()
    expect(state.pskError.value).toBeUndefined()
    state.changeVersion(5); await nextTick()
    expect(data).not.toHaveProperty('mode')
    expect(state.versionFact.value!.obfuscation).toBe(true)
    expect(state.contract.value!.obfsModes).toEqual(['none', 'http', 'tls'])
    data.obfs_mode = 'http'
    state.changeVersion(6); await nextTick()
    expect(data).not.toHaveProperty('obfs_mode')
    expect(data.userkey).toBe('retained')
    expect(data.psk).toBe('абвгдеж')
  })
  it('maps server 5 to client 4 fields without inventing URI or resetting an existing client key', () => {
    coreConfigContract.value = contract()
    const state = useSnellEditor({ reuse: false, obfs_host: 'example.invalid' }, 'out_json', 5)
    expect(state.versionFact.value!.version).toBe(4)
    expect(state.versionFact.value!.obfuscation).toBe(true)
    expect(state.contract.value!.uri).toBe(false)
    expect(createClient({ id: 7, name: 'alice', config: { snell: { name: 'alice', userkey: 'retained' } } }).config!.snell.userkey).toBe('retained')
  })
})
for (const [layout, editor] of [['Classic', InboundEditor], ['Nexus', InboundDrawer]] as const) {
  it(`${layout} restores the same Snell version/credentials and user attachment contract`, () => {
    const vm = mount(editor, { id: 0, visible: true, draftInbound: { type: 'snell', version: 5, psk: 'retained', obfs_mode: 'http', tag: 'snell', listen_port: 1234 } })
    vm.updateData(0)
    expect(vm.inbound).toMatchObject({ type: 'snell', version: 5, psk: 'retained', obfs_mode: 'http' })
    expect(vm.inboundWithUsers).toContain('snell')
    expect(vm.HasInData).toContain('snell')
    expect(vm.HasTls).not.toContain('snell')
  })
}
