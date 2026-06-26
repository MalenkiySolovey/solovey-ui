import HttpUtils from '@/plugins/httputil'
import Data, { type FailoverStatusEntry, type FailoverStatusMap } from '@/store/modules/data'
import { computed, onMounted, onUnmounted, toValue, type MaybeRefOrGetter } from 'vue'

export type { FailoverStatusEntry, FailoverStatusMap } from '@/store/modules/data'

interface OutboundType {
  type: string
}

export const useFailoverStatus = (outbounds: MaybeRefOrGetter<readonly OutboundType[]>) => {
  const statusByTag = computed<FailoverStatusMap>(() => Data().failoverStatus)
  let timer: ReturnType<typeof setInterval> | undefined

  const refresh = async () => {
    if (!toValue(outbounds).some((item) => item.type === 'failover')) {
      Data().failoverStatus = {}
      return
    }

    const response = await HttpUtils.get('api/failover-status')
    if (!response.success || !Array.isArray(response.obj)) return

    Data().failoverStatus = Object.fromEntries(
      (response.obj as FailoverStatusEntry[]).map((entry) => [
        entry.tag,
        entry,
      ]),
    )
  }

  const stop = () => {
    if (timer) clearInterval(timer)
    timer = undefined
  }

  const start = () => {
    stop()
    void refresh()
    timer = setInterval(refresh, 5000)
  }

  onMounted(start)
  onUnmounted(stop)

  return { statusByTag, refresh, start, stop }
}
