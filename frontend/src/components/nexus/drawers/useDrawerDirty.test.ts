import { describe, expect, it } from 'vitest'
import { reactive } from 'vue'

import { useDrawerDirty } from './useDrawerDirty'

describe('useDrawerDirty', () => {
  it('is clean before a baseline is captured', () => {
    const form = reactive({ tag: 'a' })
    const { dirty } = useDrawerDirty(() => JSON.stringify(form))

    expect(dirty.value).toBe(false)
  })

  it('is clean immediately after reset', () => {
    const form = reactive({ tag: 'a' })
    const { dirty, reset } = useDrawerDirty(() => JSON.stringify(form))

    reset()

    expect(dirty.value).toBe(false)
  })

  it('becomes dirty when a field changes after reset', () => {
    const form = reactive({ tag: 'a', port: 1 })
    const { dirty, reset } = useDrawerDirty(() => JSON.stringify(form))

    reset()
    form.port = 2

    expect(dirty.value).toBe(true)
  })

  it('returns to clean when the value reverts to the baseline', () => {
    const form = reactive({ tag: 'a' })
    const { dirty, reset } = useDrawerDirty(() => JSON.stringify(form))

    reset()
    form.tag = 'b'
    expect(dirty.value).toBe(true)

    form.tag = 'a'
    expect(dirty.value).toBe(false)
  })

  it('re-baselines on a fresh reset (e.g. reopening a different entity)', () => {
    const form = reactive({ tag: 'a' })
    const { dirty, reset } = useDrawerDirty(() => JSON.stringify(form))

    reset()
    form.tag = 'b'
    expect(dirty.value).toBe(true)

    reset()
    expect(dirty.value).toBe(false)
  })
})
