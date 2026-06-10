<template>
  <div class="nexus-dense-table">
    <table class="nexus-dense-table__table">
      <slot />
    </table>
  </div>
</template>

<style scoped>
.nexus-dense-table {
  /* Reference: panel/table container = surface-2 (#1a1a1a); the thead row is the
   * darker surface-1 (#151515) — set in NexusTableHeader. */
  background: var(--nexus-surface-2);
  border: 1px solid var(--nexus-border);
  border-radius: var(--nexus-radius-md);
  min-width: 0;
  overflow-x: auto;
}

.nexus-dense-table__table {
  border-collapse: collapse;
  min-width: 100%;
  table-layout: fixed;
}

.nexus-dense-table__table :deep(th),
.nexus-dense-table__table :deep(td) {
  border-block-end: 1px solid var(--nexus-border);
  font-size: 0.875rem;
  height: 44px;
  letter-spacing: 0;
  line-height: 1.4;
  min-width: 0;
  overflow-wrap: anywhere;
  padding: var(--nexus-gap-3);
  text-align: start;
  vertical-align: middle;
}

.nexus-dense-table__table :deep(th) {
  color: rgb(var(--v-theme-on-surface) / 68%);
  font-weight: 650;
}

/* Reference: table body text is secondary grey (#a0a0a0); only <strong>/tags and
 * explicit values are white. Cells inherit this unless they set their own colour
 * (tag → text-primary, badges → tone), matching the prototype. */
.nexus-dense-table__table :deep(td) {
  color: var(--nexus-text-secondary);
}

.nexus-dense-table__table :deep(tbody tr:last-child td) {
  border-block-end: 0;
}
</style>
