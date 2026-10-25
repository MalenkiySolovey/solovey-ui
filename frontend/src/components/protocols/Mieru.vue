<template>
  <v-card subtitle="Mieru">
    <v-alert type="info" variant="tonal" density="compact" class="ma-2">{{ $t('nativeInbound.mieruUsers') }}</v-alert>
    <v-card-text>
      <v-row>
        <v-col cols="12" sm="6"><v-select v-model="data.transport" :items="['TCP', 'UDP']" :label="$t('nativeInbound.transport')" /></v-col>
        <v-col cols="12" sm="6"><v-switch v-model="data.user_hint_is_mandatory" color="primary" :label="$t('nativeInbound.userHint')" /></v-col>
        <v-col cols="12"><v-combobox :model-value="ports" @update:model-value="setPorts" multiple chips clearable :label="$t('nativeInbound.listenPorts')" /></v-col>
        <v-col cols="12"><v-textarea v-model="data.traffic_pattern" rows="2" auto-grow :label="$t('nativeInbound.trafficPattern')" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model.number="data.mtu" type="number" min="0" clearable @click:clear="delete data.mtu" :label="$t('nativeInbound.mtu')" /></v-col>
      </v-row>
    </v-card-text>
  </v-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Mieru } from '@/types/inbounds'

const props = defineProps<{ data: Mieru }>()
const ports = computed(() => typeof props.data.listen_ports === 'string' ? [props.data.listen_ports] : props.data.listen_ports ?? [])
function setPorts(value: string[]) {
  if (value?.length) props.data.listen_ports = value
  else delete props.data.listen_ports
}
</script>
