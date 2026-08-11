export const factAge = (observedAt: number | string, now = Date.now()): string => {
  const timestamp = typeof observedAt === 'string' ? Date.parse(observedAt) : observedAt * 1000
  if (!Number.isFinite(timestamp) || timestamp <= 0) return 'unknown'
  const seconds = Math.max(0, Math.floor((now - timestamp) / 1000))
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`
  return `${Math.floor(seconds / 3600)}h`
}

export const factState = (value: { stale?: boolean; truncated?: boolean; reasonCodes?: string[] }): string => {
  if (value.truncated || value.reasonCodes?.includes('truncated') || value.reasonCodes?.includes('inventory_truncated')) return 'truncated'
  if (value.stale || value.reasonCodes?.includes('stale')) return 'stale'
  if (value.reasonCodes?.some(reason => ['unknown', 'ambiguous', 'invalid', 'unavailable', 'not_verified', 'fail_closed'].some(marker => reason.includes(marker)))) return 'unknown'
  return 'current'
}
