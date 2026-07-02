import { describe, expect, it } from 'vitest'
import {
  STORED_SECRET_PLACEHOLDER,
  hasStoredSecret,
  normalizeSecretFields,
  stripSecretPlaceholders,
} from '@/components/settings/settingsSecretField'

describe('settings secret field helpers', () => {
  it('detects stored secret markers', () => {
    expect(hasStoredSecret(true)).toBe(true)
    expect(hasStoredSecret('true')).toBe(true)
    expect(hasStoredSecret(false)).toBe(false)
    expect(hasStoredSecret('false')).toBe(false)
    expect(hasStoredSecret(undefined)).toBe(false)
  })

  it('normalizes HasSecret markers to empty editable values', () => {
    const settings = normalizeSecretFields({
      botTokenHasSecret: 'true',
      proxyPasswordHasSecret: 'false',
      backupPassphraseHasSecret: 'true',
      backupPassphrase: STORED_SECRET_PLACEHOLDER,
    })

    expect(settings.botToken).toBe('')
    expect(settings.proxyPassword).toBe('')
    expect(settings.backupPassphrase).toBe(STORED_SECRET_PLACEHOLDER)
  })

  it('does not submit the stored placeholder as a secret value', () => {
    const settings = stripSecretPlaceholders({
      botTokenHasSecret: 'true',
      botToken: STORED_SECRET_PLACEHOLDER,
      chatID: '42',
    })

    expect(settings.botToken).toBe('')
    expect(settings.chatID).toBe('42')
  })

  it('strips stored placeholders for all secret values by default', () => {
    const settings = stripSecretPlaceholders({
      backupPassphraseHasSecret: 'true',
      backupPassphrase: STORED_SECRET_PLACEHOLDER,
    })

    expect(settings.backupPassphrase).toBe('')
  })

  it('can preserve stored placeholders for caller-owned clearable secrets', () => {
    const settings = stripSecretPlaceholders({
      backupPassphraseHasSecret: 'true',
      backupPassphrase: STORED_SECRET_PLACEHOLDER,
    }, { preserve: ['backupPassphrase'] })

    expect(settings.backupPassphrase).toBe(STORED_SECRET_PLACEHOLDER)
  })
})
