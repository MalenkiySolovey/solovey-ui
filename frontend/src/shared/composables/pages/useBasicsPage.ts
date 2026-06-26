import Data from '@/store/modules/data'
import { computed, ref, onBeforeMount } from 'vue'
import { i18n } from '@/locales'
import { Config, Ntp } from '@/types/config'
import { FindDiff } from '@/plugins/utils'

export const useBasicsPage = () => {
  const clashModes = ['rule', 'global', 'direct']
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

  const stateChange = computed(() => {
    return FindDiff.deepCompare(appConfig.value,oldConfig.value)
  })

  const saveConfig = async () => {
    loading.value = true
    try {
      const success = await Data().save("config", "set", appConfig.value)
      if (success) {
        resyncFromStore()
      }
    } finally {
      loading.value = false
    }
  }

  const inboundTags = computed((): string[] => {
    return [...Data().inbounds?.map((i:any) => i.tag), ...Data().endpoints?.filter((e:any) => e.listen_port > 0).map((e:any) => e.tag)]
  })

  const clientNames = computed((): string[] => {
    const clients = <any[]>Data().clients
    return clients?.map(c => c.name)
  })

  const outboundTags = computed((): string[] => {
    return [...Data().outbounds?.map((o:any) => o.tag), ...Data().endpoints?.map((e:any) => e.tag)]
  })

  const levels = ["trace", "debug", "info", "warn", "error", "fatal", "panic"]

  const certificateModes = [
    { title: i18n.global.t('singbox.off'), value: 'off' },
    { title: 'System', value: 'system' },
    { title: 'Mozilla', value: 'mozilla' },
    { title: 'Chrome', value: 'chrome' },
    { title: i18n.global.t('singbox.customCaFile'), value: 'file' },
    { title: i18n.global.t('singbox.customCaDirectory'), value: 'directory' },
    { title: i18n.global.t('singbox.pastePem'), value: 'pem' },
    { title: i18n.global.t('singbox.advanced'), value: 'custom' },
  ]

  const certificateStores = ['system', 'mozilla', 'chrome', 'none']

  function textToList(value: string): string[] | undefined {
    const items = value.split('\n').map(item => item.trim()).filter(item => item.length > 0)
    return items.length > 0 ? items : undefined
  }

  const enableNtp = computed({
    get() { return appConfig.value.ntp?.enabled?? false },
    set(v:boolean) {
      if (v){
        appConfig.value.ntp = <Ntp>{ enabled: true, server: 'time.apple.com', server_port: 123, interval: '30m'}
      } else { delete appConfig.value.ntp }
    }
  })

  const ntpInterval = computed({
    get():any { return appConfig.value.ntp?.interval? parseInt(appConfig.value.ntp?.interval.replace('m','')) : null },
    set(v:number) { if (appConfig.value.ntp) v>0 ? appConfig.value.ntp.interval =  v + 'm' : delete appConfig.value.ntp.interval }
  })

  const enableCacheFile = computed({
    get() { return appConfig.value.experimental.cache_file?.enabled?? false },
    set(v:boolean) {
      if (v){
        appConfig.value.experimental.cache_file = { enabled: true }
      } else { delete appConfig.value.experimental.cache_file  }
    }
  })

  const enableDebug = computed({
    get() { return appConfig.value.experimental.debug != undefined },
    set(v:boolean) { v ? appConfig.value.experimental.debug = {} : delete appConfig.value.experimental.debug }
  })

  const certificateMode = computed({
    get(): string {
      const cert = appConfig.value.certificate
      if (!cert) return 'off'
      if (cert.certificate && cert.certificate.length > 0) return 'pem'
      if (cert.certificate_path && cert.certificate_path.length > 0) return 'file'
      if (cert.certificate_directory_path && cert.certificate_directory_path.length > 0) return 'directory'
      return cert.store || 'system'
    },
    set(v:string) {
      if (v == 'off') {
        delete appConfig.value.certificate
        return
      }
      appConfig.value.certificate = {}
      if (['system', 'mozilla', 'chrome'].includes(v)) {
        appConfig.value.certificate.store = v as 'system' | 'mozilla' | 'chrome'
      } else if (v == 'file') {
        appConfig.value.certificate.certificate_path = []
      } else if (v == 'directory') {
        appConfig.value.certificate.certificate_directory_path = []
      } else if (v == 'pem') {
        appConfig.value.certificate.certificate = []
      } else if (v == 'custom') {
        appConfig.value.certificate.store = 'none'
      }
    }
  })

  const certificateText = computed({
    get(): string { return appConfig.value.certificate?.certificate?.join('\n') ?? '' },
    set(v:string) {
      if (!appConfig.value.certificate) appConfig.value.certificate = {}
      const values = textToList(v)
      values ? appConfig.value.certificate.certificate = values : delete appConfig.value.certificate.certificate
    }
  })

  const certificatePathText = computed({
    get(): string { return appConfig.value.certificate?.certificate_path?.join('\n') ?? '' },
    set(v:string) {
      if (!appConfig.value.certificate) appConfig.value.certificate = {}
      const values = textToList(v)
      values ? appConfig.value.certificate.certificate_path = values : delete appConfig.value.certificate.certificate_path
    }
  })

  const certificateDirectoryText = computed({
    get(): string { return appConfig.value.certificate?.certificate_directory_path?.join('\n') ?? '' },
    set(v:string) {
      if (!appConfig.value.certificate) appConfig.value.certificate = {}
      const values = textToList(v)
      values ? appConfig.value.certificate.certificate_directory_path = values : delete appConfig.value.certificate.certificate_directory_path
    }
  })

  const enableClashApi = computed({
    get() { return appConfig.value.experimental.clash_api != undefined },
    set(v:boolean) { appConfig.value.experimental.clash_api = v ? { external_controller: '127.0.0.1:9090' } : undefined }
  })

  const enableV2rayApi = computed({
    get() { return appConfig.value.experimental.v2ray_api != undefined },
    set(v:boolean) { appConfig.value.experimental.v2ray_api = v ? { listen: '127.0.0.1:8080', stats: { enabled: false, inbounds: [], outbounds: [], users: [] }} : undefined }
  })

  const origin = computed({
    get() { return appConfig.value.experimental.clash_api?.access_control_allow_origin &&
      appConfig.value.experimental.clash_api.access_control_allow_origin.length>0 ? appConfig.value.experimental.clash_api.access_control_allow_origin.join(',') : '' },
    set(v:string) {
      if (appConfig.value.experimental.clash_api?.access_control_allow_origin)
        appConfig.value.experimental.clash_api.access_control_allow_origin = v.length> 0 ? v.split(',') : undefined
      }
  })

  return {
    appConfig,
    certificateDirectoryText,
    certificateMode,
    certificateModes,
    certificatePathText,
    certificateStores,
    certificateText,
    clashModes,
    clientNames,
    enableCacheFile,
    enableClashApi,
    enableDebug,
    enableNtp,
    enableV2rayApi,
    inboundTags,
    levels,
    loading,
    ntpInterval,
    origin,
    outboundTags,
    saveConfig,
    stateChange,
  }
}
