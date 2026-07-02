import { ref } from 'vue'
import HttpUtils from '@/plugins/httputil'
import type { CollectedSubscriptionData, RemoteOutboundSubscription } from './types'

export const useRemoteCollected = () => {
  const collectedDialog = ref(false)
  const collectedLoading = ref(false)
  const collectedData = ref<CollectedSubscriptionData | null>(null)

  const openCollectedData = async (subscription: RemoteOutboundSubscription) => {
    collectedDialog.value = true
    collectedLoading.value = true
    collectedData.value = null
    try {
      const msg = await HttpUtils.get('api/remote-outbound-subscriptions/collected', { id: subscription.id })
      if (msg.success) {
        collectedData.value = msg.obj
      }
    } finally {
      collectedLoading.value = false
    }
  }

  return {
    collectedData,
    collectedDialog,
    collectedLoading,
    openCollectedData,
  }
}
