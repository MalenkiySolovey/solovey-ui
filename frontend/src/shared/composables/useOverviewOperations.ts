import HttpUtils, { type Msg } from '@/plugins/httputil'

export const loadOverviewStatus = (): Promise<Msg> => HttpUtils.get('api/status', { r: 'sys,sbd,net,cpu,mem,dsk' })
export const loadRecentAuditEvents = (): Promise<Msg> => HttpUtils.get('api/security/audit', { limit: 10 })
export const loadOverviewTrafficSummary = (limit: number, buckets: number): Promise<Msg> => HttpUtils.get('api/stats/traffic', {
  limit,
  buckets,
})
export const loadOverviewStats = (tag: string, limit: number): Promise<Msg> => HttpUtils.get('api/stats', {
  resource: 'inbound',
  tag,
  limit,
})
