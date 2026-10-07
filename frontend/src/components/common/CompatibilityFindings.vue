<template>
  <v-alert v-if="preview" :type="preview.blocked ? 'error' : 'info'" variant="tonal" class="my-3">
    <div>{{ labels[preview.outcome] ?? preview.outcome }}</div>
    <div v-for="(finding, index) in preview.findings ?? []" :key="index" class="mt-2">
      <code>{{ finding.path }}</code>: {{ finding.message }} <small>({{ finding.code }})</small>
    </div>
    <slot />
  </v-alert>
</template>
<script setup lang="ts">
import type { CompatibilityPreview } from '@/types/coreConfigContract'
defineProps<{ preview?: CompatibilityPreview }>()
const labels: Record<string, string> = {
  LOSSLESS_AUTOMATIC: 'The configuration is compatible.',
  AUTOMATIC_WITH_EXPLICIT_DIAGNOSTIC: 'Review these changes before applying them to the draft.',
  MANUAL_REQUIRED: 'Correct the indicated settings before retrying. Stored configuration is preserved.',
  UNSUPPORTED_LEGACY: 'These legacy settings require a supported replacement. Stored configuration is preserved.',
}
</script>
