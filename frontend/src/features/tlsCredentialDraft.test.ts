import { afterEach, describe, expect, it, vi } from 'vitest'
import { createRenderer, defineComponent, h, reactive } from 'vue'
import TlsEditor from './useTlsEditor'
import TlsDrawer from '@/components/nexus/drawers/TlsDrawer.logic'
import OutTLS from '@/components/tls/OutTLS.logic'
import TLSProviderEditor from '@/components/tls/TLSProviderEditor.vue'
import ConsumerTuning from '@/components/protocols/ConsumerTuning.vue'
import { selectCredentialMode, serializeCredentialDraft } from './tlsCredentialDraft'

vi.mock('@/plugins/httputil', () => ({ default: { get: vi.fn(async () => ({ success: false })) } }))
vi.mock('notivue', () => ({ push: { error: vi.fn(), success: vi.fn() } }))
vi.mock('@/locales', () => ({ i18n: { global: { t: (key: string) => key } } }))
vi.mock('@/plugins/randomUtil', () => ({ default: { randomShortId: () => ['fixture-id'] } }))

const renderer = createRenderer<any, any>({ createElement: () => ({}), createText: () => ({}), createComment: () => ({}), insert: () => {}, remove: () => {}, setText: () => {}, setElementText: () => {}, parentNode: () => null, nextSibling: () => null, patchProp: () => {} })
const apps: { unmount: () => void }[] = []
const mount = (feature: any, props: Record<string, unknown>) => {
  const app = renderer.createApp(defineComponent({ extends: feature, render: () => h('div') }), props)
  apps.push(app)
  return app.mount({}) as any
}
afterEach(() => { apps.splice(0).forEach(app => app.unmount()) })

for (const [layout, feature] of [['Classic', TlsEditor], ['Nexus', TlsDrawer]] as const) describe(layout + ' TLS draft', () => {
  it('retains false, empty values, identity and masked provider across open/save/reopen', () => {
    const source = { id: 7, name: 'fixture', server: { enabled: false, certificate_provider: 'fixture-provider', handshake_timeout: '0s' }, client: { insecure: false, disable_sni: false, server_name: '' }, provider: { tag: 'fixture-provider', type: 'acme', runtimeMode: 'legacy_inline', options: { external_account: { mac_key: '[REDACTED]' }, disable_http_challenge: false } } }
    const onSave = vi.fn()
    const vm = mount(feature, { visible: true, id: 7, data: JSON.stringify(source), onSave })
    vm.updateData(7)
    vm.insecure = false
    vm.disableSni = false
    vm.saveChanges()
    expect(onSave.mock.calls[0][0]).toEqual(source)
    vm.closeModal()
    vm.updateData(7)
    expect(vm.tls).toEqual(source)
  })
  it('keeps both credential drafts until a commit copy and restores TLS after Reality switch', () => {
    const source = { id: 7, name: 'fixture', server: { enabled: true, certificate_path: 'fixture.crt', key_path: 'fixture.key', certificate: ['inert-certificate'], key: ['inert-key'] }, client: { insecure: false } }
    const onSave = vi.fn()
    const vm = mount(feature, { visible: true, id: 7, data: JSON.stringify(source), onSave })
    vm.updateData(7)
    vm.selectServerCredentialMode(0)
    vm.selectServerCredentialMode(1)
    expect(vm.dirty).toBe(true)
    expect(vm.tls.server.key_path).toBe('fixture.key')
    expect(vm.tls.server.key).toEqual(['inert-key'])
    vm.tlsType = 1
    vm.changeTlsType()
    vm.tlsType = 0
    vm.changeTlsType()
    expect(vm.tls.server).toEqual(source.server)
    vm.selectServerCredentialMode(0)
    vm.saveChanges()
    expect(onSave.mock.calls[0][0].server).toEqual({ enabled: true, certificate_path: 'fixture.crt', key_path: 'fixture.key' })
    expect(vm.tls.server.key).toEqual(['inert-key'])
    expect(vm.tls.id).toBe(7)
  })
  it('keeps server mTLS CA credentials when the option is disabled and restored before commit', () => {
    const source = { id: 7, name: 'fixture', server: { enabled: false, client_authentication: 'require-and-verify', client_certificate_path: ['fixture-ca.pem'], client_certificate_public_key_sha256: ['fixture-pin'] }, client: {} }
    const vm = mount(feature, { visible: true, id: 7, data: JSON.stringify(source) })
    vm.updateData(7)
    vm.optionClientAuth = false
    expect(vm.optionClientAuth).toBe(false)
    expect(serializeCredentialDraft(vm.tls).server).not.toHaveProperty('client_certificate_path')
    expect(vm.tls.server.client_certificate_path).toEqual(['fixture-ca.pem'])
    vm.optionClientAuth = true
    expect(vm.optionClientAuth).toBe(true)
    expect(vm.tls).toEqual(source)
  })
  it('provider selection retains DTO and failed saves leave the current draft intact', () => {
    const source = { id: 7, name: 'fixture', server: { enabled: false, certificate_provider: 'fixture-provider' }, client: {}, provider: { tag: 'fixture-provider', type: 'acme', runtimeMode: 'native', options: { account_key: '[REDACTED]' } } }
    const onSave = vi.fn()
    const vm = mount(feature, { visible: true, id: 7, data: JSON.stringify(source), onSave })
    vm.updateData(7)
    vm.selectServerCredentialMode(0)
    vm.selectServerCredentialMode(2)
    vm.saveChanges()
    expect(onSave.mock.calls[0][0].provider).toEqual(source.provider)
    expect(vm.tls).toEqual(source)
    vm.providerDraftInvalid = true
    vm.saveChanges()
    expect(onSave).toHaveBeenCalledOnce()
    expect(vm.tls).toEqual(source)
  })
})

