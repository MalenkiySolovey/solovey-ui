import { describe, expect, it } from 'vitest'
import { STORED_SECRET_PLACEHOLDER } from '@/components/settings/settingsSecretField'
import { hasWeakTelegramBackupPassphrase, pickTelegramSettings, telegramSettingsDefaults } from './telegramSettingsPayload'

describe('telegram settings payload', () => {
  it('keeps HasSecret markers only for secret Telegram settings', () => {
    const payload = pickTelegramSettings({
      telegramBotToken: '',
      telegramBotTokenHasSecret: 'true',
      telegramBackupPassphrase: '',
      telegramBackupPassphraseHasSecret: 'true',
      telegramBackupCron: '*/15 * * * *',
      telegramBackupCronHasSecret: 'true',
    })

    expect(payload.telegramBotTokenHasSecret).toBe('true')
    expect(payload.telegramBackupPassphraseHasSecret).toBe('true')
    expect(payload).not.toHaveProperty('telegramBackupCronHasSecret')
  })

  it('matches the backup passphrase minimum length before save', () => {
    expect(hasWeakTelegramBackupPassphrase('')).toBe(false)
    expect(hasWeakTelegramBackupPassphrase(STORED_SECRET_PLACEHOLDER)).toBe(false)
    expect(hasWeakTelegramBackupPassphrase('too-short')).toBe(true)
    expect(hasWeakTelegramBackupPassphrase('123456789012')).toBe(false)
  })

  it('owns the Telegram defaults outside the core settings payload', () => {
    expect(telegramSettingsDefaults.telegramTransportMode).toBe('proxy')
    expect(telegramSettingsDefaults.telegramBackupExcludeTables).toBe('stats,client_ips,audit_events,changes')
  })
})
