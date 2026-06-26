<template>
  <article class="nexus-overview-panel">
    <panel-header :title="title">
      <template #range-tabs>
        <slot name="range-tabs" />
      </template>
      <template #action>
        <slot name="action" />
      </template>
    </panel-header>

    <div class="nexus-overview-panel__content">
      <slot />
    </div>
  </article>
</template>

<script lang="ts" setup>
import PanelHeader from '@/components/nexus/primitives/PanelHeader.vue'

defineProps<{
  title: string
}>()
</script>

<style scoped>
.nexus-overview-panel {
  align-content: start;
  background: var(--nexus-surface-1);
  border: 1px solid var(--nexus-border);
  border-radius: var(--nexus-radius-lg);
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  gap: var(--nexus-gap-4);
  min-width: 0;
  padding: var(--nexus-gap-4);
}

.nexus-overview-panel__content {
  align-content: start;
  display: grid;
  gap: var(--nexus-gap-3);
  min-height: 0;
  min-width: 0;
}
</style>
