<template>
  <details v-if="hints.length" class="my-3">
    <summary>{{ $t('inboundGuidance.title') }}</summary>
    <ul class="ps-4 mt-1"><li v-for="hint in hints" :key="hint">{{ $t(hint) }}</li></ul>
  </details>
</template>
<script setup lang="ts">
defineProps<{ hints: readonly string[] }>()
</script>
