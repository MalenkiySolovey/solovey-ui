import { computed, nextTick, reactive, ref, watch } from 'vue'
import Data from '@/store/modules/data'
import HttpUtils from '@/plugins/httputil'
import { secretMutation, type SecretView, type SecretMutation } from '@/features/secretCredentialDraft'
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
  const inlineFile = ref<File | File[] | null>(null)
  const inlineFileLoading = ref(false)
  const inlineFileError = ref('')
  let fileDraftGeneration = 0

  const requiredRules = computed(() => [
    (value: unknown) => Boolean(String(value ?? '').trim()) || t('remoteOutbound.requiredField'),
  ])

  const form = reactive({
    id: 0,
    name: '',
    url: '',
    userAgent: '',
    sourceType: 'remote' as 'remote' | 'inline',
    content: undefined as SecretView | SecretMutation | undefined,
    include: '',
    exclude: '',
    removeEmojis: false,
    dialerOverride: {} as import('@/types/dial').Dial,
    tagPrefix: '',
    enabled: true,
    autoUpdate: false,
    updateInterval: 86400,
  })

  const resetInlineFile = () => {
    fileDraftGeneration++
    inlineFile.value = null
    inlineFileLoading.value = false
    inlineFileError.value = ''
  }
  watch(() => form.sourceType, resetInlineFile)

  const loadInlineFile = async (value: File | File[] | null | undefined) => {
    resetInlineFile()
    const file = Array.isArray(value) ? value[0] : value
    if (!file || form.sourceType !== 'inline') return
    inlineFile.value = file
    if (file.size === 0 || file.size > (512 << 10)) {
      inlineFileError.value = t('remoteOutbound.inlineFileSizeError')
      return
    }
    const generation = fileDraftGeneration
    const ownerID = form.id
    const previousContent = form.content
    inlineFileLoading.value = true
    try {
      const text = await file.text()
      // A file read belongs to this editor generation. It must not replace a
      // different source, a newer file selection or an intervening typed edit.
      if (fileDraftGeneration === generation && form.id === ownerID &&
          form.sourceType === 'inline' && form.content === previousContent) {
        form.content = { intent: 'replace', value: text }
      }
    } catch {
      if (fileDraftGeneration === generation) inlineFileError.value = t('remoteOutbound.inlineFileReadError')
    } finally {
      if (fileDraftGeneration === generation) inlineFileLoading.value = false
    }
  }

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
    if (inlineFileLoading.value || inlineFileError.value) return
    const validation = await subscriptionForm.value?.validate()
    if (validation && !validation.valid) return
    saving.value = true
    try {
      const payload = {
        id: form.id,
        name: form.name,
        url: form.sourceType === 'remote' ? form.url : '',
        userAgent: form.sourceType === 'remote' ? form.userAgent : '',
        sourceType: form.sourceType,
        content: form.sourceType === 'inline' ? secretMutation(form.content) : undefined,
        include: form.include,
        exclude: form.exclude,
        removeEmojis: form.removeEmojis,
        dialerOverride: form.dialerOverride,
        tagPrefix: form.tagPrefix,
        enabled: form.enabled,
        autoUpdate: form.sourceType === 'remote' && form.autoUpdate,
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
    resetInlineFile()
    form.id = 0
    form.name = ''
    form.url = ''
    form.userAgent = ''
    form.sourceType = 'remote'
    form.content = undefined
    form.include = ''
    form.exclude = ''
    form.removeEmojis = false
    form.dialerOverride = {}
    form.tagPrefix = ''
    form.enabled = true
    form.autoUpdate = false
    form.updateInterval = 86400
    nextTick(() => subscriptionForm.value?.resetValidation())
  }

  const editSubscription = (subscription: RemoteOutboundSubscription) => {
    resetInlineFile()
    form.id = subscription.id
    form.name = subscription.name
    form.url = subscription.url
    form.userAgent = subscription.userAgent ?? ''
    form.sourceType = subscription.sourceType ?? 'remote'
    form.content = subscription.content ? JSON.parse(JSON.stringify(subscription.content)) : undefined
    form.include = subscription.include ?? ''
    form.exclude = subscription.exclude ?? ''
    form.removeEmojis = subscription.removeEmojis === true
    form.dialerOverride = JSON.parse(JSON.stringify(subscription.dialerOverride ?? {}))
    form.tagPrefix = subscription.tagPrefix
    form.enabled = subscription.enabled
    form.autoUpdate = Boolean(subscription.autoUpdate)
    form.updateInterval = subscription.updateInterval || 86400
  }

  const deleteSubscription = async (subscription: RemoteOutboundSubscription) => {
    const accepted = await confirm({
      title: `${t('actions.del')} ${t('remoteOutbound.title')}`,
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
    inlineFile,
    inlineFileLoading,
    inlineFileError,
    loadInlineFile,
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
