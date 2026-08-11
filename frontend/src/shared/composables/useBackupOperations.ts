import HttpUtils, { type Msg } from '@/plugins/httputil'

export const restoreDatabase = (form: FormData, stepUpToken: string): Promise<Msg> =>
  HttpUtils.post('api/importdb', form, { headers: { 'X-Step-Up-Token': stepUpToken } })
export const loadBackupSettings = (): Promise<Msg> => HttpUtils.get('api/settings')
