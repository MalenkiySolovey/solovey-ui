import { STORED_SECRET_PLACEHOLDER } from '@/components/settings/settingsSecretField'
import {
  pickSecretAwareSettings,
  type SettingsMap,
} from '@/views/settingsPayload'

export type TelegramSettingsMap = SettingsMap

export const telegramSettingsDefaults: SettingsMap = {
  telegramEnabled: 'false',
  telegramBotToken: '',
  telegramBotTokenHasSecret: 'false',
  telegramChatID: '',
  telegramProxyURL: '',
  telegramProxyURLHasSecret: 'false',
  telegramProxyUsername: '',
  telegramProxyUsernameHasSecret: 'false',
  telegramProxyPassword: '',
  telegramProxyPasswordHasSecret: 'false',
  telegramTransportMode: 'proxy',
  telegramOutboundTag: '',
  telegramCpuThreshold: '90',
  telegramNotifyCpu: 'false',
  telegramReport: 'false',
  telegramReportCron: '',
  telegramBackupEnabled: 'false',
  telegramBackupPassphrase: '',
  telegramBackupPassphraseHasSecret: 'false',
  telegramBackupCron: '',
  telegramBackupExcludeTables: 'stats,client_ips,audit_events,changes',
  telegramBackupMaxSizeMB: '45',
}

export const telegramSettingKeys = [
  'telegramEnabled',
  'telegramBotToken',
  'telegramChatID',
  'telegramProxyURL',
  'telegramProxyUsername',
  'telegramProxyPassword',
  'telegramTransportMode',
  'telegramOutboundTag',
  'telegramCpuThreshold',
  'telegramNotifyCpu',
  'telegramReport',
  'telegramReportCron',
  'telegramBackupEnabled',
  'telegramBackupPassphrase',
  'telegramBackupCron',
  'telegramBackupExcludeTables',
  'telegramBackupMaxSizeMB',
]

export const telegramSecretSettingKeys = [
  'telegramBotToken',
  'telegramProxyURL',
  'telegramProxyUsername',
  'telegramProxyPassword',
  'telegramBackupPassphrase',
]

export const minTelegramBackupPassphraseLength = 12

export const hasWeakTelegramBackupPassphrase = (value: string): boolean => {
  return value !== ''
    && value !== STORED_SECRET_PLACEHOLDER
    && Array.from(value).length < minTelegramBackupPassphraseLength
}

export const pickTelegramSettings = (source: TelegramSettingsMap): TelegramSettingsMap => {
  return pickSecretAwareSettings(telegramSettingKeys, telegramSecretSettingKeys, source)
}
