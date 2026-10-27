<template>
  <v-card subtitle="Sudoku">
    <v-card-text>
      <v-alert v-if="!outbound" type="info" variant="tonal" density="compact" class="mb-4">{{ $t('nativeInbound.sudokuIdentityHint') }}</v-alert>
      <v-row>
        <template v-if="!outbound">
          <v-col cols="12"><SecretCredentialField v-model="data.master_key" :available="available" :label="$t('nativeInbound.sudokuMaster')" :clear-label="$t('nativeInbound.sudokuClear')" /></v-col>
          <v-col cols="12"><div class="text-caption">{{ $t('nativeInbound.sudokuMasterHint') }}</div></v-col>
          <v-col v-if="data.key" cols="12"><v-text-field :model-value="String(data.key)" readonly :label="$t('nativeInbound.sudokuPublic')" /></v-col>
        </template>
        <template v-else>
          <v-col cols="12"><SecretCredentialField v-model="data.key" :available="available" :label="$t('nativeOutbound.sudokuKey')" /></v-col>
          <v-col cols="12"><div class="text-caption">{{ $t('nativeOutbound.sudokuKeyHint') }}</div></v-col>
          <v-col cols="12" sm="6"><v-select v-model="data.multiplex" :items="['off', 'auto', 'on']" clearable @click:clear="delete data.multiplex" :label="$t('nativeOutbound.multiplexing')" /></v-col>
        </template>
        <v-col cols="12" sm="6"><v-select v-model="data.aead_method" :items="aeadMethods" :label="$t('nativeInbound.sudokuAead')" clearable @click:clear="delete data.aead_method" /></v-col>
        <v-col cols="12" sm="6"><v-select v-model="data.table_type" :items="tableTypes" :label="$t('nativeInbound.sudokuTableType')" clearable @click:clear="delete data.table_type" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model.number="data.padding_min" type="number" min="0" max="100" clearable @click:clear="delete data.padding_min" :label="$t('nativeInbound.sudokuPaddingMin')" /></v-col>
        <v-col cols="12" sm="6"><v-text-field v-model.number="data.padding_max" type="number" min="0" max="100" clearable @click:clear="delete data.padding_max" :label="$t('nativeInbound.sudokuPaddingMax')" /></v-col>
        <v-col v-if="!outbound" cols="12" sm="6"><v-text-field v-model.number="data.handshake_timeout" type="number" min="0" clearable @click:clear="delete data.handshake_timeout" :label="$t('nativeInbound.sudokuHandshake')" /></v-col>
        <v-col cols="12" sm="6"><v-select v-model="pureDownlink" :items="pureDownlinkChoices" :label="$t('nativeInbound.sudokuPureDownlink')" /></v-col>
        <v-col cols="12"><v-text-field v-model="data.custom_table" clearable @click:clear="delete data.custom_table" :label="$t('nativeInbound.sudokuCustomTable')" /></v-col>
        <v-col cols="12"><v-combobox :model-value="data.custom_tables ?? []" @update:model-value="setTables" multiple chips clearable :label="$t('nativeInbound.sudokuCustomTables')" /></v-col>
        <template v-if="!outbound">
          <v-col cols="12" sm="6"><v-switch v-model="data.disable_http_mask" color="primary" :label="$t('nativeInbound.sudokuDisableMask')" /></v-col>
          <v-col cols="12" sm="6"><v-select v-model="data.http_mask_mode" :items="maskModes" clearable @click:clear="delete data.http_mask_mode" :label="$t('nativeInbound.sudokuMaskMode')" /></v-col>
          <v-col cols="12" sm="6"><v-text-field v-model="data.path_root" clearable @click:clear="delete data.path_root" :label="$t('nativeInbound.sudokuPathRoot')" /></v-col>
          <v-col cols="12" sm="6"><v-text-field v-model="data.fallback" clearable @click:clear="delete data.fallback" :label="$t('nativeInbound.sudokuFallback')" /></v-col>
        </template>
        <template v-else>
          <v-col cols="12" sm="6"><v-switch :model-value="data.http_mask?.enabled ?? true" @update:model-value="value => setMask('enabled', !!value)" color="primary" :label="$t('nativeOutbound.sudokuMask')" /></v-col>
          <v-col cols="12" sm="6"><v-select :model-value="data.http_mask?.mode" @update:model-value="value => setMask('mode', value)" :items="maskModes" clearable :label="$t('nativeInbound.sudokuMaskMode')" /></v-col>
          <v-col cols="12" sm="6"><v-text-field :model-value="data.http_mask?.host" @update:model-value="value => setMask('host', value)" clearable :label="$t('nativeOutbound.sudokuMaskHost')" /></v-col>
          <v-col cols="12" sm="6"><v-text-field :model-value="data.http_mask?.path_root" @update:model-value="value => setMask('path_root', value)" clearable :label="$t('nativeInbound.sudokuPathRoot')" /></v-col>
          <v-col cols="12" sm="6"><v-select :model-value="data.http_mask?.multiplex" @update:model-value="value => setMask('multiplex', value)" :items="['off', 'auto', 'on']" clearable :label="$t('nativeOutbound.sudokuMaskMultiplex')" /></v-col>
          <v-col v-if="!data.http_mask?.tls" cols="12"><v-btn variant="tonal" @click="enableMaskTLS">{{ $t('nativeOutbound.sudokuMaskTLS') }}</v-btn></v-col>
          <v-col v-else cols="12"><OutTLS :outbound="maskTLSOutbound" /></v-col>
        </template>
      </v-row>
    </v-card-text>
  </v-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Sudoku as SudokuInbound } from '@/types/inbounds'
