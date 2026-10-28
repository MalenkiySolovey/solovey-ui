<template>
 <v-alert v-if="state.message" class="my-2" density="compact" variant="tonal" type="warning">
  {{ $t(state.message) }}
  <span v-if="state.requiredProvider"> {{ $t('capability.requires', { provider: state.requiredProvider }) }}</span>
  <span v-if="state.reason" class="text-caption"> ({{ state.reason }})</span>
 </v-alert>
</template>
<script setup lang="ts">
import type { CapabilityEditState } from '@/types/capabilityEditors'
defineProps<{ state: CapabilityEditState }>()
</script>