describe('outbound TLS and provider presentation', () => {
  it('disabling/re-enabling TLS and mTLS mode selection keep all stored credentials before commit', () => {
    const outbound = reactive({ tls: { enabled: true, insecure: false, client_certificate_path: 'fixture.crt', client_key_path: 'fixture.key', client_certificate: ['inert-certificate'], client_key: ['inert-key'], certificate_public_key_sha256: ['fixture-pin'] } })
    const vm = mount(OutTLS, { outbound })
    vm.tlsEnable = false
    vm.tlsEnable = true
    vm.insecure = false
    vm.optionClientCert = false
    expect(vm.optionClientCert).toBe(false)
    expect(serializeCredentialDraft(outbound).tls).not.toHaveProperty('client_key_path')
    expect(outbound.tls.client_key).toEqual(['inert-key'])
    vm.optionClientCert = true
    expect(vm.optionClientCert).toBe(true)
    vm.selectClientPairMode(1)
    vm.selectClientPairMode(0)
    expect(outbound.tls.client_key).toEqual(['inert-key'])
    const submitted = serializeCredentialDraft(outbound)
    expect(submitted.tls.client_key_path).toBe('fixture.key')
    expect(submitted.tls).not.toHaveProperty('client_key')
    expect(submitted.tls.insecure).toBe(false)
    expect(submitted.tls.certificate_public_key_sha256).toEqual(['fixture-pin'])
  })
  it('an untouched conflicting draft is preserved for backend correction, including nested callers', () => {
    const tls = reactive({ certificate: '', certificate_path: 'fixture.crt', insecure: false })
    const data = { addrs: [{ tls }] }
    expect(serializeCredentialDraft(data)).toEqual(data)
    selectCredentialMode(tls, 'certificate', 'text', ['certificate'], ['certificate_path'])
    expect(serializeCredentialDraft(data).addrs[0].tls).toEqual({ certificate: '', insecure: false })
    expect(tls.certificate_path).toBe('fixture.crt')
  })
  it('invalid provider JSON preserves the accepted options and emits a save guard', () => {
    const profile = reactive({ server: { certificate_provider: 'fixture' }, provider: { tag: 'fixture', options: { account_key: '[REDACTED]', enabled: false } } })
    const validity = vi.fn()
    const vm = mount(TLSProviderEditor, { profile, onValidity: validity })
    vm.optionsText = '{broken'
    vm.applyOptions()
    expect(profile.provider.options).toEqual({ account_key: '[REDACTED]', enabled: false })
    expect(validity).toHaveBeenLastCalledWith(false)
    vm.optionsText = '{"account_key":"[REDACTED]","enabled":false,"extra":0}'
    vm.applyOptions()
    expect(profile.provider.options).toEqual({ account_key: '[REDACTED]', enabled: false, extra: 0 })
  })
  it('size editing preserves unit strings and turns whole numeric input into bytes', () => {
    const data = reactive<Record<string, any>>({})
    const vm = mount(ConsumerTuning, { type: 'hysteria2', direction: 'out', data })
    vm.setField('stream_receive_window', '1MB')
    expect(data.stream_receive_window).toBe('1MB')
    vm.setField('connection_receive_window', '0')
    expect(data.connection_receive_window).toBe(0)
    vm.setField('stream_receive_window', '')
    expect(data).not.toHaveProperty('stream_receive_window')
  })
})
