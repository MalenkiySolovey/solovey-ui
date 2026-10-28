<template>
  <v-card>
    <v-card-text>
      <p class="mb-4">{{ $t('nativeMasque.profileHint') }}</p>
      <v-row>
        <v-col cols="12" sm="6"><v-text-field v-model="data.profile.id" :label="$t('nativeMasque.deviceId')" autocomplete="off" /></v-col>
        <v-col cols="12" sm="6"><SecretCredentialField v-model="data.profile.auth_token" :available="available" :label="$t('nativeMasque.authToken')" /></v-col>
        <v-col cols="12" sm="6"><SecretCredentialField v-model="data.profile.license_key" :available="available" :label="$t('nativeMasque.licenseKey')" /></v-col>
        <v-col cols="12" sm="6"><v-switch v-model="data.profile.recreate" :label="$t('nativeMasque.recreate')" color="primary" /></v-col>
      </v-row>
      <p class="mb-4">{{ $t('nativeMasque.transportHint') }}</p>
      <v-row>
        <v-col cols="12" sm="4"><v-switch v-model="data.use_l4_proxy" :label="$t('nativeMasque.l4Proxy')" color="primary" /></v-col>
        <v-col cols="12" sm="4"><v-switch v-model="data.use_http2" :disabled="data.use_l4_proxy" :label="$t('nativeMasque.http2')" color="primary" /></v-col>
        <v-col cols="12" sm="4"><v-switch v-model="data.use_ipv6" :label="$t('nativeMasque.ipv6')" color="primary" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model="data.address" :label="$t('nativeMasque.address')" placeholder="162.159.197.2" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model="port" type="number" min="1" max="65535" :label="$t('nativeMasque.port')" placeholder="443" /></v-col>
        <v-col cols="12" sm="6"><v-switch v-model="data.system" :disabled="data.use_l4_proxy" :label="$t('nativeMasque.system')" color="primary" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model="data.name" :disabled="data.use_l4_proxy || !data.system" :label="$t('nativeMasque.name')" /></v-col>
        <v-col cols="12"><v-combobox v-model="allowedIPs" :disabled="data.use_l4_proxy" multiple chips closable-chips :label="$t('nativeMasque.allowedIPs')" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model="data.udp_timeout" :disabled="data.use_l4_proxy" :label="$t('nativeMasque.udpTimeout')" placeholder="5m" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model="data.udp_keepalive_period" :disabled="!data.use_l4_proxy && data.use_http2" :label="$t('nativeMasque.keepalive')" placeholder="30s" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model="packetSize" :disabled="!data.use_l4_proxy && data.use_http2" type="number" min="1200" max="65535" :label="$t('nativeMasque.initialPacketSize')" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model="data.handshake_timeout" :label="$t('nativeMasque.handshakeTimeout')" placeholder="10s" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model="data.reconnect_delay" :label="$t('nativeMasque.reconnectDelay')" placeholder="1s" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model="data.congestion_controller" :label="$t('nativeMasque.congestion')" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model="cwnd" type="number" min="0" :label="$t('nativeMasque.cwnd')" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model="tls.server_name" :label="$t('nativeMasque.tlsServerName')" /></v-col>
        <v-col cols="12" sm="6"><v-switch v-model="tls.insecure" :label="$t('tls.insecure')" color="primary" /></v-col>
      </v-row>
      <v-expansion-panels variant="accordion" class="mt-4">
        <v-expansion-panel :title="$t('nativeMasque.tlsOptions')">
          <v-expansion-panel-text>
            <v-row>
              <v-col cols="12"><v-combobox v-model="cipherSuites" multiple chips closable-chips :label="$t('tls.cs')" /></v-col>
              <v-col cols="12"><v-combobox v-model="curves" multiple chips closable-chips :label="$t('tls.curves')" /></v-col>
              <v-col cols="12" sm="6"><v-switch v-model="tls.fragment" :label="$t('tls.fragment')" color="primary" /></v-col>
              <v-col cols="12" sm="6"><v-text-field v-model="tls.fragment_fallback_delay" :disabled="!tls.fragment" :label="$t('tls.fragmentDelay')" placeholder="100ms" /></v-col>
              <v-col cols="12" sm="6"><v-switch v-model="tls.record_fragment" :label="$t('tls.recordFragment')" color="primary" /></v-col>
            </v-row>
          </v-expansion-panel-text>
        </v-expansion-panel>
      </v-expansion-panels>
    </v-card-text>
  </v-card>
</template>

<script setup lang="ts">
import { computed, watchEffect } from 'vue'
import type { MASQUE } from '@/types/outbounds'
import Data from '@/store/modules/data'
import { runtimeCapability } from '@/types/runtimeCapabilities'
import SecretCredentialField from '@/components/fields/SecretCredentialField.vue'

const props = defineProps<{ data: MASQUE }>()
watchEffect(() => { props.data.profile ??= {}; props.data.tls ??= {} })
const tls = computed(() => props.data.tls!)
function tlsList(field: 'cipher_suites' | 'curve_preferences') {
  return computed({ get: () => {
    const value: string | string[] | undefined = props.data.tls?.[field]
    return typeof value === 'string' ? [value] : value ?? []
  }, set: (value: string[]) => { if (value.length) props.data.tls![field] = value; else delete props.data.tls![field] } })
}
const cipherSuites = tlsList('cipher_suites'), curves = tlsList('curve_preferences')
const allowedIPs = computed({ get: () => typeof props.data.allowed_ips === 'string' ? [props.data.allowed_ips] : props.data.allowed_ips ?? [], set: value => { props.data.allowed_ips = value } })
function optionalNumber(field: 'port' | 'udp_initial_packet_size' | 'cwnd') {
  return computed({ get: () => props.data[field] ?? null, set: (value: string | number | null) => {
    if (value === '' || value === null) delete props.data[field]
    else props.data[field] = Number(value)
  } })
}
const port = optionalNumber('port'), packetSize = optionalNumber('udp_initial_packet_size'), cwnd = optionalNumber('cwnd')
const available = computed(() => runtimeCapability(Data().capabilities, 'outbounds', 'masque')?.available === true)
</script>
