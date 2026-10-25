<template>
  <v-card subtitle="TrustTunnel">
    <v-card-text>
      <v-row>
        <v-col cols="12" sm="6"><v-select :model-value="network" @update:model-value="setNetwork" :items="['tcp', 'udp']" multiple chips :label="$t('network')" /></v-col>
        <v-col cols="12" sm="6"><v-select v-model="data.congestion_controller" :items="['bbr', 'cubic', 'reno']" clearable @click:clear="delete data.congestion_controller" :label="$t('nativeInbound.congestion')" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model.number="data.cwnd" type="number" min="0" clearable @click:clear="delete data.cwnd" label="CWND" :hint="$t('nativeInbound.cwndHint')" persistent-hint /></v-col>
        <v-col cols="12" sm="6"><v-switch v-model="data.disable_path_mtu_discovery" color="primary" :label="$t('nativeInbound.disablePathMtuDiscovery')" /></v-col>
      </v-row>
    </v-card-text>
  </v-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { TrustTunnel } from '@/types/inbounds'

const props = defineProps<{ data: TrustTunnel }>()
const network = computed(() => typeof props.data.network === 'string' ? [props.data.network] : props.data.network ?? [])
function setNetwork(value: string[]) {
  if (value?.length) props.data.network = value
  else delete props.data.network
}
</script>
