<template>
  <form-shell
    :dirty="dirty"
    :save-disabled="!selectedCapability?.available || missingService"
    :title="$t('actions.' + title) + ' ' + $t('objects.dnsserver')"
    @close="close"
    @save="save"
  >
        <v-alert v-if="!selectedCapability?.available" type="warning" variant="tonal" class="mb-3">
          {{ $t('dns.capabilityUnavailable') }} {{ selectedCapability?.reason ?? $t('capability.waiting') }}
        </v-alert>
        <v-alert v-if="missingService" type="error" variant="tonal" class="mb-3">{{ $t('dns.resolvedMissingService') }}</v-alert>
        <v-row>
          <v-col cols="12" sm="6" md="4">
            <v-select
              v-model="dnsServer.type"
              :items="dnsTypes"
              :label="$t('type')"
              @update:modelValue="changeType"
              hide-details
            />
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="dnsServer.tag" :label="$t('objects.tag')" hide-details />
          </v-col>
        </v-row>
        <v-row v-if="HasServer.includes(dnsServer.type)">
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="dnsServer.server" :label="$t('in.addr')" hide-details />
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model.number="dnsServer.server_port" type="number" min="0" :label="$t('in.port')" hide-details />
          </v-col>
        </v-row>
        <v-row v-if="HasHeaders.includes(dnsServer.type)">
          <v-col cols="12" sm="8">
            <v-text-field v-model="dnsServer.path" :label="$t('transport.path')" hide-details />
          </v-col>
        </v-row>
        <DialVue :dial="dnsServer" v-if="!WithoutDial.includes(dnsServer.type)" />
        <oTlsVue :outbound="dnsServer" v-if="HasTls.includes(dnsServer.type)" />
        <Headers :data="dnsServer" v-if="HasHeaders.includes(dnsServer.type)" />
        <template v-if="dnsServer.type == 'hosts'">
          <v-row>
            <v-col cols="12" sm="6">
              <v-text-field v-model="hostsPath" :label="$t('transport.path') + $t('commaSeparated')" hide-details />
            </v-col>
          </v-row>
          <v-card>
            <v-card-subtitle>{{ $t('dns.rule.action.predefined') }}
              <v-chip color="primary" density="compact" variant="elevated" @click="addHostsPredefined"><v-icon icon="mdi-plus" /></v-chip>
            </v-card-subtitle>
            <v-row v-for="(pd, index) in hostsPredefined">
              <v-col cols="12" sm="6" md="4">
                <v-text-field v-model="pd.name" :label="$t('setting.domain')" @input="update_pds_key(index,$event.target.value)" hide-details></v-text-field>
              </v-col>
              <v-col cols="12" sm="6">
                <v-text-field
                  v-model="pd.value"
                  :label="$t('types.tun.addr') + $t('commaSeparated')"
                  @input="update_pds_value(index,$event.target.value)"
                  hide-details>
                  <template v-slot:append>
                    <v-icon @click="delHostsPredefined(index)" color="error" icon="mdi-delete" />
                  </template>
                </v-text-field>
              </v-col>
            </v-row>
          </v-card>
        </template>
        <v-row v-if="dnsServer.type == 'local'">
          <v-col cols="12"><v-alert type="info" variant="tonal">Local DNS follows system configuration and may use multicast for local and link-local names. Choose an explicit unicast server when that is required by your policy.</v-alert></v-col>
          <v-col cols="12" sm="8"><v-text-field :model-value="neighborDomains" @update:model-value="neighborDomains = $event" label="Preferred neighbor domains" hint="Comma separated" persistent-hint /></v-col>
          <v-col cols="12" sm="6" md="4">
            <v-switch v-model="dnsServer.prefer_go" color="primary" :label="$t('dns.local.preferGo')" hide-details></v-switch>
          </v-col>
        </v-row>
        <v-row v-if="dnsServer.type == 'dhcp'">
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="dnsServer.interface" :label="$t('types.tun.ifName')" hide-details />
          </v-col>
        </v-row>
        <v-row v-if="dnsServer.type == 'mdns'">
          <v-col cols="12"><v-alert type="info" variant="tonal">{{ $t('dns.mdnsSemantics') }}</v-alert></v-col>
          <v-col cols="12" sm="8">
            <v-text-field v-model="multicastInterfaces" :label="$t('dns.mdnsInterfaces')" :hint="$t('dns.mdnsInterfaceHint')" persistent-hint />
            <div class="text-caption">{{ $t('dns.mdnsObserved') }} {{ observedInterfaces.join(', ') || '—' }}</div>
          </v-col>
        </v-row>
        <v-alert v-if="dnsServer.type == 'resolved'" type="info" variant="tonal" class="my-3">{{ $t('dns.resolvedSemantics') }}</v-alert>
        <v-row v-if="dnsServer.type == 'fakeip'">
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="dnsServer.inet4_range" :label="$t('dns.rule.inet4Range')" hide-details />
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-text-field v-model="dnsServer.inet6_range" :label="$t('dns.rule.inet6Range')" hide-details />
          </v-col>
        </v-row>
        <v-row v-if="dnsServer.type == 'tailscale' || dnsServer.type == 'resolved'">
          <v-col cols="12" sm="6" md="4" v-if="dnsServer.type == 'tailscale'">
            <v-select v-model="dnsServer.endpoint" :label="$t('objects.endpoint')" :items="tsTags" hide-details />
          </v-col>
          <v-col cols="12" sm="6" md="4" v-if="dnsServer.type == 'resolved'">
            <v-select v-model="dnsServer.service" :label="$t('objects.service')" :items="rslvdTags" hide-details />
          </v-col>
          <v-col cols="12" sm="6" md="4">
            <v-switch v-model="dnsServer.accept_default_resolvers" :label="$t('dns.rule.acceptDefault')" hide-details></v-switch>
          </v-col>
        </v-row>
  </form-shell>
