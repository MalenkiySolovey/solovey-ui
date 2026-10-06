type RecoveryStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>
const reloadKey = 'sui:preload-error-reload'

export const isPreloadError = (error: unknown): boolean => {
  if (!error) return false
  const value = error as { message?: unknown; name?: unknown }
  const message = typeof value.message === 'string' ? value.message : String(error)
  return [
    'Failed to fetch dynamically imported module',
    'Importing a module script failed',
    'Failed to load module script',
    'error loading dynamically imported module',
  ].some(form => message.toLowerCase().includes(form.toLowerCase())) || value.name === 'ChunkLoadError'
}

export const createPreloadRecovery = (reload: () => void, storage: () => RecoveryStorage) => {
  let attempted = false
  return {
    reloadOnce() {
      if (attempted) return
      attempted = true
      try {
        const session = storage()
        if (session.getItem(reloadKey) === '1') return
        session.setItem(reloadKey, '1')
        if (session.getItem(reloadKey) !== '1') return
      } catch {
        // A memory-only fence would disappear on reload and permit a loop.
        return
      }
      reload()
    },
    navigationSucceeded() {
      attempted = false
      try { storage().removeItem(reloadKey) } catch { /* Storage may be disabled. */ }
    },
  }
}
