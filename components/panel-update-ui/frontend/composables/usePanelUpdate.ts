import HttpUtils from '@/plugins/httputil'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import Data from '@/store/modules/data'
import type { ComponentCatalogInventory, ComponentCatalogStatus } from '../types'

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
  const data = Data()
  const status = ref<UpdateStatus>()
  const channel = ref<'main' | 'beta'>('main')
  const checking = ref(false)
  const applying = ref(false)
  const componentAction = ref('')
  const componentInventory = ref<ComponentCatalogInventory>()
  const componentRemoveConfirm = ref(false)
  const componentRemoveTarget = ref<ComponentCatalogStatus>()
  const componentRemovePassword = ref('')
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
  const runtimeInstalledComponents = computed<ComponentCatalogStatus[]>(() => data.components.map(component => ({
    ...component,
    latestVersion: component.version,
    availableInBinary: true,
    installable: false,
    removable: false,
    group: 'installed',
  })))
  const installedComponents = computed<ComponentCatalogStatus[]>(() => componentInventory.value?.installed ?? runtimeInstalledComponents.value)
  const availableComponents = computed<ComponentCatalogStatus[]>(() => componentInventory.value?.available ?? [])
  const unavailableComponents = computed<ComponentCatalogStatus[]>(() => componentInventory.value?.unavailable ?? [])

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

  const loadComponents = async () => {
    const message = await HttpUtils.get('api/update/components')
    if (message.success) componentInventory.value = message.obj as ComponentCatalogInventory
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

  const setComponentEnabled = async (component: ComponentCatalogStatus, enabled: boolean) => {
    componentAction.value = `${component.id}:${enabled ? 'enable' : 'disable'}`
    try {
      const message = await HttpUtils.post(`api/update/components/${component.id}/${enabled ? 'enable' : 'disable'}`, {})
      if (message.success) {
        await loadComponents()
        await data.loadData()
      }
      return message.success
    } finally {
      componentAction.value = ''
    }
  }

  const setComponentInstalled = async (component: ComponentCatalogStatus, installed: boolean, password = '') => {
    componentAction.value = `${component.id}:${installed ? 'install' : 'remove'}`
    try {
      const body = installed ? {} : { password }
      const message = await HttpUtils.post(`api/update/components/${component.id}/${installed ? 'install' : 'remove'}`, body)
      if (message.success) {
        await loadComponents()
        await data.loadData()
      }
      return message.success
    } finally {
      componentAction.value = ''
    }
  }

  const openComponentRemoveConfirm = (component: ComponentCatalogStatus) => {
    componentRemoveTarget.value = component
    componentRemovePassword.value = ''
    componentRemoveConfirm.value = true
  }

  const removeComponent = async () => {
    if (!componentRemoveTarget.value) return
    const ok = await setComponentInstalled(componentRemoveTarget.value, false, componentRemovePassword.value)
    if (ok) {
      componentRemoveConfirm.value = false
      componentRemovePassword.value = ''
      componentRemoveTarget.value = undefined
    }
  }

  watch(channel, () => {
    if (suppressChannelCheck) {
      suppressChannelCheck = false
      return
    }
    void checkUpdates()
  })
  onMounted(() => {
    void loadStatus()
    void loadComponents()
  })
  onUnmounted(stopPolling)

  return {
    applying,
    canUpdate,
    channel,
    checkUpdates,
    checking,
    componentAction,
    componentInventory,
    componentRemoveConfirm,
    componentRemovePassword,
    componentRemoveTarget,
    confirm,
    jobActive,
    openConfirm,
    openComponentRemoveConfirm,
    password,
    removeComponent,
    runUpdate,
    availableComponents,
    installedComponents,
    setComponentEnabled,
    setComponentInstalled,
    status,
    unavailableComponents,
  }
}
