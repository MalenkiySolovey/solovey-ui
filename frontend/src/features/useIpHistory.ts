import HttpUtils from '@/plugins/httputil'
import type { ClientIPHistoryRow } from '@/components/security/ipHistory'

export const fetchClientIPHistory = async (client: string): Promise<ClientIPHistoryRow[] | undefined> => {
  const response = await HttpUtils.get(`api/ip-monitor/${encodeURIComponent(client)}`)
  return response.success ? (response.obj ?? []) as ClientIPHistoryRow[] : undefined
}

export const clearClientIPHistory = async (client: string): Promise<boolean> => {
  const response = await HttpUtils.post(`api/ip-monitor/${encodeURIComponent(client)}/clear`, {})
  return response.success
}
