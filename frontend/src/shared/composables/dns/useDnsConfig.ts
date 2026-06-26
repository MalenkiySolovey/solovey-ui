import Data from '@/store/modules/data'
import { computed, onBeforeMount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Config } from '@/types/config'
import { dnsRule } from '@/types/dns'
import { FindDiff } from '@/plugins/utils'

export const useDnsConfig = () => {
  const { t } = useI18n()

  const oldConfig = ref(<any>{})
  const loading = ref(false)

  // Edit a LOCAL clone of the store config. A background reload (data.ts setNewData
  // replaces Data().config wholesale, driven by the 10s poll / WS events) must not wipe
  // unsaved edits, so the form binds to this clone instead of the live store object.
  const cloneStoreConfig = (): Config => JSON.parse(JSON.stringify(Data().config ?? {}))

  const ensureDnsShape = (cfg: Config) => {
    // fix old configs
    if (!cfg.dns) cfg.dns = { servers: [], rules: [] }
    if (!cfg.dns.servers) cfg.dns.servers = []
    if (!cfg.dns.rules) cfg.dns.rules = []
  }

  const appConfig = ref<Config>((() => { const c = cloneStoreConfig(); ensureDnsShape(c); return c })())

  const resyncFromStore = () => {
    const c = cloneStoreConfig()
    ensureDnsShape(c)
    appConfig.value = c
    oldConfig.value = JSON.parse(JSON.stringify(c))
  }

  onBeforeMount( async () => {
    loading.value = true
    while (Data().lastLoad == 0) {
      await new Promise(resolve => setTimeout(resolve, 100))
    }
    resyncFromStore()
    loading.value = false
  })

  const tsTags = computed((): string[] => {
    return Data().endpoints?.filter((e:any) => e.type == "tailscale").map((e:any) => e.tag)
  })

  const rslvdTags = computed((): string[] => {
    return Data().services?.filter((e:any) => e.type == "resolved").map((e:any) => e.tag)
  })

  const clients = computed((): string[] => {
    return Data().clients.map((c:any) => c.name)
  })

  const stateChange = computed(() => {
    return FindDiff.deepCompare(appConfig.value.dns,oldConfig.value.dns)
  })

  const saveConfig = async () => {
    loading.value = true
    const success = await Data().save("config", "set", appConfig.value)
    if (success) {
      resyncFromStore()
    }
    loading.value = false
  }

  const applyPresetConfig = (config: Config) => {
    ensureDnsShape(config)
    appConfig.value = config
  }

  const inboundTags = computed((): string[] => {
    return [...Data().inbounds?.map((o:any) => o.tag), ...Data().endpoints?.filter((e:any) => e.listen_port > 0).map((e:any) => e.tag)]
  })

  const outboundTags = computed((): string[] => [
    ...Data().outbounds?.map((o:any) => o.tag),
    ...Data().endpoints?.map((e:any) => e.tag)
  ])

  const dns = computed((): any => {
    return appConfig.value.dns
  })

  const dnsServerTags = computed((): string[] => {
    return dns.value?.servers?.filter((s:any) => s.tag && s.tag != "")?.map((s:any) => s.tag) ?? []
  })

  const finalDns = computed({
    get() { return dns.value?.final?? '' },
    set(v:string) { dns.value.final = v.length>0 ? v : undefined }
  })

  const dnsRules = computed((): dnsRule[] => {
    return <dnsRule[]>dns.value.rules
  })

  const ruleSets = computed((): string[] => {
    return appConfig.value?.route?.rule_set?.map((r:any) => r.tag) ?? []
  })

  const subtitle = computed(() => {
    const servers = dns.value?.servers?.length ?? 0
    const rules = dnsRules.value?.length ?? 0

    return t('nexus.summary.dns', { servers, rules })
  })

  return {
    appConfig,
    applyPresetConfig,
    clients,
    dns,
    dnsRules,
    dnsServerTags,
    finalDns,
    inboundTags,
    loading,
    outboundTags,
    rslvdTags,
    ruleSets,
    saveConfig,
    stateChange,
    subtitle,
    t,
    tsTags,
  }
}
