import HttpUtils from '@/plugins/httputil'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'

export interface UpdateStatus {
  current: string
  channel: 'main' | 'beta'
  latest?: string
  prerelease?: boolean
  updateAvailable?: boolean
  assetAvailable?: boolean
  releaseNotes?: string
  checkError?: string
  job?: { stage: string; error?: string }
}

const runningStages = ['downloading', 'verifying', 'applying', 'restarting']

export const usePanelUpdate = () => {
  const status = ref<UpdateStatus>()
  const channel = ref<'main' | 'beta'>('main')
  const checking = ref(false)
  const applying = ref(false)
  const confirm = ref(false)
  const password = ref('')
  let suppressChannelCheck = false
  let pollTimer: ReturnType<typeof setInterval> | undefined

  const jobActive = computed(() => runningStages.includes(status.value?.job?.stage || ''))
  const canUpdate = computed(() => Boolean(
    status.value?.updateAvailable
      && status.value?.assetAvailable
      && !jobActive.value
      && !applying.value,
  ))

  const applyStatus = (object: unknown) => {
    status.value = object as UpdateStatus
    const incoming = status.value?.channel
    if ((incoming === 'main' || incoming === 'beta') && incoming !== channel.value) {
      suppressChannelCheck = true
      channel.value = incoming
    }
  }

  const loadStatus = async () => {
    const message = await HttpUtils.get('api/update/status')
    if (message.success) applyStatus(message.obj)
  }

  const checkUpdates = async () => {
    checking.value = true
    try {
      const message = await HttpUtils.post('api/update/check', { channel: channel.value })
      if (message.success) applyStatus(message.obj)
    } finally {
      checking.value = false
    }
  }

  const stopPolling = () => {
    if (pollTimer) clearInterval(pollTimer)
    pollTimer = undefined
  }

  const startPolling = () => {
    stopPolling()
    pollTimer = setInterval(async () => {
      await loadStatus()
      if (!jobActive.value) stopPolling()
    }, 2000)
  }

  const openConfirm = () => {
    password.value = ''
    confirm.value = true
  }

  const runUpdate = async () => {
    applying.value = true
    try {
      const message = await HttpUtils.post('api/update/apply', {
        channel: channel.value,
        targetVersion: status.value?.latest ?? '',
        password: password.value,
      })
      password.value = ''
      if (message.success) {
        confirm.value = false
        applyStatus(message.obj)
        startPolling()
      }
    } finally {
      applying.value = false
    }
  }

  watch(channel, () => {
    if (suppressChannelCheck) {
      suppressChannelCheck = false
      return
    }
    void checkUpdates()
  })
  onMounted(loadStatus)
  onUnmounted(stopPolling)

  return {
    applying,
    canUpdate,
    channel,
    checkUpdates,
    checking,
    confirm,
    jobActive,
    openConfirm,
    password,
    runUpdate,
    status,
  }
}
