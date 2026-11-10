// Public presentation and authorized-reader exports for observation UI contributions.
// The frontend retains ownership of the reader, UI mode, locale and shared widgets.
export { currentLocale, dateLocale } from '@/locales'
export { loadAuditEvents } from '@/shared/composables/useAuditOperations'
export { useUiMode } from '@/uiMode/useUiMode'
export type { Column } from '@/components/nexus/data/dataTableColumns'
export { default as NexusDataTable } from '@/components/nexus/data/NexusDataTable.vue'
export { default as EmptyState } from '@/components/nexus/primitives/EmptyState.vue'
export { default as PageHeader } from '@/components/nexus/primitives/PageHeader.vue'
export { default as PageToolbar } from '@/components/nexus/primitives/PageToolbar.vue'
export { default as StatusBadge } from '@/components/nexus/primitives/StatusBadge.vue'
