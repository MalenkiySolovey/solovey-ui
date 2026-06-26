import HttpUtils, { logout as httpLogout, type Msg } from '@/plugins/httputil'

export const login = (user: string, pass: string): Promise<Msg> => HttpUtils.post('api/login', { user, pass })
export const logout = (): Promise<void> => httpLogout()
