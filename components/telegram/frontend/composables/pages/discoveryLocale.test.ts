import { describe, expect, it } from 'vitest'
const modules = import.meta.glob('../../locales/*.ts', { eager: true, import: 'default' }) as Record<string, any>
const classes = ['request', 'maintenance', 'settings', 'missing_token', 'proxy', 'canceled', 'timeout', 'network', 'unauthorized', 'rate_limited', 'telegram_error', 'response', 'no_chat']
describe('Telegram discovery locale contract', () => {
  for (const locale of ['en', 'ru', 'fa', 'vi', 'zhcn', 'zhtw']) {
    it(locale + ' defines every component-owned action and result key', () => {
      const telegram = modules['../../locales/' + locale + '.ts'].telegram
      for (const key of ['detectChat', 'chatDetected', 'saveResponseInvalid', 'settingsUnavailable', 'reloadSettings']) expect(typeof telegram[key]).toBe('string')
      expect(telegram.chatDetected).toContain('{chatId}')
      expect(telegram.hint.detectChat.length).toBeGreaterThan(0)
      expect(Object.keys(telegram.discoveryErrors).sort()).toEqual([...classes].sort())
      for (const value of Object.values(telegram.discoveryErrors)) expect(typeof value === 'string' && value.length > 0).toBe(true)
    })
  }
})
