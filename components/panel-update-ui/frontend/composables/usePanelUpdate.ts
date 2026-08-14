import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import Data from '@/store/modules/data'
import { acquireStepUpToken } from '@/shared/composables/useSecurityOperations'
import {
  activateSignedUpdate,
  checkSignedUpdate,
  getUpdatePosture,
  operationsMessage,
  prepareSignedUpdate,
  type UpdateCheck,
  type UpdateOperation,
  type UpdatePosture,
} from '@/shared/composables/useOperations'
import HttpUtils from '@/plugins/httputil'
import type { ComponentCatalogInventory, ComponentCatalogStatus } from '../types'

export interface UpdateStatus {
  current: string
  channel: 'main' | 'beta'
  latest?: string
  prerelease?: boolean
  updateAvailable?: boolean
  assetAvailable?: boolean
  checkError?: string
  signingStatus?: string
  sequence?: number
  manifestDigest?: string
  operation?: UpdateOperation
  job?: { stage: string; error?: string }
}

const operationStage = (operation?: UpdateOperation): string => {
  switch (operation?.state) {
    case 'DOWNLOADING': return 'downloading'
    case 'VERIFYING':
    case 'PREFLIGHTING':
    case 'PREPARED': return 'verifying'
    case 'ACTIVATING':
    case 'VERIFYING_ACTIVE': return 'applying'
    case 'RESTARTING': return 'restarting'
    case 'FAILED':
    case 'RECOVERY_REQUIRED': return 'failed'
    default: return 'idle'
  }
}

