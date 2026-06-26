import { describe, expect, it } from 'vitest'

import { manualDropIndicatorFor } from './manualDrag'

describe('manualDrag', () => {
  it('does not show a drop indicator for a no-op single item drop beside itself', () => {
    const keys = [1, 2, 3, 4]

    expect(manualDropIndicatorFor(2, 3, keys, [], 'before')).toBeNull()
    expect(manualDropIndicatorFor(3, 2, keys, [], 'after')).toBeNull()
  })

  it('treats a one-item selection as a single item drag', () => {
    const keys = [1, 2, 3, 4]

    expect(manualDropIndicatorFor(2, 3, keys, [2], 'before')).toBeNull()
  })

  it('keeps valid indicators for real single item moves', () => {
    const keys = [1, 2, 3, 4]

    expect(manualDropIndicatorFor(2, 4, keys, [], 'before')).toEqual({ target: 4, position: 'before' })
    expect(manualDropIndicatorFor(3, 1, keys, [], 'after')).toEqual({ target: 1, position: 'after' })
  })

  it('allows multi-item drops beside the selected block boundary', () => {
    const keys = [1, 2, 3, 4]

    expect(manualDropIndicatorFor(2, 4, keys, [2, 3], 'before')).toEqual({ target: 4, position: 'before' })
    expect(manualDropIndicatorFor(3, 1, keys, [2, 3], 'after')).toEqual({ target: 1, position: 'after' })
  })
})
