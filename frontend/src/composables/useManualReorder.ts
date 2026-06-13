import Data from '@/store/modules/data'

export type ManualSortDirection = 'asc' | 'desc'

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

export const dragManualOrder = async <T extends Record<string, any>>(
  object: string,
  items: T[],
  draggedId: number,
  targetId: number,
  key = 'id',
): Promise<boolean> => {
  if (draggedId === targetId) return false

  const rows = [...items]
  const from = rows.findIndex(item => Number(item[key]) === draggedId)
  const to = rows.findIndex(item => Number(item[key]) === targetId)

  if (from < 0 || to < 0) return false

  const [item] = rows.splice(from, 1)
  rows.splice(to, 0, item)

  return Data().reorder(object, rows.map(row => Number(row[key])))
}
