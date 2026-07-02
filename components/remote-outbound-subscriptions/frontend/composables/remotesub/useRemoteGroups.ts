import { computed, reactive, ref, type Ref } from 'vue'
import { push } from 'notivue'
import Data from '@/store/modules/data'
import HttpUtils from '@/plugins/httputil'
import {
  connectionGroupIds,
  connectionGroupNames,
  isDefaultGroup,
} from './helpers'
import type {
  GroupConnectionBulkAction,
  RemoteConfirm,
  RemoteOutboundConnection,
  RemoteOutboundGroup,
  RemoteOutboundSubscription,
  RemoteTranslate,
} from './types'

interface UseRemoteGroupsOptions {
  subscriptions: Ref<RemoteOutboundSubscription[]>
  load: () => Promise<void>
  t: RemoteTranslate
  confirm: RemoteConfirm
}

export const useRemoteGroups = ({ subscriptions, load, t, confirm }: UseRemoteGroupsOptions) => {
  const bulkGroupDialog = ref(false)
  const bulkGroupName = ref('')
  const savingBulkGroup = ref(false)
  const savingGroups = reactive<Record<number, boolean>>({})
  const togglingGroups = reactive<Record<number, boolean>>({})
  const groupNames = reactive<Record<number, string>>({})
  const groupConnectionSearch = reactive<Record<number, string>>({})

  const canAddBulkGroup = computed(() => subscriptions.value.length > 0 && !savingBulkGroup.value)

  const saveGroup = async (subscriptionId: number) => {
    const name = (groupNames[subscriptionId] ?? '').trim()
    if (!name) return
    const msg = await HttpUtils.post('api/remote-outbound-subscriptions/groups/save', {
      data: JSON.stringify({ subscriptionId, name, enabled: true }),
    })
    if (msg.success) {
      groupNames[subscriptionId] = ''
      await load()
    }
  }

  const openBulkGroupDialog = () => {
    if (!canAddBulkGroup.value) return
    bulkGroupDialog.value = true
  }

  const saveBulkGroup = async () => {
    const name = bulkGroupName.value.trim()
    if (!name || subscriptions.value.length === 0) return
    savingBulkGroup.value = true
    try {
      const msg = await HttpUtils.post('api/remote-outbound-subscriptions/groups/bulk', {
        data: JSON.stringify({ name }),
      })
      if (msg.success) {
        const created = Number(msg.obj?.created ?? 0)
        const skipped = Number(msg.obj?.skipped ?? 0)
        push.success({
          message: `Added ${created} / Skipped ${skipped}`,
          duration: 5000,
        })
        bulkGroupName.value = ''
        bulkGroupDialog.value = false
        await load()
      }
    } finally {
      savingBulkGroup.value = false
    }
  }

  const deleteGroup = async (group: RemoteOutboundGroup) => {
    if (isDefaultGroup(group)) return
    const accepted = await confirm({
      title: `${t('actions.del')} ${t('remoteOutbound.group')}`,
      message: group.name,
      confirmLabel: t('actions.del'),
      tone: 'error',
    })
    if (!accepted) return
    const msg = await HttpUtils.post('api/remote-outbound-subscriptions/groups/delete', { id: group.id })
    if (msg.success) {
      await load()
    }
  }

  const saveGroupConnections = async (groupId: number, ids: unknown) => {
    const connectionIds = Array.isArray(ids) ? ids.map(Number).filter(Boolean) : []
    savingGroups[groupId] = true
    try {
      const msg = await HttpUtils.post('api/remote-outbound-subscriptions/groups/connections', {
        data: JSON.stringify({ groupId, connectionIds }),
      })
      if (msg.success) {
        await load()
      }
    } finally {
      savingGroups[groupId] = false
    }
  }

  const groupConnectionIds = (subscription: RemoteOutboundSubscription, group: RemoteOutboundGroup): number[] => {
    return (subscription.connections ?? [])
      .filter(connection => connectionGroupIds(connection).includes(group.id))
      .map(connection => connection.id)
  }

  const toggleGroupConnection = async (
    subscription: RemoteOutboundSubscription,
    group: RemoteOutboundGroup,
    connectionId: number,
    checked: boolean,
  ) => {
    const ids = new Set(groupConnectionIds(subscription, group))
    if (checked) ids.add(connectionId)
    else ids.delete(connectionId)
    await saveGroupConnections(group.id, [...ids])
  }

  const groupOutboundOn = (group: RemoteOutboundGroup): boolean => {
    return Boolean(group.outboundEnabled)
  }

  const setGroupConnectionsBulk = async (
    subscription: RemoteOutboundSubscription,
    group: RemoteOutboundGroup,
    action: GroupConnectionBulkAction,
  ) => {
    const allIds = (subscription.connections ?? []).map(connection => connection.id)
    const current = new Set(groupConnectionIds(subscription, group))
    let next: number[]

    switch (action) {
      case 'all':
        next = allIds
        break
      case 'none':
        next = []
        break
      case 'invert':
        next = allIds.filter(id => !current.has(id))
        break
      default:
        next = [...current]
    }

    const currentKey = [...current].sort((a, b) => a - b).join(',')
    const nextKey = [...next].sort((a, b) => a - b).join(',')
    if (currentKey === nextKey) return

    if (groupOutboundOn(group)) {
      const accepted = await confirm({
        title: 'Update synced remote group',
        message: `${group.name} is synced to outbounds. Changing its connections can update generated outbounds used by routes, DNS rules or clients.`,
        confirmLabel: t('actions.update'),
        tone: 'primary',
      })
      if (!accepted) return
    }

    await saveGroupConnections(group.id, next)
    await Data().loadData()
  }

  const toggleGroupOutbounds = async (groupId: number) => {
    togglingGroups[groupId] = true
    try {
      const msg = await HttpUtils.post('api/remote-outbound-subscriptions/groups/outbounds', { groupId })
      if (msg.success) {
        await load()
        await Data().loadData()
      }
    } finally {
      togglingGroups[groupId] = false
    }
  }

  const groupConnectionCount = (subscription: RemoteOutboundSubscription, group: RemoteOutboundGroup): number => {
    return groupConnectionIds(subscription, group).length
  }

  const usableGroupConnections = (subscription: RemoteOutboundSubscription, group: RemoteOutboundGroup): RemoteOutboundConnection[] => {
    return (subscription.connections ?? [])
      .filter(connection => connectionGroupIds(connection).includes(group.id) && connection.enabled)
  }

  const usableGroupCount = (subscription: RemoteOutboundSubscription, group: RemoteOutboundGroup): number => {
    return usableGroupConnections(subscription, group).length
  }

  return {
    bulkGroupDialog,
    bulkGroupName,
    canAddBulkGroup,
    connectionGroupNames,
    deleteGroup,
    groupConnectionCount,
    groupConnectionIds,
    groupConnectionSearch,
    groupNames,
    groupOutboundOn,
    isDefaultGroup,
    openBulkGroupDialog,
    saveBulkGroup,
    saveGroup,
    savingBulkGroup,
    savingGroups,
    setGroupConnectionsBulk,
    toggleGroupConnection,
    toggleGroupOutbounds,
    togglingGroups,
    usableGroupConnections,
    usableGroupCount,
  }
}
