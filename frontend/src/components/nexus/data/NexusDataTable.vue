<template>
  <div class="nexus-data-table">
    <table-skeleton v-if="loading" :columns="skeletonColumns" :rows="6" />

    <div v-else-if="sortedItems.length === 0" class="nexus-data-table__empty">
      <slot v-if="$slots.empty" name="empty" />
      <empty-state v-else :title="emptyTitle ?? $t('table.noData')" />
    </div>

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
              :class="{
                'nexus-data-table__row--draggable': draggableRows && !dragDisabled,
                'nexus-data-table__row--selected': selection.isSelected(keyOf(item)),
                'nexus-data-table__row--drop-before': dropIndicator?.key === keyOf(item) && dropIndicator.position === 'before',
                'nexus-data-table__row--drop-after': dropIndicator?.key === keyOf(item) && dropIndicator.position === 'after',
              }"
              :draggable="false"
              @pointerdown="onRowPointerDown"
              @dragstart="onRowDragStart($event, item)"
              @dragover="onRowDragOver($event, item)"
              @dragleave="onRowDragLeave($event, item)"
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
                :data-column-key="column.key"
              >
                <span class="nexus-data-table__cell-content manual-drag-no-drag">
                  <slot
                    :item="item"
                    :name="`col.${column.key}`"
                    :value="item[column.key]"
                  >{{ item[column.key] }}</slot>
                </span>
              </td>

              <td v-if="$slots.actions" class="nexus-data-table__actions" data-column-key="actions">
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

import { canStartManualDrag, manualDropIndicatorFor, manualDropPositionFromEvent, prepareManualDrag, type ManualDropPosition } from '@/shared/composables/dragSelection/manualDrag'
import DenseTable from '@/components/nexus/primitives/DenseTable.vue'
import EmptyState from '@/components/nexus/primitives/EmptyState.vue'
import TableSkeleton from '@/components/nexus/primitives/TableSkeleton.vue'
import { type Column, nextSortState, type SortState, sortItems } from './dataTableColumns'
import NexusTableHeader from './NexusTableHeader.vue'
import { type RowKey, useRowSelection } from '@/shared/composables/dragSelection/rowSelection'
import TablePagination from './TablePagination.vue'

const ITEMS_PER_PAGE_KEY = 'items-per-page'

const props = withDefaults(defineProps<{
  columns: Column<T>[]
  items: T[]
  rowKey?: (item: T) => RowKey
  loading?: boolean
  selectable?: boolean
  selected?: RowKey[]
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
  'row-drop': [dragged: T, target: T, position: ManualDropPosition | null]
  'rows-drop': [dragged: T[], target: T, position: ManualDropPosition | null]
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
const dropIndicator = ref<{ key: RowKey; position: 'before' | 'after' } | null>(null)

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

const onRowDragOver = (event: DragEvent, target: T) => {
  if (!props.draggableRows || props.dragDisabled || draggedRow.value == null) return
  event.preventDefault()
  if (event.dataTransfer) event.dataTransfer.dropEffect = 'move'
  const nextIndicator = dropIndicatorForEvent(event, target)
  if (sameDropIndicator(dropIndicator.value, nextIndicator)) return
  dropIndicator.value = nextIndicator
}

const onRowDragLeave = (event: DragEvent, item: T) => {
  const current = event.currentTarget
  const related = event.relatedTarget
  if (current instanceof HTMLElement && related instanceof Node && current.contains(related)) return
  if (draggedRow.value != null) return
  if (dropIndicator.value?.key === keyOf(item)) dropIndicator.value = null
}

const onRowDrop = (event: DragEvent, target: T) => {
  if (!props.draggableRows || props.dragDisabled || draggedRow.value == null) return
  event.preventDefault()
  const source = draggedRow.value as T
  const activeDrop = dropIndicator.value
  if (!activeDrop) {
    draggedRow.value = null
    dropIndicator.value = null
    return
  }
  const effectiveTarget = sortedItems.value.find(item => String(keyOf(item)) === String(activeDrop.key)) ?? target
  const position = activeDrop?.position ?? null
  draggedRow.value = null
  dropIndicator.value = null
  if (keyOf(source) === keyOf(effectiveTarget)) return
  if (props.selectable && selection.isSelected(keyOf(source))) {
    const selectedRows = sortedItems.value.filter(item => selection.isSelected(keyOf(item)))
    if (selectedRows.length > 1) {
      emit('rows-drop', selectedRows, effectiveTarget, position)
      return
    }
  }
  emit('row-drop', source, effectiveTarget, position)
}

const onRowDragEnd = () => {
  draggedRow.value = null
  dropIndicator.value = null
}

// Clamp the page when the underlying list shrinks (delete/filter).
watch([sortedItems, itemsPerPage], () => {
  const pageCount = Math.max(1, Math.ceil(sortedItems.value.length / itemsPerPage.value))

  if (page.value > pageCount) page.value = pageCount
})

watch(selection.selectedKeys, keys => emit('update:selected', keys))

watch(() => props.selected, (keys) => {
  if (!keys) return
  if (rowKeysEqual(keys, selection.selectedKeys.value)) return
  selection.replace(new Set(keys))
}, { immediate: true })

watch(() => props.selectable, (selectable) => {
  if (!selectable) selection.clear()
})

watch(sortedItems, () => {
  dropIndicator.value = null
})

defineExpose({ clearSelection: selection.clear })

function dropIndicatorForEvent(event: DragEvent, target: T): { key: RowKey; position: ManualDropPosition } | null {
  const position = manualDropPositionFromEvent(event, 'vertical')
  if (!position) return null

  const rows = sortedItems.value
  const targetIndex = rows.findIndex(item => String(keyOf(item)) === String(keyOf(target)))
  if (position === 'after' && targetIndex >= 0 && targetIndex < rows.length - 1) {
    return dropIndicatorFor(rows[targetIndex + 1], 'before')
  }

  return dropIndicatorFor(target, position)
}

function dropIndicatorFor(target: T, position: ManualDropPosition): { key: RowKey; position: ManualDropPosition } | null {
  const source = draggedRow.value
  if (!source) return null

  const targetKey = keyOf(target)
  const sourceKey = keyOf(source)
  const rows = sortedItems.value
  const orderedKeys = rows.map(keyOf)

  if (props.selectable && selection.isSelected(sourceKey)) {
    const indicator = manualDropIndicatorFor(sourceKey, targetKey, orderedKeys, rows.filter(item => selection.isSelected(keyOf(item))).map(keyOf), position)
    return indicator ? { key: indicator.target, position: indicator.position } : null
  }

  const indicator = manualDropIndicatorFor(sourceKey, targetKey, orderedKeys, [], position)
  return indicator ? { key: indicator.target, position: indicator.position } : null
}

function rowKeysEqual(a: readonly RowKey[], b: readonly RowKey[]): boolean {
  if (a.length !== b.length) return false
  for (let index = 0; index < a.length; index++) {
    if (String(a[index]) !== String(b[index])) return false
  }
  return true
}

function sameDropIndicator(
  left: { key: RowKey; position: ManualDropPosition } | null,
  right: { key: RowKey; position: ManualDropPosition } | null,
): boolean {
  if (!left || !right) return left === right
  return String(left.key) === String(right.key) && left.position === right.position
}
</script>

<style scoped src="./NexusDataTable.scss"></style>