import type { Sudoku as SudokuOutbound } from '@/types/outbounds'
import SecretCredentialField from '@/components/fields/SecretCredentialField.vue'
import OutTLS from '@/components/tls/OutTLS.vue'
import { runtimeCapability } from '@/types/runtimeCapabilities'
import Data from '@/store/modules/data'

const props = defineProps<{ data: Omit<Partial<SudokuInbound>, 'key'> & Pick<Partial<SudokuOutbound>, 'http_mask' | 'multiplex'> & { key?: unknown }; outbound?: boolean }>()
const { t } = useI18n()
const available = computed(() => runtimeCapability(Data().capabilities, props.outbound ? 'outbounds' : 'inbounds', 'sudoku')?.available === true)
const maskTLSOutbound = computed(() => ({ type: 'sudoku', tls: props.data.http_mask?.tls }))
function setMask(field: 'enabled' | 'mode' | 'host' | 'path_root' | 'multiplex', value: string | boolean | null | undefined) {
  props.data.http_mask ??= { enabled: true }
  if (value === null || value === undefined || value === '') delete props.data.http_mask[field]
  else Object.assign(props.data.http_mask, { [field]: value })
}
function enableMaskTLS() {
  props.data.http_mask ??= { enabled: true }
  props.data.http_mask.tls = { enabled: true }
}
const aeadMethods = ['chacha20-poly1305', 'aes-128-gcm', 'none']
const tableTypes = ['prefer_ascii', 'prefer_entropy', 'up_ascii_down_entropy', 'up_entropy_down_ascii']
const maskModes = ['legacy', 'stream', 'poll', 'auto', 'ws']
const pureDownlinkChoices = computed(() => [
  { title: t('nativeInbound.sudokuDefault'), value: 'default' },
  { title: t('enable'), value: 'enabled' },
  { title: t('disable'), value: 'disabled' },
])
const pureDownlink = computed({
  get: () => props.data.enable_pure_downlink === undefined ? 'default' : props.data.enable_pure_downlink ? 'enabled' : 'disabled',
  set: (value: string) => {
    if (value === 'default') delete props.data.enable_pure_downlink
    else props.data.enable_pure_downlink = value === 'enabled'
  },
})
function setTables(value: string[]) {
  if (value?.length) props.data.custom_tables = value
  else delete props.data.custom_tables
}
</script>
