import { beforeEach, describe, expect, it, vi } from 'vitest'

const storage = new Map<string, string>()

const stubLocalStorage = () => {
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
    removeItem: (key: string) => storage.delete(key),
    clear: () => storage.clear(),
  })
}

describe('locale loading', () => {
  beforeEach(() => {
    storage.clear()
    vi.resetModules()
    vi.unstubAllGlobals()
    stubLocalStorage()
  })

  it('loads only default messages on default startup', async () => {
    const { i18n, loadInitialLocaleMessages } = await import('./index')

    await loadInitialLocaleMessages()

    expect(i18n.global.availableLocales).toContain('en')
    expect(i18n.global.availableLocales).not.toContain('ru')
    expect(i18n.global.getLocaleMessage('en')).not.toHaveProperty('telegram')
    expect(i18n.global.getLocaleMessage('en')).not.toHaveProperty('paidSub')
    expect(i18n.global.getLocaleMessage('en')).not.toHaveProperty('remoteOutbound')
    expect(i18n.global.getLocaleMessage('en')).not.toHaveProperty('migrateXui')
    expect(i18n.global.getLocaleMessage('en')).not.toHaveProperty('audit')
    expect(i18n.global.getLocaleMessage('en')).not.toHaveProperty('diagnostics')
  })

  it('loads stored locale with english fallback on startup', async () => {
    storage.set('locale', 'ru')
    const { i18n, loadInitialLocaleMessages } = await import('./index')

    await loadInitialLocaleMessages()

    expect(i18n.global.availableLocales).toEqual(expect.arrayContaining(['en', 'ru']))
    expect(i18n.global.availableLocales).not.toContain('fa')
  })

  it('loads and stores locales when changed', async () => {
    const { i18n, setI18nLocale } = await import('./index')

    const selectedLocale = await setI18nLocale('zhHans')

    expect(selectedLocale).toBe('zhHans')
    expect(storage.get('locale')).toBe('zhHans')
    expect(i18n.global.locale.value).toBe('zhHans')
    expect(i18n.global.availableLocales).toEqual(expect.arrayContaining(['en', 'zhHans']))
  })

  it('falls back to english for unsupported locales', async () => {
    const { i18n, setI18nLocale } = await import('./index')

    const selectedLocale = await setI18nLocale('missing')

    expect(selectedLocale).toBe('en')
    expect(storage.get('locale')).toBe('en')
    expect(i18n.global.locale.value).toBe('en')
    expect(i18n.global.availableLocales).toEqual(['en'])
  })

  it('loads optional component locale namespaces on demand', async () => {
    const { i18n, loadInitialLocaleMessages } = await import('./index')
    const { registerComponent, unregisterComponent } = await import('@/componentSystem/registry')
    const { loadComponentLocaleMessages } = await import('@/componentSystem/locales')

    await loadInitialLocaleMessages()
    registerComponent({
      id: 'test-locale',
      apiVersion: '1.0',
      locales: {
        en: async () => ({ default: { optionalLocaleTest: { title: 'Optional Locale Test' } } }),
      },
    })
    await loadComponentLocaleMessages('test-locale')
    unregisterComponent('test-locale')

    expect(i18n.global.getLocaleMessage('en')).toHaveProperty('optionalLocaleTest')
    expect(i18n.global.getLocaleMessage('en')).not.toHaveProperty('paidSub')
  })
})
