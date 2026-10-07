<template>
  <form-shell
    :dirty="dirty"
    :loading="loading"
    :title="$t('actions.' + title) + ' ' + $t('objects.dnsrule')"
    @close="closeModal"
    @save="saveChanges"
  >
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-switch color="primary" v-model="logical" :label="$t('rule.logical')" hide-details></v-switch>
          </v-col>
          <v-spacer></v-spacer>
          <v-col cols="auto" v-if="logical" justify="center" align="center">
            <v-btn color="primary" @click="ruleData.rules.push(<dnsRule>{})" hide-details>{{ $t('actions.add') + " " + $t('objects.rule') }}</v-btn>
          </v-col>
        </v-row>
        <v-card style="background-color: inherit; margin-bottom: 5px;" v-for="(r, index) in ruleData.rules" :key="ruleObjectKey(r)" v-if="ruleData.type == 'logical'">
          <v-card-subtitle>{{ $t('objects.rule') + ' ' + (Number(index)+1) }}
            <v-icon @click="ruleData.rules.splice(index,1)" icon="mdi-delete" v-if="ruleData.rules.length>1" />
          </v-card-subtitle>
          <v-card-text style="padding: 0;">
            <RuleOptions
              :rule="r"
              :clients="clients"
              :inTags="inTags"
              :ruleSets="ruleSets" />
          </v-card-text>
        </v-card>
        <RuleOptions
          v-else
          :rule="ruleData.rules[0]"
          :clients="clients"
          :inTags="inTags"
          :ruleSets="ruleSets" />
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-select
              v-model="ruleData.action"
              :items="actions"
              :label="$t('dns.rule.action.title')"
              hide-details
            ></v-select>
          </v-col>
          <v-col cols="12" sm="6" md="4" v-if="logical">
            <v-select
              v-model="ruleData.mode"
              :items="['and', 'or']"
              :label="$t('rule.mode')"
              hide-details
            ></v-select>
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-switch color="primary" v-model="ruleData.invert" :label="$t('rule.invert')" hide-details></v-switch>
          </v-col>
        </v-row>
        <v-row>
          <v-col cols="12" sm="4"><v-switch v-model="ruleData.race" label="Race with following rules" hide-details /></v-col>
          <v-col v-if="ruleData.action === 'evaluate'" cols="12" sm="4"><v-text-field v-model="ruleData.tag" label="Evaluate response tag" clearable @click:clear="delete ruleData.tag" hide-details /></v-col>
          <v-col v-if="['route', 'evaluate'].includes(ruleData.action)" cols="12" sm="4"><v-switch v-model="ruleData.speculative" label="Speculative evaluation" hide-details /></v-col>
        </v-row>
        <v-card :subtitle="$t(ruleData.action == 'route' ? 'dns.rule.action.route' : 'dns.rule.action.routeOptions')" v-if="['route', 'evaluate', 'route-options'].includes(ruleData.action)">
          <v-row v-if="['route', 'evaluate'].includes(ruleData.action)">
            <v-col cols="12" sm="6" md="4">
              <v-select
                v-model="ruleData.server"
                :items="serverTags"
                :label="$t('dns.server')"
                hide-details
              ></v-select>
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-select
                v-model="ruleData.strategy"
                :items="strategies"
                :label="$t('rule.strategy')"
                clearable
                @click:clear="delete ruleData.strategy"
                hide-details>
              </v-select>
            </v-col>
          </v-row>
          <v-row>
            <v-col cols="12" sm="4"><v-text-field v-model="ruleData.timeout" label="Query timeout" placeholder="5s" clearable @click:clear="delete ruleData.timeout" hide-details /></v-col>
            <v-col cols="12" sm="4"><v-switch v-model="ruleData.disable_optimistic_cache" label="Disable optimistic cache" hide-details /></v-col>
            <v-col cols="12" sm="6" md="4">
              <v-switch v-model="ruleData.disable_cache" :label="$t('dns.disableCache')" hide-details></v-switch>
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-text-field v-model.number="ruleData.rewrite_ttl" type="number" min="0" :label="$t('dns.rule.action.rewriteTtl')" hide-details></v-text-field>
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-text-field v-model="ruleData.client_subnet" :label="$t('dns.rule.action.clientSubnet')" hide-details></v-text-field>
            </v-col>
          </v-row>
        </v-card>
        <v-card :subtitle="$t('dns.rule.action.reject')" v-if="ruleData.action == 'reject'">
          <v-row>
            <v-col cols="12" sm="6" md="4">
              <v-select
                v-model="ruleData.method"
                :items="[{ title: 'Default', value: 'default' },{ title: 'Drop', value: 'drop'}]"
                :label="$t('rule.method')"
                clearable
                @click:clear="delete ruleData.method"
                hide-details>
            </v-select>
            </v-col>
            <v-col cols="12" sm="6" md="4">
              <v-switch v-model="ruleData.no_drop" :label="$t('rule.noDrop')" hide-details></v-switch>
            </v-col>
          </v-row>
        </v-card>
        <v-card :subtitle="$t('dns.rule.action.predefined')" v-if="ruleData.action == 'predefined'">
          <v-row>
            <v-col cols="12" sm="6" md="4">
              <v-select
                v-model="ruleData.rcode"
                :items="predefinedRcode"
                :label="$t('dns.rule.action.rcode')"
                clearable
                @click:clear="delete ruleData.rcode"
                hide-details>
              </v-select>
            </v-col>
          </v-row>
          <v-row v-if="ruleData.rcode == 'NOERROR'">
            <v-col cols="12" sm="8">
              <v-text-field v-model="answer" :label="$t('dns.rule.action.answer') + ' ' + $t('commaSeparated')" hide-details></v-text-field>
            </v-col>
            <v-col cols="12" sm="8">
              <v-text-field v-model="ns" :label="$t('dns.rule.action.ns') + ' ' + $t('commaSeparated')" hide-details></v-text-field>
            </v-col>
            <v-col cols="12" sm="8">
              <v-text-field v-model="extra" :label="$t('dns.rule.action.extra') + ' ' + $t('commaSeparated')" hide-details></v-text-field>
            </v-col>
          </v-row>
        </v-card>
  </form-shell>
