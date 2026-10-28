<template>
  <v-row>
    <v-col v-for="field in visible(['host', 'path', 'x_padding_bytes', 'sc_max_each_post_bytes', 'sc_min_posts_interval_ms'])" :key="field" cols="12" sm="6" md="4">
      <v-text-field :model-value="data[field]" :label="label(field)" @update:model-value="set(field, $event)" />
    </v-col>
    <v-col v-if="has('domain_strategy')" cols="12" sm="6" md="4">
      <StrictSelect :model-value="data.domain_strategy" :items="choices('domain_strategy')" :label="label('domain_strategy')" clearable @update:model-value="set('domain_strategy', $event)" />
    </v-col>
    <v-col v-for="field in visible(['no_grpc_header', ...(direction === 'in' ? ['no_sse_header'] : [])])" :key="field" cols="12" sm="6" md="4">
      <v-switch v-model="data[field]" :label="label(field)" color="primary" />
    </v-col>
  </v-row>
  <template v-if="direction === 'in'">
    <v-row>
      <v-col v-if="has('sc_stream_up_server_secs')" cols="12" sm="6" md="4"><v-text-field :model-value="data.sc_stream_up_server_secs" :label="label('sc_stream_up_server_secs')" @update:model-value="set('sc_stream_up_server_secs', $event)" /></v-col>
      <v-col v-for="field in visible(['sc_max_buffered_posts', 'server_max_header_bytes'])" :key="field" cols="12" sm="6" md="4"><v-text-field :model-value="data[field]" type="number" min="0" :label="label(field)" @update:model-value="set(field, $event, true)" /></v-col>
      <v-col v-if="has('trusted_x_forwarded_for')" cols="12"><v-combobox :model-value="data.trusted_x_forwarded_for" :label="label('trusted_x_forwarded_for')" multiple chips clearable @update:model-value="set('trusted_x_forwarded_for', $event)" /></v-col>
    </v-row>
  </template>
  <v-expansion-panels variant="accordion" class="my-3">
    <v-expansion-panel :title="$t('transport.xhttp.padding')">
      <v-expansion-panel-text>
        <v-row>
          <v-col v-if="has('x_padding_obfs_mode')" cols="12" sm="6" md="4"><v-switch v-model="data.x_padding_obfs_mode" :label="label('x_padding_obfs_mode')" color="primary" /></v-col>
          <v-col v-for="field in visible(['x_padding_key', 'x_padding_header'])" :key="field" cols="12" sm="6" md="4"><v-text-field :model-value="data[field]" :label="label(field)" @update:model-value="set(field, $event)" /></v-col>
          <v-col v-for="field in visible(['x_padding_placement', 'x_padding_method'])" :key="field" cols="12" sm="6" md="4"><StrictSelect :model-value="data[field]" :items="choices(field)" :label="label(field)" clearable @update:model-value="set(field, $event)" /></v-col>
        </v-row>
      </v-expansion-panel-text>
    </v-expansion-panel>
    <v-expansion-panel :title="$t('transport.xhttp.placement')">
      <v-expansion-panel-text>
        <v-row>
          <v-col v-for="field in visible(['uplink_http_method', 'session_placement', 'seq_placement', 'uplink_data_placement'])" :key="field" cols="12" sm="6" md="4"><StrictSelect :model-value="data[field]" :items="choices(field)" :label="label(field)" clearable @update:model-value="set(field, $event)" /></v-col>
          <v-col v-for="field in visible(['session_key', 'seq_key', 'uplink_data_key', 'uplink_chunk_size'])" :key="field" cols="12" sm="6" md="4"><v-text-field :model-value="data[field]" :label="label(field)" @update:model-value="set(field, $event)" /></v-col>
        </v-row>
      </v-expansion-panel-text>
    </v-expansion-panel>
    <v-expansion-panel v-if="direction === 'out' && has('xmux')" :title="$t('transport.xhttp.xmux')">
      <v-expansion-panel-text>
        <v-row>
          <v-col v-for="field in xmuxFields" :key="field" cols="12" sm="6" md="4"><v-text-field :model-value="data.xmux?.[field]" :label="label(field)" @update:model-value="setXmux(field, $event)" /></v-col>
        </v-row>
      </v-expansion-panel-text>
    </v-expansion-panel>
  </v-expansion-panels>
  <Headers :data="data" default-name="X-Custom" />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Headers from '@/components/fields/Headers.vue'
import StrictSelect from '@/shared/ui/StrictSelect.vue'
import { coreConfigContract } from '@/types/coreConfigContract'

const props = defineProps<{ data: Record<string, any>; direction: 'in' | 'out'; download?: boolean }>()
const { t } = useI18n()
const contract = computed(() => coreConfigContract.value?.protocol?.transports?.xhttp)
const fields = computed(() => contract.value?.fields?.[props.download ? 'download' : 'root'] ?? [])
const xmuxFields = computed(() => contract.value?.fields?.xmux ?? [])
const has = (field: string) => fields.value.includes(field)
const visible = (values: string[]) => values.filter(has)
const choices = (field: string) => contract.value?.choices?.[field] ?? []
const label = (field: string) => t('transport.xhttp.' + field)
function set(field: string, value: unknown, numeric = false) {
  if (value === '' || value == null || (Array.isArray(value) && value.length === 0)) delete props.data[field]
  else props.data[field] = numeric ? Number(value) : value
}
function setXmux(field: string, value: unknown) {
  const next = { ...props.data.xmux }
  if (value == null || value === '') delete next[field]
  else next[field] = field === 'h_keep_alive_period' ? Number(value) : value
  if (Object.keys(next).length) props.data.xmux = next
  else delete props.data.xmux
}
</script>
