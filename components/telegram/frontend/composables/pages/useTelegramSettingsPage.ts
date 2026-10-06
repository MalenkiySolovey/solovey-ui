import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { i18n } from '@/locales'
import HttpUtils from '@/plugins/httputil'
import { FindDiff } from '@/plugins/utils'
import { push } from 'notivue'
import { normalizeSecretFields, stripSecretPlaceholders, STORED_SECRET_PLACEHOLDER } from '@/components/settings/settingsSecretField'
import {
  parseTelegramBackupSchedule,
  serializeTelegramBackupSchedule,
  validateTelegramBackupSchedule,
  type TelegramBackupScheduleMode,
  type TelegramBackupScheduleUnit,
} from '../../views/telegramBackupSchedule'
import {
  hasWeakTelegramBackupPassphrase,
  pickTelegramSettings,
  telegramSettingsDefaults,
  type TelegramSettingsMap,
} from '../../views/telegramSettingsPayload'

export const useTelegramSettingsPage = () => {
  type TelegramResult = {
    success: boolean
    errorClass?: string
  }

  type BackupRunStatus = {
    success: boolean
    timestamp: string
    errorClass?: string
  }

  const defaultTelegramSettings: TelegramSettingsMap = telegramSettingsDefaults

  const loading = ref(false)
  const settingsReady = ref(false)

  const testLoading = ref(false)

  type DiscoveryResult = TelegramResult & { chatId?: string }
  const discoveryLoading = ref(false)
  const discoveryResult = ref<DiscoveryResult | null>(null)
  const busy = computed(() => loading.value || testLoading.value || discoveryLoading.value)
  const pageController = new AbortController()
  let saveController: AbortController | undefined
  let testController: AbortController | undefined
  let discoveryController: AbortController | undefined
  let disposed = false
  let draftGeneration = 0
  let actionGeneration = 0

  const backupRunLoading = ref(false)

  const settings = ref<TelegramSettingsMap>({ ...defaultTelegramSettings })

  const oldSettings = ref<TelegramSettingsMap>({ ...defaultTelegramSettings })

  const testResult = ref<TelegramResult | null>(null)

  const backupRunStatus = ref<BackupRunStatus | null>(null)

  const backupRunController = ref<AbortController | null>(null)

  const telegramBackupExcludeTableOptions = ['stats', 'client_ips', 'audit_events', 'changes']

  const telegramBackupScheduleMode = ref<TelegramBackupScheduleMode>('manual')

  const telegramBackupCustomValue = ref(15)

  const telegramBackupCustomUnit = ref<TelegramBackupScheduleUnit>('minutes')

  const telegramBackupAdvancedCron = ref('')

  watch(settings, () => {
    draftGeneration++
    testResult.value = null
    discoveryResult.value = null
    testController?.abort()
    discoveryController?.abort()
  }, { deep: true, flush: 'sync' })

  const loadData = async () => {
    const generation = draftGeneration
    loading.value = true
    try {
      const msg = await HttpUtils.get('api/settings', {}, { signal: pageController.signal })
      if (!disposed && generation === draftGeneration && msg.success && msg.obj && typeof msg.obj === 'object' && !Array.isArray(msg.obj)) {
        setData(msg.obj)
        settingsReady.value = true
      }
    } finally { if (!disposed) loading.value = false }
  }

  const reloadSettings = async () => {
    if (busy.value || disposed || settingsReady.value) return
    await loadData()
  }

  const transportModes = computed(() => [
    { title: i18n.global.t('telegram.transportProxy'), value: 'proxy' },
    { title: i18n.global.t('telegram.transportOutbound'), value: 'outbound' },
  ])

  const outboundOptions = ref<{ title: string; value: string }[]>([])

  const loadOutbounds = async () => {
    const msg = await HttpUtils.get('api/outbounds', {}, { signal: pageController.signal })
    const list = msg?.obj?.outbounds
    if (!disposed && msg.success && Array.isArray(list)) {
      outboundOptions.value = list.map((o: any) => ({ title: `${o.tag} (${o.type})`, value: o.tag }))
    }
  }

  onMounted(() => {
    loadData()
    loadOutbounds()
  })

  onUnmounted(() => {
    disposed = true
    actionGeneration++
    draftGeneration++
    pageController.abort()
    saveController?.abort()
    testController?.abort()
    discoveryController?.abort()
    loading.value = false
    testLoading.value = false
    discoveryLoading.value = false
    const controller = backupRunController.value
    backupRunController.value = null
    backupRunLoading.value = false
    controller?.abort()
  })

  const normalizedSettings = (data: TelegramSettingsMap) => pickTelegramSettings(normalizeSecretFields({ ...defaultTelegramSettings, ...data }))

  const setData = (data: TelegramSettingsMap) => {
    settings.value = normalizedSettings(data)
    syncTelegramBackupScheduleFromCron(settings.value.telegramBackupCron)
    oldSettings.value = { ...settings.value }
  }

  const boolSetting = (key: string) => computed({
    get: () => settings.value[key] === 'true',
    set: (value: boolean) => { settings.value[key] = value ? 'true' : 'false' },
  })

  const telegramEnabled = boolSetting('telegramEnabled')

  const telegramNotifyCpu = boolSetting('telegramNotifyCpu')

  const telegramReport = boolSetting('telegramReport')

  const telegramBackupEnabled = boolSetting('telegramBackupEnabled')

  const telegramCpuThreshold = computed({
    get: () => Number(settings.value.telegramCpuThreshold || 90),
    set: (value: number) => {
      const normalized = Number.isFinite(value) && value > 0 ? Math.min(Math.trunc(value), 100) : 90
      settings.value.telegramCpuThreshold = normalized.toString()
    },
  })

  const telegramBackupMaxSizeMB = computed({
    get: () => Number(settings.value.telegramBackupMaxSizeMB || 45),
    set: (value: number) => {
      const normalized = Number.isFinite(value) ? Math.min(Math.max(Math.trunc(value), 1), 50) : 45
      settings.value.telegramBackupMaxSizeMB = normalized.toString()
    },
  })

  const telegramBackupExcludeTables = computed({
    get: () => settings.value.telegramBackupExcludeTables
      .split(',')
      .map(item => item.trim())
      .filter(item => telegramBackupExcludeTableOptions.includes(item)),
    set: (value: string[]) => {
      settings.value.telegramBackupExcludeTables = telegramBackupExcludeTableOptions
        .filter(item => value.includes(item))
        .join(',')
    },
  })

  const telegramBackupScheduleOptions = computed(() => [
    { title: i18n.global.t('telegram.backup.schedule.manual'), value: 'manual' },
    { title: i18n.global.t('telegram.backup.schedule.every15m'), value: 'every15m' },
    { title: i18n.global.t('telegram.backup.schedule.every30m'), value: 'every30m' },
    { title: i18n.global.t('telegram.backup.schedule.hourly'), value: 'hourly' },
    { title: i18n.global.t('telegram.backup.schedule.every6h'), value: 'every6h' },
    { title: i18n.global.t('telegram.backup.schedule.every12h'), value: 'every12h' },
    { title: i18n.global.t('telegram.backup.schedule.daily3'), value: 'daily3' },
    { title: i18n.global.t('telegram.backup.schedule.custom'), value: 'custom' },
    { title: i18n.global.t('telegram.backup.schedule.advanced'), value: 'advanced' },
  ])

  const telegramBackupScheduleUnitOptions = computed(() => [
    { title: i18n.global.t('telegram.backup.schedule.minutes'), value: 'minutes' },
    { title: i18n.global.t('telegram.backup.schedule.hours'), value: 'hours' },
  ])

  const telegramBackupCustomMax = computed(() => telegramBackupCustomUnit.value === 'hours' ? 23 : 59)

  const telegramBackupScheduleState = computed(() => ({
    mode: telegramBackupScheduleMode.value,
    customValue: Number(telegramBackupCustomValue.value),
    customUnit: telegramBackupCustomUnit.value,
    advancedCron: telegramBackupAdvancedCron.value,
  }))

  const telegramBackupScheduleErrors = computed(() => {
    return validateTelegramBackupSchedule(telegramBackupScheduleState.value)
      .map(error => i18n.global.t('telegram.backup.schedule.errors.' + error))
  })

  const telegramBackupPassphraseErrors = computed(() => {
    if (!hasWeakTelegramBackupPassphrase(settings.value.telegramBackupPassphrase)) {
      return []
    }
    return [i18n.global.t('telegram.backup.passphraseMinLength')]
  })

  const syncTelegramBackupScheduleFromCron = (cron: string) => {
    const schedule = parseTelegramBackupSchedule(cron)
    telegramBackupScheduleMode.value = schedule.mode
    telegramBackupCustomValue.value = schedule.customValue
    telegramBackupCustomUnit.value = schedule.customUnit
    telegramBackupAdvancedCron.value = schedule.advancedCron
  }

  const updateTelegramBackupCronFromSchedule = () => {
    settings.value.telegramBackupCron = serializeTelegramBackupSchedule(telegramBackupScheduleState.value)
  }

  const handleTelegramBackupScheduleModeChange = () => {
    if (telegramBackupScheduleMode.value === 'advanced' && !telegramBackupAdvancedCron.value.trim()) {
      telegramBackupAdvancedCron.value = settings.value.telegramBackupCron.trim()
    }
    updateTelegramBackupCronFromSchedule()
  }

  // The same validated persistence path serves Save and save-before-Test.
  // A saved baseline may advance while a newer draft remains untouched.
  const saveCurrentSettings = async (): Promise<{ generation: number } | undefined> => {
    if (disposed || !settingsReady.value || telegramBackupScheduleErrors.value.length > 0 || telegramBackupPassphraseErrors.value.length > 0) return
    const generation = draftGeneration
    const controller = new AbortController()
    saveController = controller
    loading.value = true
    try {
      const payload = stripSecretPlaceholders(pickTelegramSettings(settings.value), { preserve: ['telegramBackupPassphrase'] })
      if (payload.telegramEnabled !== 'true') {
        for (const key of ['telegramBackupEnabled', 'telegramBackupPassphrase', 'telegramBackupPassphraseHasSecret', 'telegramBackupCron', 'telegramBackupExcludeTables', 'telegramBackupMaxSizeMB']) delete payload[key]
      }
      const msg = await HttpUtils.post('api/save', { object: 'settings', action: 'set', data: JSON.stringify(payload) }, { signal: controller.signal })
      if (disposed || controller.signal.aborted || !msg.success) return
      if (!msg.obj?.settings || typeof msg.obj.settings !== 'object' || Array.isArray(msg.obj.settings)) {
        push.error({ message: i18n.global.t('telegram.saveResponseInvalid') })
        return
      }
      const persisted = normalizedSettings(msg.obj.settings)
      oldSettings.value = { ...persisted }
      push.success({ title: i18n.global.t('success'), duration: 5000, message: i18n.global.t('actions.set') + ' ' + i18n.global.t('telegram.title') })
      if (generation !== draftGeneration) return
      setData(persisted)
      return { generation: draftGeneration }
    } finally {
      if (saveController === controller) saveController = undefined
      if (!disposed) loading.value = false
    }
  }

  const save = async (): Promise<boolean> => {
    if (busy.value || !settingsReady.value || disposed) return false
    return !!await saveCurrentSettings()
  }

  const testTelegram = async () => {
    if (busy.value || !settingsReady.value || disposed) return
    const action = ++actionGeneration
    let generation = draftGeneration
    let controller: AbortController | undefined
    testLoading.value = true
    testResult.value = null
    try {
      if (stateChange.value) {
        const saved = await saveCurrentSettings()
        if (!saved || saved.generation !== draftGeneration || disposed) return
        generation = saved.generation
      }
      if (generation !== draftGeneration || disposed) return
      controller = new AbortController()
      testController = controller
      const msg = await HttpUtils.post('api/telegram/test', {}, { signal: controller.signal })
      if (!disposed && !controller.signal.aborted && action === actionGeneration && generation === draftGeneration && msg.success) testResult.value = msg.obj as TelegramResult
    } finally {
      if (testController === controller) testController = undefined
      if (!disposed && action === actionGeneration) testLoading.value = false
    }
  }

  const detectTelegramChat = async () => {
    if (busy.value || !settingsReady.value || disposed) return
    const action = ++actionGeneration
    const generation = draftGeneration
    const controller = new AbortController()
    discoveryController = controller
    discoveryLoading.value = true
    discoveryResult.value = null
    const token = settings.value.telegramBotToken
    try {
      const msg = await HttpUtils.post('api/telegram/detect-chat', token && token !== STORED_SECRET_PLACEHOLDER ? { token } : {}, { signal: controller.signal })
      if (disposed || controller.signal.aborted || action !== actionGeneration || generation !== draftGeneration) return
      const result = msg.obj as DiscoveryResult | undefined
      if (msg.success && result?.success && typeof result.chatId === 'string' && /^-?[1-9]\d*$/.test(result.chatId)) {
        settings.value.telegramChatID = result.chatId
        discoveryResult.value = result
      } else discoveryResult.value = { success: false, errorClass: result?.errorClass ?? 'request' }
    } finally {
      if (discoveryController === controller) discoveryController = undefined
      if (!disposed && action === actionGeneration) discoveryLoading.value = false
    }
  }

  const sendTelegramBackupNow = async () => {
    backupRunController.value?.abort()
    const controller = new AbortController()
    backupRunController.value = controller
    backupRunLoading.value = true
    const msg = await HttpUtils.post('api/telegram/backup/run', {}, { signal: controller.signal })
    if (backupRunController.value !== controller) {
      return
    }
    backupRunStatus.value = {
      success: msg.success,
      timestamp: new Date().toLocaleString(),
      errorClass: msg.success ? undefined : String(msg.obj?.errorClass ?? msg.msg),
    }
    backupRunLoading.value = false
    backupRunController.value = null
  }

  const stateChange = computed(() => {
    return !FindDiff.deepCompare(settings.value, oldSettings.value)
  })

  return {
    settingsReady,
    reloadSettings,
    busy,
    discoveryLoading,
    discoveryResult,
    detectTelegramChat,
    backupRunLoading,
    backupRunStatus,
    handleTelegramBackupScheduleModeChange,
    loading,
    outboundOptions,
    save,
    sendTelegramBackupNow,
    settings,
    stateChange,
    telegramBackupAdvancedCron,
    telegramBackupCustomMax,
    telegramBackupCustomUnit,
    telegramBackupCustomValue,
    telegramBackupEnabled,
    telegramBackupExcludeTableOptions,
    telegramBackupExcludeTables,
    telegramBackupMaxSizeMB,
    telegramBackupPassphraseErrors,
    telegramBackupScheduleErrors,
    telegramBackupScheduleMode,
    telegramBackupScheduleOptions,
    telegramBackupScheduleUnitOptions,
    telegramCpuThreshold,
    telegramEnabled,
    telegramNotifyCpu,
    telegramReport,
    testLoading,
    testResult,
    testTelegram,
    transportModes,
    updateTelegramBackupCronFromSchedule,
  }
}
