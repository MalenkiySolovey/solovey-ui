import { STORED_SECRET_PLACEHOLDER } from '@/components/settings/settingsSecretField'
import {
  pickSecretAwareSettings,
  telegramSecretSettingKeys,
  telegramSettingKeys,
  telegramSettingsDefaults,
  type SettingsMap,
} from '@/views/settingsPayload'

export type TelegramSettingsMap = SettingsMap

export { telegramSettingKeys, telegramSettingsDefaults }

export const minTelegramBackupPassphraseLength = 12

export const hasWeakTelegramBackupPassphrase = (value: string): boolean => {
  return value !== ''
    && value !== STORED_SECRET_PLACEHOLDER
    && Array.from(value).length < minTelegramBackupPassphraseLength
}

export const pickTelegramSettings = (source: TelegramSettingsMap): TelegramSettingsMap => {
  return pickSecretAwareSettings(telegramSettingKeys, telegramSecretSettingKeys, source)
}
