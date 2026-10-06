import { describe, expect, it } from 'vitest'
import { resolveTrafficTimeZone, trafficLabelFormatter, trafficTimeZoneKey, trafficTimeZoneOptions, useTrafficTimeZone } from './trafficTimeZone'
import { selectTrafficSeries } from './selectors/trafficSelectors'
const sec = (value: string) => Date.parse(value) / 1000

describe('traffic presentation timezone', () => {
  it('validates IANA zones and deterministically falls back for invalid values', () => {
    expect(resolveTrafficTimeZone('Asia/Tokyo')).toBe('Asia/Tokyo')
    for (const value of ['stale-zone', '', null, 1, '+03:00']) expect(resolveTrafficTimeZone(value)).toBe('UTC')
    expect(resolveTrafficTimeZone('system', 'Pacific/Auckland')).toBe('Pacific/Auckland')
    expect(resolveTrafficTimeZone('system', 'stale-zone')).toBe('UTC')
    expect(trafficTimeZoneOptions()).toEqual(expect.arrayContaining(['system', 'UTC', 'Asia/Tokyo']))
  })
  it('formats another date boundary and never changes bucket times or traffic', () => {
    const summary = { buckets: [{ startTime: sec('2024-03-09T23:00:00Z'), download: 7, upload: 3 }] }
    const original = structuredClone(summary)
    const utc = selectTrafficSeries({ summary, timeZone: 'UTC', locale: 'en-US' })
    const tokyo = selectTrafficSeries({ summary, timeZone: 'Asia/Tokyo', locale: 'en-US' })
    expect(utc.labels).toEqual(['2024-03-09T23:00:00.000Z'])
    expect(tokyo.labels[0]).toContain('03/10/2024')
    expect(tokyo.labels[0]).toContain('08:00:00')
    expect(tokyo.labels[0]).toContain('GMT+9')
    expect(tokyo.download).toEqual(utc.download)
    expect(tokyo.upload).toEqual(utc.upload)
    expect(summary).toEqual(original)
    expect(selectTrafficSeries({ summary, timeZone: 'invalid' })).toEqual(utc)
  })
  it('disambiguates repeated DST hours and skips the missing spring hour with Intl', () => {
    const format = trafficLabelFormatter('America/New_York', 'en-US')
    const first = format(sec('2024-11-03T05:30:00Z'))
    const second = format(sec('2024-11-03T06:30:00Z'))
    expect(first).toContain('01:30:00 GMT-4')
    expect(second).toContain('01:30:00 GMT-5')
    expect(first).not.toBe(second)
    expect(format(sec('2024-03-10T06:30:00Z'))).toContain('01:30:00 GMT-5')
    expect(format(sec('2024-03-10T07:30:00Z'))).toContain('03:30:00 GMT-4')
  })
  it('preserves default UTC and resolves browser system behavior with an invalid-locale fallback', () => {
    const timestamp = sec('2024-03-09T23:00:00Z')
    expect(trafficLabelFormatter(undefined)(timestamp)).toBe('2024-03-09T23:00:00.000Z')
    expect(trafficLabelFormatter('system')(timestamp)).toBe(trafficLabelFormatter(Intl.DateTimeFormat().resolvedOptions().timeZone)(timestamp))
    expect(trafficLabelFormatter('Asia/Tokyo', 'invalid_locale')(timestamp)).toBe(trafficLabelFormatter('Asia/Tokyo', 'en')(timestamp))
  })
  it('owns validated persistence and works when storage is unavailable', () => {
    const values = new Map<string, string>()
    const storage = { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value) } }
    const owner = useTrafficTimeZone(storage)
    expect(owner.timeZone.value).toBe('system')
    owner.timeZone.value = 'Asia/Tokyo'
    expect(useTrafficTimeZone(storage).timeZone.value).toBe('Asia/Tokyo')
    values.set(trafficTimeZoneKey, 'stale-zone')
    expect(useTrafficTimeZone(storage).timeZone.value).toBe('UTC')
    owner.timeZone.value = 'invalid'
    expect(values.get(trafficTimeZoneKey)).toBe('UTC')
    const unavailable = useTrafficTimeZone({ getItem: () => { throw new Error('disabled') }, setItem: () => { throw new Error('disabled') } })
    unavailable.timeZone.value = 'Asia/Tokyo'
    expect(unavailable.timeZone.value).toBe('Asia/Tokyo')
  })
})
