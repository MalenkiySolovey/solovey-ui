import { isNexusEnabled } from '@/uiMode/featureGate'
import { useUiMode } from '@/uiMode/useUiMode'
import { i18n } from '@/locales'
import { Ref, computed, inject, onMounted, ref } from 'vue'
import HttpUtils from '@/plugins/httputil'
import { FindDiff } from '@/plugins/utils'
import { normalizeSecretFields, stripSecretPlaceholders } from '@/components/settings/settingsSecretField'
import { settingsPageDefaults, type SettingsMap } from '@/views/settingsPayload'
import { push } from 'notivue'
import { useRoute } from 'vue-router'

const settingsTabs = new Set(['t1', 't2', 't3', 't4', 't5', 't6', 'basics'])

export const useSettingsPage = () => {
  const timezones = typeof Intl.supportedValuesOf === 'function'
    ? Intl.supportedValuesOf('timeZone')
    : ['Etc/UTC', 'Europe/Moscow']
  const route = useRoute()
  const queryTab = typeof route.query.tab === 'string' ? route.query.tab : ''
  const tab = ref(settingsTabs.has(queryTab) ? queryTab : "t1")

  const { mode } = useUiMode()

  const nexus = computed(() => mode.value === 'nexus')

  const showNexusControls = isNexusEnabled()

  const loading:Ref = inject('loading')?? ref(false)

  const oldSettings = ref<SettingsMap>({ ...settingsPageDefaults })

  const settings = ref<SettingsMap>({ ...settingsPageDefaults })

  onMounted(async () => {
    loading.value = true
    await loadData()
    loading.value = false
  })

  const loadData = async () => {
    loading.value = true
    const msg = await HttpUtils.get('api/settings')
    loading.value = false
    if (msg.success) {
      setData(msg.obj)
    }
  }

  const setData = (data: any) => {
    const normalized = normalizeSecretFields({ ...settingsPageDefaults, ...data }) as SettingsMap
    settings.value = normalized
    oldSettings.value = { ...normalized }
  }

  const save = async () => {
    loading.value = true
    const payload = stripSecretPlaceholders(settings.value)
    const restartRequired = subscriptionPathChanged()
    const msg = await HttpUtils.post('api/save', { object: 'settings', action: 'set', data: JSON.stringify(payload) })
    if (msg.success) {
      push.success({
        title: i18n.global.t('success'),
        duration: 5000,
        message: i18n.global.t('actions.set') + " " + i18n.global.t('pages.settings')
      })
      if (restartRequired) {
        push.warning({
          title: i18n.global.t('setting.restartRequired'),
          duration: 8000,
          message: i18n.global.t('setting.subPathRestartNotice')
        })
      }
      setData(msg.obj.settings)
    }
    loading.value = false
  }

  const sleep = (ms: number) => new Promise(resolve => setTimeout(resolve, ms))

  const restartApp = async () => {
    loading.value = true
    const msg = await HttpUtils.post('api/restartApp',{})
    if (msg.success) {
      let url = settings.value.webURI
      if (url !== "") {
        const isTLS = settings.value.webCertFile !== "" || settings.value.webKeyFile !== ""
        url = buildURL(settings.value.webDomain,settings.value.webPort.toString(),isTLS, settings.value.webPath)
      }
      await sleep(3000)
      window.location.replace(url)
    }
    loading.value = false
  }

  const buildURL = (host: string, port: string, isTLS: boolean, path: string) => {
    if (!host || host.length == 0) host = window.location.hostname
    if (!port || port.length == 0) port = window.location.port

    const protocol = isTLS ? "https:" : "http:"

    if (port === "" || (isTLS && port === "443") || (!isTLS && port === "80")) {
        port = ""
    } else {
        port = `:${port}`
    }

    return `${protocol}//${host}${port}${path}settings`
  }

  const subEncode = computed({
    get: () => { return settings.value.subEncode == "true" },
    set: (v:boolean) => { settings.value.subEncode = v ? "true" : "false" }
  })

  const subShowInfo = computed({
    get: () => { return settings.value.subShowInfo == "true" },
    set: (v:boolean) => { settings.value.subShowInfo = v ? "true" : "false" }
  })

  const subSecretRequired = computed({
    get: () => { return settings.value.subSecretRequired == "true" },
    set: (v:boolean) => { settings.value.subSecretRequired = v ? "true" : "false" }
  })

  const subLinkEnable = computed({
    get: () => { return settings.value.subLinkEnable == "true" },
    set: (v:boolean) => { settings.value.subLinkEnable = v ? "true" : "false" }
  })

  const subJsonEnable = computed({
    get: () => { return settings.value.subJsonEnable == "true" },
    set: (v:boolean) => { settings.value.subJsonEnable = v ? "true" : "false" }
  })

  const subClashEnable = computed({
    get: () => { return settings.value.subClashEnable == "true" },
    set: (v:boolean) => { settings.value.subClashEnable = v ? "true" : "false" }
  })

  const subXrayEnable = computed({
    get: () => { return settings.value.subXrayEnable == "true" },
    set: (v:boolean) => { settings.value.subXrayEnable = v ? "true" : "false" }
  })

  const subNameInRemark = computed({
    get: () => { return settings.value.subNameInRemark == "true" },
    set: (v:boolean) => { settings.value.subNameInRemark = v ? "true" : "false" }
  })

  const webPort = computed({
    get: () => { return settings.value.webPort.length>0 ? parseInt(settings.value.webPort) : 2095 },
    set: (v:number) => { settings.value.webPort = v>0 ? v.toString() : "2095" }
  })

  const sessionMaxAge = computed({
    get: () => { return settings.value.sessionMaxAge.length>0 ? parseInt(settings.value.sessionMaxAge) : 0 },
    set: (v:number) => { settings.value.sessionMaxAge = v>0 ? v.toString() : "0" }
  })

  const trafficAge = computed({
    get: () => { return settings.value.trafficAge.length>0 ? parseInt(settings.value.trafficAge) : 0 },
    set: (v:number) => { settings.value.trafficAge = v>0 ? v.toString() : "0" }
  })

  const subPort = computed({
    get: () => { return settings.value.subPort.length>0 ? parseInt(settings.value.subPort) : 2096 },
    set: (v:number) => { settings.value.subPort = v>0 ? v.toString() : "2096" }
  })

  const subUpdates = computed({
    get: () => { return settings.value.subUpdates.length>0 ? parseInt(settings.value.subUpdates) : 12 },
    set: (v:number) => { settings.value.subUpdates = v>0 ? v.toString() : "12" }
  })

  const subRateLimitPerIP = computed({
    get: () => { return settings.value.subRateLimitPerIP.length>0 ? parseInt(settings.value.subRateLimitPerIP) : 60 },
    set: (v:number) => { settings.value.subRateLimitPerIP = v>=0 ? v.toString() : "60" }
  })

  const subscriptionPathKeys = ['subPath', 'subJsonPath', 'subClashPath', 'subXrayPath'] as const

  const subscriptionPathChanged = () => {
    return subscriptionPathKeys.some((key) => settings.value[key] !== (oldSettings.value as any)[key])
  }

  const stateChange = computed(() => {
    return !FindDiff.deepCompare(settings.value,oldSettings.value)
  })

  return {
    loading,
    nexus,
    restartApp,
    save,
    sessionMaxAge,
    settings,
    showNexusControls,
    stateChange,
    subClashEnable,
    subEncode,
    subJsonEnable,
    subLinkEnable,
    subNameInRemark,
    subPort,
    subRateLimitPerIP,
    subSecretRequired,
    subShowInfo,
    subUpdates,
    subXrayEnable,
    tab,
    trafficAge,
    timezones,
    webPort,
  }
}
