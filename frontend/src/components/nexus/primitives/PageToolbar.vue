<template>
  <div class="nexus-toolbar">
    <div v-if="$slots.filters" class="nexus-toolbar__filters">
      <slot name="filters" />
    </div>

    <div v-if="$slots.actions" class="nexus-toolbar__actions">
      <slot name="actions" />
    </div>
  </div>
</template>

<script lang="ts" setup>
// Layout shell for the content row above a table: filters (left) + actions
// (right). Search lives solely in the topbar (PageHeader); this component
// previously carried a duplicate, always-disabled search box — now removed.
</script>

<style scoped>
/* Content actions row above the table: filters (left) + actions/Add (right). */
.nexus-toolbar {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: var(--nexus-gap-2);
  margin-block-end: var(--nexus-gap-4);
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
