<template>
 <v-alert v-if="state.message" class="my-2" density="compact" variant="tonal" type="warning">
  {{ $t(state.message) }}
 </v-alert>
</template>
<script setup lang="ts">
import type { CapabilityEditState } from '@/types/capabilityEditors'
defineProps<{ state: CapabilityEditState }>()
</script>
