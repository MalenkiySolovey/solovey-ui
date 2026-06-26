import { ref } from 'vue'
import { push } from 'notivue'
import HttpUtils from '@/plugins/httputil'
import { settingsPageDefaults } from '@/views/settingsPayload'
import type {
  ClientConversionTarget,
  ConversionFeature,
  ConversionMode,
  ConversionRules,
  RemoteConversionPolicy,
  RemoteTranslate,
} from './types'

export const conversionFeatures: { key: ConversionFeature, label: string }[] = [
  { key: 'xrayBalancer', label: 'Xray balancer' },
  { key: 'mihomoFallback', label: 'Mihomo fallback' },
  { key: 'mihomoLoadBalance', label: 'Mihomo load-balance' },
  { key: 'mihomoSmart', label: 'Mihomo smart' },
  { key: 'mihomoRelay', label: 'Mihomo relay' },
  { key: 'mihomoSsid', label: 'Mihomo ssid' },
]

export const runtimeConversionModes: ConversionMode[] = ['urltest', 'selector', 'failover']

const xrayConversionModes: ConversionMode[] = ['balancer']

const mihomoConversionModes: ConversionMode[] = ['url-test', 'select', 'fallback', 'load-balance']

const defaultConversionPolicy = (): RemoteConversionPolicy => JSON.parse(settingsPageDefaults.subRemoteConversionPolicy)

const nativeClientTarget = (feature: ConversionFeature): ClientConversionTarget => {
  return feature === 'xrayBalancer' ? 'xray' : 'mihomo'
}

export const isNativeClientConversion = (feature: ConversionFeature, target: ClientConversionTarget): boolean => {
  return target !== 'singBox' && nativeClientTarget(feature) === target
}

export const clientConversionModesFor = (feature: ConversionFeature, target: ClientConversionTarget): ConversionMode[] => {
  if (isNativeClientConversion(feature, target)) return ['original']
  if (target === 'xray') return xrayConversionModes
  if (target === 'mihomo') return mihomoConversionModes
  return runtimeConversionModes
}

const normalizeLegacyConversionMode = (mode: string, modes: ConversionMode[]): ConversionMode | '' => {
  if (modes.includes(mode as ConversionMode)) return mode as ConversionMode
  if (modes.includes('balancer') && ['urltest', 'selector', 'failover'].includes(mode)) return 'balancer'
  if (modes.includes('url-test') && mode === 'urltest') return 'url-test'
  if (modes.includes('select') && mode === 'selector') return 'select'
  if (modes.includes('fallback') && mode === 'failover') return 'fallback'
  return ''
}

const normalizeConversionPolicy = (value: any, fallback: RemoteConversionPolicy): RemoteConversionPolicy => {
  const next = defaultConversionPolicy()
  const normalizeRules = (
    source: any,
    defaults: ConversionRules,
    modesForFeature: (feature: ConversionFeature) => ConversionMode[],
  ): ConversionRules => {
    const rules = { ...defaults } as ConversionRules
    for (const feature of conversionFeatures) {
      const mode = normalizeLegacyConversionMode(String(source?.[feature.key] ?? '').trim(), modesForFeature(feature.key))
      if (mode) {
        rules[feature.key] = mode
      }
    }
    return rules
  }
  next.outbound = normalizeRules(value?.outbound, fallback.outbound, () => runtimeConversionModes)
  next.client.singBox = normalizeRules(value?.client?.singBox, fallback.client.singBox, () => runtimeConversionModes)
  next.client.xray = normalizeRules(value?.client?.xray, fallback.client.xray, feature => clientConversionModesFor(feature, 'xray'))
  next.client.mihomo = normalizeRules(value?.client?.mihomo, fallback.client.mihomo, feature => clientConversionModesFor(feature, 'mihomo'))
  return next
}

const parseConversionPolicy = (raw: unknown): RemoteConversionPolicy => {
  const fallback = defaultConversionPolicy()
  if (typeof raw !== 'string' || !raw.trim()) return fallback
  try {
    return normalizeConversionPolicy(JSON.parse(raw), fallback)
  } catch {
    return fallback
  }
}

export const useRemoteConversionPolicy = (t: RemoteTranslate) => {
  const conversionDialog = ref(false)
  const savingConversionPolicy = ref(false)
  const conversionPolicy = ref<RemoteConversionPolicy>(defaultConversionPolicy())

  const loadConversionPolicy = async () => {
    const msg = await HttpUtils.get('api/settings')
    if (msg.success) {
      conversionPolicy.value = parseConversionPolicy(msg.obj?.subRemoteConversionPolicy)
    }
  }

  const openConversionPolicy = () => {
    conversionDialog.value = true
  }

  const saveConversionPolicy = async () => {
    savingConversionPolicy.value = true
    try {
      const payload = {
        subRemoteConversionPolicy: JSON.stringify(conversionPolicy.value),
      }
      const msg = await HttpUtils.post('api/save', {
        object: 'settings',
        action: 'set',
        data: JSON.stringify(payload),
      })
      if (msg.success) {
        conversionPolicy.value = parseConversionPolicy(msg.obj?.settings?.subRemoteConversionPolicy ?? payload.subRemoteConversionPolicy)
        conversionDialog.value = false
        push.success({ message: t('remoteOutbound.conversionSaved'), duration: 5000 })
      }
    } finally {
      savingConversionPolicy.value = false
    }
  }

  return {
    clientConversionModesFor,
    conversionDialog,
    conversionFeatures,
    conversionPolicy,
    isNativeClientConversion,
    loadConversionPolicy,
    openConversionPolicy,
    runtimeConversionModes,
    saveConversionPolicy,
    savingConversionPolicy,
  }
}
