<template>
  <v-card subtitle="Mieru">
    <v-alert v-if="!outbound" type="info" variant="tonal" density="compact" class="ma-2">{{ $t('nativeInbound.mieruUsers') }}</v-alert>
    <v-card-text>
      <v-row>
        <v-col cols="12" sm="6"><v-select v-model="data.transport" :items="['TCP', 'UDP']" :label="$t('nativeInbound.transport')" /></v-col>
        <v-col v-if="!outbound" cols="12" sm="6"><v-switch v-model="data.user_hint_is_mandatory" color="primary" :label="$t('nativeInbound.userHint')" /></v-col>
        <v-col cols="12"><v-combobox :model-value="ports" @update:model-value="setPorts" multiple chips clearable :label="$t(outbound ? 'nativeOutbound.serverPorts' : 'nativeInbound.listenPorts')" /></v-col>
        <template v-if="outbound">
          <v-col cols="12" sm="6"><v-select v-model="data.multiplexing" :items="multiplexing" clearable @click:clear="delete data.multiplexing" :label="$t('nativeOutbound.multiplexing')" /></v-col>
          <v-col cols="12" sm="6"><v-select v-model="data.handshake_mode" :items="['HANDSHAKE_DEFAULT', 'HANDSHAKE_STANDARD', 'HANDSHAKE_NO_WAIT']" clearable @click:clear="delete data.handshake_mode" :label="$t('nativeOutbound.handshakeMode')" /></v-col>
        </template>
        <v-col cols="12"><v-textarea v-model="data.traffic_pattern" rows="2" auto-grow :label="$t('nativeInbound.trafficPattern')" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model.number="data.mtu" type="number" min="0" clearable @click:clear="delete data.mtu" :label="$t('nativeInbound.mtu')" /></v-col>
      </v-row>
    </v-card-text>
  </v-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Mieru } from '@/types/inbounds'

const props = defineProps<{ data: Partial<Mieru> & { server_ports?: string | string[]; multiplexing?: string; handshake_mode?: string }; outbound?: boolean }>()
const multiplexing = ['MULTIPLEXING_DEFAULT', 'MULTIPLEXING_OFF', 'MULTIPLEXING_LOW', 'MULTIPLEXING_MIDDLE', 'MULTIPLEXING_HIGH']
const ports = computed(() => {
  const value = props.outbound ? props.data.server_ports : props.data.listen_ports
  return typeof value === 'string' ? [value] : value ?? []
})
function setPorts(value: string[]) {
  const field = props.outbound ? 'server_ports' : 'listen_ports'
  if (value?.length) props.data[field] = value
  else delete props.data[field]
}
</script>
