import { describe, expect, it } from 'vitest'

import en from './en'
import fa from './fa'
import ru from './ru'
import vi from './vi'
import zhcn from './zhcn'
import zhtw from './zhtw'

// Project convention: en is the source of truth
// and ru is the second fully-maintained locale; fa/vi/zhcn/zhtw intentionally fall
// back to en for newer keys (fallbackLocale='en'). So we enforce en<->ru parity
// only — this catches a translation added to one but forgotten in the other (which
// would surface as an unexpected English string in RU).
const flatten = (obj: Record<string, unknown>, prefix = ''): string[] => {
  const out: string[] = []
  for (const [k, v] of Object.entries(obj)) {
    const key = prefix ? `${prefix}.${k}` : k
    if (v && typeof v === 'object' && !Array.isArray(v)) {
      out.push(...flatten(v as Record<string, unknown>, key))
    } else {
      out.push(key)
    }
  }
  return out
}

describe('en/ru locale key parity', () => {
  const enKeys = new Set(flatten(en as Record<string, unknown>))
  const ruKeys = new Set(flatten(ru as Record<string, unknown>))

  it('ru defines every key en defines', () => {
    const missing = [...enKeys].filter((k) => !ruKeys.has(k)).sort()
    expect(missing, `keys in en but missing from ru: ${missing.join(', ')}`).toEqual([])
  })

  it('ru does not define keys absent from en', () => {
    const extra = [...ruKeys].filter((k) => !enKeys.has(k)).sort()
    expect(extra, `keys in ru but missing from en: ${extra.join(', ')}`).toEqual([])
  })
})

describe('fully translated feature namespaces', () => {
  const source = en as Record<string, unknown>
  const namespaces = ['remoteOutbound', 'paidSub']
  const expected = new Set(flatten(Object.fromEntries(namespaces.map((key) => [key, source[key]]))))
  const locales = { fa, vi, zhcn, zhtw }

  for (const [locale, messages] of Object.entries(locales)) {
    it(`${locale} defines every subscription key`, () => {
      const localized = messages as Record<string, unknown>
      const actual = new Set(flatten(Object.fromEntries(namespaces.map((key) => [key, localized[key]]))))
      const missing = [...expected].filter((key) => !actual.has(key)).sort()
      const extra = [...actual].filter((key) => !expected.has(key)).sort()
      expect({ missing, extra }).toEqual({ missing: [], extra: [] })
    })
  }
})

describe('ported UX translations', () => {
  const locales = { en, fa, ru, vi, zhcn, zhtw }
  const paths = [
    'basic.hint',
    'rule.action',
    'setting.hint',
    'telegram.hint',
  ]

  const pick = (messages: Record<string, unknown>, path: string) => {
    return path.split('.').reduce<unknown>((value, key) => {
      if (!value || typeof value !== 'object') return undefined
      return (value as Record<string, unknown>)[key]
    }, messages)
  }

  const expected = new Set(paths.flatMap((path) => {
    const value = pick(en as Record<string, unknown>, path) as Record<string, unknown>
    return flatten(value, path)
  }))

  for (const [locale, messages] of Object.entries(locales)) {
    it(`${locale} defines every ported UX key`, () => {
      const localized = messages as Record<string, unknown>
      const actual = new Set(paths.flatMap((path) => {
        const value = pick(localized, path) as Record<string, unknown>
        return flatten(value ?? {}, path)
      }))
      const missing = [...expected].filter((key) => !actual.has(key)).sort()
      expect(missing).toEqual([])
    })

    it(`${locale} has no empty top-level namespace`, () => {
      const empty = Object.entries(messages as Record<string, unknown>)
        .filter(([, value]) => value && typeof value === 'object' && !Array.isArray(value))
        .filter(([, value]) => Object.keys(value as Record<string, unknown>).length === 0)
        .map(([key]) => key)
      expect(empty).toEqual([])
    })
  }
})
