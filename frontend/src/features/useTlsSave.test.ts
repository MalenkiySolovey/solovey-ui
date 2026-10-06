import { describe, expect, it, vi } from 'vitest'
import { effectScope, ref } from 'vue'
import { useTlsSave } from './useTlsSave'
const save = vi.hoisted(() => vi.fn())
vi.mock('@/store/modules/data', () => ({ default: () => ({ save }) }))

describe('TLS page persistence owner', () => {
  it('owns pending save, rejects duplicate, and does not close a later dialog', async () => {
    let finish!: (value: boolean) => void
    save.mockReturnValue(new Promise<boolean>(resolve => { finish = resolve }))
    const scope = effectScope()
    const dialog = ref<[boolean, number, string]>([true, 1, 'first'])
    const close = vi.fn()
    const owner = scope.run(() => useTlsSave(() => dialog.value, close))!
    const pending = owner.saveModal({ id: 1, name: 'first' } as any)
    expect(owner.saving.value).toBe(true)
    expect(await owner.saveModal({ id: 1 } as any)).toBe(false)
    dialog.value = [false, 1, 'first']
    dialog.value = [true, 1, 'first']
    finish(true)
    await pending
    expect(close).not.toHaveBeenCalled()
    expect(owner.saving.value).toBe(false)
    save.mockResolvedValueOnce(true)
    await owner.saveModal({ id: 1, name: 'first' } as any)
    expect(close).toHaveBeenCalledOnce()
    save.mockRejectedValueOnce(new Error('programming failure'))
    await expect(owner.saveModal({ id: 1 } as any)).rejects.toThrow('programming failure')
    expect(owner.saving.value).toBe(false)
    scope.stop()
  })
})
