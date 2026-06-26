import Data from '@/store/modules/data'
import type { ManualDropPosition } from './manualDrag'

export type ManualSortDirection = 'asc' | 'desc'
export type ArraySelectionKey = number | string

export interface ArrayMoveSelectionResult {
  moved: boolean
  selectedIndexes: number[]
}

export const sortRowsByText = <T extends Record<string, any>>(
  items: T[],
  direction: ManualSortDirection,
  key = 'tag',
): T[] => {
  const factor = direction === 'asc' ? 1 : -1

  return items
    .map((item, index) => ({ item, index }))
    .sort((a, b) => {
      const result = String(a.item[key] ?? '').localeCompare(String(b.item[key] ?? ''), undefined, {
        numeric: true,
        sensitivity: 'base',
      })

      return result === 0 ? a.index - b.index : result * factor
    })
    .map(row => row.item)
}

export const moveArrayItem = <T>(items: T[], from: number, to: number): boolean => {
  if (from < 0 || to < 0 || from >= items.length || to >= items.length || from === to) return false
  const [item] = items.splice(from, 1)
  items.splice(to, 0, item)
  return true
}

export const moveArrayItemToPosition = <T>(
  items: T[],
  from: number,
  target: number,
  position: ManualDropPosition | null,
): boolean => {
  if (!position) return moveArrayItem(items, from, target)
  if (from < 0 || target < 0 || from >= items.length || target >= items.length || from === target) return false

  const [item] = items.splice(from, 1)
  const targetAfterRemoval = target > from ? target - 1 : target
  const insertAt = position === 'after' ? targetAfterRemoval + 1 : targetAfterRemoval
  items.splice(insertAt, 0, item)
  return true
}

export const moveManualOrder = async <T extends Record<string, any>>(
  object: string,
  items: T[],
  id: number,
  dir: number,
  key = 'id',
): Promise<boolean> => {
  const rows = [...items]
  const index = rows.findIndex(item => Number(item[key]) === id)
  const target = index + dir

  if (index < 0 || target < 0 || target >= rows.length) return false

  const [item] = rows.splice(index, 1)
  rows.splice(target, 0, item)

  return Data().reorder(object, rows.map(row => Number(row[key])))
}

export const moveRowsTo = <T extends Record<string, any>>(
  items: T[],
  draggedIds: Array<number | string>,
  targetId: number | string,
  key = 'id',
): T[] | null => {
  const selected = new Set(draggedIds.map(String))
  if (selected.size === 0 || selected.has(String(targetId))) return null

  const rows = [...items]
  const targetOriginalIndex = rows.findIndex(item => String(item[key]) === String(targetId))
  const firstSelectedIndex = rows.findIndex(item => selected.has(String(item[key])))
  if (targetOriginalIndex < 0 || firstSelectedIndex < 0) return null

  const moving = rows.filter(item => selected.has(String(item[key])))
  if (moving.length === 0) return null

  const remaining = rows.filter(item => !selected.has(String(item[key])))
  const targetIndex = remaining.findIndex(item => String(item[key]) === String(targetId))
  if (targetIndex < 0) return null

  const insertAt = firstSelectedIndex < targetOriginalIndex ? targetIndex + 1 : targetIndex
  remaining.splice(insertAt, 0, ...moving)
  return remaining
}

export const moveRowsToPosition = <T extends Record<string, any>>(
  items: T[],
  draggedIds: Array<number | string>,
  targetId: number | string,
  position: ManualDropPosition | null,
  key = 'id',
): T[] | null => {
  if (!position) return moveRowsTo(items, draggedIds, targetId, key)

  const selected = new Set(draggedIds.map(String))
  if (selected.size === 0 || selected.has(String(targetId))) return null

  const remaining = items.filter(item => !selected.has(String(item[key])))
  const targetIndex = remaining.findIndex(item => String(item[key]) === String(targetId))
  if (targetIndex < 0) return null

  const moving = items.filter(item => selected.has(String(item[key])))
  if (moving.length === 0) return null

  const insertAt = position === 'after' ? targetIndex + 1 : targetIndex
  remaining.splice(insertAt, 0, ...moving)
  return remaining
}

export const moveManyManualOrder = async <T extends Record<string, any>>(
  object: string,
  items: T[],
  draggedIds: Array<number | string>,
  targetId: number | string,
  key = 'id',
  position: ManualDropPosition | null = null,
): Promise<boolean> => {
  const rows = moveRowsToPosition(items, draggedIds, targetId, position, key)
  if (!rows) return false

  return Data().reorder(object, rows.map(row => Number(row[key])))
}

