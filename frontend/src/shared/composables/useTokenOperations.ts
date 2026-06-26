import HttpUtils, { type Msg } from '@/plugins/httputil'

export const loadTokens = (): Promise<Msg> => HttpUtils.get('api/tokens')
export const addToken = (desc: string, expiry: number | string, scope: string): Promise<Msg> => HttpUtils.post('api/addToken', { desc, expiry, scope })
export const setTokenEnabled = (id: number, enabled: boolean): Promise<Msg> => HttpUtils.post('api/setTokenEnabled', { id, enabled })
export const deleteToken = (id: number): Promise<Msg> => HttpUtils.post('api/deleteToken', { id })
