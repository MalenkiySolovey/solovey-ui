import { afterEach, describe, expect, it, vi } from 'vitest'
import { createRenderer, createSSRApp, defineComponent, h, reactive, ref } from 'vue'
import { renderToString } from '@vue/server-renderer'
import { createI18n } from 'vue-i18n'
import { createVuetify } from 'vuetify'
import ClientEditor from '@/layouts/modals/Client.vue'
import ClientLogic from '@/layouts/modals/Client.logic'
import SubClashExt from '@/components/subscription/SubClashExt.vue'
import OutJson from '@/components/subscription/OutJson.vue'
import { clientCredentialKeys, createClient, getClientPublicRemark, setClientPublicRemark, shuffleConfigs, updateConfigs } from '@/types/clients'
import { pickSettingsByDefaults, settingsPageDefaults } from '@/features/settingsPayload'

const fixture = vi.hoisted(() => ({ store: null as any, mode: null as any }))
vi.mock('@/uiMode/useUiMode', () => ({ useUiMode: () => ({ mode: fixture.mode }) }))
vi.mock('@/store/modules/data', () => ({ default: () => fixture.store }))
vi.mock('@/plugins/randomUtil', () => ({ default: { randomSeq: () => 'fixture-random', randomUUID: () => '11111111-1111-4111-8111-111111111111', randomShadowsocksPassword: () => 'fixture-key' } }))

const renderer = createRenderer<any, any>({ createElement: () => ({}), createText: () => ({}), createComment: () => ({}), insert: () => {}, remove: () => {}, setText: () => {}, setElementText: () => {}, parentNode: () => null, nextSibling: () => null, patchProp: () => {} })
const apps: { unmount: () => void }[] = []
afterEach(() => { apps.splice(0).forEach(app => app.unmount()) })

async function markup(component: any, props: Record<string, any>, initialize?: (vm: any) => void) {
  const app = createSSRApp(defineComponent({ ...component, created() { initialize?.(this) } }), props)
  app.use(createI18n({ legacy: false, locale: 'en', messages: { en: {} }, missingWarn: false, fallbackWarn: false }))
  app.use(createVuetify({ ssr: true }))
  app.config.globalProperties.$t = (key: string) => key
  app.config.warnHandler = () => {}
  for (const name of ['v-row', 'v-col', 'v-select', 'v-text-field', 'v-alert', 'v-card', 'v-card-title', 'v-card-subtitle', 'v-card-text', 'v-card-actions', 'v-divider', 'v-spacer', 'v-btn', 'v-dialog', 'v-navigation-drawer', 'v-toolbar', 'v-toolbar-title', 'v-icon', 'v-tabs', 'v-tab', 'v-window', 'v-window-item', 'v-switch', 'v-combobox', 'v-progress-linear']) {
    app.component(name, defineComponent({ inheritAttrs: false, setup: (_, context) => () => h('div', context.attrs, context.slots.default?.()) }))
  }
  return renderToString(app)
}

describe('subscription public contracts shared by Classic and Nexus', () => {
  it('keeps public metadata separate from authentication rename and credential shuffle', () => {
    const client = createClient({ id: 7, name: 'private-auth', desc: 'private-description', config: { trojan: { name: 'private-auth', password: 'before' }, _subscription: { publicRemark: 'Public 雪', name: 'metadata-owned' } } })
    updateConfigs(client.config!, 'private-renamed')
    shuffleConfigs(client.config!)
    expect(getClientPublicRemark(client.config!)).toBe('Public 雪')
    expect(client.config!._subscription.name).toBe('metadata-owned')
    expect(client.config!.trojan.name).toBe('private-renamed')
    expect(clientCredentialKeys(client.config!)).not.toContain('_subscription')
    setClientPublicRemark(client.config!, '')
    expect(client.config!._subscription.publicRemark).toBe('')
  })

  for (const mode of ['classic', 'nexus']) {
    it(`${mode} saves an explicit public remark through the existing client payload`, async () => {
      fixture.mode = ref(mode)
      const saved = vi.fn().mockResolvedValue(false)
      fixture.store = reactive({ loadClients: async () => ({ id: 7, name: 'private-auth', config: { trojan: { name: 'private-auth', password: 'fixture' }, _subscription: { publicRemark: 'Before' } }, links: [{ type: 'local', uri: '', diagnostic: 'Use JSON for the configured header.' }] }), checkClientName: () => false, save: saved })
      const app = renderer.createApp(defineComponent({ extends: ClientLogic, render: () => h('div') }), { visible: true, id: 7, inboundTags: [], groups: [] })
      app.config.globalProperties.$t = (key: string) => key
      app.config.warnHandler = () => {}
      apps.push(app)
      const vm = app.mount({}) as any
      await vm.updateData(7)
      expect(vm.publicRemark).toBe('Before')
      vm.publicRemark = 'Public 雪'
      vm.client.name = 'private-renamed'
      expect(vm.dirty).toBe(true)
      await vm.saveChanges()
      expect(saved).toHaveBeenCalledWith('clients', 'edit', expect.objectContaining({ config: expect.objectContaining({ _subscription: { publicRemark: 'Public 雪' } }) }))
      expect(vm.links[0].diagnostic).toContain('JSON')
    })

    it(`${mode} renders the public remark and export diagnosis in its actual form shell`, async () => {
      fixture.mode = ref(mode)
      const html = await markup(ClientEditor, { visible: true, modelValue: true, id: 7, inboundTags: [], groups: [] }, vm => {
        vm.clientConfig = { trojan: { password: 'fixture' }, _subscription: { publicRemark: 'Public 雪' } }
        vm.links = [{ type: 'local', uri: '', diagnostic: 'Use JSON for the configured header.' }]
      })
      expect(html).toContain('client.publicRemark')
      expect(html).toContain('client.publicRemarkHint')
      expect(html).toContain('Use JSON for the configured header.')
      expect(html).not.toContain('>_subscription<')
    })

    it(`${mode} preserves all three Clash UDP setting states`, async () => {
      fixture.mode = ref(mode)
      for (const policy of ['', 'true', 'false']) {
        const settings = reactive(pickSettingsByDefaults(settingsPageDefaults, { subClashUDP: policy }))
        expect(settings.subClashUDP).toBe(policy)
        const html = await markup(SubClashExt, { settings })
        expect(html).toContain('setting.subClashUDP')
        expect(html).toContain('setting.subClashUDPHint')
        expect(settings.subClashUDP).toBe(policy)
      }
    })

    it(`${mode} exposes SIP002 plugin metadata in the shared subscription editor`, async () => {
      fixture.mode = ref(mode)
      const data = reactive({ out_json: { type: 'shadowsocks', plugin: 'v2ray-plugin', plugin_opts: 'tls;host=example.com;path=/fixture' } })
      const html = await markup(OutJson, { inData: data, type: 'shadowsocks' })
      expect(html).toContain('singbox.plugin')
      expect(html).toContain('setting.shadowsocksPluginShareHint')
      expect(data.out_json.plugin_opts).toBe('tls;host=example.com;path=/fixture')
    })
  }
})