export const sortManualOrderByText = async <T extends Record<string, any>>(
  object: string,
  items: T[],
  direction: ManualSortDirection,
  textKey = 'tag',
  idKey = 'id',
): Promise<boolean> => {
  if (items.length < 2) return false

  const rows = sortRowsByText(items, direction, textKey)

  return Data().reorder(object, rows.map(row => Number(row[idKey])))
}

export const sortArrayByText = <T extends Record<string, any>>(
  items: T[],
  direction: ManualSortDirection,
  textKey = 'tag',
): boolean => {
  if (items.length < 2) return false

  const rows = sortRowsByText(items, direction, textKey)
  items.splice(0, items.length, ...rows)
  return true
}

export const moveManyArrayItems = <T>(
  items: T[],
  draggedIndices: Array<number | string>,
  targetIndex: number | string,
  position: ManualDropPosition | null = null,
): boolean => {
  const rows = moveRowsToPosition(
    items.map((item, index) => ({ item, index })),
    draggedIndices,
    targetIndex,
    position,
    'index',
  )
  if (!rows) return false

  items.splice(0, items.length, ...rows.map(row => row.item))
  return true
}

export const dragManualOrder = async <T extends Record<string, any>>(
  object: string,
  items: T[],
  draggedId: number,
  targetId: number,
  key = 'id',
  position: ManualDropPosition | null = null,
): Promise<boolean> => {
  const rows = moveRowsToPosition(items, [draggedId], targetId, position, key)
  if (!rows) return false

  return Data().reorder(object, rows.map(row => Number(row[key])))
}

const selectedItemsByIndex = <T>(items: T[], selectedIndexes: ArraySelectionKey[]): T[] => {
  const indexes = new Set(selectedIndexes.map(Number).filter(Number.isInteger))

  return items.filter((_, index) => indexes.has(index))
}

const indexesForSelectedItems = <T>(items: T[], selectedItems: T[]): number[] => {
  const used = new Set<number>()
  const next: number[] = []

  for (const selectedItem of selectedItems) {
    const index = items.findIndex((item, candidate) => !used.has(candidate) && Object.is(item, selectedItem))
    if (index < 0) continue
    used.add(index)
    next.push(index)
  }

  return next
}

export const moveArrayItemKeepingSelection = <T>(
  items: T[],
  from: number,
  to: number,
  selectedIndexes: ArraySelectionKey[],
  position: ManualDropPosition | null = null,
): ArrayMoveSelectionResult => {
  const selectedItems = selectedItemsByIndex(items, selectedIndexes)
  const moved = moveArrayItemToPosition(items, from, to, position)

  return {
    moved,
    selectedIndexes: moved ? indexesForSelectedItems(items, selectedItems) : selectedIndexes.map(Number),
  }
}

export const moveManyArrayItemsKeepingSelection = <T>(
  items: T[],
  draggedIndices: ArraySelectionKey[],
  targetIndex: ArraySelectionKey,
  selectedIndexes: ArraySelectionKey[],
  position: ManualDropPosition | null = null,
): ArrayMoveSelectionResult => {
  const selectedItems = selectedItemsByIndex(items, selectedIndexes)
  const moved = moveManyArrayItems(items, draggedIndices, targetIndex, position)

  return {
    moved,
    selectedIndexes: moved ? indexesForSelectedItems(items, selectedItems) : selectedIndexes.map(Number),
  }
}

export const removeArrayItemKeepingSelection = <T>(
  items: T[],
  index: number,
  selectedIndexes: ArraySelectionKey[],
): ArrayMoveSelectionResult => {
  if (index < 0 || index >= items.length) {
    return { moved: false, selectedIndexes: selectedIndexes.map(Number) }
  }

  const removedItem = items[index]
  const selectedItems = selectedItemsByIndex(items, selectedIndexes)
    .filter(item => !Object.is(item, removedItem))
  items.splice(index, 1)

  return { moved: true, selectedIndexes: indexesForSelectedItems(items, selectedItems) }
}

export const sortArrayByTextKeepingSelection = <T extends Record<string, any>>(
  items: T[],
  direction: ManualSortDirection,
  selectedIndexes: ArraySelectionKey[],
  textKey = 'tag',
): ArrayMoveSelectionResult => {
  const selectedItems = selectedItemsByIndex(items, selectedIndexes)
  const moved = sortArrayByText(items, direction, textKey)

  return {
    moved,
    selectedIndexes: moved ? indexesForSelectedItems(items, selectedItems) : selectedIndexes.map(Number),
  }
}
