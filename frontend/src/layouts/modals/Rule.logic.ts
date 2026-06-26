import { defineComponent } from 'vue'
import { logicalRule, rule, actionKeys } from '@/types/rules'
import RuleOptions from '@/components/rules/Rule.vue'
import FormShell from '@/components/nexus/drawers/FormShell.vue'
import { i18n } from '@/locales'

const ruleObjectKeys = new WeakMap<object, number>()
let ruleObjectKeySeq = 0

export default defineComponent({
  props: ['visible', 'data', 'index', 'clients', 'inTags', 'outTags', 'rsTags'],
  emits: ['close', 'save'],
  data() {
    return {
      title: 'add',
      loading: false,
      snapshot: '',
      ruleData: <any>{
        type: 'logical',
        mode: 'and',
        rules: <rule[]>[{}],
        invert: false,
        action: 'route',
        outbound: 'direct',
      },
      actions: [
        { title: i18n.global.t('rule.action.route'), value: 'route'},
        { title: i18n.global.t('rule.action.routeOption'), value: 'route-options'},
        { title: i18n.global.t('rule.action.bypass'), value: 'bypass'},
        { title: i18n.global.t('rule.action.reject'), value: 'reject'},
        { title: i18n.global.t('rule.action.hijackDns'), value: 'hijack-dns'},
        { title: i18n.global.t('rule.action.sniff'), value: 'sniff'},
        { title: i18n.global.t('rule.action.resolve'), value: 'resolve'}
      ],
      sniffers: [
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
      domainStrategies: [
        { title: 'Prefer IPv4', value: 'prefer_ipv4' },
        { title: 'Prefer IPv6', value: 'prefer_ipv6' },
        { title: 'IPv4 Only', value: 'ipv4_only' },
        { title: 'IPv6 Only', value: 'ipv6_only' },
      ],
      networkStrategies: [
        { title: 'Fallback', value: 'fallback' },
        { title: 'Hybrid', value: 'hybrid' },
      ],
    }
  },
  methods: {
    ruleObjectKey(r: any): number {
      if (r == null || typeof r !== 'object') return -1
      let key = ruleObjectKeys.get(r)
      if (key === undefined) {
        key = ++ruleObjectKeySeq
        ruleObjectKeys.set(r, key)
      }
      return key
    },
    updateData() {
      if (this.$props.index != -1) {
        const newData = JSON.parse(this.$props.data)
        if (newData.type) {
          this.ruleData = newData
        } else {
          this.ruleData = {
            type: 'simple',
            mode: 'and',
            rules: <rule[]>[{}],
          }
          Object.keys(newData).forEach(key => {
            if (actionKeys.includes(key)) {
              this.ruleData[key] = newData[key]
            } else {
              this.ruleData.rules[0][key] = newData[key]
            }
          })
        }
        this.title = 'edit'
      }
      else {
        this.ruleData = <logicalRule>{
            type: 'simple',
            mode: 'and',
            rules: <rule[]>[{}],
            invert: false,
            action: 'route',
            outbound: this.$props.outTags[0]?? 'direct',
          }
        this.title = 'add'
      }
      this.snapshot = JSON.stringify(this.ruleData)
    },
    closeModal() {
      this.updateData() // reset
      this.$emit('close')
    },
    saveChanges() {
      this.loading = true
      let newRule = <any>{
        action: this.ruleData.action,
        invert: this.ruleData.invert? this.ruleData.invert : undefined,
      }

      // Filter action data
      switch (newRule.action){
        case 'route':
          newRule.outbound = this.ruleData.outbound
          this.applyRouteOptions(newRule)
          break
        case 'bypass':
          newRule.outbound = this.ruleData.outbound?.length > 0 ? this.ruleData.outbound : undefined
          this.applyRouteOptions(newRule)
          break
        case 'route-options':
          this.applyRouteOptions(newRule)
          break
        case 'reject':
          newRule.method = this.ruleData.method?.length > 0 ? this.ruleData.method : undefined
          newRule.no_drop = this.ruleData.no_drop? true : undefined
          break
        case 'sniff':
          newRule.sniffer = this.ruleData.sniffer?.length > 0 ? this.ruleData.sniffer : undefined
          newRule.timeout = this.ruleData.timeout?.length > 0 ? this.ruleData.timeout : undefined
          break
        case 'resolve':
          newRule.strategy = this.ruleData.strategy?.length > 0 ? this.ruleData.strategy : undefined
          newRule.server = this.ruleData.server?.length > 0 ? this.ruleData.server : undefined
          break
      }

      // Add rules
      if (this.ruleData.type == 'simple'){
        newRule = { ...this.ruleData.rules[0], ...newRule }
      } else {
        newRule.type = 'logical'
        newRule.mode = this.ruleData.mode
        newRule.rules = this.ruleData.rules
      }
      this.$emit('save', newRule)
      this.loading = false
    },
    deleteRule(index:number) {
      this.ruleData.rules.splice(index,1)
    },
    applyRouteOptions(newRule:any) {
      newRule.override_address = this.ruleData.override_address?.length > 0 ? this.ruleData.override_address : undefined
      newRule.override_port = this.ruleData?.override_port > 0 ? this.ruleData.override_port : undefined
      newRule.network_strategy = this.ruleData.network_strategy?.length > 0 ? this.ruleData.network_strategy : undefined
      newRule.fallback_delay = this.ruleData.fallback_delay > 0 ? this.ruleData.fallback_delay : undefined
      newRule.udp_disable_domain_unmapping = this.ruleData.udp_disable_domain_unmapping? true : undefined
      newRule.udp_connect = this.ruleData.udp_connect? true : undefined
      newRule.udp_timeout = this.ruleData.udp_timeout?.length > 0 ? this.ruleData.udp_timeout : undefined
      newRule.tls_record_fragment = this.ruleData.tls_record_fragment ? true : undefined
      newRule.tls_fragment = this.ruleData.tls_fragment && !this.ruleData.tls_record_fragment ? true : undefined
      newRule.tls_fragment_fallback_delay = newRule.tls_fragment && this.ruleData.tls_fragment_fallback_delay?.length > 0 ? this.ruleData.tls_fragment_fallback_delay : undefined
    }
  },
  computed: {
    dirty(): boolean {
      return this.snapshot !== '' && JSON.stringify(this.ruleData) !== this.snapshot
    },
    logical: {
      get() { return this.ruleData.type == 'logical' },
      set(v:boolean) {
        this.ruleData.type = v? 'logical' : 'simple'
      }
    },
    tlsRecordFragment: {
      get() { return this.ruleData.tls_record_fragment ?? false },
      set(v:boolean) {
        this.ruleData.tls_record_fragment = v ? true : undefined
        if (v) {
          delete this.ruleData.tls_fragment
          delete this.ruleData.tls_fragment_fallback_delay
        }
      }
    },
    tlsFragment: {
      get() { return this.ruleData.tls_fragment ?? false },
      set(v:boolean) {
        this.ruleData.tls_fragment = v ? true : undefined
        if (v) delete this.ruleData.tls_record_fragment
        else delete this.ruleData.tls_fragment_fallback_delay
      }
    }
  },
  watch: {
    visible(newValue) {
      if (newValue) {
        this.updateData()
      }
    },
  },
  components: { FormShell, RuleOptions }
})
