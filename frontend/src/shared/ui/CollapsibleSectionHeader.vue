<template>
  <div class="collapsible-section-header" :class="{ 'collapsible-section-header--nexus': nexus }">
    <v-btn
      :aria-expanded="modelValue"
      :aria-label="toggleLabel"
      class="collapsible-section-header__toggle"
      density="comfortable"
      :icon="modelValue ? expandedIcon : collapsedIcon"
      size="small"
      variant="text"
      @click="$emit('update:modelValue', !modelValue)"
    />
    <div class="collapsible-section-header__title">{{ title }}</div>
    <div v-if="$slots.actions" class="collapsible-section-header__actions">
      <slot name="actions" />
    </div>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  modelValue: boolean
  title: string
  nexus?: boolean
}>(), {
  nexus: false,
})

defineEmits<{
  'update:modelValue': [value: boolean]
}>()

const expandedIcon = computed(() => props.nexus ? 'lucide:chevron-down' : 'mdi-chevron-down')
const collapsedIcon = computed(() => props.nexus ? 'lucide:chevron-right' : 'mdi-chevron-right')
const toggleLabel = computed(() => props.modelValue ? 'Collapse section' : 'Expand section')
</script>

<style scoped>
.collapsible-section-header {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  min-height: 40px;
}

.collapsible-section-header--nexus {
  gap: var(--nexus-gap-2);
  margin-block: var(--nexus-gap-4) var(--nexus-gap-2);
}

.collapsible-section-header__toggle {
  flex: 0 0 auto;
}

.collapsible-section-header__title {
  color: inherit;
  font: inherit;
}

.collapsible-section-header--nexus .collapsible-section-header__title {
  color: var(--nexus-text-secondary);
  font-size: 0.78rem;
  font-weight: 650;
  letter-spacing: 0;
  text-transform: uppercase;
}

.collapsible-section-header__actions {
  align-items: center;
  display: inline-flex;
  gap: 6px;
}
</style>
