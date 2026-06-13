<template>
  <div class="nexus-row-actions">
    <v-btn
      v-for="action in inlineActions"
      :key="action.key"
      :aria-label="$t(action.labelKey)"
      :class="{ 'nexus-row-actions__reserved': action.hidden }"
      :color="action.tone === 'error' ? 'error' : undefined"
      density="comfortable"
      :disabled="action.hidden"
      :icon="action.icon"
      size="small"
      :title="$t(action.labelKey)"
      variant="text"
      @click="emit('action', action.key)"
    />

    <v-menu v-if="menuActions.length">
      <template #activator="{ props }">
        <v-btn
          :aria-label="$t('actions.action')"
          density="comfortable"
          icon="lucide:more-vertical"
          size="small"
          variant="text"
          v-bind="props"
        />
      </template>
      <v-list density="compact">
        <template v-for="action in menuActions" :key="action.key">
          <v-divider v-if="action.divider" />
          <v-list-item
            :base-color="action.tone === 'error' ? 'error' : undefined"
            :prepend-icon="action.icon"
            :title="$t(action.labelKey)"
            @click="emit('action', action.key)"
          />
        </template>
      </v-list>
    </v-menu>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue'

import type { RowAction } from './rowActions'

const props = defineProps<{
  actions: RowAction[]
}>()

const emit = defineEmits<{
  action: [key: string]
}>()

const visible = computed(() => props.actions.filter(action => !action.hidden))
const inlineActions = computed(() => props.actions.filter(action => action.inline && (!action.hidden || action.reserveSpace)))
const menuActions = computed(() => visible.value.filter(action => !action.inline))
</script>

<style scoped>
.nexus-row-actions {
  align-items: center;
  display: flex;
  gap: var(--nexus-gap-1);
  justify-content: flex-end;
}

.nexus-row-actions__reserved {
  pointer-events: none;
  visibility: hidden;
}
</style>
