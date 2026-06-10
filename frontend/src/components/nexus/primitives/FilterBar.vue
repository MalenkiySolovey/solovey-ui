<template>
  <div class="nexus-filter-bar">
    <slot />
    <v-btn
      v-if="active"
      :aria-label="$t('table.clearFilters')"
      prepend-icon="lucide:filter-x"
      size="small"
      variant="text"
      @click="emit('clear')"
    >
      {{ $t('table.clearFilters') }}
    </v-btn>
  </div>
</template>

<script lang="ts" setup>
defineProps<{
  // True when any filter is applied; reveals the Clear button.
  active?: boolean
}>()

const emit = defineEmits<{
  apply: []
  clear: []
}>()
</script>

<style scoped>
.nexus-filter-bar {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: var(--nexus-gap-2);
}
</style>