</template>

<script lang="ts">
import { logicalDnsRule, dnsRule, actionDnsRuleKeys } from '@/types/dns'
import RuleOptions from '@/components/dns/DNSRuleOptions.vue'
import { coreConfigContract, loadCoreConfigContract, serializeDNSRule } from '@/types/coreConfigContract'
import { i18n } from '@/locales'
import FormShell from '@/components/nexus/drawers/FormShell.vue'

const dnsRuleObjectKeys = new WeakMap<object, number>()
let dnsRuleObjectKeySeq = 0

export default {
  props: ['visible', 'data', 'index', 'clients', 'inTags', 'serverTags', 'ruleSets'],
  emits: ['close', 'save'],
  data() {
    return {
      title: 'add',
      loading: false,
      snapshot: '',
      ruleData: <any>{
        type: 'logical',
        mode: 'and',
        rules: <dnsRule[]>[{}],
        invert: false,
        action: 'route',
        server: 'local',
      },
      strategies: [
        { title: 'Prefer IPv4', value: 'prefer_ipv4' },
        { title: 'Prefer IPv6', value: 'prefer_ipv6' },
        { title: 'IPv4 Only', value: 'ipv4_only' },
        { title: 'IPv6 Only', value: 'ipv6_only' },
      ],
      predefinedRcode: [
        { title: i18n.global.t('dns.rule.action.rcodes.noError'), value: 'NOERROR' },
        { title: i18n.global.t('dns.rule.action.rcodes.formerr'), value: 'FORMERR' },
        { title: i18n.global.t('dns.rule.action.rcodes.servFail'), value: 'SERVFAIL' },
        { title: i18n.global.t('dns.rule.action.rcodes.nxDomain'), value: 'NXDOMAIN' },
        { title: i18n.global.t('dns.rule.action.rcodes.notImp'), value: 'NOTIMP' },
        { title: i18n.global.t('dns.rule.action.rcodes.refused'), value: 'REFUSED' },
      ],
    }
  },
  methods: {
    ruleObjectKey(r: any): number {
      if (r == null || typeof r !== 'object') return -1
      let key = dnsRuleObjectKeys.get(r)
      if (key === undefined) {
        key = ++dnsRuleObjectKeySeq
        dnsRuleObjectKeys.set(r, key)
      }
      return key
    },
    updateData() {
      if (this.$props.index != -1) {
        const newData = JSON.parse(this.$props.data)
        if (newData.type === 'logical') {
          this.ruleData = newData
        } else {
          this.ruleData = {
            type: 'simple',
            mode: 'and',
            rules: <dnsRule[]>[{}],
          }
          Object.keys(newData).forEach(key => {
            if (actionDnsRuleKeys.includes(key)) {
              this.ruleData[key] = newData[key]
            } else {
              this.ruleData.rules[0][key] = newData[key]
            }
          })
        }
        this.title = 'edit'
      }
      else {
        this.ruleData = <logicalDnsRule>{
            type: 'simple',
            mode: 'and',
            rules: <dnsRule[]>[{}],
            invert: false,
            action: 'route',
            server: this.$props.serverTags[0] ?? undefined,
          }
        this.title = 'add'
      }
      this.snapshot = JSON.stringify(this.ruleData)
    },
    closeModal() {
      this.$emit('close')
    },
    saveChanges() {
      this.loading = true
      const originalAction = this.snapshot ? (JSON.parse(this.snapshot).action ?? 'route') : undefined
      const newRule = serializeDNSRule(this.ruleData, originalAction, coreConfigContract.value)
      this.$emit('save', newRule)
      this.loading = false
    },
    deleteRule(index:number) {
      this.ruleData.rules.splice(index,1)
    }
  },
  computed: {
    actions() {
      const actions = Object.keys(coreConfigContract.value?.dnsActions ?? {})
      const current = this.ruleData.action ?? 'route'
      if (!actions.includes(current)) actions.push(current)
      return actions.map(value => ({ title: ['evaluate', 'respond'].includes(value) ? value : i18n.global.t('dns.rule.action.' + (value === 'route-options' ? 'routeOptions' : value)), value }))
    },
    dirty(): boolean {
      return this.snapshot !== '' && JSON.stringify(this.ruleData) !== this.snapshot
    },
    logical: {
      get() { return this.ruleData.type == 'logical' },
      set(v:boolean) {
        this.ruleData.type = v? 'logical' : 'simple'
      }
    },
    answer: {
      get() { return this.ruleData.answer?.length > 0 ? this.ruleData.answer.join(',') : "" },
      set(v:string) { this.ruleData.answer = v.length > 0 ? v.split(',') : undefined }
    },
    ns: {
      get() { return this.ruleData.ns?.length > 0 ? this.ruleData.ns.join(',') : "" },
      set(v:string) { this.ruleData.ns = v.length > 0 ? v.split(',') : undefined }
    },
    extra: {
      get() { return this.ruleData.extra?.length > 0 ? this.ruleData.extra.join(',') : "" },
      set(v:string) { this.ruleData.extra = v.length > 0 ? v.split(',') : undefined }
    },
  },
  watch: {
    visible(newValue) {
      if (newValue) {
        this.updateData()
      }
    },
  },
  mounted() { void loadCoreConfigContract() },
  components: { FormShell, RuleOptions }
}

</script>
