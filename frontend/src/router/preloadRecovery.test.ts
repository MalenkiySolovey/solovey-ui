import { describe, expect, it, vi } from 'vitest'
import { createPreloadRecovery, isPreloadError } from './preloadRecovery'

describe('bounded browser preload recovery', () => {
  it.each([
    new TypeError('Failed to fetch dynamically imported module: /assets/old.js'),
    new TypeError('error loading dynamically imported module: https://panel.test/assets/old.js'),
    new TypeError('Importing a module script failed.'),
    new Error('Failed to load module script: old chunk'),
    { name: 'ChunkLoadError', message: 'missing chunk' },
  ])('recognizes a known module failure %s', error => { expect(isPreloadError(error)).toBe(true) })
  it.each([undefined, new Error('navigation cancelled'), new TypeError('Cannot read properties of undefined'), new Error('Network Error')])('rejects unrelated errors %s', error => { expect(isPreloadError(error)).toBe(false) })
  it('retains one reload across router instances until navigation succeeds', () => {
    const values = new Map<string, string>()
    const storage = { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value) }, removeItem: (key: string) => { values.delete(key) } }
    const reload = vi.fn()
    const first = createPreloadRecovery(reload, () => storage)
    first.reloadOnce(); first.reloadOnce()
    const afterReload = createPreloadRecovery(reload, () => storage)
    afterReload.reloadOnce()
    expect(reload).toHaveBeenCalledTimes(1)
    afterReload.navigationSucceeded()
    afterReload.reloadOnce()
    expect(reload).toHaveBeenCalledTimes(2)
  })
  it('requires a persistent fence across new routers when session storage is unavailable', () => {
    const reload = vi.fn()
    const storage = () => { throw new Error('storage disabled') }
    const recovery = createPreloadRecovery(reload, storage)
    recovery.reloadOnce(); recovery.reloadOnce()
    createPreloadRecovery(reload, storage).reloadOnce()
    recovery.navigationSucceeded()
    recovery.reloadOnce()
    expect(reload).not.toHaveBeenCalled()
  })
  it('does not reload if a marker write is silently lost', () => {
    const reload = vi.fn()
    const storage = { getItem: () => null, setItem: () => {}, removeItem: () => {} }
    createPreloadRecovery(reload, () => storage).reloadOnce()
    createPreloadRecovery(reload, () => storage).reloadOnce()
    expect(reload).not.toHaveBeenCalled()
  })
})
