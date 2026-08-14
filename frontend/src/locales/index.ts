import { createI18n } from 'vue-i18n'
import type { LocaleCode, LocaleMessages } from './types'

export type { LocaleCode, LocaleMessages } from './types'

export const DEFAULT_LOCALE: LocaleCode = 'en'

const localeLoaders: Record<LocaleCode, () => Promise<{ default: LocaleMessages }>> = {
  en: () => import('./en'),
  fa: () => import('./fa'),
  vi: () => import('./vi'),
  zhHans: () => import('./zhcn'),
  zhHant: () => import('./zhtw'),
  ru: () => import('./ru'),
}

const supportedLocales = new Set<LocaleCode>(Object.keys(localeLoaders) as LocaleCode[])
const loadedLocales = new Set<LocaleCode>()
let loadLocaleExtensions: (localeCode: LocaleCode) => Promise<void> = async () => {}

export const configureLocaleExtensions = (
  loader: (localeCode: LocaleCode) => Promise<void>,
) => {
  loadLocaleExtensions = loader
}

export const normalizeLocale = (value?: string | null): LocaleCode => {
  if (value && supportedLocales.has(value as LocaleCode)) {
    return value as LocaleCode
  }
  return DEFAULT_LOCALE
}

const storageGet = (key: string): string | null => {
  if (typeof localStorage === 'undefined' || typeof localStorage.getItem !== 'function') {
    return null
  }
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

const storageSet = (key: string, value: string) => {
  if (typeof localStorage === 'undefined' || typeof localStorage.setItem !== 'function') {
    return
  }
  try {
    localStorage.setItem(key, value)
  } catch {
    // Storage can be unavailable in private mode or test shims.
  }
}

const storedLocale = () => {
  return normalizeLocale(storageGet('locale'))
}

const initialLocale = storedLocale()

export const i18n = createI18n({
  legacy: false,
  locale: initialLocale,
  fallbackLocale: DEFAULT_LOCALE,
  messages: {},
})

const loadMessages = async (localeCode: LocaleCode) => {
  if (loadedLocales.has(localeCode)) {
    return
  }
  const messages = await localeLoaders[localeCode]()
  i18n.global.setLocaleMessage(localeCode, messages.default)
  loadedLocales.add(localeCode)
}

export const loadLocaleMessages = async (localeCode: string) => {
  const normalized = normalizeLocale(localeCode)
  await loadMessages(DEFAULT_LOCALE)
  if (normalized !== DEFAULT_LOCALE) {
    await loadMessages(normalized)
  }
  return normalized
}

export const loadInitialLocaleMessages = () => loadLocaleMessages(initialLocale)

export const setI18nLocale = async (localeCode: string) => {
  const normalized = await loadLocaleMessages(localeCode)
  await loadLocaleExtensions(normalized)
  i18n.global.locale.value = normalized
  storageSet('locale', normalized)
  return normalized
}

export const mergeLocaleMessages = async (localeCode: string, messages: LocaleMessages) => {
  const normalized = await loadLocaleMessages(localeCode)
  const current = i18n.global.getLocaleMessage(normalized) as LocaleMessages
  i18n.global.setLocaleMessage(normalized, {
    ...current,
    ...messages,
  })
  return normalized
}

export const currentLocale = (): LocaleCode => normalizeLocale(String(i18n.global.locale.value))

export const dateLocale = (): string => {
  const localeCode = currentLocale()
  switch (localeCode) {
    case 'zhHans':
      return 'zh-cn'
    case 'zhHant':
      return 'zh-tw'
    default:
      return localeCode
  }
}

export const languages = [
  { title: 'English', value: 'en' },
  { title: 'فارسی', value: 'fa' },
  { title: 'Tiếng Việt', value: 'vi' },
  { title: '简体中文', value: 'zhHans' },
  { title: '繁體中文', value: 'zhHant' },
  { title: 'Русский', value: 'ru' },
]
