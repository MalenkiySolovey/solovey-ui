<template>
  <v-card subtitle="MTProxy">
    <v-card-text>
      <v-row>
        <v-col cols="12" sm="6"><v-select v-model="data.prefer_ip" :items="['prefer-ipv4', 'prefer-ipv6', 'only-ipv4', 'only-ipv6']" :label="$t('nativeInbound.preferIP')" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model.number="data.concurrency" type="number" min="1" clearable @click:clear="delete data.concurrency" :label="$t('nativeInbound.concurrency')" /></v-col>
        <v-col cols="12" sm="6"><v-switch v-model="data.auto_update" color="primary" :label="$t('nativeInbound.autoUpdate')" /></v-col>
        <v-col cols="12" sm="6"><v-switch v-model="data.allow_fallback_on_unknown_dc" color="primary" :label="$t('nativeInbound.unknownDC')" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model="data.domain_fronting_host" clearable @click:clear="delete data.domain_fronting_host" :label="$t('nativeInbound.frontingHost')" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model.number="data.domain_fronting_port" type="number" min="1" max="65535" clearable @click:clear="delete data.domain_fronting_port" :label="$t('nativeInbound.frontingPort')" /></v-col>
        <v-col cols="12" sm="6"><v-switch v-model="data.domain_fronting_proxy_protocol" color="primary" :label="$t('nativeInbound.frontingProxyProtocol')" /></v-col>
        <v-col v-for="field in durations" :key="field" cols="12" sm="6"><v-text-field v-model="data[field]" clearable @click:clear="delete data[field]" :label="$t('nativeInbound.' + field)" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model.number="data.throttle_max_connections" type="number" min="0" clearable @click:clear="delete data.throttle_max_connections" :label="$t('nativeInbound.throttleMax')" /></v-col>
        <v-col cols="12"><v-combobox :model-value="urls" @update:model-value="setUrls" multiple chips clearable :label="$t('nativeInbound.doppelUrls')" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model.number="data.doppelganger_per_raid" type="number" min="1" clearable @click:clear="delete data.doppelganger_per_raid" :label="$t('nativeInbound.doppelPerRaid')" /></v-col>
        <v-col cols="12" sm="6"><v-switch v-model="data.doppelganger_drs" color="primary" :label="$t('nativeInbound.doppelDRS')" /></v-col>
      </v-row>
    </v-card-text>
  </v-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { MTProxy } from '@/types/inbounds'

const props = defineProps<{ data: MTProxy }>()
const durations = ['idle_timeout', 'handshake_timeout', 'tolerate_time_skewness', 'throttle_check_interval', 'doppelganger_each'] as const
const urls = computed(() => typeof props.data.doppelganger_urls === 'string' ? [props.data.doppelganger_urls] : props.data.doppelganger_urls ?? [])
function setUrls(value: string[]) {
  if (value?.length) props.data.doppelganger_urls = value
  else delete props.data.doppelganger_urls
}
</script>
