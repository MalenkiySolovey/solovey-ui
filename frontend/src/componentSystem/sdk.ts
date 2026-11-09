// Public first-party UI contribution API. These exports retain their existing
// semantic owners; components do not acquire a second store or HTTP client.
export { registerComponent } from './registry'
export type { ComponentContribution } from './types'
export { i18n } from '@/locales'
export { default as HttpUtils } from '@/plugins/httputil'
export { FindDiff } from '@/plugins/utils'
export { normalizeSecretFields, stripSecretPlaceholders, STORED_SECRET_PLACEHOLDER } from '@/components/settings/settingsSecretField'
export { default as SettingsSecretField } from '@/components/settings/SettingsSecretField.vue'
export { default as SettingInfo } from '@/components/settings/SettingInfo.vue'
export { pickSecretAwareSettings, type SettingsMap } from '@/features/settingsPayload'
export { loadBackupSettings } from '@/shared/composables/useBackupOperations'
