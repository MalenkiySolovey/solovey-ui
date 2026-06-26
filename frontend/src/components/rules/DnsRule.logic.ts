import { defineComponent } from 'vue'
import ExpTextarea from '@/components/fields/ExpTextarea.vue'
import RuleInterfaceAddress from '@/components/rules/RuleInterfaceAddress.vue'
import RuleNetworkState from '@/components/rules/RuleNetworkState.vue'
import StrictSelect from '@/shared/ui/StrictSelect.vue'

const splitLineList = (value: string): string[] =>
  value.length > 0
    ? value.split('\n').map((item: string) => item.trim()).filter((item: string) => item.length > 0)
    : []

const splitNumberLineList = (value: string): number[] =>
  splitLineList(value)
    .map((item: string) => parseInt(item, 10))
    .filter((item: number) => Number.isFinite(item))

export default defineComponent({
  components: { ExpTextarea, RuleInterfaceAddress, RuleNetworkState, StrictSelect },
  props: ['rule', 'clients', 'inTags', 'rsTags', 'deleteable', 'ruleSets'],
  data() {
    return {
      menu: false,
      domainKeys: ['domain', 'domain_suffix', 'domain_keyword', 'domain_regex', 'ip_cidr', 'ip_is_private', 'ip_accept_any'],
      portKeys: ['port', 'port_range'],
      srcIPKeys: ['source_ip_cidr', 'source_ip_is_private'],
      srcPortKeys: ['source_port', 'source_port_range'],
      domainOption: 'domain',
      portOption: 'port',
      srcIPOption: 'source_ip_cidr',
      srcPortOption: 'source_port',
      queryTypes: ['A', 'AAAA', 'CNAME', 'MX', 'NS', 'PTR', 'HTTPS', 'SVCB', 'TXT'],
      expTextarea: {
        visible: false,
        title: '',
        content: '',
        key: '',
      },
    }
  },
  methods: {
    openExpTextarea(title: string, key: string) {
      this.expTextarea.title = title
      this.expTextarea.key = key
      this.expTextarea.content = this.$props.rule[key]?.join('\n') ?? ''
      this.expTextarea.visible = true
    },
    saveExpTextarea(data: string[]) {
      const key = this.expTextarea.key
      this.$props.rule[key] = ['port', 'source_port'].includes(key)
        ? data.map((item: string) => parseInt(item, 10)).filter((item: number) => Number.isFinite(item))
        : data
    },
    closeExpTextarea() {
      this.expTextarea.visible = false
    },
    updateDomainOption(option:string) {
      this.domainKeys.forEach(k => delete this.$props.rule[k])
      this.$props.rule[option] = ['ip_is_private', 'ip_accept_any'].includes(option) ? false : []
    },
    updatePortOption(option:string) {
      this.portKeys.forEach(k => delete this.$props.rule[k])
      this.$props.rule[option] = []
    },
    updateSrcIPOption(option:string) {
      this.srcIPKeys.forEach(k => delete this.$props.rule[k])
      this.$props.rule[option] = option == 'source_ip_is_private' ? false : []
    },
    updateSrcPortOption(option:string) {
      this.srcPortKeys.forEach(k => delete this.$props.rule[k])
      this.$props.rule[option] = []
    },
  },
  computed: {
    optionInbound: {
      get() { return this.$props.rule.inbound != undefined },
      set(v:boolean) { this.$props.rule.inbound = v ? [] : undefined }
    },
    optionClient: {
      get() { return this.$props.rule.auth_user != undefined },
      set(v:boolean) { this.$props.rule.auth_user = v ? [] : undefined }
    },
    optionIPver: {
      get() { return this.$props.rule.ip_version != undefined },
      set(v:boolean) { this.$props.rule.ip_version = v ? 4 : undefined }
    },
    optionQueryType: {
      get() { return this.$props.rule.query_type != undefined },
      set(v:boolean) { this.$props.rule.query_type = v ? [] : undefined }
    },
    optionNetwork: {
      get() { return this.$props.rule.network != undefined },
      set(v:boolean) { this.$props.rule.network = v ? [] : undefined }
    },
    optionProtocol: {
      get() { return this.$props.rule.protocol != undefined },
      set(v:boolean) { this.$props.rule.protocol = v ? ['http'] : undefined }
    },
    optionDomain: {
      get() { return Object.keys(this.$props.rule).some(r => this.domainKeys.includes(r)) },
      set(v:boolean) {
        if (v) {
          this.$props.rule.domain = []
        } else {
          this.domainKeys.forEach(k => delete this.$props.rule[k])
        }
        this.domainOption = 'domain'
      }
    },
    optionPort: {
      get() { return Object.keys(this.$props.rule).some(r => this.portKeys.includes(r)) },
      set(v:boolean) {
        if (v) {
          this.$props.rule.port = []
        } else {
          this.portKeys.forEach(k => delete this.$props.rule[k])
        }
        this.portOption = 'port'
      }
    },
    optionSrcIP: {
      get() { return Object.keys(this.$props.rule).some(r => this.srcIPKeys.includes(r)) },
      set(v:boolean) {
        if (v) {
          this.$props.rule.source_ip_cidr = []
        } else {
          this.srcIPKeys.forEach(k => delete this.$props.rule[k])
        }
        this.srcIPOption = 'source_ip_cidr'
      }
    },
    optionSrcPort: {
      get() { return Object.keys(this.$props.rule).some(r => this.srcPortKeys.includes(r)) },
      set(v:boolean) {
        if (v) {
          this.$props.rule.source_port = []
        } else {
          this.srcPortKeys.forEach(k => delete this.$props.rule[k])
        }
        this.srcPortOption = 'source_port'
      }
    },
    optionRuleSet: {
      get() { return this.$props.rule.rule_set != undefined },
      set(v:boolean) {
        if (v) {
          this.$props.rule.rule_set = []
          this.$props.rule.rule_set_ip_cidr_match_source = false
          this.$props.rule.rule_set_ip_cidr_accept_empty = false
        } else {
          delete this.$props.rule.rule_set
          delete this.$props.rule.rule_set_ip_cidr_match_source
          delete this.$props.rule.rule_set_ip_cidr_accept_empty
        }
      }
    },
    optionNetworkState: {
      get() {
        return this.$props.rule.network_type != undefined ||
               this.$props.rule.network_is_expensive != undefined ||
               this.$props.rule.network_is_constrained != undefined ||
               this.$props.rule.wifi_ssid != undefined ||
               this.$props.rule.wifi_bssid != undefined
      },
      set(v:boolean) {
        if (v) {
          this.$props.rule.network_type = []
          this.$props.rule.network_is_expensive = false
          this.$props.rule.network_is_constrained = false
          this.$props.rule.wifi_ssid = []
          this.$props.rule.wifi_bssid = []
        } else {
          delete this.$props.rule.network_type
          delete this.$props.rule.network_is_expensive
          delete this.$props.rule.network_is_constrained
          delete this.$props.rule.wifi_ssid
          delete this.$props.rule.wifi_bssid
        }
      }
    },
    optionInterface: {
      get() { return ['interface_address', 'network_interface_address', 'default_interface_address'].some(k => this.$props.rule[k] != undefined) },
      set(v:boolean) {
        if (v) {
          this.$props.rule.interface_address = {}
        } else {
          ;['interface_address', 'network_interface_address', 'default_interface_address'].forEach(k => delete this.$props.rule[k])
        }
      }
    },
    domain: {
      get() { return this.$props.rule.domain?.join('\n') ?? '' },
      set(v:string) { this.$props.rule.domain = splitLineList(v) }
    },
    domain_suffix: {
      get() { return this.$props.rule.domain_suffix?.join('\n') ?? '' },
      set(v:string) { this.$props.rule.domain_suffix = splitLineList(v) }
    },
    domain_keyword: {
      get() { return this.$props.rule.domain_keyword?.join('\n') ?? '' },
      set(v:string) { this.$props.rule.domain_keyword = splitLineList(v) }
    },
    domain_regex: {
      get() { return this.$props.rule.domain_regex?.join('\n') ?? '' },
      set(v:string) { this.$props.rule.domain_regex = splitLineList(v) }
    },
    ip_cidr: {
      get() { return this.$props.rule.ip_cidr?.join('\n') ?? '' },
      set(v:string) { this.$props.rule.ip_cidr = splitLineList(v) }
    },
    port: {
      get() { return this.$props.rule.port?.join('\n') ?? '' },
      set(v:string) { this.$props.rule.port = splitNumberLineList(v) }
    },
    port_range: {
      get() { return this.$props.rule.port_range?.join('\n') ?? '' },
      set(v:string) { this.$props.rule.port_range = splitLineList(v) }
    },
    source_ip_cidr: {
      get() { return this.$props.rule.source_ip_cidr?.join('\n') ?? '' },
      set(v:string) { this.$props.rule.source_ip_cidr = splitLineList(v) }
    },
    source_port: {
      get() { return this.$props.rule.source_port?.join('\n') ?? '' },
      set(v:string) { this.$props.rule.source_port = splitNumberLineList(v) }
    },
    source_port_range: {
      get() { return this.$props.rule.source_port_range?.join('\n') ?? '' },
      set(v:string) { this.$props.rule.source_port_range = splitLineList(v) }
    },
  },
  mounted() {
    const ruleKeys = Object.keys(this.$props.rule)
    if (this.optionDomain) {
      const enabledOption = this.domainKeys.filter(k => ruleKeys.includes(k))
      this.domainOption = enabledOption.length>0 ? enabledOption[0] : 'domain'
    }
    if (this.optionPort) {
      const enabledOption = this.portKeys.filter(k => ruleKeys.includes(k))
      this.portOption = enabledOption.length>0 ? enabledOption[0] : 'port'
    }
    if (this.optionSrcIP) {
      const enabledOption = this.srcIPKeys.filter(k => ruleKeys.includes(k))
      this.srcIPOption = enabledOption.length>0 ? enabledOption[0] : 'source_ip_cidr'
    }
    if (this.optionSrcPort) {
      const enabledOption = this.srcPortKeys.filter(k => ruleKeys.includes(k))
      this.srcPortOption = enabledOption.length>0 ? enabledOption[0] : 'source_port'
    }
  }
})
