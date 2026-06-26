import { describe, expect, it, vi } from 'vitest'

vi.mock('@/store/modules/data', () => ({
  default: () => ({ reorder: vi.fn() }),
}))

import {
  moveArrayItemKeepingSelection,
  moveManyArrayItems,
  moveManyArrayItemsKeepingSelection,
  moveRowsTo,
  moveRowsToPosition,
  removeArrayItemKeepingSelection,
} from './manualReorder'

describe('manualReorder', () => {
  it('moves selected rows as one block by id', () => {
    const rows = [
      { id: 1, tag: 'a' },
      { id: 2, tag: 'b' },
      { id: 3, tag: 'c' },
      { id: 4, tag: 'd' },
      { id: 5, tag: 'e' },
    ]

    const next = moveRowsTo(rows, [2, 4], 5)

    expect(next?.map(row => row.id)).toEqual([1, 3, 5, 2, 4])
    expect(rows.map(row => row.id)).toEqual([1, 2, 3, 4, 5])
  })

  it('moves selected array indexes as one block', () => {
    const rows = ['a', 'b', 'c', 'd', 'e']

    expect(moveManyArrayItems(rows, [1, 3], 4)).toBe(true)

    expect(rows).toEqual(['a', 'c', 'e', 'b', 'd'])
  })

  it('moves selected rows before or after the explicit drop target', () => {
    const rows = [
      { id: 1, tag: 'a' },
      { id: 2, tag: 'b' },
      { id: 3, tag: 'c' },
      { id: 4, tag: 'd' },
    ]

    expect(moveRowsToPosition(rows, [1], 3, 'before')?.map(row => row.id)).toEqual([2, 1, 3, 4])
    expect(moveRowsToPosition(rows, [1], 3, 'after')?.map(row => row.id)).toEqual([2, 3, 1, 4])
  })

  it('keeps index selection when moving a block to an explicit side', () => {
    const rows = [{ tag: 'a' }, { tag: 'b' }, { tag: 'c' }, { tag: 'd' }, { tag: 'e' }]

    const result = moveManyArrayItemsKeepingSelection(rows, [1, 3], 2, [1, 4], 'before')

    expect(result.moved).toBe(true)
    expect(rows.map(row => row.tag)).toEqual(['a', 'b', 'd', 'c', 'e'])
    expect(result.selectedIndexes).toEqual([1, 4])
  })

  it('keeps index selection on the same object after a single row move', () => {
    const rows = [{ tag: 'a' }, { tag: 'b' }, { tag: 'c' }]

    const result = moveArrayItemKeepingSelection(rows, 0, 2, [0, 2])

    expect(result.moved).toBe(true)
    expect(rows.map(row => row.tag)).toEqual(['b', 'c', 'a'])
    expect(result.selectedIndexes).toEqual([2, 1])
  })

  it('keeps index selection on the same objects after a multi-row move', () => {
    const rows = [{ tag: 'a' }, { tag: 'b' }, { tag: 'c' }, { tag: 'd' }, { tag: 'e' }]

    const result = moveManyArrayItemsKeepingSelection(rows, [1, 3], 4, [1, 4])

    expect(result.moved).toBe(true)
    expect(rows.map(row => row.tag)).toEqual(['a', 'c', 'e', 'b', 'd'])
    expect(result.selectedIndexes).toEqual([3, 2])
  })

  it('drops a removed row from index selection and remaps the rest', () => {
    const rows = [{ tag: 'a' }, { tag: 'b' }, { tag: 'c' }]

    const result = removeArrayItemKeepingSelection(rows, 1, [1, 2])

    expect(result.moved).toBe(true)
    expect(rows.map(row => row.tag)).toEqual(['a', 'c'])
    expect(result.selectedIndexes).toEqual([1])
  })
})
