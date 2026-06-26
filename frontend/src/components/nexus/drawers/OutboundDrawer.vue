<template>
  <entity-drawer
    :dirty="dirty"
    :loading="loading"
    :model-value="visible"
    :saving="loading"
    :title="$t('actions.' + title) + ' ' + $t('objects.outbound')"
    :width="720"
    @close="closeModal"
    @save="saveChanges"
  >
    <form-section icon="lucide:sliders-horizontal" :title="$t('form.sections.configuration')">
      <v-row>
        <v-col cols="12" sm="6">
          <v-select
            hide-details
            :items="Object.keys(outTypes).map((key,index) => ({title: key, value: Object.values(outTypes)[index]}))"
            :label="$t('type')"
            v-model="outbound.type"
            @update:modelValue="changeType">
          </v-select>
        </v-col>
        <v-col cols="12" sm="6">
          <v-text-field v-model="outbound.tag" :label="$t('objects.tag')" hide-details></v-text-field>
        </v-col>
      </v-row>
      <v-row v-if="!NoServer.includes(outbound.type)">
        <v-col cols="12" sm="6">
          <v-text-field :label="$t('out.addr')" hide-details v-model="outbound.server"></v-text-field>
        </v-col>
        <v-col cols="12" sm="6">
          <v-text-field :label="$t('out.port')" type="number" min="0" hide-details v-model.number="outbound.server_port"></v-text-field>
        </v-col>
      </v-row>
      <Socks v-if="outbound.type == outTypes.SOCKS" :data="outbound" />
      <Http v-if="outbound.type == outTypes.HTTP" :data="outbound" />
      <Shadowsocks v-if="outbound.type == outTypes.Shadowsocks" direction="out" :data="outbound" />
      <Vmess v-if="outbound.type == outTypes.VMess" :data="outbound" />
      <Trojan v-if="outbound.type == outTypes.Trojan" direction="out" :data="outbound" />
      <Hysteria v-if="outbound.type == outTypes.Hysteria" direction="out" :data="outbound" />
      <Naive v-if="outbound.type == outTypes.Naive" direction="out" :data="outbound" />
      <ShadowTls v-if="outbound.type == outTypes.ShadowTLS" :data="outbound" />
      <Vless v-if="outbound.type == outTypes.VLESS" :data="outbound" />
      <Tuic v-if="outbound.type == outTypes.TUIC" direction="out" :data="outbound" />
      <Hysteria2 v-if="outbound.type == outTypes.Hysteria2" direction="out" :data="outbound" />
      <AnyTls v-if="outbound.type == outTypes.AnyTls" :data="outbound" direction="out" />
      <Tor v-if="outbound.type == outTypes.Tor" :data="outbound" />
      <Ssh v-if="outbound.type == outTypes.SSH" :data="outbound" />
      <Selector v-if="outbound.type == outTypes.Selector" :data="outbound" :tags="tags" />
      <UrlTest v-if="outbound.type == outTypes.URLTest" :data="outbound" :tags="tags" />
      <Failover v-if="outbound.type == outTypes.Failover" :data="outbound" :tags="tags" />
      <Transport v-if="Object.hasOwn(outbound,'transport')" :data="outbound" />
      <OutTLS v-if="Object.hasOwn(outbound,'tls')" :outbound="outbound" />
      <Multiplex v-if="Object.hasOwn(outbound,'multiplex')" direction="out" :data="outbound" />
      <Dial v-if="!NoDial.includes(outbound.type)" :dial="outbound" />
    </form-section>

    <form-section icon="lucide:globe" :title="$t('client.external')" :default-open="false">
      <v-row>
        <v-col cols="12">
          <v-text-field v-model="link" :label="$t('client.external')" hide-details />
        </v-col>
        <v-col cols="12" align="center">
          <v-btn variant="tonal" :loading="loading" @click="linkConvert">{{ $t('submit') }}</v-btn>
        </v-col>
      </v-row>
    </form-section>
  </entity-drawer>
</template>

<script lang="ts" src="./OutboundDrawer.logic.ts"></script>
