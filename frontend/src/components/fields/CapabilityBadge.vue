<template>
  <div v-if="!state.allowed" role="status" class="text-warning text-caption my-1">
    {{ $t(state.message === 'capability.waiting' ? 'capability.waiting' : 'capability.inactive') }}
    <span v-if="state.requiredProvider">{{ $t('capability.requires', { provider: state.requiredProvider }) }}</span>
  </div>
</template>
<script setup lang="ts">
import type { CapabilityEditState } from '@/types/capabilityEditors'
defineProps<{ state: CapabilityEditState }>()
</script>
