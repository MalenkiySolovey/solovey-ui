import HttpUtils, { type Msg } from '@/plugins/httputil'

const stepUpHeaders = (token: string) => ({ headers: { 'X-Step-Up-Token': token } })

export const importXuiDatabase = (form: FormData, stepUpToken = ''): Promise<Msg> =>
  stepUpToken
    ? HttpUtils.post('api/import-xui', form, stepUpHeaders(stepUpToken))
    : HttpUtils.post('api/import-xui', form)
export const planXuiMigration = (form: FormData): Promise<Msg> => HttpUtils.post('api/import-xui/plan', form)
export const applyXuiMigration = (form: FormData, stepUpToken: string): Promise<Msg> =>
  HttpUtils.post('api/import-xui/apply', form, stepUpHeaders(stepUpToken))
export const rollbackXuiMigration = (backup: string, stepUpToken: string): Promise<Msg> => {
  const body = new URLSearchParams()
  body.set('backup', backup)
  return HttpUtils.post('api/import-xui/rollback', body, stepUpHeaders(stepUpToken))
}
