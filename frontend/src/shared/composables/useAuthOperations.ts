import HttpUtils, { logout as httpLogout, type Msg } from '@/plugins/httputil'
import { clearCSRFToken } from '@/store/csrf'

export const login = async (user: string, pass: string, remember = false): Promise<Msg> => {
  const response = await HttpUtils.post('api/login', { user, pass, remember })
  if (response.success) {
    // Login rotates the server-side session and invalidates the pre-auth CSRF
    // token used for this request. Fetch the first authenticated mutation's
    // token from the new session instead of replaying the cached pre-auth one.
    clearCSRFToken()
  }
  return response
}
export const logout = (): Promise<void> => httpLogout()
