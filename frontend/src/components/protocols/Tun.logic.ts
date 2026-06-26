import { defineComponent } from 'vue'
import Data from '@/store/modules/data'

export default defineComponent({
  props: ['data'],
  data() {
    return {
      menu: false
    }
  },
  computed: {
    ruleSetTags() { return Data().config.route?.rule_set?.map((rs:any) => rs.tag) ?? [] },
    emptyAppPreset() {
      return (this.$props.data.include_package != undefined && this.$props.data.include_package.length == 0) ||
        (this.$props.data.exclude_package != undefined && this.$props.data.exclude_package.length == 0)
    },
    optionEndpointIndependentNat: {
      get() { return this.$props.data.endpoint_independent_nat ?? false },
      set(v:boolean) { v ? this.$props.data.endpoint_independent_nat = true : delete this.$props.data.endpoint_independent_nat }
    },
    addrs: {
      get() { return this.$props.data.address?.join(',') },
      set(v:string) { this.$props.data.address = v.length > 0 ? v.split(',') : undefined }
    },
    udpTimeout: {
      get() { return this.$props.data.udp_timeout ? parseInt(this.$props.data.udp_timeout.replace('m','')) : 5 },
      set(v:number) { this.$props.data.udp_timeout = v > 0 ? v + 'm' : '5m' }
    },
    autoRoute: {
      get() { return this.$props.data.auto_route ?? false },
      set(v:boolean) {
        if (v) {
          this.$props.data.auto_route = true
        } else {
          delete this.$props.data.auto_route
          delete this.$props.data.auto_redirect
          delete this.$props.data.strict_route
          delete this.$props.data.exclude_mptcp
          delete this.$props.data.auto_redirect_reset_mark
          delete this.$props.data.auto_redirect_input_mark
          delete this.$props.data.auto_redirect_output_mark
          delete this.$props.data.auto_redirect_nfqueue
          delete this.$props.data.auto_redirect_iproute2_fallback_rule_index
          delete this.$props.data.route_address
          delete this.$props.data.route_address_set
          delete this.$props.data.route_exclude_address
          delete this.$props.data.route_exclude_address_set
        }
      }
    },
    autoRedirect: {
      get() { return this.$props.data.auto_redirect === true },
      set(v:boolean) { v ? this.$props.data.auto_redirect = true : delete this.$props.data.auto_redirect }
    },
    strictRoute: {
      get() { return this.$props.data.strict_route === true },
      set(v:boolean) { v ? this.$props.data.strict_route = true : delete this.$props.data.strict_route }
    },
    excludeMptcp: {
      get() { return this.$props.data.exclude_mptcp === true },
      set(v:boolean) { v ? this.$props.data.exclude_mptcp = true : delete this.$props.data.exclude_mptcp }
    },
    routeAddressSet: {
      get() { return this.$props.data.route_address_set ?? [] },
      set(v:string[]) { v.length > 0 ? this.$props.data.route_address_set = v : delete this.$props.data.route_address_set }
    },
    autoRedirectResetMark: markText('auto_redirect_reset_mark'),
    autoRedirectInputMark: markText('auto_redirect_input_mark'),
    autoRedirectOutputMark: markText('auto_redirect_output_mark'),
    fallbackRuleIndex: {
      get() { return this.$props.data.auto_redirect_iproute2_fallback_rule_index ?? 32768 },
      set(v: number) {
        const val = typeof v === 'number' && !isNaN(v) && v >= 0 ? v : undefined
        this.$props.data.auto_redirect_iproute2_fallback_rule_index = val
      }
    },
    nfqueue: {
      get() { return this.$props.data.auto_redirect_nfqueue ?? 0 },
      set(v: number) {
        this.$props.data.auto_redirect_nfqueue = typeof v === 'number' && !isNaN(v) && v > 0 ? v : undefined
      }
    },
    routeAddressText: listText('route_address'),
    routeExcludeAddressText: listText('route_exclude_address'),
    includePackageText: listText('include_package'),
    excludePackageText: listText('exclude_package'),
    includeInterfaceText: listText('include_interface'),
    excludeInterfaceText: listText('exclude_interface'),
    includeUidRangeText: listText('include_uid_range'),
    excludeUidRangeText: listText('exclude_uid_range'),
    loopbackAddressText: listText('loopback_address'),
    includeUidText: numberListText('include_uid'),
    excludeUidText: numberListText('exclude_uid'),
    httpProxyEnabled: {
      get() { return this.$props.data.platform?.http_proxy != undefined },
      set(v:boolean) {
        if (v) {
          if (!this.$props.data.platform) this.$props.data.platform = {}
          this.$props.data.platform.http_proxy = { enabled: true, server: '127.0.0.1', server_port: 8080 }
        } else if (this.$props.data.platform) {
          delete this.$props.data.platform.http_proxy
          if (Object.keys(this.$props.data.platform).length == 0) delete this.$props.data.platform
        }
      }
    },
    httpProxyBypassText: nestedListText(['platform', 'http_proxy'], 'bypass_domain'),
    httpProxyMatchText: nestedListText(['platform', 'http_proxy'], 'match_domain')
  },
  methods: {
    applyLanDirect() {
      this.$props.data.route_exclude_address = ['10.0.0.0/8','172.16.0.0/12','192.168.0.0/16','fc00::/7','fe80::/10']
    }
  }
})

function splitText(v:string): string[] | undefined {
  const values = v.split('\n').map(item => item.trim()).filter(item => item.length > 0)
  return values.length > 0 ? values : undefined
}

function listText(field:string) {
  return {
    get(this:any): string { return this.$props.data[field]?.join('\n') ?? '' },
    set(this:any, v:string) {
      const values = splitText(v)
      values ? this.$props.data[field] = values : delete this.$props.data[field]
    }
  }
}

function numberListText(field:string) {
  return {
    get(this:any): string { return this.$props.data[field]?.join('\n') ?? '' },
    set(this:any, v:string) {
      const values = splitText(v)?.map(item => Number(item)).filter(item => !isNaN(item))
      values && values.length > 0 ? this.$props.data[field] = values : delete this.$props.data[field]
    }
  }
}

function nestedListText(path:string[], field:string) {
  return {
    get(this:any): string {
      let target = this.$props.data
      for (const key of path) target = target?.[key]
      return target?.[field]?.join('\n') ?? ''
    },
    set(this:any, v:string) {
      let target = this.$props.data
      for (const key of path) target = target?.[key]
      if (!target) return
      const values = splitText(v)
      values ? target[field] = values : delete target[field]
    }
  }
}

function markText(field:string) {
  return {
    get(this:any): string { return this.$props.data[field]?.toString() ?? '' },
    set(this:any, v:string) {
      const trimmed = (v ?? '').toString().trim()
      if (trimmed.length == 0 || trimmed == '0' || trimmed == '0x0') {
        delete this.$props.data[field]
      } else if (trimmed.startsWith('0x')) {
        this.$props.data[field] = trimmed
      } else {
        const parsed = Number(trimmed)
        if (Number.isFinite(parsed) && parsed > 0) this.$props.data[field] = parsed
        else delete this.$props.data[field]
      }
    }
  }
}
