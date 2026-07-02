import { describe, expect, it } from 'vitest'
import {
  paidSubSecretSettingKeys,
  paidSubSettingKeys,
  paidSubSettingsDefaults,
  pickPaidSubSettings,
} from './paidSubSettingsPayload'

describe('paid subscription settings payload', () => {
  it('keeps the component payload coverage explicit', () => {
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

  it('includes every secret marker default', () => {
    for (const key of paidSubSecretSettingKeys) {
      expect(paidSubSettingsDefaults[key + 'HasSecret']).toBe('false')
    }
  })
})
