import { computed, ref, type Ref } from 'vue'

export const trafficTimeZoneKey = 'sui:overview:traffic-time-zone'
type PreferenceStorage = Pick<Storage, 'getItem' | 'setItem'>
let sharedPreference: Ref<string> | undefined

export function resolveTrafficTimeZone(value: unknown, systemZone?: string): string {
  try {
    const zone = value === 'system' ? systemZone ?? Intl.DateTimeFormat().resolvedOptions().timeZone : value
    if (typeof zone !== 'string' || !zone || /^[+-]/.test(zone)) return 'UTC'
    return new Intl.DateTimeFormat('en', { timeZone: zone }).resolvedOptions().timeZone
  } catch { return 'UTC' }
}

export function trafficTimeZoneOptions(): string[] {
  try { return ['system', 'UTC', ...Intl.supportedValuesOf('timeZone').filter(zone => zone !== 'UTC')] }
  catch { return ['system', 'UTC'] }
}

// Traffic views share this UI preference. It never reads or writes the
// core scheduling timezone, bucket timestamps or server aggregation settings.
export function useTrafficTimeZone(storage?: PreferenceStorage) {
  const read = (): string => {
    try {
      const raw = (storage ?? localStorage).getItem(trafficTimeZoneKey)
      return raw === null || raw === 'system' ? 'system' : resolveTrafficTimeZone(raw)
    } catch { return 'system' }
  }
  const preference = storage ? ref(read()) : (sharedPreference ??= ref(read()))
  const timeZone = computed({
    get: () => preference.value,
    set: (value: string) => {
      preference.value = value === 'system' ? value : resolveTrafficTimeZone(value)
      try { (storage ?? localStorage).setItem(trafficTimeZoneKey, preference.value) }
      catch { /* The in-memory preference remains usable without storage. */ }
    },
  })
  return { timeZone }
}

export function trafficLabelFormatter(timeZone: unknown, locale = 'en'): (dateTime: number) => string {
  const zone = resolveTrafficTimeZone(timeZone)
  let formatter: Intl.DateTimeFormat | undefined
  if (zone !== 'UTC') {
    const options: Intl.DateTimeFormatOptions = { timeZone: zone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23', timeZoneName: 'shortOffset' }
    try { formatter = new Intl.DateTimeFormat(locale, options) }
    catch { formatter = new Intl.DateTimeFormat('en', options) }
  }
  return (dateTime: number) => {
    const date = new Date(dateTime * 1000)
    if (Number.isNaN(date.getTime())) return String(dateTime)
    return formatter ? formatter.format(date) : date.toISOString()
  }
}
