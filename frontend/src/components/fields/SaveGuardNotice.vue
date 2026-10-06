<template>
  <div v-if="reasons.length" class="my-2" role="status" aria-live="polite" aria-atomic="true">
    <ul class="ps-4"><li v-for="reason in reasons" :key="reason">{{ $t(reason) }}</li></ul>
  </div>
</template>
<script setup lang="ts">
import type { SaveReason } from '@/features/entitySaveReview'
defineProps<{ reasons: readonly SaveReason[] }>()
</script>
