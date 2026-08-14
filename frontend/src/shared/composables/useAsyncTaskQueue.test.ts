import { describe, expect, it, vi } from 'vitest'

import { useAsyncTaskQueue } from './useAsyncTaskQueue'

describe('useAsyncTaskQueue', () => {
  it('does not retain completed task keys', async () => {
    const queue = useAsyncTaskQueue()
    const task = vi.fn(async () => 'done')

    await expect(queue.runOne('temporary-outbound', task)).resolves.toBe('done')

    expect(task).toHaveBeenCalledTimes(1)
    expect(queue.active).toEqual({})
  })
})
