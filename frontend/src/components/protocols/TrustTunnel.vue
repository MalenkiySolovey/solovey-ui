<template>
  <v-card subtitle="TrustTunnel">
    <v-card-text>
      <v-row>
        <v-col cols="12" sm="6"><v-select :model-value="network" @update:model-value="setNetwork" :items="['tcp', 'udp']" multiple chips :label="$t('network')" /></v-col>
        <v-col cols="12" sm="6"><v-select v-model="data.congestion_controller" :items="['bbr', 'cubic', 'reno']" clearable @click:clear="delete data.congestion_controller" :label="$t('nativeInbound.congestion')" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model.number="data.cwnd" type="number" min="0" clearable @click:clear="delete data.cwnd" label="CWND" :hint="$t('nativeInbound.cwndHint')" persistent-hint /></v-col>
        <v-col cols="12" sm="6"><v-switch v-model="data.disable_path_mtu_discovery" color="primary" :label="$t('nativeInbound.disablePathMtuDiscovery')" /></v-col>
        <template v-if="outbound">
          <v-col cols="12" sm="6"><v-switch v-model="data.quic" color="primary" label="QUIC (HTTP/3)" /></v-col>
          <v-col cols="12" sm="6"><v-switch v-model="data.health_check" color="primary" :label="$t('nativeOutbound.healthCheck')" /></v-col>
          <v-col cols="12"><v-switch :model-value="data.multiplex?.enabled ?? false" @update:model-value="setMultiplexEnabled" color="primary" :label="$t('nativeOutbound.multiplexing')" /></v-col>
          <template v-if="data.multiplex?.enabled">
            <v-col cols="12" sm="4"><v-text-field v-model.number="data.multiplex.max_connections" type="number" min="0" clearable @click:clear="delete data.multiplex.max_connections" :label="$t('nativeOutbound.maxConnections')" /></v-col>
            <v-col cols="12" sm="4"><v-text-field v-model.number="data.multiplex.min_streams" type="number" min="0" clearable @click:clear="delete data.multiplex.min_streams" :label="$t('nativeOutbound.minStreams')" /></v-col>
            <v-col cols="12" sm="4"><v-text-field v-model.number="data.multiplex.max_streams" type="number" min="0" clearable @click:clear="delete data.multiplex.max_streams" :label="$t('nativeOutbound.maxStreams')" /></v-col>
          </template>
        </template>
      </v-row>
    </v-card-text>
  </v-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { TrustTunnel } from '@/types/inbounds'

const props = defineProps<{ data: Partial<TrustTunnel> & { quic?: boolean; health_check?: boolean; multiplex?: { enabled?: boolean; max_connections?: number; min_streams?: number; max_streams?: number } }; outbound?: boolean }>()
const network = computed(() => typeof props.data.network === 'string' ? [props.data.network] : props.data.network ?? [])
function setNetwork(value: string[]) {
  if (value?.length) props.data.network = value
  else delete props.data.network
}
function setMultiplexEnabled(value: boolean | null) {
  props.data.multiplex ??= {}
  props.data.multiplex.enabled = !!value
}
</script>
