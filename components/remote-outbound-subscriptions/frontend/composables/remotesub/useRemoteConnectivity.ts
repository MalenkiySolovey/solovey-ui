import { nextTick, onMounted, onUnmounted, reactive, ref, watch, type Ref } from 'vue'
import { push } from 'notivue'
import HttpUtils from '@/plugins/httputil'
import Data from '@/store/modules/data'
import { runWithConcurrency, useAsyncTaskQueue } from '@/shared/composables/useAsyncTaskQueue'
import type {
  RemoteOutboundConnection,
  RemoteOutboundGroup,
  RemoteOutboundSubscription,
  RemoteTranslate,
  TestState,
} from './types'

interface UseRemoteConnectivityOptions {
  subscriptions: Ref<RemoteOutboundSubscription[]>
  t: RemoteTranslate
  testableConnections: (subscription: RemoteOutboundSubscription) => RemoteOutboundConnection[]
  usableGroupConnections: (subscription: RemoteOutboundSubscription, group: RemoteOutboundGroup) => RemoteOutboundConnection[]
}

export const useRemoteConnectivity = ({
  subscriptions,
  t,
  testableConnections,
  usableGroupConnections,
}: UseRemoteConnectivityOptions) => {
  const testingAll = ref(false)
  const testingSubscriptions = reactive<Record<number, boolean>>({})
  const testingGroups = reactive<Record<number, boolean>>({})
  const connectionCheckQueue = useAsyncTaskQueue(8)
  const testingConnections = connectionCheckQueue.active
  const testResults = reactive<Record<number, TestState>>({})
  const healthNow = ref(Date.now())
  watch(() => Data().onlines.outboundHealth, () => { healthNow.value = Date.now() }, { flush: 'sync' })
  let healthTimer = 0
  onMounted(() => { healthTimer = window.setInterval(() => { healthNow.value = Date.now() }, 30000) })
  onUnmounted(() => window.clearInterval(healthTimer))

  const sourceHealth = (subscription: RemoteOutboundSubscription, connection: RemoteOutboundConnection) => {
    if (!subscription.enabled || !subscription.healthCheck?.enabled || !connection.enabled || !connection.synced) return undefined
    const health = Data().onlines.outboundHealth?.[connection.outboundTag]
    if (!health || health.source !== 'subscription' || health.targetId !== subscription.healthTargetId ||
        health.checkedAt > healthNow.value / 1000 || healthNow.value / 1000 - health.checkedAt >= 600) return undefined
    return health
  }

  const recordTestFailure = (connectionId: number, error: string) => {
    if (!connectionId) return
    testResults[connectionId] = {
      ok: false,
      delay: 0,
      error,
    }
  }

  const recordTestResult = (connectionId: number, payload: any) => {
    if (!connectionId || !payload) return
    const result = payload.result ?? payload.Result ?? payload
    const skippedError = payload.error ?? payload.Error ?? ''
    testResults[connectionId] = {
      ok: Boolean(result?.OK),
      delay: Number(result?.Delay ?? 0),
      error: String(result?.Error ?? skippedError ?? ''),
    }
  }

  const performConnectionTest = async (id: number): Promise<boolean> => {
    await nextTick()
    const msg = await HttpUtils.get('api/remote-outbound-subscriptions/connections/test', { id })
    if (msg.success) {
      recordTestResult(id, msg.obj)
      return Boolean(testResults[id]?.ok)
    }

    const error = msg.msg || t('failed')
    recordTestFailure(id, error)
    push.error({ message: error, duration: 5000 })
    return false
  }

  const testConnection = async (id: number): Promise<boolean> => {
    try {
      const result = await connectionCheckQueue.runOne(id, () => performConnectionTest(id))
      return Boolean(result)
    } catch (error: any) {
      const message = String(error?.message ?? error ?? t('failed'))
      recordTestFailure(id, message)
      push.error({ message, duration: 5000 })
      return false
    }
  }

  const testConnections = async (connections: RemoteOutboundConnection[]) => {
    const unique = new Map<number, RemoteOutboundConnection>()
    for (const connection of connections) unique.set(connection.id, connection)
    await runWithConcurrency([...unique.values()], async (connection) => {
      await testConnection(connection.id)
    }, 8)
  }

  const testSubscription = async (id: number) => {
    testingSubscriptions[id] = true
    try {
      const subscription = subscriptions.value.find(item => item.id === id)
      if (!subscription) return
      await testConnections(testableConnections(subscription))
    } finally {
      testingSubscriptions[id] = false
    }
  }

  const testGroup = async (subscription: RemoteOutboundSubscription, group: RemoteOutboundGroup) => {
    testingGroups[group.id] = true
    try {
      await testConnections(usableGroupConnections(subscription, group))
    } finally {
      testingGroups[group.id] = false
    }
  }

  const testAll = async () => {
    testingAll.value = true
    try {
      const unique = new Map<number, RemoteOutboundConnection>()
      for (const subscription of subscriptions.value) {
        for (const connection of testableConnections(subscription)) {
          unique.set(connection.id, connection)
        }
      }
      await testConnections([...unique.values()])
    } finally {
      testingAll.value = false
    }
  }

  return {
    sourceHealth,
    testAll,
    testConnection,
    testGroup,
    testResults,
    testSubscription,
    testingAll,
    testingConnections,
    testingGroups,
    testingSubscriptions,
  }
}