</template>

<script lang="ts">
import DialVue from '@/components/fields/Dial.vue'
import oTlsVue from '@/components/tls/OutTLS.vue'
import Headers from '@/components/fields/Headers.vue'
import RandomUtil from '@/plugins/randomUtil'
import { DnsTypes, createDnsServer } from '@/types/dns'
import FormShell from '@/components/nexus/drawers/FormShell.vue'
import Data from '@/store/modules/data'
import { runtimeCapability } from '@/types/runtimeCapabilities'
export default {
  props: ['visible', 'data', 'index', 'tsTags', 'rslvdTags'],
  emits: ['close', 'save'],
  data() {
    return {
      title: "add",
      snapshot: '',
      dnsServer: createDnsServer("local",{tag: "dns-" + RandomUtil.randomSeq(3)}),
      HasServer: [DnsTypes.TCP, DnsTypes.UDP, DnsTypes.TLS, DnsTypes.QUIC, DnsTypes.HTTPS, DnsTypes.HTTP3],
      HasHeaders: [DnsTypes.HTTPS, DnsTypes.HTTP3],
      HasTls: [DnsTypes.TLS, DnsTypes.QUIC, DnsTypes.HTTPS, DnsTypes.HTTP3],
      WithoutDial: [DnsTypes.Hosts, DnsTypes.MDNS, DnsTypes.Tailscale, DnsTypes.FakeIP, DnsTypes.Resolved],
    }
  },
  methods: {
    updateData() {
      if (this.$props.index != -1) {
        this.dnsServer = JSON.parse(this.$props.data)
        this.title = 'edit'
      }
      else {
        this.dnsServer = createDnsServer("local",{tag: "dns-" + RandomUtil.randomSeq(3)})
        this.title = 'add'
      }
      this.snapshot = JSON.stringify(this.dnsServer)
    },
    changeType(dnsType: string) {
      this.dnsServer = createDnsServer(dnsType,{tag: this.dnsServer.tag})
    },
    close() {
      this.$emit('close')
    },
    save() {
      if (!this.selectedCapability?.available || this.missingService) return
      this.$emit('save', this.dnsServer)
    },
    addHostsPredefined() {
      const newPredefined = { name:'localhost', value: '127.0.0.1,::1' }
      this.hostsPredefined = [...this.hostsPredefined, newPredefined]
    },
    delHostsPredefined(i:number) {
      let pds = this.hostsPredefined
      pds.splice(i,1)
      this.hostsPredefined = pds
    },
    update_pds_key(i:number,k:string) {
      let pds = this.hostsPredefined
      pds[i].name = k
      this.hostsPredefined = pds
    },
    update_pds_value(i:number,v:string) {
      let pds = this.hostsPredefined
      pds[i].value = v
      this.hostsPredefined = pds
    },
  },
  computed:{
    selectedCapability() { return runtimeCapability(Data().capabilities, 'dns', this.dnsServer.type) },
    missingService(): boolean { return this.dnsServer.type === 'resolved' && (!this.dnsServer.service || !this.rslvdTags?.includes(this.dnsServer.service)) },
    observedInterfaces(): string[] { return Data().capabilities?.multicastInterfaces ?? [] },
    multicastInterfaces: {
      get(): string { const value = this.dnsServer.interface; return Array.isArray(value) ? value.join(',') : value ?? '' },
      set(value: string) { if (value === '') delete this.dnsServer.interface; else this.dnsServer.interface = value.split(',').map(s => s.trim()) },
    },
    dnsTypes() {
      return Data().capabilities?.facts.filter(f => f.category === 'dns').map(f => ({ title: f.type + (f.available ? '' : ' (' + f.reason + ')'), value: f.type, props: { disabled: !f.available } })) ?? []
    },
    neighborDomains: {
      get() { const value = this.dnsServer.neighbor_domain; return Array.isArray(value) ? value.join(',') : value ?? '' },
      set(value: string) { if (value === '') delete this.dnsServer.neighbor_domain; else this.dnsServer.neighbor_domain = value.split(',').map(s => s.trim()) },
    },
    dirty(): boolean {
      return this.snapshot !== '' && JSON.stringify(this.dnsServer) !== this.snapshot
    },
    hostsPath: {
      get() { return this.dnsServer.path },
      set(v: string) {
        this.dnsServer.path = v.length > 0 ? v.split(',').map((item: string) => item.trim()) : undefined
      }
    },
    hostsPredefined: {
      get() :any[] {
        let pds :any[] = []
        const h = this.dnsServer.predefined
        if (h) {
          Object.keys(h).forEach(key => {
            if (Array.isArray(h[key])){
              pds.push({ name: key, value: h[key].join(',') })
            } else {
              pds.push({ name: key, value: h[key] })
            }
          })
        }
        return pds
       },
      set(v: any[]) {
        if (v.length>0) {
          let pds:any = {}
          v.forEach((pd:any) => {
            pds[pd.name] = pd.value.split(',').map((item: string) => item.trim())
          })
          this.dnsServer.predefined = pds
        } else {
          this.dnsServer.predefined = undefined
        }
      }
    },
  },
  watch: {
    visible(v) {
      if (v) {
        this.updateData()
      }
    },
  },
  components: { FormShell, DialVue, oTlsVue, Headers }
}
</script>
