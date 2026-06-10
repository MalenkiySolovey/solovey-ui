<template>
  <div class="nexus-toolbar">
    <div v-if="searchable" class="nexus-toolbar__search">
      <v-text-field
        :aria-label="$t('table.search')"
        clearable
        density="compact"
        hide-details
        :model-value="search"
        :placeholder="$t('table.search')"
        prepend-inner-icon="lucide:search"
        variant="outlined"
        @update:model-value="onSearch"
      />
    </div>

    <div v-if="$slots.filters" class="nexus-toolbar__filters">
      <slot name="filters" />
    </div>

    <div v-if="$slots.actions" class="nexus-toolbar__actions">
      <slot name="actions" />
    </div>
  </div>
</template>

<script lang="ts" setup>
import { onBeforeUnmount } from 'vue'

const props = withDefaults(defineProps<{
  search?: string
  searchable?: boolean
  debounce?: number
}>(), {
  search: '',
  searchable: true,
  debounce: 250,
})

const emit = defineEmits<{
  'update:search': [value: string]
}>()

let timer: ReturnType<typeof setTimeout> | undefined

const onSearch = (value: string | null) => {
  clearTimeout(timer)
  timer = setTimeout(() => emit('update:search', value ?? ''), props.debounce)
}

onBeforeUnmount(() => clearTimeout(timer))
</script>

<style scoped>
/* Content actions row above the table: filters (left) + actions/Add (right).
 * Search now lives in the topbar (PageHeader), so this row carries the rest. */
.nexus-toolbar {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: var(--nexus-gap-2);
  margin-block-end: var(--nexus-gap-4);
}

.nexus-toolbar__search {
  min-width: 200px;
}

/* Reference search input: filled #202020 surface, 36px, cyan focus border. */
.nexus-toolbar__search :deep(.v-field) {
  background: var(--nexus-elevated);
  border-radius: var(--nexus-radius-sm);
}

.nexus-toolbar__filters,
.nexus-toolbar__actions {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: var(--nexus-gap-2);
}

/* Push actions (Add, etc.) to the right; filters stay left. */
.nexus-toolbar__actions {
  margin-inline-start: auto;
}
</style>
