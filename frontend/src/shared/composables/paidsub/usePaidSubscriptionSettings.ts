import { computed, ref, type Ref } from 'vue'
import HttpUtils from '@/plugins/httputil'
import { normalizeSecretFields, stripSecretPlaceholders } from '@/components/settings/settingsSecretField'
import { push } from 'notivue'
import { i18n } from '@/locales'
import {
  paidSubSettingsDefaults,
  pickPaidSubSettings,
  type SettingsMap as SMap,
} from '@/views/settingsPayload'
import { paidSubTransportModes } from '@/shared/composables/paidsub/tableConfig'

export const usePaidSubscriptionSettings = (loading: Ref<boolean>) => {
  const defaults: SMap = paidSubSettingsDefaults
  const settings = ref<SMap>({ ...defaults })
  const secretboxKeySet = ref(true)
  const pickSettings = pickPaidSubSettings

  const boolSetting = (key: string) => computed({
    get: () => settings.value[key] === 'true',
    set: (v: boolean) => { settings.value[key] = v ? 'true' : 'false' },
  })

  const enabled = boolSetting('paidSubEnabled')
  const autoRegister = boolSetting('paidSubAutoRegister')
  const starsEnabled = boolSetting('paidSubStarsEnabled')
  const yooEnabled = boolSetting('paidSubYooKassaEnabled')
  const stripeEnabled = boolSetting('paidSubStripeEnabled')
  const paymasterEnabled = boolSetting('paidSubPayMasterEnabled')
  const cryptoEnabled = boolSetting('paidSubCryptoBotEnabled')
  const externalEnabled = boolSetting('paidSubExternalEnabled')

  const autoInbounds = computed<number[]>({
    get: () => {
      try { return JSON.parse(settings.value.paidSubAutoInbounds || '[]') } catch { return [] }
    },
    set: (v: number[]) => { settings.value.paidSubAutoInbounds = JSON.stringify(v) },
  })

  const loadSettings = async () => {
    const msg = await HttpUtils.get('api/settings')
    if (msg.success) {
      const normalized = normalizeSecretFields({ ...defaults, ...(msg.obj ?? {}) }) as SMap
      settings.value = pickSettings(normalized)
    }
  }

  const loadStatus = async () => {
    const msg = await HttpUtils.get('api/paidsub/status')
    if (msg.success) secretboxKeySet.value = !!msg.obj?.secretboxKeySet
  }

  const saveSettings = async () => {
    loading.value = true
    const payload = stripSecretPlaceholders(pickSettings(settings.value)) as SMap
    const msg = await HttpUtils.post('api/save', { object: 'settings', action: 'set', data: JSON.stringify(payload) })
    if (msg.success) {
      push.success({ title: i18n.global.t('success'), message: i18n.global.t('pages.paidSub'), duration: 4000 })
      if (msg.obj?.settings) {
        const normalized = normalizeSecretFields({ ...defaults, ...msg.obj.settings }) as SMap
        settings.value = pickSettings(normalized)
      }
    }
    loading.value = false
  }

  const inboundOptions = ref<{ title: string; value: number }[]>([])

  const loadInbounds = async () => {
    const msg = await HttpUtils.get('api/inbounds')
    // api/inbounds returns { obj: { inbounds: [...] } } (LoadPartialData envelope).
    const list = msg?.obj?.inbounds
    if (msg.success && Array.isArray(list)) {
      inboundOptions.value = list.map((i: any) => ({ title: `${i.tag} (${i.type})`, value: i.id }))
    }
  }

  const transportModes = paidSubTransportModes()
  const outboundOptions = ref<{ title: string; value: string }[]>([])

  const loadOutbounds = async () => {
    const msg = await HttpUtils.get('api/outbounds')
    const list = msg?.obj?.outbounds
    if (msg.success && Array.isArray(list)) {
      outboundOptions.value = list.map((o: any) => ({ title: `${o.tag} (${o.type})`, value: o.tag }))
    }
  }

  return {
    autoInbounds,
    autoRegister,
    cryptoEnabled,
    enabled,
    externalEnabled,
    inboundOptions,
    loadInbounds,
    loadOutbounds,
    loadSettings,
    loadStatus,
    outboundOptions,
    paymasterEnabled,
    saveSettings,
    secretboxKeySet,
    settings,
    starsEnabled,
    stripeEnabled,
    transportModes,
    yooEnabled,
  }
}
