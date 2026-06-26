import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useConfirm } from '@/components/nexus/primitives/useConfirm'
import { useUiMode } from '@/uiMode/useUiMode'
import { connectionConvertedType, connectionSourceType, formatInterval, formatTime } from '@/shared/composables/remotesub/helpers'
import { useRemoteCollected } from '@/shared/composables/remotesub/useRemoteCollected'
import { useRemoteConnectivity } from '@/shared/composables/remotesub/useRemoteConnectivity'
import { useRemoteConversionPolicy } from '@/shared/composables/remotesub/useRemoteConversionPolicy'
import { useRemoteGroups } from '@/shared/composables/remotesub/useRemoteGroups'
import { useRemoteSubscriptionsCrud } from '@/shared/composables/remotesub/useRemoteSubscriptionsCrud'

export const useRemoteOutboundSubscriptionsPage = () => {
  const { mode } = useUiMode()
  const { t } = useI18n()
  const { confirm } = useConfirm()

  const conversion = useRemoteConversionPolicy(t)
  const crud = useRemoteSubscriptionsCrud({
    t,
    confirm,
    loadConversionPolicy: conversion.loadConversionPolicy,
  })
  const groups = useRemoteGroups({
    subscriptions: crud.subscriptions,
    load: crud.load,
    t,
    confirm,
  })
  const collected = useRemoteCollected()
  const connectivity = useRemoteConnectivity({
    subscriptions: crud.subscriptions,
    t,
    testableConnections: crud.testableConnections,
    usableGroupConnections: groups.usableGroupConnections,
  })

  const totalConnections = computed(() => crud.subscriptions.value.reduce((sum, subscription) => {
    return sum + (subscription.connections?.length ?? 0)
  }, 0))

  const totalTestableConnections = computed(() => crud.subscriptions.value.reduce((sum, subscription) => {
    return sum + crud.testableConnections(subscription).length
  }, 0))

  const totalSynced = computed(() => crud.subscriptions.value.reduce((sum, subscription) => {
    return sum + (subscription.connections ?? []).filter(connection => connection.synced).length
  }, 0))

  const subtitle = computed(() => {
    const total = crud.subscriptions.value.length
    return t('remoteOutbound.summary', { total, connections: totalConnections.value, synced: totalSynced.value })
  })

  onMounted(crud.load)

  return {
    ...collected,
    ...connectivity,
    ...conversion,
    ...crud,
    ...groups,
    connectionConvertedType,
    connectionSourceType,
    formatInterval,
    formatTime,
    mode,
    subtitle,
    t,
    totalConnections,
    totalSynced,
    totalTestableConnections,
  }
}
