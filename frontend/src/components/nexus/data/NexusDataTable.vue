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
            <tr
              class="nexus-data-table__row"
              :class="{ 'nexus-data-table__row--draggable': draggableRows && !dragDisabled }"
              :draggable="false"
              @pointerdown="onRowPointerDown"
              @dragstart="onRowDragStart($event, item)"
              @dragover="onRowDragOver"
              @drop="onRowDrop($event, item)"
              @dragend="onRowDragEnd"
            >
              <td v-if="selectable" class="nexus-data-table__select">
                <v-checkbox-btn
                  :aria-label="$t('table.selectRow')"
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
                <span class="nexus-data-table__cell-content manual-drag-no-drag">
                  <slot
                    :item="item"
                    :name="`col.${column.key}`"
                    :value="item[column.key]"
                  >{{ item[column.key] }}</slot>
                </span>
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
import { computed, ref, useSlots, watch } from 'vue'

import { canStartManualDrag, prepareManualDrag } from '@/composables/useManualDrag'
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
  // Namespaces the persisted rows-per-page preference so each table keeps its
  // own size. When omitted, the legacy shared key is used (back-compatible).
  storageKey?: string
  draggableRows?: boolean
  dragDisabled?: boolean
}>(), {
  rowKey: undefined,
  selectable: false,
  expandable: false,
  paginated: true,
  draggableRows: false,
  dragDisabled: false,
})

const emit = defineEmits<{
  'update:selected': [keys: RowKey[]]
  'row-drop': [dragged: T, target: T]
}>()

const keyOf = (item: T): RowKey => (props.rowKey ? props.rowKey(item) : (item.id as RowKey))

// Per-table storage key: namespaced by `storageKey` when provided, else the
// legacy shared key (so existing installs keep their saved size).
const itemsPerPageStorageKey = (): string =>
  props.storageKey ? `${ITEMS_PER_PAGE_KEY}:${props.storageKey}` : ITEMS_PER_PAGE_KEY

const readItemsPerPage = (): number => {
  const stored = Number(localStorage.getItem(itemsPerPageStorageKey()))

  return Number.isFinite(stored) && stored > 0 ? stored : 10
}

const sort = ref<SortState | null>(null)
const page = ref(1)
const itemsPerPage = ref(readItemsPerPage())
const expanded = ref<Set<RowKey>>(new Set())
const draggedRow = ref<T | null>(null)

// The header never reads the row generic; widen so the invariant Column<T>
// (its sortValue accessor makes Column invariant in T) assigns to Column[].
const headerColumns = computed(() => {
  const columns = props.draggableRows
    ? props.columns.map(column => ({ ...column, sortable: false }))
    : props.columns

  return columns as unknown as Column[]
})
const sortedItems = computed(() =>
  props.draggableRows ? [...props.items] : sortItems(props.items, sort.value, props.columns),
)
const pagedItems = computed(() => {
  if (!props.paginated) return sortedItems.value

  const start = (page.value - 1) * itemsPerPage.value

  return sortedItems.value.slice(start, start + itemsPerPage.value)
})

const selection = useRowSelection(() => pagedItems.value.map(keyOf))
const skeletonColumns = computed(() =>
  props.columns.length + (props.selectable ? 1 : 0) + (props.expandable ? 1 : 0),
)
// The trailing actions <td> is rendered only when an `actions` slot is provided
// (see template), so the expansion row must span columns + selection/expand
// toggles + the actions column only when it actually exists.
const slots = useSlots()
const expandColspan = computed(() => skeletonColumns.value + (slots.actions ? 1 : 0))

const onSort = (key: string) => {
  if (props.draggableRows) return
  sort.value = nextSortState(sort.value, key)
  page.value = 1
}

const onItemsPerPage = (value: number) => {
  itemsPerPage.value = value
  page.value = 1
  try {
    localStorage.setItem(itemsPerPageStorageKey(), String(value))
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

const onRowPointerDown = (event: PointerEvent) => {
  prepareManualDrag(event, !props.draggableRows || props.dragDisabled)
}

const onRowDragStart = (event: DragEvent, item: T) => {
  if (!props.draggableRows || props.dragDisabled || !canStartManualDrag(event)) {
    event.preventDefault()
    return
  }
  draggedRow.value = item as any
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('text/plain', String(keyOf(item)))
  }
}

const onRowDragOver = (event: DragEvent) => {
  if (!props.draggableRows || props.dragDisabled || draggedRow.value == null) return
  event.preventDefault()
  if (event.dataTransfer) event.dataTransfer.dropEffect = 'move'
}

const onRowDrop = (event: DragEvent, target: T) => {
  if (!props.draggableRows || props.dragDisabled || draggedRow.value == null) return
  event.preventDefault()
  const source = draggedRow.value as T
  draggedRow.value = null
  if (keyOf(source) === keyOf(target)) return
  emit('row-drop', source, target)
}

const onRowDragEnd = () => {
  draggedRow.value = null
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

.nexus-data-table__row--draggable {
  cursor: grab;
}

.nexus-data-table__row--draggable:active {
  cursor: grabbing;
}

.nexus-data-table__row:hover {
  background: var(--nexus-surface-hover);
}

.nexus-data-table__cell--center { text-align: center; }
.nexus-data-table__cell--end { text-align: end; }
.nexus-data-table__cell-content {
  cursor: text;
  display: inline-block;
  max-width: 100%;
  user-select: text;
}
.nexus-data-table__actions { text-align: end; white-space: nowrap; }
.nexus-data-table__select,
.nexus-data-table__expand { width: 44px; }

.nexus-data-table__expansion > td {
  background: var(--nexus-surface-0);
}
</style>
