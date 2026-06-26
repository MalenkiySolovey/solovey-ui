import { describe, expect, it } from 'vitest'
import {
  paidSubSecretSettingKeys,
  paidSubSettingKeys,
  paidSubSettingsDefaults,
  pickPaidSubSettings,
  pickSecretAwareSettings,
  pickSettingsByDefaults,
  settingsPageDefaults,
  telegramSecretSettingKeys,
  telegramSettingKeys,
  telegramSettingsDefaults,
} from './settingsPayload'

describe('settings payload defaults', () => {
  it('keeps Settings page defaults in one shared map', () => {
    expect(settingsPageDefaults.webPort).toBe('2095')
    expect(settingsPageDefaults.timeLocation).toBe('Europe/Moscow')
    expect(settingsPageDefaults.subJsonPath).toBe('/json/')
    expect(settingsPageDefaults.subClashPath).toBe('/clash/')
    expect(settingsPageDefaults.subXrayPath).toBe('/xray/')
    expect(settingsPageDefaults.subRemoteGroupAdaptation).toBe('urltest')
  })

  it('picks only keys from the selected defaults map', () => {
    const picked = pickSettingsByDefaults(settingsPageDefaults, {
      webPort: '3000',
      subPath: '/users/',
      unknown: 'value',
    })

    expect(picked.webPort).toBe('3000')
    expect(picked.subPath).toBe('/users/')
    expect(picked.webPath).toBe('/app/')
    expect(picked).not.toHaveProperty('unknown')
  })

  it('keeps secret markers only for declared secret fields', () => {
    const picked = pickSecretAwareSettings(telegramSettingKeys, telegramSecretSettingKeys, {
      telegramBotToken: '',
      telegramBotTokenHasSecret: 'true',
      telegramBackupCron: '* * * * *',
      telegramBackupCronHasSecret: 'true',
    })

    expect(picked.telegramBotTokenHasSecret).toBe('true')
    expect(picked.telegramBackupCron).toBe('* * * * *')
    expect(picked).not.toHaveProperty('telegramBackupCronHasSecret')
  })

  it('keeps paid subscription payload coverage explicit', () => {
    expect(paidSubSettingKeys).toContain('paidSubRefundRevoke')
    expect(paidSubSecretSettingKeys).toContain('paidSubBotToken')

    const picked = pickPaidSubSettings({
      paidSubEnabled: 'true',
      paidSubRefundRevoke: 'false',
      paidSubBotTokenHasSecret: 'true',
      unrelated: 'ignored',
    })

    expect(picked.paidSubEnabled).toBe('true')
    expect(picked.paidSubRefundRevoke).toBe('false')
    expect(picked.paidSubBotTokenHasSecret).toBe('true')
    expect(picked.paidSubCurrency).toBe('RUB')
    expect(picked).not.toHaveProperty('unrelated')
  })

  it('matches the Telegram defaults used by the Telegram page', () => {
    expect(telegramSettingsDefaults.telegramTransportMode).toBe('proxy')
    expect(telegramSettingsDefaults.telegramBackupExcludeTables).toBe('stats,client_ips,audit_events,changes')
  })

  it('includes every paid subscription secret marker default', () => {
    for (const key of paidSubSecretSettingKeys) {
      expect(paidSubSettingsDefaults[key + 'HasSecret']).toBe('false')
    }
  })
})
