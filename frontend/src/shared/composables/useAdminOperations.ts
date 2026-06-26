import HttpUtils, { type Msg } from '@/plugins/httputil'

export const loadAdmins = (): Promise<Msg> => HttpUtils.get('api/users')
export const changeAdminPassword = (data: object): Promise<Msg> => HttpUtils.post('api/changePass', data)
export const createAdmin = (data: object): Promise<Msg> => HttpUtils.post('api/addAdmin', data)
export const removeAdmin = (data: object): Promise<Msg> => HttpUtils.post('api/deleteAdmin', data)
export const logoutAllAdmins = (): Promise<Msg> => HttpUtils.post('api/logoutAllAdmins', {})
export const reorderAdmins = (ids: number[]): Promise<Msg> => HttpUtils.post('api/reorder', {
  object: 'admins',
  data: JSON.stringify(ids),
})
