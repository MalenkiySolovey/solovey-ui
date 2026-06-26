import { computed, onMounted, onUnmounted, ref } from 'vue'
import { i18n } from '@/locales'
import HttpUtils from '@/plugins/httputil'
import { FindDiff } from '@/plugins/utils'
import { push } from 'notivue'
import { normalizeSecretFields, stripSecretPlaceholders } from '@/components/settings/settingsSecretField'
import {
  parseTelegramBackupSchedule,
  serializeTelegramBackupSchedule,
  validateTelegramBackupSchedule,
  type TelegramBackupScheduleMode,
  type TelegramBackupScheduleUnit,
} from '@/views/telegramBackupSchedule'
import {
  hasWeakTelegramBackupPassphrase,
  pickTelegramSettings,
  telegramSettingsDefaults,
  type TelegramSettingsMap,
} from '@/views/telegramSettingsPayload'

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

  const testLoading = ref(false)

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

  const loadData = async () => {
    loading.value = true
    const msg = await HttpUtils.get('api/settings')
    if (msg.success) {
      setData(msg.obj ?? {})
    }
    loading.value = false
  }

  const transportModes = computed(() => [
    { title: i18n.global.t('telegram.transportProxy'), value: 'proxy' },
    { title: i18n.global.t('telegram.transportOutbound'), value: 'outbound' },
  ])

  const outboundOptions = ref<{ title: string; value: string }[]>([])

  const loadOutbounds = async () => {
    const msg = await HttpUtils.get('api/outbounds')
    const list = msg?.obj?.outbounds
    if (msg.success && Array.isArray(list)) {
      outboundOptions.value = list.map((o: any) => ({ title: `${o.tag} (${o.type})`, value: o.tag }))
    }
  }

  onMounted(() => {
    loadData()
    loadOutbounds()
  })

  onUnmounted(() => {
    backupRunController.value?.abort()
  })

  const setData = (data: TelegramSettingsMap) => {
    const normalized = normalizeSecretFields({ ...defaultTelegramSettings, ...data })
    settings.value = pickTelegramSettings(normalized)
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

  const save = async () => {
    if (telegramBackupScheduleErrors.value.length > 0 || telegramBackupPassphraseErrors.value.length > 0) {
      return
    }
    loading.value = true
    const payload = stripSecretPlaceholders(pickTelegramSettings(settings.value))
    if (payload.telegramEnabled !== 'true') {
      delete payload.telegramBackupEnabled
      delete payload.telegramBackupPassphrase
      delete payload.telegramBackupPassphraseHasSecret
      delete payload.telegramBackupCron
      delete payload.telegramBackupExcludeTables
      delete payload.telegramBackupMaxSizeMB
    }
    const msg = await HttpUtils.post('api/save', { object: 'settings', action: 'set', data: JSON.stringify(payload) })
    if (msg.success) {
      push.success({
        title: i18n.global.t('success'),
        duration: 5000,
        message: i18n.global.t('actions.set') + ' ' + i18n.global.t('telegram.title'),
      })
      setData(msg.obj.settings)
    }
    loading.value = false
  }

  const testTelegram = async () => {
    testLoading.value = true
    testResult.value = null
    const msg = await HttpUtils.post('api/telegram/test', {})
    if (msg.success) {
      testResult.value = msg.obj as TelegramResult
    }
    testLoading.value = false
  }

  const sendTelegramBackupNow = async () => {
    backupRunController.value?.abort()
    const controller = new AbortController()
    backupRunController.value = controller
    backupRunLoading.value = true
    const msg = await HttpUtils.post('api/telegram/backup/run', {}, { signal: controller.signal })
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
