import HttpUtils, { type Msg } from '@/plugins/httputil'
import { acquireStepUpToken } from '@/shared/composables/useSecurityOperations'

export const loadTokens = (): Promise<Msg> => HttpUtils.get('api/tokens')
const protectedTokenPost = async (
  url: string,
  payload: Record<string, any>,
  operation: string,
  target: string,
  credential: string,
): Promise<Msg> => {
  const { token, response } = await acquireStepUpToken(operation, target, credential)
  if (!token) return response
  return HttpUtils.post(url, payload, { headers: { 'X-Step-Up-Token': token } })
}

export const addToken = (desc: string, expiry: number | string, scope: string, credential: string): Promise<Msg> =>
  protectedTokenPost('api/addToken', { desc, expiry, scope }, 'token.create', 'new-token', credential)
export const setTokenEnabled = (id: number, enabled: boolean, credential: string): Promise<Msg> =>
  protectedTokenPost('api/setTokenEnabled', { id, enabled }, 'token.change', `token:${id}`, credential)
export const deleteToken = (id: number, credential: string): Promise<Msg> =>
  protectedTokenPost('api/deleteToken', { id }, 'token.revoke', `token:${id}`, credential)
