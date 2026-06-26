import { computed, ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'

vi.mock('@/store/modules/data', () => ({
  default: () => ({ reorder: vi.fn() }),
}))

import { usePendingManualOrder } from './usePendingManualOrder'

describe('usePendingManualOrder', () => {
  it('moves selected rows as one block while preserving their order', () => {
    const rows = ref([
      { id: 1, tag: 'a' },
      { id: 2, tag: 'b' },
      { id: 3, tag: 'c' },
      { id: 4, tag: 'd' },
      { id: 5, tag: 'e' },
    ])
    const order = usePendingManualOrder('outbounds', computed(() => rows.value))

    expect(order.moveManyTo([2, 4], 5)).toBe(true)

    expect(order.displayItems.value.map(item => item.id)).toEqual([1, 3, 5, 2, 4])
  })

  it('does not move a selected block onto itself', () => {
    const rows = ref([
      { id: 1, tag: 'a' },
      { id: 2, tag: 'b' },
      { id: 3, tag: 'c' },
    ])
    const order = usePendingManualOrder('outbounds', computed(() => rows.value))

    expect(order.moveManyTo([1, 2], 2)).toBe(false)

    expect(order.displayItems.value.map(item => item.id)).toEqual([1, 2, 3])
  })
})
