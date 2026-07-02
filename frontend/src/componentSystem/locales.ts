import { DEFAULT_LOCALE, i18n, mergeLocaleMessages, normalizeLocale, type LocaleCode } from '@/locales'
import { contributionFor, registeredComponents } from './registry'

const loadedLocales = new Set<string>()

export const loadComponentLocaleMessages = async (componentId: string, localeCode?: string) => {
  const contribution = contributionFor(componentId)
  if (!contribution?.locales) return

  const currentLocale = normalizeLocale(localeCode ?? String(i18n.global.locale.value))
  await Promise.all([
    loadComponentLocale(componentId, DEFAULT_LOCALE, contribution.locales),
    currentLocale === DEFAULT_LOCALE
      ? Promise.resolve()
      : loadComponentLocale(componentId, currentLocale, contribution.locales),
  ])
}

export const loadActiveComponentLocaleMessages = async (localeCode?: string) => {
  await Promise.all(registeredComponents.value.map(component => loadComponentLocaleMessages(component.id, localeCode)))
}

const loadComponentLocale = async (
  componentId: string,
  localeCode: LocaleCode,
  loaders: NonNullable<ReturnType<typeof contributionFor>>['locales'],
) => {
  const cacheKey = `${componentId}:${localeCode}`
  if (loadedLocales.has(cacheKey)) return

  const loader = loaders?.[localeCode]
  if (!loader) return

  const messages = await loader()
  await mergeLocaleMessages(localeCode, messages.default)
  loadedLocales.add(cacheKey)
}
