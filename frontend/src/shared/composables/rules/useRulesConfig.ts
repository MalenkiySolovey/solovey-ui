import Data from '@/store/modules/data'
import { computed, onBeforeMount, ref } from 'vue'
import { Config } from '@/types/config'
import { FindDiff } from '@/plugins/utils'
import { i18n } from '@/locales'

export const useRulesConfig = () => {
  const oldConfig = ref(<any>{})
  const loading = ref(false)

  // Edit a LOCAL clone of the store config. A background reload (data.ts setNewData
  // replaces Data().config wholesale, driven by the 10s poll / WS events) must not wipe
  // unsaved edits, so the form binds to this clone instead of the live store object.
  const cloneStoreConfig = (): Config => JSON.parse(JSON.stringify(Data().config ?? {}))

  const appConfig = ref<Config>(cloneStoreConfig())

  const resyncFromStore = () => {
    appConfig.value = cloneStoreConfig()
    oldConfig.value = cloneStoreConfig()
  }

  onBeforeMount(async () => {
    loading.value = true
    while (Data().lastLoad == 0) {
      await new Promise(resolve => setTimeout(resolve, 100))
    }
    resyncFromStore()
    loading.value = false
  })

  const route = computed((): any => appConfig.value.route ?? {})

  const routeMark = computed({
    get() { return route.value.default_mark ?? 0 },
    set(v:number) { v>0 ? route.value.default_mark = v : delete appConfig.value.route.default_mark }
  })

  const routePresets = [
    { title: i18n.global.t('singbox.defaultPreset'), value: 'default' },
    { title: i18n.global.t('singbox.mobileStable'), value: 'mobile' },
    { title: i18n.global.t('singbox.preferWifi'), value: 'wifi' },
    { title: i18n.global.t('singbox.processRules'), value: 'process' },
  ]

  const networkTypes = ['wifi', 'cellular', 'ethernet', 'other']

  const clearRouteNetworkDefaults = () => {
    delete route.value.default_network_strategy
    delete route.value.default_network_type
    delete route.value.default_fallback_network_type
    delete route.value.default_fallback_delay
  }

  const routePreset = computed({
    get(): string {
      if (route.value.default_network_strategy == 'fallback' &&
        JSON.stringify(route.value.default_network_type ?? []) == JSON.stringify(['wifi']) &&
        JSON.stringify(route.value.default_fallback_network_type ?? []) == JSON.stringify(['cellular'])) return 'wifi'
      if (route.value.default_network_strategy == 'fallback') return 'mobile'
      if (route.value.find_process) return 'process'
      return 'default'
    },
    set(v:string) {
      if (v == 'default') {
        clearRouteNetworkDefaults()
        delete route.value.find_process
      } else if (v == 'mobile') {
        delete route.value.default_interface
        route.value.auto_detect_interface = true
        route.value.default_network_strategy = 'fallback'
        delete route.value.default_network_type
        delete route.value.default_fallback_network_type
        delete route.value.default_fallback_delay
      } else if (v == 'wifi') {
        delete route.value.default_interface
        route.value.auto_detect_interface = true
        route.value.default_network_strategy = 'fallback'
        route.value.default_network_type = ['wifi']
        route.value.default_fallback_network_type = ['cellular']
        delete route.value.default_fallback_delay
      } else if (v == 'process') {
        route.value.find_process = true
      }
    }
  })

  const defaultFallbackDelayMs = computed({
    get(): number | undefined { return route.value.default_fallback_delay ? parseInt(route.value.default_fallback_delay.replace('ms', '')) : undefined },
    set(v:number | undefined) {
      if (typeof v == 'number' && !isNaN(v) && v > 0 && v != 300) route.value.default_fallback_delay = `${v}ms`
      else delete route.value.default_fallback_delay
    }
  })

  const routeDefaultNetworkStrategy = computed({
    get(): string | undefined { return route.value.default_network_strategy },
    set(v:string | undefined) {
      if (!v) {
        clearRouteNetworkDefaults()
        return
      }
      route.value.default_network_strategy = v
      if (v != 'fallback') {
        delete route.value.default_fallback_network_type
      }
    }
  })

  const findProcess = computed({
    get(): boolean { return route.value.find_process === true },
    set(v:boolean) { v ? route.value.find_process = true : delete route.value.find_process }
  })

  const overrideAndroidVpn = computed({
    get(): boolean { return route.value.override_android_vpn === true },
    set(v:boolean) { v ? route.value.override_android_vpn = true : delete route.value.override_android_vpn }
  })

  const stateChange = computed(() => FindDiff.deepCompare(appConfig.value, oldConfig.value))

  const saveConfig = async () => {
    loading.value = true
    const success = await Data().save("config", "set", appConfig.value)
    if (success) {
      resyncFromStore()
      loading.value = false
    }
  }

  const applyPresetConfig = (config: Config) => {
    appConfig.value = config
  }

  const clients = computed((): string[] => Data().clients.map((c:any) => c.name))

  const rules = computed((): any[] => {
    const data = route.value
    if (!data) return []
    if (!('rules' in data) || !Array.isArray(data.rules)) data.rules = []
    return data.rules
  })

  const rulesets = computed((): any[] => {
    const data = route.value
    if (!data) return []
    if (!('rule_set' in data) || !Array.isArray(data.rule_set)) data.rule_set = []
    return data.rule_set
  })

  const rulesetTags = computed((): string[] => rulesets.value.map((rs:any) => rs.tag))

  const outboundTags = computed((): string[] => [
    ...Data().outbounds?.map((o:any) => o.tag),
    ...Data().endpoints?.map((e:any) => e.tag)
  ])

  const inboundTags = computed((): string[] => [
    ...Data().inbounds?.map((o:any) => o.tag),
    ...Data().endpoints?.filter((e:any) => e.listen_port > 0).map((e:any) => e.tag)
  ])

  return {
    appConfig,
    applyPresetConfig,
    clients,
    defaultFallbackDelayMs,
    findProcess,
    inboundTags,
    loading,
    networkTypes,
    outboundTags,
    overrideAndroidVpn,
    route,
    routeDefaultNetworkStrategy,
    routeMark,
    routePreset,
    routePresets,
    rules,
    rulesetTags,
    rulesets,
    saveConfig,
    stateChange,
  }
}