const runningStages = ['downloading', 'verifying', 'applying', 'restarting']
const unhealthySigningStatuses = new Set([
  'SIGNING_UNAVAILABLE',
  'VERIFICATION_FAILED',
  'VERIFIED_STALE',
  'PREVIOUSLY_VERIFIED_TRUST_UNAVAILABLE',
])

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
  let pollTimer: ReturnType<typeof setInterval> | undefined

  const jobActive = computed(() => runningStages.includes(status.value?.job?.stage || ''))
  const canUpdate = computed(() => Boolean(
    (status.value?.operation?.state === 'PREPARED'
      || (status.value?.updateAvailable && status.value?.assetAvailable
        && status.value?.sequence && status.value?.manifestDigest))
      && !jobActive.value
      && !applying.value,
  ))
  const runtimeInstalledComponents = computed<ComponentCatalogStatus[]>(() => data.components.map(component => ({
    ...component,
    latestVersion: component.version,
    compatible: true,
    availableInBinary: true,
    installable: false,
    removable: false,
    group: 'installed',
  })))
  const installedComponents = computed<ComponentCatalogStatus[]>(() => componentInventory.value?.installed ?? runtimeInstalledComponents.value)
  const availableComponents = computed<ComponentCatalogStatus[]>(() => componentInventory.value?.available ?? [])
  const unavailableComponents = computed<ComponentCatalogStatus[]>(() => componentInventory.value?.unavailable ?? [])

  const applyPosture = (posture: UpdatePosture) => {
    const selected = posture.selected
    status.value = {
      current: posture.actual.version,
      channel: posture.desired.channel,
      latest: selected?.version,
      prerelease: posture.desired.channel === 'beta',
      updateAvailable: posture.state === 'UPDATE_AVAILABLE',
      assetAvailable: posture.signingStatus === 'VERIFIED' && Boolean(selected?.sequence && selected?.manifestDigest),
      checkError: posture.state === 'RECOVERY_REQUIRED' || unhealthySigningStatuses.has(posture.signingStatus)
        ? posture.reasonCodes?.[0]
        : undefined,
      signingStatus: posture.signingStatus,
      sequence: selected?.sequence,
      manifestDigest: selected?.manifestDigest,
      operation: posture.operation,
      job: {
        stage: operationStage(posture.operation),
        error: posture.operation?.reasonCode,
      },
    }
  }

  const applyCheck = (check: UpdateCheck) => {
    status.value = {
      current: check.currentVersion,
      channel: check.channel,
      latest: check.version,
      prerelease: check.channel === 'beta',
      updateAvailable: check.updateAvailable,
      assetAvailable: check.signingStatus === 'VERIFIED' && Boolean(check.sequence && check.manifestDigest),
      signingStatus: check.signingStatus,
      sequence: check.sequence,
      manifestDigest: check.manifestDigest,
      job: { stage: 'idle' },
    }
  }

  const loadStatus = async () => {
    const message = await getUpdatePosture(channel.value)
    const posture = operationsMessage<UpdatePosture>(message)
    if (posture) applyPosture(posture)
  }

  const loadComponents = async () => {
    const message = await HttpUtils.get('api/update/components')
    if (message.success) componentInventory.value = message.obj as ComponentCatalogInventory
  }

  const checkUpdates = async () => {
    checking.value = true
    try {
      const message = await checkSignedUpdate(channel.value)
      const check = operationsMessage<UpdateCheck>(message)
      if (check) applyCheck(check)
      else if (message.msg) {
        status.value = { ...(status.value ?? { current: '', channel: channel.value }), checkError: message.msg }
      }
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
    const credential = password.value
    applying.value = true
    try {
      let operation = status.value?.operation?.state === 'PREPARED' ? status.value.operation : undefined
      if (!operation) {
        const sequence = status.value?.sequence ?? 0
        const digest = status.value?.manifestDigest ?? ''
        if (!sequence || !digest) return
        const target = `release:${digest}:${sequence}`
        const stepUp = await acquireStepUpToken('update.prepare', target, credential)
        if (!stepUp.token) return
        const prepared = await prepareSignedUpdate({
          channel: channel.value,
          expectedSequence: sequence,
          expectedManifestDigest: digest,
          idempotencyKey: `panel-update-${sequence}-${Date.now()}`,
          confirmation: `PREPARE_UPDATE_${sequence}`,
          acknowledged: true,
        }, stepUp.token)
        operation = operationsMessage<UpdateOperation>(prepared) ?? undefined
        if (!operation) return
      }
      if (operation.state !== 'PREPARED') {
        await loadStatus()
        startPolling()
        return
      }
      const activation = await acquireStepUpToken(
        'update.activate',
        `${operation.operationId}:${operation.revision}`,
        credential,
      )
      if (!activation.token) return
      const activated = await activateSignedUpdate({
        operationId: operation.operationId,
        expectedRevision: operation.revision,
        confirmation: `ACTIVATE_UPDATE_${operation.sequence}`,
      }, activation.token)
      if (activated.success) {
        confirm.value = false
        await loadStatus()
        startPolling()
      }
    } finally {
      password.value = ''
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

  const setComponentInstalled = async (
    component: ComponentCatalogStatus,
    installed: boolean,
    credential = '',
  ) => {
    componentAction.value = `${component.id}:${installed ? 'install' : 'remove'}`
    try {
      const body = installed ? {} : { password: credential }
      const message = await HttpUtils.post(
        `api/update/components/${component.id}/${installed ? 'install' : 'remove'}`,
        body,
      )
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
    const ok = await setComponentInstalled(
      componentRemoveTarget.value,
      false,
      componentRemovePassword.value,
    )
    if (ok) {
      componentRemoveConfirm.value = false
      componentRemovePassword.value = ''
      componentRemoveTarget.value = undefined
    }
  }

  watch(channel, () => { void checkUpdates() })
  onMounted(() => {
    void loadStatus()
    void loadComponents()
  })
  onUnmounted(stopPolling)

  return {
    applying, availableComponents, canUpdate, channel, checkUpdates, checking,
    componentAction, componentInventory, componentRemoveConfirm,
    componentRemovePassword, componentRemoveTarget, confirm, installedComponents, jobActive,
    openConfirm, openComponentRemoveConfirm, password, removeComponent, runUpdate,
    setComponentEnabled, setComponentInstalled, status, unavailableComponents,
  }
}
