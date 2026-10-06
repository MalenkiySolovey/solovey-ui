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
        if (storage().getItem(reloadKey) === '1') return
        storage().setItem(reloadKey, '1')
      } catch {
        // Keep a fence in this router instance when session storage is disabled.
      }
      reload()
    },
    navigationSucceeded() {
      attempted = false
      try { storage().removeItem(reloadKey) } catch { /* Storage may be disabled. */ }
    },
  }
}
