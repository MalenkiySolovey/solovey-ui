import HttpUtils, { type Msg } from '@/plugins/httputil'

export const planXuiMigration = (form: FormData): Promise<Msg> => HttpUtils.post('api/import-xui/plan', form)
export const applyXuiMigration = (form: FormData): Promise<Msg> => HttpUtils.post('api/import-xui/apply', form)
export const rollbackXuiMigration = (backup: string): Promise<Msg> => {
  const body = new URLSearchParams()
  body.set('backup', backup)
  return HttpUtils.post('api/import-xui/rollback', body)
}
