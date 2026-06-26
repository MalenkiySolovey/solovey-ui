<template>
  <entity-drawer
    :dirty="dirty"
    :loading="loading"
    :model-value="visible"
    :save-disabled="!validate"
    :saving="loading"
    :title="$t('actions.' + title) + ' ' + $t('objects.inbound')"
    :width="720"
    @close="closeModal"
    @save="saveChanges"
  >
    <form-section icon="lucide:zap" :title="$t('form.sections.basic')">
      <v-row>
        <v-col cols="12" sm="6">
          <v-select
            hide-details
            :items="Object.keys(inTypes).map((key,index) => ({title: key, value: Object.values(inTypes)[index]}))"
            :label="$t('type')"
            v-model="inbound.type"
            @update:modelValue="changeType">
          </v-select>
        </v-col>
        <v-col cols="12" sm="6">
          <v-text-field v-model="inbound.tag" :label="$t('objects.tag')" hide-details></v-text-field>
        </v-col>
      </v-row>
      <v-card
        v-if="[inTypes.HTTP, inTypes.Mixed].includes(inbound.type)"
        border
        density="compact"
        color="background"
        style="margin-top: 8px;">
        <v-card-text>
          <v-row>
            <v-col cols="12" sm="6">
              <v-switch
                v-model="setSystemProxy"
                color="primary"
                :label="$t('singbox.setSystemProxy')"
                hide-details>
              </v-switch>
            </v-col>
            <v-col cols="12" v-if="setSystemProxy">
              <v-alert type="warning" variant="tonal" density="compact">
                {{ $t('singbox.setSystemProxyWarning') }}
              </v-alert>
            </v-col>
          </v-row>
        </v-card-text>
      </v-card>
      <DomainResolver
        v-if="[inTypes.SOCKS, inTypes.HTTP, inTypes.Mixed].includes(inbound.type)"
        :data="inbound"
        field="domain_resolver"
        :label="$t('singbox.inboundDomainResolver')" />
    </form-section>

    <form-section icon="lucide:sliders-horizontal" :title="$t('form.sections.configuration')">
      <v-tabs
        v-if="HasInData.includes(inbound.type)"
        v-model="side"
        density="compact"
        fixed-tabs
        align-tabs="center"
      >
        <v-tab value="s">{{ $t('in.sSide') }}</v-tab>
        <v-tab value="c">{{ $t('in.cSide') }}</v-tab>
      </v-tabs>
      <v-window v-model="side" style="margin-top: 10px;">
        <v-window-item value="s">
          <Listen :data="inbound" :inTags="inTags" v-if="inbound.type != inTypes.Tun" />
          <Direct v-if="inbound.type == inTypes.Direct" :data="inbound" />
          <Shadowsocks v-if="inbound.type == inTypes.Shadowsocks" direction="in" :data="inbound" />
          <Hysteria v-if="inbound.type == inTypes.Hysteria" direction="in" :data="inbound" />
          <Hysteria2 v-if="inbound.type == inTypes.Hysteria2" direction="in" :data="inbound" />
          <Naive v-if="inbound.type == inTypes.Naive" direction="in" :data="inbound" />
          <Trojan v-if="inbound.type == inTypes.Trojan" direction="in" :data="inbound" />
          <ShadowTls v-if="inbound.type == inTypes.ShadowTLS" direction="in" :data="inbound" />
          <Tuic v-if="inbound.type == inTypes.TUIC" direction="in" :data="inbound" />
          <Tun v-if="inbound.type == inTypes.Tun" :data="inbound" />
          <AnyTls v-if="inbound.type == inTypes.AnyTls" :data="inbound" direction="in" />
          <TProxy v-if="inbound.type == inTypes.TProxy" :inbound="inbound" />
          <Transport v-if="Object.hasOwn(inbound,'transport')" :data="inbound" />
          <Users v-if="hasUser" :clients="clients" :data="initUsers" />
          <InTls v-if="HasTls.includes(inbound.type)"  :inbound="inbound" :tlsConfigs="tlsConfigs" :tls_id="inbound.tls_id" />
          <Multiplex v-if="MuxAvailable.includes(inbound.type)" direction="in" :data="inbound" />
        </v-window-item>
        <v-window-item value="c">
          <OutJsonVue :inData="inbound" :type="inbound.type" />
          <Multiplex v-if="Object.hasOwn(inbound,'multiplex')" direction="out" :data="inbound.out_json" />
          <Dial v-if="inbound.out_json" :dial="inbound.out_json" mode="client" />
          <v-card>
            <v-card-text>
              <v-card-subtitle>{{ $t('in.multiDomain') }}
                <v-chip color="primary" density="compact" variant="elevated" @click="add_addr"><v-icon icon="mdi-plus" /></v-chip>
              </v-card-subtitle>
              <template v-for="addr,index in inbound.addrs">
                {{ $t('in.addr') }} #{{ (index+1) }} <v-icon icon="mdi-delete" color="error" @click="inbound.addrs?.splice(index,1)" />
                <v-divider></v-divider>
                <AddrVue :addr="addr" :hasTls="HasTls.includes(inbound.type)" />
              </template>
            </v-card-text>
          </v-card>
        </v-window-item>
      </v-window>
    </form-section>
  </entity-drawer>
</template>

<script lang="ts" src="./InboundDrawer.logic"></script>
