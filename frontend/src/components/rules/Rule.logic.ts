import { defineComponent } from 'vue'
import ExpTextarea from '@/components/fields/ExpTextarea.vue'
import RuleInterfaceAddress from '@/components/rules/RuleInterfaceAddress.vue'
import RuleNetworkState from '@/components/rules/RuleNetworkState.vue'
import StrictSelect from '@/shared/ui/StrictSelect.vue'
export default defineComponent({
  components: { ExpTextarea, RuleInterfaceAddress, RuleNetworkState, StrictSelect },
  props: ['rule', 'clients', 'inTags', 'outTags', 'rsTags', 'deleteable'],
  data() {
    return {
      menu: false,
      domainKeys: ['domain', 'domain_suffix', 'domain_keyword', 'domain_regex', 'ip_cidr', 'ip_is_private'],
      portKeys: ['port', 'port_range'],
      srcIPKeys: ['source_ip_cidr', 'source_ip_is_private'],
      srcPortKeys: ['source_port', 'source_port_range'],
      domainOption: 'domain',
      portOption: 'port',
      srcIPOption: 'source_ip_cidr',
      srcPortOption: 'source_port',
      protocols: [
        { title: 'HTTP', value: 'http' },
        { title: 'TLS', value: 'tls' },
        { title: 'QUIC', value: 'quic' },
        { title: 'STUN', value: 'stun' },
        { title: 'DNS', value: 'dns' },
        { title: 'BitTorrent', value: 'bittorrent' },
        { title: 'DTLS', value: 'dtls' },
        { title: 'SSH', value: 'ssh' },
        { title: 'RDP', value: 'rdp' },
        { title: 'NTP', value: 'ntp' },
      ],
      sniffClients: ['chromium', 'safari', 'firefox', 'quic-go', 'unknown'],
      expTextarea: {
        visible: false,
        title: '',
        content: '',
        object: '',
      }
    }
  },
  methods: {
    updateDomainOption(option:string) {
      this.domainKeys.forEach(k => delete this.$props.rule[k])
      this.$props.rule[option] = option == 'ip_is_private' ? false : []
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
    openExpTextarea(title:string, object:string) {
      this.expTextarea.visible = !this.expTextarea.visible
      this.expTextarea.title = title
      this.expTextarea.content = this.$props.rule[object]?.join('\n') ?? ''
      this.expTextarea.object = object
    },
    saveExpTextarea(results:string[]) {
      this.$props.rule[this.expTextarea.object] = results
      this.closeExpTextarea()
    },
    closeExpTextarea() {
      this.expTextarea.visible = false
      this.expTextarea.title = ''
      this.expTextarea.object = ''
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
    optionProtocol: {
      get() { return this.$props.rule.protocol != undefined },
      set(v:boolean) { this.$props.rule.protocol = v ? ['http'] : undefined }
    },
    optionSniffClient: {
      get() { return this.$props.rule.client != undefined },
      set(v:boolean) { this.$props.rule.client = v ? [] : undefined }
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
    optionPreferredBy: {
      get() { return this.$props.rule.preferred_by != undefined },
      set(v:boolean) { this.$props.rule.preferred_by = v ? [] : undefined }
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
    optionRuleSet: {
      get() { return this.$props.rule.rule_set != undefined },
      set(v:boolean) {
        if (v) {
          this.$props.rule.rule_set = []
          this.$props.rule.rule_set_ip_cidr_match_source = false
        } else {
          delete this.$props.rule.rule_set
          delete this.$props.rule.rule_set_ip_cidr_match_source
        }
      }
    },
    optionNetwork: {
      get() { return this.$props.rule.network != undefined },
      set(v:boolean) { this.$props.rule.network = v ? [] : undefined }
    },
    domain: {
      get() { return this.$props.rule.domain?.join('\n') ?? '' },
      set(v:string) { this.$props.rule.domain = v.length > 0 ? v.split('\n').map((s:string) => s.trim()).filter((s:string) => s.length > 0) : [] }
    },
    domain_suffix: {
      get() { return this.$props.rule.domain_suffix?.join('\n') ?? '' },
      set(v:string) { this.$props.rule.domain_suffix = v.length > 0 ? v.split('\n').map((s:string) => s.trim()).filter((s:string) => s.length > 0) : [] }
    },
    domain_keyword: {
      get() { return this.$props.rule.domain_keyword?.join('\n') ?? '' },
      set(v:string) { this.$props.rule.domain_keyword = v.length > 0 ? v.split('\n').map((s:string) => s.trim()).filter((s:string) => s.length > 0) : [] }
    },
    domain_regex: {
      get() { return this.$props.rule.domain_regex?.join('\n') ?? '' },
      set(v:string) { this.$props.rule.domain_regex = v.length > 0 ? v.split('\n').map((s:string) => s.trim()).filter((s:string) => s.length > 0) : [] }
    },
    ip_cidr: {
      get() { return this.$props.rule.ip_cidr?.join('\n') ?? '' },
      set(v:string) { this.$props.rule.ip_cidr = v.length > 0 ? v.split('\n').map((s:string) => s.trim()).filter((s:string) => s.length > 0) : [] }
    },
    port: {
      get() { return this.$props.rule.port?.join('\n') ?? '' },
      set(v:string) {
        const lines = v.split('\n').map((s:string) => s.trim()).filter((s:string) => s.length > 0)
        if (!v.endsWith('\n')) {
          this.$props.rule.port = lines.length > 0 ? lines.map((str:string) => parseInt(str, 10)).filter((n:number) => !isNaN(n)) : []
        }
      }
    },
    port_range: {
      get() { return this.$props.rule.port_range?.join('\n') ?? '' },
      set(v:string) { this.$props.rule.port_range = v.length > 0 ? v.split('\n').map((s:string) => s.trim()).filter((s:string) => s.length > 0) : [] }
    },
    source_ip_cidr: {
      get() { return this.$props.rule.source_ip_cidr?.join('\n') ?? '' },
      set(v:string) { this.$props.rule.source_ip_cidr = v.length > 0 ? v.split('\n').map((s:string) => s.trim()).filter((s:string) => s.length > 0) : [] }
    },
    source_port: {
      get() { return this.$props.rule.source_port?.join('\n') ?? '' },
      set(v:string) {
        const lines = v.split('\n').map((s:string) => s.trim()).filter((s:string) => s.length > 0)
        if (!v.endsWith('\n')) {
          this.$props.rule.source_port = lines.length > 0 ? lines.map((str:string) => parseInt(str, 10)).filter((n:number) => !isNaN(n)) : []
        }
      }
    },
    source_port_range: {
      get() { return this.$props.rule.source_port_range?.join('\n') ?? '' },
      set(v:string) { this.$props.rule.source_port_range = v.length > 0 ? v.split('\n').map((s:string) => s.trim()).filter((s:string) => s.length > 0) : [] }
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
