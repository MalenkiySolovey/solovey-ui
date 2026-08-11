import HttpUtils, { logout as httpLogout, type Msg } from '@/plugins/httputil'

export const login = (user: string, pass: string, remember = false): Promise<Msg> =>
  HttpUtils.post('api/login', { user, pass, remember })
export const logout = (): Promise<void> => httpLogout()
