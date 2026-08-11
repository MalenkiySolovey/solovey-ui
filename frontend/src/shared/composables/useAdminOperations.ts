import HttpUtils, { type Msg } from '@/plugins/httputil'
import { acquireStepUpToken } from '@/shared/composables/useSecurityOperations'

export const loadAdmins = (): Promise<Msg> => HttpUtils.get('api/users')
const protectedAdminPost = async (
  url: string,
  data: Record<string, any>,
  operation: string,
  target: string,
  fallbackCredential: string,
): Promise<Msg> => {
  const { token, response } = await acquireStepUpToken(
    operation,
    target,
    String(data.stepUpCredential || fallbackCredential || ''),
  )
  if (!token) return response
  const payload = { ...data }
  delete payload.stepUpCredential
  return HttpUtils.post(url, payload, { headers: { 'X-Step-Up-Token': token } })
}

export const changeAdminPassword = (data: Record<string, any>): Promise<Msg> =>
  protectedAdminPost('api/changePass', data, 'admin.credential', '$self', String(data.oldPass ?? ''))
export const createAdmin = (data: Record<string, any>): Promise<Msg> =>
  protectedAdminPost('api/addAdmin', data, 'admin.create', `new-admin:${String(data.username ?? '').trim()}`, String(data.currentPass ?? ''))
export const removeAdmin = (data: Record<string, any>): Promise<Msg> =>
  protectedAdminPost('api/deleteAdmin', data, 'admin.delete', `user:${String(data.id ?? '').trim()}`, String(data.currentPass ?? ''))
export const logoutAllAdmins = (): Promise<Msg> => HttpUtils.post('api/logoutAllAdmins', {})
export const reorderAdmins = (ids: number[]): Promise<Msg> => HttpUtils.post('api/reorder', {
  object: 'admins',
  data: JSON.stringify(ids),
})
