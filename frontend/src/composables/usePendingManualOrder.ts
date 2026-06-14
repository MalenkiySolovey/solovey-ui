import { computed, ref, watch, type ComputedRef } from 'vue'

import Data from '@/store/modules/data'
import { sortRowsByText, type ManualSortDirection } from './useManualReorder'

type RowId = number | string

const idsEqual = (left: RowId[], right: RowId[]) =>
  left.length === right.length && left.every((id, index) => id === right[index])

export const usePendingManualOrder = <T extends Record<string, any>>(
  object: string,
  items: ComputedRef<T[]>,
  idKey = 'id',
) => {
  const pendingIds = ref<RowId[] | null>(null)
  const saving = ref(false)

  const sourceIds = computed<RowId[]>(() => items.value.map(item => item[idKey] as RowId))

  const currentIds = computed<RowId[]>(() => {
    if (!pendingIds.value) return sourceIds.value

    const source = new Set(sourceIds.value)
    const ordered = pendingIds.value.filter(id => source.has(id))
    const orderedSet = new Set(ordered)
    const appended = sourceIds.value.filter(id => !orderedSet.has(id))

    return [...ordered, ...appended]
  })

  const dirty = computed(() => pendingIds.value !== null && !idsEqual(currentIds.value, sourceIds.value))

  const displayItems = computed<T[]>(() => {
    const byId = new Map<RowId, T>()
    items.value.forEach(item => byId.set(item[idKey] as RowId, item))

    return currentIds.value
      .map(id => byId.get(id))
      .filter((item): item is T => item !== undefined)
  })

  const setOrder = (rows: T[]) => {
    pendingIds.value = rows.map(row => row[idKey] as RowId)
  }

  const move = (id: RowId, dir: number): boolean => {
    const rows = [...displayItems.value]
    const index = rows.findIndex(item => item[idKey] === id)
    const target = index + dir

    if (index < 0 || target < 0 || target >= rows.length) return false

    const [item] = rows.splice(index, 1)
    rows.splice(target, 0, item)
    setOrder(rows)
    return true
  }

  const moveTo = (draggedId: RowId, targetId: RowId): boolean => {
    if (draggedId === targetId) return false

    const rows = [...displayItems.value]
    const from = rows.findIndex(item => item[idKey] === draggedId)
    const to = rows.findIndex(item => item[idKey] === targetId)

    if (from < 0 || to < 0) return false

    const [item] = rows.splice(from, 1)
    rows.splice(to, 0, item)
    setOrder(rows)
    return true
  }

  const sortByText = (direction: ManualSortDirection, textKey = 'tag'): boolean => {
    if (displayItems.value.length < 2) return false
    setOrder(sortRowsByText(displayItems.value, direction, textKey))
    return true
  }

  const reset = () => {
    pendingIds.value = null
  }

  const save = async (): Promise<boolean> => {
    if (!dirty.value || saving.value) return false

    saving.value = true
    try {
      const success = await Data().reorder(object, currentIds.value)
      if (success) reset()
      return success
    } finally {
      saving.value = false
    }
  }

  watch(sourceIds, (ids) => {
    if (!pendingIds.value) return

    const source = new Set(ids)
    const ordered = pendingIds.value.filter(id => source.has(id))
    const orderedSet = new Set(ordered)
    const appended = ids.filter(id => !orderedSet.has(id))
    pendingIds.value = [...ordered, ...appended]
  })

  return {
    dirty,
    displayItems,
    move,
    moveTo,
    reset,
    save,
    saving,
    sortByText,
  }
}
