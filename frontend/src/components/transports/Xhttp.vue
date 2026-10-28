<template>
  <v-row>
    <v-col cols="12" sm="6" md="4"><StrictSelect v-model="transport.mode" :items="contract?.choices?.mode ?? []" :label="$t('transport.xhttp.mode')" /></v-col>
  </v-row>
  <XhttpBase :data="transport" :direction="direction" />
  <v-card v-if="direction === 'out'" variant="outlined" class="my-3">
    <v-card-text>
      <v-switch v-model="downloadEnabled" :label="$t('transport.xhttp.download')" color="primary" />
      <template v-if="transport.download">
        <v-alert v-if="transport.mode === 'stream-one'" type="warning" variant="tonal">{{ $t('transport.xhttp.downloadMode') }}</v-alert>
        <v-row>
          <v-col cols="12" sm="6" md="4"><v-text-field v-model="transport.download.server" :label="$t('out.addr')" /></v-col>
          <v-col cols="12" sm="6" md="4"><v-text-field v-model.number="transport.download.server_port" type="number" min="1" max="65535" :label="$t('out.port')" /></v-col>
          <v-col cols="12" sm="6" md="4"><StrictSelect :model-value="transport.download.detour" :items="detours" :label="$t('dial.detourText')" clearable @update:model-value="setDetour" /></v-col>
        </v-row>
        <XhttpBase :data="transport.download" direction="out" download />
        <OutTLS :outbound="transport.download" managed-client-key />
      </template>
    </v-card-text>
  </v-card>
</template>

<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import Data from '@/store/modules/data'
import StrictSelect from '@/shared/ui/StrictSelect.vue'
import OutTLS from '@/components/tls/OutTLS.vue'
import { coreConfigContract } from '@/types/coreConfigContract'
import XhttpBase from './XhttpBase.vue'

const props = defineProps<{ transport: Record<string, any>; direction: 'in' | 'out'; parentTag?: string }>()
const contract = computed(() => coreConfigContract.value?.protocol?.transports?.xhttp)
const detours = computed(() => [...Data().outbounds, ...Data().endpoints].map(row => row.tag).filter(tag => tag !== props.parentTag))
const downloadDraft = shallowRef(props.transport.download)
const downloadEnabled = computed({
  get: () => props.transport.download != null,
  set: (enabled: boolean) => {
    if (enabled) props.transport.download = downloadDraft.value ?? { x_padding_bytes: contract.value?.defaults?.x_padding_bytes, tls: {} }
    else {
      downloadDraft.value = props.transport.download
      delete props.transport.download
    }
  },
})
function setDetour(value: unknown) {
  if (value == null || value === '') delete props.transport.download.detour
  else props.transport.download.detour = value
}
</script>
