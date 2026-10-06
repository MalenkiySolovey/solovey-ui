import { onScopeDispose, ref, watch } from 'vue'
import Data from '@/store/modules/data'
import type { tls } from '@/types/tls'

// The TLS page owns persistence. An emitted event is not an awaited save.
export function useTlsSave(dialog: () => readonly [boolean, number, string], close: () => void) {
  const saving = ref(false)
  let generation = 0
  let disposed = false
  watch(dialog, () => { generation++ }, { flush: 'sync' })
  onScopeDispose(() => { disposed = true; generation++ })
  const saveModal = async (data: tls) => {
    if (saving.value || disposed) return false
    const current = generation
    const wasVisible = dialog()[0]
    saving.value = true
    try {
      const success = await Data().save('tls', data.id > 0 ? 'edit' : 'new', data)
      if (success && wasVisible && current === generation) close()
      return success
    } finally { saving.value = false }
  }
  return { saving, saveModal }
}
