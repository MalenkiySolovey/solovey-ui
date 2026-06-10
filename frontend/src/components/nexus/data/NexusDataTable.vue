<template>
  <div class="nexus-data-table">
    <table-skeleton v-if="loading" :columns="skeletonColumns" :rows="6" />

    <template v-else-if="sortedItems.length === 0">
      <slot v-if="$slots.empty" name="empty" />
      <empty-state v-else :title="emptyTitle ?? $t('table.noData')" />
    </template>

    <template v-else>
      <dense-table>
        <nexus-table-header
          :all-selected="selection.allSelected.value"
          :columns="headerColumns"
          :expandable="expandable"
          :has-actions="!!$slots.actions"
          :indeterminate="selection.indeterminate.value"
          :selectable="selectable"
          :sort="sort"
          @sort="onSort"
          @toggle-all="onToggleAll"
        />
        <tbody>
          <template v-for="item in pagedItems" :key="keyOf(item)">
            <tr class="nexus-data-table__row">
              <td v-if="selectable" class="nexus-data-table__select">
                <v-checkbox-btn
                  :aria-label="$t('table.selectAll')"
                  density="compact"
                  hide-details
                  :model-value="selection.isSelected(keyOf(item))"
                  @update:model-value="selection.toggle(keyOf(item))"
                />
              </td>

              <td v-if="expandable" class="nexus-data-table__expand">
                <v-btn
                  :aria-label="$t('table.expandRow')"
                  density="comfortable"
                  :icon="isExpanded(item) ? 'lucide:chevron-down' : 'lucide:chevron-right'"
                  size="small"
                  variant="text"
                  @click="toggleExpand(item)"
                />
              </td>

              <td
                v-for="column in columns"
                :key="column.key"
                :class="`nexus-data-table__cell nexus-data-table__cell--${column.align ?? 'start'}`"
              >
                <slot
                  :item="item"
                  :name="`col.${column.key}`"
                  :value="item[column.key]"
                >{{ item[column.key] }}</slot>
              </td>

              <td v-if="$slots.actions" class="nexus-data-table__actions">
                <slot name="actions" :item="item" />
              </td>
            </tr>

            <tr v-if="expandable && isExpanded(item)" class="nexus-data-table__expansion">
              <td :colspan="expandColspan">
                <slot name="expand" :item="item" />
              </td>
            </tr>
          </template>
        </tbody>
      </dense-table>

      <table-pagination
        v-if="paginated"
        :items-per-page="itemsPerPage"
        :page="page"
        :total="sortedItems.length"
        @update:items-per-page="onItemsPerPage"
        @update:page="page = $event"
      />
    </template>
  </div>
</template>

<script lang="ts" setup generic="T extends Record<string, any>">
import { computed, ref, watch } from 'vue'

import DenseTable from '@/components/nexus/primitives/DenseTable.vue'
import EmptyState from '@/components/nexus/primitives/EmptyState.vue'
import TableSkeleton from '@/components/nexus/primitives/TableSkeleton.vue'
import { type Column, nextSortState, type SortState, sortItems } from './dataTableColumns'
import NexusTableHeader from './NexusTableHeader.vue'
import { type RowKey, useRowSelection } from './RowSelection'
import TablePagination from './TablePagination.vue'

const ITEMS_PER_PAGE_KEY = 'items-per-page'

const props = withDefaults(defineProps<{
  columns: Column<T>[]
  items: T[]
  rowKey?: (item: T) => RowKey
  loading?: boolean
  selectable?: boolean
  expandable?: boolean
  emptyTitle?: string
  // When false, the table renders every item with no footer pager. Used by
  // server-paginated callers (e.g. Audit) that page through their own cursor.
  paginated?: boolean
}>(), {
  rowKey: undefined,
  selectable: false,
  expandable: false,
  paginated: true,
})

const emit = defineEmits<{
  'update:selected': [keys: RowKey[]]
}>()

const keyOf = (item: T): RowKey => (props.rowKey ? props.rowKey(item) : (item.id as RowKey))

const readItemsPerPage = (): number => {
  const stored = Number(localStorage.getItem(ITEMS_PER_PAGE_KEY))

  return Number.isFinite(stored) && stored > 0 ? stored : 10
}

const sort = ref<SortState | null>(null)
const page = ref(1)
const itemsPerPage = ref(readItemsPerPage())
const expanded = ref<Set<RowKey>>(new Set())

// The header never reads the row generic; widen so the invariant Column<T>
// (its sortValue accessor makes Column invariant in T) assigns to Column[].
const headerColumns = computed(() => props.columns as unknown as Column[])
const sortedItems = computed(() => sortItems(props.items, sort.value, props.columns))
const pagedItems = computed(() => {
  if (!props.paginated) return sortedItems.value

  const start = (page.value - 1) * itemsPerPage.value

  return sortedItems.value.slice(start, start + itemsPerPage.value)
})

const selection = useRowSelection(() => pagedItems.value.map(keyOf))
const skeletonColumns = computed(() =>
  props.columns.length + (props.selectable ? 1 : 0) + (props.expandable ? 1 : 0),
)
const expandColspan = computed(() => skeletonColumns.value + 1)

const onSort = (key: string) => {
  sort.value = nextSortState(sort.value, key)
  page.value = 1
}

const onItemsPerPage = (value: number) => {
  itemsPerPage.value = value
  page.value = 1
  try {
    localStorage.setItem(ITEMS_PER_PAGE_KEY, String(value))
  } catch {
    // Ignore unavailable storage; pagination still works in-memory.
  }
}

const onToggleAll = () => selection.toggleAll()
const isExpanded = (item: T) => expanded.value.has(keyOf(item))
const toggleExpand = (item: T) => {
  const next = new Set(expanded.value)
  const key = keyOf(item)

  if (next.has(key)) next.delete(key)
  else next.add(key)

  expanded.value = next
}

// Clamp the page when the underlying list shrinks (delete/filter).
watch([sortedItems, itemsPerPage], () => {
  const pageCount = Math.max(1, Math.ceil(sortedItems.value.length / itemsPerPage.value))

  if (page.value > pageCount) page.value = pageCount
})

watch(selection.selectedKeys, keys => emit('update:selected', keys))

defineExpose({ clearSelection: selection.clear })
</script>

<style scoped>
.nexus-data-table__row {
  transition: background var(--nexus-transition-fast);
}

.nexus-data-table__row:hover {
  background: var(--nexus-surface-hover);
}

.nexus-data-table__cell--center { text-align: center; }
.nexus-data-table__cell--end { text-align: end; }
.nexus-data-table__actions { text-align: end; white-space: nowrap; }
.nexus-data-table__select,
.nexus-data-table__expand { width: 44px; }

.nexus-data-table__expansion > td {
  background: var(--nexus-surface-0);
}
</style>
