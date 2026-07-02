import { computed, ref, watch, type ComputedRef } from 'vue'

export type BulkSelectionId = number | string

export const useBulkSelection = <T>(
  items: ComputedRef<T[]>,
  keyOf: (item: T) => BulkSelectionId = (item: any) => item.id,
) => {
  const active = ref(false)
  const selectedIds = ref<BulkSelectionId[]>([])

  const selectedSet = computed(() => new Set(selectedIds.value.map(String)))
  const selectedItems = computed(() => items.value.filter(item => selectedSet.value.has(String(keyOf(item)))))
  const selectedCount = computed(() => selectedItems.value.length)

  const clear = () => {
    selectedIds.value = []
  }

  const toggleActive = () => {
    active.value = !active.value
    if (!active.value) clear()
  }

  const isSelected = (id: BulkSelectionId) => selectedSet.value.has(String(id))

  const toggle = (id: BulkSelectionId, selected?: boolean) => {
    const next = new Set(selectedIds.value.map(String))
    const key = String(id)
    const checked = selected ?? !next.has(key)

    if (checked) next.add(key)
    else next.delete(key)

    selectedIds.value = [...next]
  }

  watch(items, (rows) => {
    const live = new Set(rows.map(item => String(keyOf(item))))
    selectedIds.value = selectedIds.value.filter(id => live.has(String(id)))
  })

  return {
    active,
    clear,
    isSelected,
    selectedCount,
    selectedIds,
    selectedItems,
    selectedSet,
    toggle,
    toggleActive,
  }
}
