<template>
  <v-card v-if="profile.provider" subtitle="Certificate provider">
    <v-alert type="info" variant="tonal" density="compact">Stored secrets remain masked. Keep the marker to retain a secret; enter an empty value to clear it. Editing a shared provider updates all bound TLS profiles.</v-alert>
    <v-alert v-if="profile.provider.runtimeMode === 'legacy_inline'" type="info" variant="tonal" density="compact">Legacy ACME keeps its existing transport and account behavior. Native mode uses the configured core HTTP client.</v-alert>
    <v-row>
      <v-col cols="12" sm="6"><v-text-field :model-value="profile.provider.tag" @update:model-value="setTag" label="Provider identity" /></v-col>
      <v-col cols="12" sm="6"><v-select v-model="profile.provider.type" :items="types" label="Provider type" :disabled="!contract" /></v-col>
      <v-col cols="12" sm="6"><v-select v-model="profile.provider.runtimeMode" :items="contract?.providerModes ?? []" label="Runtime mode" :disabled="!contract" /></v-col>
    </v-row>
    <v-alert v-if="unavailable" type="error" variant="tonal" density="compact">{{ unavailable }}</v-alert>
    <v-textarea v-model="optionsText" @update:model-value="applyOptions" label="Provider options (JSON)" :error-messages="parseError" rows="6" auto-grow />
    <div v-if="contract" class="text-caption">Available fields: {{ contract.providerFields[profile.provider.runtimeMode]?.join(', ') }}</div>
  </v-card>
</template>

<script lang="ts">
import { defineComponent, type PropType } from 'vue'
import type { tls } from '@/types/tls'
import { coreConfigContract, loadCoreConfigContract } from '@/types/coreConfigContract'

export default defineComponent({
  props: { profile: { type: Object as PropType<tls>, required: true } },
  emits: ['validity'],
  data: () => ({ optionsText: '', parseError: '' }),
  computed: {
    contract: () => coreConfigContract.value?.tls,
    types(): string[] { return [...new Set([...(this.contract?.providerTypes ?? []), ...(this.profile.provider?.type ? [this.profile.provider.type] : [])])] },
    unavailable(): string { return this.contract?.providerUnavailable[this.profile.provider?.type ?? ''] ?? '' },
  },
  methods: {
    setTag(value: string) {
      if (!this.profile.provider) return
      this.profile.provider.tag = value
      this.profile.server.certificate_provider = value
    },
    applyOptions() {
      try {
        const options = JSON.parse(this.optionsText)
        if (!options || typeof options !== 'object' || Array.isArray(options)) throw new Error('object required')
        if (this.profile.provider) this.profile.provider.options = options
        this.parseError = ''
        this.$emit('validity', true)
      } catch {
        this.parseError = 'Use a JSON object. The existing provider draft is preserved.'
        this.$emit('validity', false)
      }
    },
  },
  mounted() { this.optionsText = JSON.stringify(this.profile.provider?.options ?? {}, null, 2); void loadCoreConfigContract() },
  watch: {
    'profile.provider': { handler() { this.optionsText = JSON.stringify(this.profile.provider?.options ?? {}, null, 2); this.parseError = ''; this.$emit('validity', true) } },
  },
})
</script>
