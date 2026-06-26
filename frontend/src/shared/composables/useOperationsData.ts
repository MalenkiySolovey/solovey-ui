import HttpUtils, { type Msg } from '@/plugins/httputil'

export const loadChanges = (actor: string, key: string, count: number): Promise<Msg> => HttpUtils.get('api/changes', { a: actor, k: key, c: count })
export const loadLogs = (count: number, level: string): Promise<Msg> => HttpUtils.get('api/logs', { c: count, l: level })
export const loadStats = (resource: string, tag: string, limit: number): Promise<Msg> => HttpUtils.get('api/stats', { resource, tag, limit })
export const loadUsageStats = (): Promise<Msg> => HttpUtils.get('api/status', { r: 'db' })
