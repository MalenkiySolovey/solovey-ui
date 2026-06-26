import HttpUtils, { type Msg } from '@/plugins/httputil'

export const loadAuditEvents = (query: Record<string, string | number>): Promise<Msg> => HttpUtils.get('api/security/audit', query)
