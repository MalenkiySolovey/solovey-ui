<template>
  <v-row>
    <v-col v-for="field in fields" :key="field" cols="12" sm="6" md="4">
      <v-text-field :model-value="transport[field]" type="number" min="0" :label="$t('transport.mkcp.' + field)" @update:model-value="setNumber(field, $event)" />
    </v-col>
    <v-col cols="12" sm="6" md="4"><StrictSelect v-model="transport.header_type" :items="contract?.choices?.header_type ?? []" :label="$t('transport.mkcp.header_type')" clearable /></v-col>
    <v-col cols="12" sm="6" md="4"><v-switch v-model="transport.congestion" :label="$t('transport.mkcp.congestion')" color="primary" /></v-col>
    <v-col cols="12"><SecretCredentialField v-model="transport.seed" :available="contract?.available === true" :label="$t('transport.mkcp.seed')" /></v-col>
  </v-row>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import SecretCredentialField from '@/components/fields/SecretCredentialField.vue'
import StrictSelect from '@/shared/ui/StrictSelect.vue'
import { coreConfigContract } from '@/types/coreConfigContract'

const props = defineProps<{ transport: Record<string, any> }>()
const contract = computed(() => coreConfigContract.value?.protocol?.transports?.mkcp)
const fields = computed(() => ['mtu', 'tti', 'uplink_capacity', 'downlink_capacity', 'read_buffer_size', 'write_buffer_size'].filter(field => contract.value?.fields?.root?.includes(field)))
function setNumber(field: string, value: unknown) {
  if (value == null || value === '') delete props.transport[field]
  else props.transport[field] = Number(value)
}
</script>
