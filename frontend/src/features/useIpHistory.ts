import HttpUtils from '@/plugins/httputil'
import type { ClientIPHistoryRow } from '@/components/security/ipHistory'
import { onScopeDispose, ref, watch } from 'vue'

export const fetchClientIPHistory = async (client: string, signal?: AbortSignal): Promise<ClientIPHistoryRow[] | undefined> => {
  const response = await HttpUtils.get(`api/ip-monitor/${encodeURIComponent(client)}`, {}, { signal })
  return response.success ? (response.obj ?? []) as ClientIPHistoryRow[] : undefined
}

export const clearClientIPHistory = async (client: string, signal?: AbortSignal): Promise<boolean> => {
  const response = await HttpUtils.post(`api/ip-monitor/${encodeURIComponent(client)}/clear`, {}, { signal })
  return response.success
}

// This lifecycle belongs to the IP-history feature. A clear and a load share
// the same request fence so neither can apply to a later client or dialog.
export const useIpHistory = (context: () => readonly [boolean, string], cleared: () => void) => {
  const rows = ref<ClientIPHistoryRow[]>([])
  const loading = ref(false)
  let entityGeneration = 0
  let requestGeneration = 0
  let controller: AbortController | undefined

  const cancel = () => {
    entityGeneration++
    requestGeneration++
    controller?.abort()
    controller = undefined
    rows.value = []
    loading.value = false
  }

  const run = async (clear: boolean) => {
    const [visible, client] = context()
    if (!visible || !client) return
    controller?.abort()
    const operation = new AbortController()
    controller = operation
    const entity = entityGeneration
    const request = ++requestGeneration
    const current = () => entity === entityGeneration && request === requestGeneration &&
      controller === operation && !operation.signal.aborted
    loading.value = true
    try {
      if (clear) {
        const success = await clearClientIPHistory(client, operation.signal)
        if (current() && success) {
          rows.value = []
          cleared()
        }
      } else {
        const history = await fetchClientIPHistory(client, operation.signal)
        if (current() && history) rows.value = history
      }
    } catch (error) {
      // HttpUtils owns current network errors. A throwing current operation is
      // still a programming error; stale/cancelled work no longer owns this UI.
      if (current()) throw error
    } finally {
      if (current()) {
        loading.value = false
        controller = undefined
      }
    }
  }

  watch(context, () => {
    cancel()
    return run(false)
  }, { immediate: true, flush: 'sync' })
  onScopeDispose(cancel)
  return { rows, loading, cancel, reload: () => run(false), clearHistory: () => run(true) }
}
