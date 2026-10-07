<template>
  <v-expansion-panel title="Core HTTP clients">
    <v-expansion-panel-text>
      <v-alert type="info" variant="tonal" density="compact">Shared clients control core downloads. An empty client uses the configured default; select direct explicitly. Definitions are validated by the backend before saving.</v-alert>
      <v-select :model-value="data.route?.default_http_client" @update:model-value="setDefault" :items="clientTags" label="Default HTTP client" clearable hide-details />
      <v-textarea v-model="clientText" label="Shared definitions (JSON array)" rows="5" auto-grow :error-messages="parseError" />
      <v-btn variant="outlined" @click="applyDefinitions">Apply definitions to draft</v-btn>
      <v-btn variant="text" :loading="loading" @click="previewCompatibility">Preview migration</v-btn>
      <div v-if="contract">Engines: {{ contract.httpEngines?.join(', ') }}; versions: {{ contract.httpVersions?.join(', ') }}.</div>
      <CompatibilityFindings :preview="preview" />
      <v-btn v-if="preview && !preview.blocked" variant="outlined" @click="applyPreview">Apply preview to draft</v-btn>
    </v-expansion-panel-text>
  </v-expansion-panel>
</template>

<script lang="ts">
import { defineComponent, type PropType } from 'vue'
import type { Config } from '@/types/config'
import HttpUtils from '@/plugins/httputil'
import CompatibilityFindings from '@/components/common/CompatibilityFindings.vue'
import { coreConfigContract, loadCoreConfigContract, type CompatibilityPreview } from '@/types/coreConfigContract'

export default defineComponent({
  components: { CompatibilityFindings },
  props: { data: { type: Object as PropType<Config>, required: true } },
  data: () => ({ clientText: '', parseError: '', loading: false, preview: undefined as CompatibilityPreview | undefined, previewSource: '' }),
  computed: {
    contract: () => coreConfigContract.value,
    clientTags(): string[] { return (this.data.http_clients ?? []).map(client => client.tag).filter((tag): tag is string => typeof tag === 'string') },
  },
  methods: {
    refreshText() { this.clientText = JSON.stringify(this.data.http_clients ?? [], null, 2) },
    setDefault(value: string | null) {
      if (!this.data.route) return
      if (value === null) delete this.data.route.default_http_client
      else this.data.route.default_http_client = value
    },
    applyDefinitions() {
      try {
        const value = JSON.parse(this.clientText)
        if (!Array.isArray(value) || value.some(item => !item || typeof item !== 'object' || Array.isArray(item))) throw new Error('invalid collection')
        this.data.http_clients = value
        this.parseError = ''
      } catch { this.parseError = 'Use a JSON array of objects. Existing definitions are preserved.' }
    },
    async previewCompatibility() {
      this.loading = true
      const source = JSON.stringify(this.data)
      try {
        const response = await HttpUtils.post('api/config/compatibility-preview', { config: this.data, includeHttp: true })
        if (response.success && source === JSON.stringify(this.data)) {
          this.preview = response.obj
          this.previewSource = source
        }
      } finally { this.loading = false }
    },
    applyPreview() {
      if (!this.preview || this.preview.blocked || this.previewSource !== JSON.stringify(this.data)) return
      if (this.preview.http_clients) this.data.http_clients = JSON.parse(JSON.stringify(this.preview.http_clients))
      if (this.preview.route) this.data.route = JSON.parse(JSON.stringify(this.preview.route))
      this.preview = undefined
      this.refreshText()
    },
  },
  mounted() { this.refreshText(); void loadCoreConfigContract() },
  watch: {
    'data.http_clients': { handler() { this.refreshText() }, deep: true },
    data: { handler(value) { if (JSON.stringify(value) !== this.previewSource) this.preview = undefined }, deep: true },
  },
})
</script>
