import { computed, nextTick, reactive, ref } from 'vue'
import Data from '@/store/modules/data'
import HttpUtils from '@/plugins/httputil'
import {
  connectionConvertedType,
  connectionSourceType,
} from './helpers'
import type {
  RemoteConfirm,
  RemoteOutboundConnection,
  RemoteOutboundSubscription,
  RemoteTranslate,
  SubscriptionFormRef,
} from './types'

interface UseRemoteSubscriptionsCrudOptions {
  t: RemoteTranslate
  confirm: RemoteConfirm
  loadConversionPolicy: () => Promise<void>
}

export const useRemoteSubscriptionsCrud = ({ t, confirm, loadConversionPolicy }: UseRemoteSubscriptionsCrudOptions) => {
  const subscriptions = ref<RemoteOutboundSubscription[]>([])
  const subscriptionForm = ref<SubscriptionFormRef | null>(null)
  const search = ref('')
  const loading = ref(false)
  const saving = ref(false)
  const refreshing = reactive<Record<number, boolean>>({})

  const requiredRules = computed(() => [
    (value: unknown) => Boolean(String(value ?? '').trim()) || t('remoteOutbound.requiredField'),
  ])

  const form = reactive({
    id: 0,
    name: '',
    url: '',
    tagPrefix: '',
    enabled: true,
    autoUpdate: false,
    updateInterval: 86400,
  })

  const testableConnections = (subscription: RemoteOutboundSubscription): RemoteOutboundConnection[] => {
    return (subscription.connections ?? []).filter(connection => connection.enabled)
  }

  const updateIntervalMinutes = computed({
    get: () => Math.max(5, Math.round((Number(form.updateInterval) || 86400) / 60)),
    set: (value: number) => {
      const minutes = Math.max(5, Number(value) || 1440)
      form.updateInterval = minutes * 60
    },
  })

  const filteredSubscriptions = computed(() => {
    const query = search.value.trim().toLowerCase()
    if (!query) return subscriptions.value
    return subscriptions.value.filter((subscription) => {
      if (subscription.name.toLowerCase().includes(query) || subscription.url.toLowerCase().includes(query)) return true
      if ((subscription.tagPrefix ?? '').toLowerCase().includes(query)) return true
      if ((subscription.groups ?? []).some(group => group.name.toLowerCase().includes(query))) return true
      return (subscription.connections ?? []).some(connection =>
        connection.name.toLowerCase().includes(query) ||
        connection.outboundTag.toLowerCase().includes(query) ||
        connection.type.toLowerCase().includes(query) ||
        connectionSourceType(connection).toLowerCase().includes(query) ||
        connectionConvertedType(connection).toLowerCase().includes(query),
      )
    })
  })

  const load = async () => {
    loading.value = true
    try {
      const msg = await HttpUtils.get('api/remote-outbound-subscriptions')
      if (msg.success) {
        subscriptions.value = msg.obj ?? []
      }
      await loadConversionPolicy()
    } finally {
      loading.value = false
    }
  }

  const saveSubscription = async () => {
    const validation = await subscriptionForm.value?.validate()
    if (validation && !validation.valid) return
    saving.value = true
    try {
      const payload = {
        id: form.id,
        name: form.name,
        url: form.url,
        tagPrefix: form.tagPrefix,
        enabled: form.enabled,
        autoUpdate: form.autoUpdate,
        updateInterval: form.updateInterval,
      }
      const msg = await HttpUtils.post('api/remote-outbound-subscriptions/save', {
        data: JSON.stringify(payload),
      })
      if (msg.success) {
        resetForm()
        await load()
        await Data().loadData()
      }
    } finally {
      saving.value = false
    }
  }

  const resetForm = () => {
    form.id = 0
    form.name = ''
    form.url = ''
    form.tagPrefix = ''
    form.enabled = true
    form.autoUpdate = false
    form.updateInterval = 86400
    nextTick(() => subscriptionForm.value?.resetValidation())
  }

  const editSubscription = (subscription: RemoteOutboundSubscription) => {
    form.id = subscription.id
    form.name = subscription.name
    form.url = subscription.url
    form.tagPrefix = subscription.tagPrefix
    form.enabled = subscription.enabled
    form.autoUpdate = Boolean(subscription.autoUpdate)
    form.updateInterval = subscription.updateInterval || 86400
  }

  const deleteSubscription = async (subscription: RemoteOutboundSubscription) => {
    const accepted = await confirm({
      title: `${t('actions.del')} ${t('pages.remoteOutboundSubscriptions')}`,
      message: subscription.name,
      confirmLabel: t('actions.del'),
      tone: 'error',
    })
    if (!accepted) return
    const msg = await HttpUtils.post('api/remote-outbound-subscriptions/delete', { id: subscription.id })
    if (msg.success) {
      await load()
      await Data().loadData()
    }
  }

  const refreshSubscription = async (id: number) => {
    refreshing[id] = true
    try {
      const msg = await HttpUtils.post('api/remote-outbound-subscriptions/refresh', { id })
      if (msg.success) {
        await load()
        await Data().loadData()
      }
    } finally {
      refreshing[id] = false
    }
  }

  return {
    deleteSubscription,
    editSubscription,
    filteredSubscriptions,
    form,
    load,
    loading,
    refreshSubscription,
    refreshing,
    requiredRules,
    resetForm,
    saveSubscription,
    saving,
    search,
    subscriptionForm,
    subscriptions,
    testableConnections,
    updateIntervalMinutes,
  }
}
