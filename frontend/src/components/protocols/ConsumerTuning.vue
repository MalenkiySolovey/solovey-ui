<template>
  <v-card v-if="fields.length" subtitle="HTTP2 / QUIC tuning">
    <div class="text-caption" v-if="contract">Sizes accept numeric bytes or {{ Object.keys(contract.memoryUnits).join(', ') }}. KB and MB use binary units. Validation follows this protocol and direction.</div>
    <v-row>
      <v-col v-for="field in fields" :key="field" cols="12" sm="6">
        <v-switch v-if="booleans.includes(field)" v-model="data[field]" :label="field" color="primary" hide-details />
        <v-text-field v-else :model-value="data[field]" @update:model-value="setField(field, $event)" :label="field" :placeholder="sizes.includes(field) ? '1MB' : durations.includes(field) ? '10s' : undefined" clearable @click:clear="delete data[field]" />
      </v-col>
    </v-row>
  </v-card>
</template>

<script lang="ts">
import { defineComponent, type PropType } from 'vue'
import { coreConfigContract, loadCoreConfigContract } from '@/types/coreConfigContract'

export default defineComponent({
  props: { data: { type: Object as PropType<Record<string, any>>, required: true }, type: { type: String, required: true }, direction: { type: String, required: true } },
  data: () => ({ sizes: ['stream_receive_window', 'connection_receive_window', 'quic_session_receive_window'], durations: ['idle_timeout', 'keep_alive_period'], numbers: ['max_concurrent_streams', 'initial_packet_size'], booleans: ['disable_path_mtu_discovery', 'disable_chrome_parrot'] }),
  computed: {
    contract: () => coreConfigContract.value?.protocol,
    fields(): string[] {
      const side = this.direction === 'out_json' ? 'out' : this.direction
      const controls = [...this.sizes, ...this.durations, ...this.numbers, ...this.booleans]
      return (this.contract?.fields[`${this.type}/${side}`] ?? []).filter(field => controls.includes(field))
    },
  },
  methods: {
    setField(field: string, value: string | number | null) {
      if (value === null || value === '') { delete this.data[field]; return }
      if ((this.sizes.includes(field) || this.numbers.includes(field)) && /^\d+$/.test(String(value)) && Number.isSafeInteger(Number(value))) this.data[field] = Number(value)
      else this.data[field] = value
    },
  },
  mounted() { void loadCoreConfigContract() },
})
</script>
